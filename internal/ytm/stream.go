package ytm

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// player is what the visionOS client says of a video: whether it plays,
// its AAC stream and its HLS manifest.
type player struct {
	status, reason string
	stream         *Stream
	hls            string
}

// Streams last about six hours; they are kept for less, so a stream given
// out is good for a while yet.
var playerCache = newCache[*player](30*time.Minute, 64)

func playerOf(id string) (*player, error) {
	if id == "" {
		return nil, ErrUnavailable
	}
	return playerCache.get(Language+"/"+Region+"/"+id, func() (*player, error) {
		resp, err := callPlayer(id)
		if err != nil {
			return nil, err
		}
		ps := dig(resp, "playabilityStatus")
		p := &player{status: str(ps, "status"), reason: str(ps, "reason")}
		if p.reason == "" {
			p.reason = text(dig(ps, "errorScreen", "playerErrorMessageRenderer", "reason"))
		}
		if p.status == "LOGIN_REQUIRED" {
			// "Sign in to confirm you're not a bot": not the song's fault,
			// so not kept, and the next try comes as another visitor.
			resetVisitorData()
			return nil, fmt.Errorf("%w: %s", ErrUnavailable, p.reason)
		}
		if p.status != "OK" {
			return p, nil
		}
		sd := dig(resp, "streamingData")
		p.hls = str(sd, "hlsManifestUrl")
		for _, f := range arr(sd, "adaptiveFormats") {
			itag, _ := num(f, "itag")
			u := str(f, "url")
			if itag != 140 || u == "" {
				continue
			}
			size, _ := strconv.ParseInt(str(f, "contentLength"), 10, 64)
			p.stream = &Stream{URL: u, Size: size, Type: "m4a", Level: "standard", Gain: gainOf(f, resp)}
			break
		}
		return p, nil
	})
}

// gainOf returns how many dB to change a stream by to sound as loud as
// YouTube's reference: its loudnessDb says how much louder it is.
func gainOf(format, resp any) float64 {
	ac := dig(resp, "playerConfig", "audioConfig")
	var g float64
	if l, ok := num(format, "loudnessDb"); ok {
		g = -l
	} else if l, ok := num(ac, "loudnessDb"); ok {
		g = -l
	} else if abs, ok := num(ac, "trackAbsoluteLoudnessLkfs"); ok {
		target, ok := num(ac, "loudnessTargetLkfs")
		if !ok {
			target = -14
		}
		g = target - abs
	} else {
		return 0
	}
	return max(-12, min(6, g))
}

// SongURL returns where to stream a track's audio, with its size and
// loudness: AAC in MP4 at 128 kbps (itag 140, "standard") to anyone, or at
// 256 kbps (itag 141, "exhigh") to a Premium account that asks, else the
// standard one. Both are asked at once, so the standard one waits on
// nothing.
func SongURL(id string, level string) (*Stream, error) {
	if level != "exhigh" || !SignedIn() || notPremium() {
		return standardStream(id)
	}
	type result struct {
		s   *Stream
		err error
	}
	std := make(chan result, 1)
	go func() {
		s, err := standardStream(id)
		std <- result{s, err}
	}()
	if s, err := premiumStream(id); err == nil {
		return s, nil
	}
	r := <-std
	return r.s, r.err
}

// standardStream returns a track's AAC at 128 kbps, which the visionOS
// client gives anyone, as a plain URL.
func standardStream(id string) (*Stream, error) {
	p, err := playerOf(id)
	if err != nil {
		return nil, err
	}
	if p.status != "OK" {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, p.reason)
	}
	if p.stream == nil {
		return nil, fmt.Errorf("%w: no AAC stream", ErrUnavailable)
	}
	s := *p.stream
	return &s, nil
}

// premium says whether the account signed in has Premium's streams: once
// a few songs in a row came without them, and none with, it is taken to
// have none, and later songs ask no more, until the account changes. Not
// at the first: some uploads have no 256 kbps stream even for Premium.
var premium struct {
	mu     sync.Mutex
	seen   bool // a song came with Premium's stream
	misses int  // songs since that came without
}

func notPremium() bool {
	premium.mu.Lock()
	defer premium.mu.Unlock()
	return !premium.seen && premium.misses >= 3
}

// sawPremium notes whether a song came with Premium's stream.
func sawPremium(had bool) {
	premium.mu.Lock()
	defer premium.mu.Unlock()
	if had {
		premium.seen, premium.misses = true, 0
	} else {
		premium.misses++
	}
}

func forgetPremium() {
	premium.mu.Lock()
	premium.seen, premium.misses = false, 0
	premium.mu.Unlock()
}

var errNotPremium = errors.New("ytm: the account has no Premium streams")

var premiumCache = newCache[*Stream](30*time.Minute, 64)

