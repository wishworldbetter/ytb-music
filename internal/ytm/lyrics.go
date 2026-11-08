package ytm

import (
	"encoding/json"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Lyrics come from LRCLib, which times them, and else from YouTube Music,
// which has them plain.

const lrclibUA = "ytb-music (https://github.com/)"

var lrclibAPI = "https://lrclib.net/api/"

type lrclibRecord struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// Lyrics returns a track's lyrics: timed (LRC) where they are found,
// else plain lines, one per line, without times. translation and yrc
// (word-timed) are empty: YouTube has neither.
func Lyrics(t *Track) (lrc, translation, yrc string, err error) {
	if t == nil {
		return "", "", "", nil
	}
	title, artist := lyricsQuery(t)
	// YouTube's own lyrics, untimed, are asked for beside LRCLib's, not
	// after them: when LRCLib has none, they are on their way already.
	type ytResult struct {
		lrc string
		err error
	}
	yt := make(chan ytResult, 1)
	if t.ID != "" {
		go func() {
			lrc, err := ytmLyrics(t.ID)
			yt <- ytResult{lrc, err}
		}()
	}
	var lerr error
	if title != "" && artist != "" {
		var instrumental bool
		lrc, instrumental, lerr = lrclib(title, artist, cleanTitle(t.Album.Name), t.Duration/1000, t.VideoType != VideoATV)
		if lrc != "" || instrumental {
			return lrc, "", "", nil
		}
	}
	if t.ID == "" {
		return "", "", "", lerr
	}
	r := <-yt
	if r.lrc == "" && r.err != nil && lerr != nil {
		return "", "", "", lerr
	}
	return r.lrc, "", "", nil
}

// lyricsQuery returns the song a track is, as LRCLib names it: a video's
// "Artist - Title (Official Video)" read as its artist and title.
func lyricsQuery(t *Track) (title, artist string) {
	if len(t.Artists) > 0 {
		artist = cleanArtist(t.Artists[0].Name)
	}
	title = cleanTitle(t.Name)
	if t.VideoType != VideoATV {
		if a, ti, ok := splitVideoTitle(title); ok {
			artist, title = cleanArtist(a), stripFeat(ti)
		}
	}
	return title, artist
}

var (
	junkRe    = regexp.MustCompile(`(?i)\s*[(\[【（「]([^)\]】）」]*)[)\]】）」]`)
	junkWords = regexp.MustCompile(`(?i)\b(official|video|audio|lyrics?|mv|m/v|visuali[sz]er|hd|hq|4k|remaster(ed)?|explicit|color coded|eng sub|letra|music video|performance)\b|官方|歌词|歌詞|字幕|高清|完整版`)
	featRe    = regexp.MustCompile(`(?i)\s*([(\[]\s*(feat\.?|ft\.?|featuring|with)\s[^)\]]*[)\]]|\s(feat\.?|ft\.?|featuring)\s.*$)`)
	dashRe    = regexp.MustCompile(`\s+[-–—|]\s+`)
	spacesRe  = regexp.MustCompile(`\s{2,}`)
)

// cleanTitle drops what a video's title adds to the song's name:
// "(Official Video)", "[MV]", "(Lyrics)", and the like.
func cleanTitle(s string) string {
	s = junkRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := junkRe.FindStringSubmatch(m)[1]
		if junkWords.MatchString(inner) {
			return ""
		}
		return m
	})
	return strings.TrimSpace(spacesRe.ReplaceAllString(s, " "))
}

// stripFeat drops the artists a title credits as featured.
func stripFeat(s string) string {
	return strings.TrimSpace(featRe.ReplaceAllString(s, ""))
}

// splitVideoTitle reads "Artist - Title" as its artist and title.
func splitVideoTitle(s string) (artist, title string, ok bool) {
	loc := dashRe.FindStringIndex(s)
	if loc == nil {
		return "", "", false
	}
	artist, title = strings.TrimSpace(s[:loc[0]]), strings.TrimSpace(s[loc[1]:])
	if artist == "" || title == "" {
		return "", "", false
	}
	return artist, title, true
}