// premiumStream returns a track's AAC at 256 kbps, which YouTube Music's
// web client gives an account with Premium, unscrambled.
func premiumStream(id string) (*Stream, error) {
	return premiumCache.get(Language+"/"+Region+"/"+id, func() (*Stream, error) {
		sv, err := currentSolver()
		if err != nil {
			return nil, err
		}
		resp, err := call("player", map[string]any{
			"videoId": id, "contentCheckOk": true, "racyCheckOk": true,
			"playbackContext": map[string]any{"contentPlaybackContext": map[string]any{"signatureTimestamp": sv.sts}},
		})
		if err != nil {
			return nil, err
		}
		if st := str(dig(resp, "playabilityStatus"), "status"); st != "OK" {
			return nil, fmt.Errorf("%w: %s", ErrUnavailable, st)
		}
		for _, f := range arr(resp, "streamingData", "adaptiveFormats") {
			if itag, _ := num(f, "itag"); itag != 141 {
				continue
			}
			sawPremium(true)
			u, err := streamURL(f, sv.solve)
			if err != nil {
				return nil, err
			}
			size, _ := strconv.ParseInt(str(f, "contentLength"), 10, 64)
			return &Stream{URL: u, Size: size, Type: "m4a", Level: "exhigh", Gain: gainOf(f, resp)}, nil
		}
		sawPremium(false)
		return nil, errNotPremium
	})
}

// VideoURL returns the HLS manifest of a video, which the system's player
// plays adaptively.
func VideoURL(id string) (string, error) {
	p, err := playerOf(id)
	if err != nil {
		return "", err
	}
	if p.status != "OK" {
		return "", fmt.Errorf("%w: %s", ErrUnavailable, p.reason)
	}
	if p.hls == "" {
		return "", fmt.Errorf("%w: no HLS manifest", ErrUnavailable)
	}
	return p.hls, nil
}

// VideoStream returns the stream of a video's pictures alone: of its HLS
// manifest, the tallest H.264 variant at most height pixels high and, if
// bps is more than 0, taking at most bps bits a second. A muted video kept
// in time with the song needs neither the variants' sound nor their
// switching, which starts from the lowest and climbs for a while; else,
// the manifest.
func VideoStream(id string, height, bps int) (string, error) {
	master, err := VideoURL(id)
	if err != nil {
		return "", err
	}
	body, _, err := get(master, UserAgent)
	if err != nil {
		return master, nil
	}
	if v := pickVariant(string(body), height, bps); v != "" {
		return v, nil
	}
	return master, nil
}

// pickVariant returns the URI of the tallest H.264 variant of a master
// playlist no taller than height and, if bps is more than 0, of no more
// bandwidth than bps, or of the shortest if none is.
func pickVariant(m3u8 string, height, bps int) string {
	lines := strings.Split(m3u8, "\n")
	best, bestH, low, lowH := "", 0, "", 1<<30
	for i, l := range lines {
		if !strings.HasPrefix(l, "#EXT-X-STREAM-INF:") || i+1 >= len(lines) || !strings.Contains(l, "avc1") {
			continue
		}
		uri := strings.TrimSpace(lines[i+1])
		res := attr(l, "RESOLUTION")
		_, hs, ok := strings.Cut(res, "x")
		h, err := strconv.Atoi(hs)
		if !ok || err != nil || uri == "" || strings.HasPrefix(uri, "#") {
			continue
		}
		bw, _ := strconv.Atoi(attr(l, "BANDWIDTH"))
		if h <= height && h > bestH && (bps <= 0 || bw <= bps) {
			best, bestH = uri, h
		}
		if h < lowH {
			low, lowH = uri, h
		}
	}
	if best == "" {
		return low
	}
	return best
}

// attr returns an attribute of an HLS tag's list, as RESOLUTION of
// "#EXT-X-STREAM-INF:BANDWIDTH=1,RESOLUTION=1280x720".
func attr(tag, name string) string {
	for _, part := range strings.Split(tag[strings.Index(tag, ":")+1:], ",") {
		if k, v, ok := strings.Cut(part, "="); ok && k == name {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// SongDetails returns the tracks of video IDs, in their order, with what
// YouTube Music knows of them (music/get_queue).
func SongDetails(ids []string) ([]Track, error) {
	byID := map[string]Track{}
	for start := 0; start < len(ids); start += 50 {
		batch := ids[start:min(start+50, len(ids))]
		resp, err := call("music/get_queue", map[string]any{"videoIds": batch})
		if err != nil {
			return nil, err
		}
		for _, q := range arr(resp, "queueDatas") {
			t, ok := panelTrack(dig(q, "content"))
			if !ok {
				continue
			}
			byID[t.ID] = t
			if _, ok := byID[t.VideoID]; t.VideoID != "" && !ok {
				byID[t.VideoID] = t // asked for by its music video
			}
		}
	}
	out := make([]Track, 0, len(ids))
	for _, id := range ids {
		if t, ok := byID[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

// fillDurations gives the tracks a shelf lists without their length (the
// home page's Quick picks, an artist's top songs) the length the queue
// knows them by.
func fillDurations(ts []Track) {
	var ids []string
	for _, t := range ts {
		if t.Duration == 0 && t.ID != "" && len(ids) < 50 {
			ids = append(ids, t.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	known, err := SongDetails(ids)
	if err != nil {
		return
	}
	length := make(map[string]int64, len(known))
	for _, k := range known {
		length[k.ID] = k.Duration
	}
	for i := range ts {
		if ts[i].Duration == 0 {
			ts[i].Duration = length[ts[i].ID]
		}
	}
}

// Radio returns the songs of a radio started from a track (next, with
// playlist "RDAMVM"+id), the track first, and a token for more.
func Radio(id string, token string) (tracks []Track, next string, err error) {
	body := map[string]any{
		"isAudioOnly":                   true,
		"enablePersistentPlaylistPanel": true,
		"tunerSettingValue":             "AUTOMIX_SETTING_NORMAL",
	}
	if token == "" {
		body["videoId"] = id
		body["playlistId"] = "RDAMVM" + id
	} else {
		body["continuation"] = token
	}
	resp, err := call("next", body)
	if err != nil {
		return nil, "", err
	}
	tracks, next = parseRadio(resp, id, token == "")
	if len(tracks) == 0 && token == "" {
		return nil, "", errors.New("ytm: no radio for this song")
	}
	return tracks, next, nil
}

// parseRadio reads the songs of a radio, from its first page (with the song
// it started from first) or from more of it, and the token for more.
func parseRadio(resp any, id string, first bool) (tracks []Track, next string) {
	var items []any
	if first {
		panel := dig(resp, "contents", "singleColumnMusicWatchNextResultsRenderer", "tabbedRenderer",
			"watchNextTabbedResultsRenderer", "tabs", 0, "tabRenderer", "content", "musicQueueRenderer", "content", "playlistPanelRenderer")
		if panel == nil {
			panel = find(dig(resp, "contents"), "playlistPanelRenderer")
		}
		items, next = arr(panel, "contents"), continuation(panel)
	} else {
		items, next = continued(resp)
	}
	tracks = tracksOf(items)
	if first && len(tracks) > 0 && tracks[0].ID != id && tracks[0].VideoID != id {
		for i, t := range tracks {
			if t.ID == id || t.VideoID == id {
				seed := tracks[i]
				copy(tracks[1:i+1], tracks[:i])
				tracks[0] = seed
				break
			}
		}
	}
	return tracks, next
}

// Sized asks the image CDN for a picture of n×n pixels, as far as it
// can: googleusercontent pictures to that size, a video's thumbnail to
// one that big.
func Sized(pic string, n int) string {
	if pic == "" || n <= 0 {
		return pic
	}
	u, err := url.Parse(pic)
	if err != nil {
		return pic
	}
	host := u.Hostname()
	switch {
	case strings.HasSuffix(host, "googleusercontent.com") || strings.HasSuffix(host, "ggpht.com"):
		return resize(pic, n, n, false)
	case strings.HasSuffix(host, "ytimg.com"):
		parts := strings.Split(u.Path, "/")
		if len(parts) >= 4 && (parts[1] == "vi" || parts[1] == "vi_webp") && parts[2] != "" {
			name := "sddefault.jpg"
			switch {
			case n <= 120:
				name = "default.jpg"
			case n <= 320:
				name = "mqdefault.jpg"
			case n <= 480:
				name = "hqdefault.jpg"
			}
			return "https://i.ytimg.com/vi/" + parts[2] + "/" + name
		}
	}
	return pic
}

// resize asks googleusercontent for a picture of w×h, cropped to fill it
// if crop or if the picture was cropped already; else fit inside it.
func resize(pic string, w, h int, crop bool) string {
	if pic == "" {
		return ""
	}
	base, opts := pic, ""
	if i := strings.LastIndexByte(pic, '='); i > strings.LastIndexByte(pic, '/') {
		base, opts = pic[:i], pic[i+1:]
	}
	for _, o := range strings.Split(opts, "-") {
		if o == "p" {
			crop = true
		}
	}
	size := "=w" + strconv.Itoa(w) + "-h" + strconv.Itoa(h)
	if crop {
		size += "-p"
	}
	return base + size + "-l90-rj"
}

// Fetch returns what is at url, as a cover the image CDN serves.
func Fetch(url string) ([]byte, error) {
	body, _, err := get(url, UserAgent)
	return body, err
}