// cleanArtist drops what a channel's name adds to the artist's: " - Topic",
// "VEVO", " Official".
func cleanArtist(s string) string {
	s = strings.TrimSpace(s)
	for _, suffix := range []string{" - Topic", "VEVO", " Official", " official"} {
		s = strings.TrimSpace(strings.TrimSuffix(s, suffix))
	}
	return s
}

// lrclib returns a song's lyrics from LRCLib, timed if it has them: the
// one that matches its name, artist, album and length, else the closest in
// length of the ones a search finds. loose lets a video's length differ
// from the song's.
func lrclib(title, artist, album string, secs int64, loose bool) (lyrics string, instrumental bool, err error) {
	q := url.Values{"track_name": {title}, "artist_name": {artist}}
	if album != "" && album != title {
		q.Set("album_name", album)
	}
	if secs > 0 {
		q.Set("duration", strconv.FormatInt(secs, 10))
	}
	body, status, err := get(lrclibAPI+"get?"+q.Encode(), lrclibUA)
	if err == nil {
		var r lrclibRecord
		if json.Unmarshal(body, &r) == nil {
			if r.Instrumental {
				return "", true, nil
			}
			if l := pickLyrics(r); l != "" {
				return l, false, nil
			}
		}
	} else if status != 404 {
		return "", false, err
	}
	q = url.Values{"track_name": {stripFeat(title)}, "artist_name": {artist}}
	body, _, err = get(lrclibAPI+"search?"+q.Encode(), lrclibUA)
	if err != nil {
		return "", false, err
	}
	var rs []lrclibRecord
	if err := json.Unmarshal(body, &rs); err != nil {
		return "", false, err
	}
	r, ok := bestRecord(rs, secs, loose)
	if !ok {
		return "", false, nil
	}
	return pickLyrics(r), r.Instrumental, nil
}

func pickLyrics(r lrclibRecord) string {
	if s := strings.TrimSpace(r.SyncedLyrics); s != "" {
		return s
	}
	return strings.TrimSpace(r.PlainLyrics)
}

// bestRecord picks, of what a search found, the lyrics of the song secs
// long: within 3 seconds, timed ones first; when loose, the closest within
// 20 seconds.
func bestRecord(rs []lrclibRecord, secs int64, loose bool) (lrclibRecord, bool) {
	best, bestScore := lrclibRecord{}, math.Inf(1)
	for _, r := range rs {
		if pickLyrics(r) == "" && !r.Instrumental {
			continue
		}
		diff := 0.0
		if secs > 0 {
			diff = math.Abs(r.Duration - float64(secs))
			if diff > 3 && !(loose && diff <= 20) {
				continue
			}
		}
		score := diff
		if r.SyncedLyrics == "" {
			score += 10
		}
		if score < bestScore {
			best, bestScore = r, score
		}
	}
	return best, !math.IsInf(bestScore, 1)
}

// ytmLyrics returns YouTube Music's own lyrics of a video, plain: what the
// lyrics tab of its watch page leads to.
func ytmLyrics(id string) (string, error) {
	resp, err := call("next", map[string]any{"videoId": id})
	if err != nil {
		return "", err
	}
	tabs := arr(resp, "contents", "singleColumnMusicWatchNextResultsRenderer", "tabbedRenderer", "watchNextTabbedResultsRenderer", "tabs")
	bid := ""
	for _, t := range tabs {
		be := dig(t, "tabRenderer", "endpoint", "browseEndpoint")
		id := str(be, "browseId")
		if str(be, "browseEndpointContextSupportedConfigs", "browseEndpointContextMusicConfig", "pageType") == pageLyrics || strings.HasPrefix(id, "MPLY") {
			bid = id
			break
		}
	}
	if bid == "" {
		return "", nil
	}
	page, err := browse(bid, "")
	if err != nil {
		return "", err
	}
	shelf := find(dig(page, "contents"), "musicDescriptionShelfRenderer")
	return strings.TrimSpace(strings.ReplaceAll(text(dig(shelf, "description")), "\r\n", "\n")), nil
}
