package ytm

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Parsing what YouTube Music lists: songs and videos, albums, playlists and
// artists, from the renderers its pages show them in. Words differ by
// Language, so what a run is goes by where it leads (a browse ID, a page
// type) and its shape (3:45, 2017, 1.2B plays) rather than by what it says.

const greyOut = "MUSIC_ITEM_RENDERER_DISPLAY_POLICY_GREY_OUT"

const (
	pageAlbum    = "MUSIC_PAGE_TYPE_ALBUM"
	pageArtist   = "MUSIC_PAGE_TYPE_ARTIST"
	pageChannel  = "MUSIC_PAGE_TYPE_USER_CHANNEL"
	pagePlaylist = "MUSIC_PAGE_TYPE_PLAYLIST"
	pagePodcast  = "MUSIC_PAGE_TYPE_PODCAST_SHOW_DETAIL_PAGE"
	pageDisco    = "MUSIC_PAGE_TYPE_ARTIST_DISCOGRAPHY"
	pageLyrics   = "MUSIC_PAGE_TYPE_TRACK_LYRICS"
)

var (
	durationRe = regexp.MustCompile(`^\d{1,3}(:\d{2}){1,2}$`)
	yearRe     = regexp.MustCompile(`^(\d{4})\s?[年년]?$`)
	countRe    = regexp.MustCompile(`^(\d[\d.,]*)\s?([KMBkmb万萬亿億]?)`)
	// A count after its label, as "播放次数：2亿".
	labelledCountRe = regexp.MustCompile(`^[^\d:：]{1,16}[:：]\s*(\d.*)$`)
	tracksRe        = regexp.MustCompile(`(?i)^(\d[\d,.\s]*)\s*(songs?|tracks?|videos?|episodes?|首|曲|个视频|個影片)`)
)

// parseDuration reads "3:45" or "1:02:03" as milliseconds, or 0.
func parseDuration(s string) int64 {
	s = strings.TrimSpace(s)
	if !durationRe.MatchString(s) {
		return 0
	}
	var secs int64
	for _, p := range strings.Split(s, ":") {
		n, _ := strconv.ParseInt(p, 10, 64)
		secs = secs*60 + n
	}
	return secs * 1000
}

// parseCount reads a count as YouTube words it ("1.2B views", "350K",
// "12,345 plays"), or 0.
func parseCount(s string) float64 {
	s = strings.TrimSpace(s)
	if m := labelledCountRe.FindStringSubmatch(s); m != nil {
		s = m[1]
	}
	m := countRe.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	digits := m[1]
	if m[2] != "" {
		digits = strings.ReplaceAll(digits, ",", ".")
	} else {
		digits = strings.NewReplacer(",", "", ".", "").Replace(digits)
	}
	n, err := strconv.ParseFloat(digits, 64)
	if err != nil {
		return 0
	}
	switch m[2] {
	case "K", "k":
		n *= 1e3
	case "M", "m":
		n *= 1e6
	case "B", "b":
		n *= 1e9
	case "万", "萬":
		n *= 1e4
	case "亿", "億":
		n *= 1e8
	}
	return n
}

// looksLikeCount reports whether s is a count of plays or views, as
// "1.2B plays" or "12亿次观看": a number, then words in lowercase or CJK
// (so "50 Cent" stays an artist).
func looksLikeCount(s string) bool {
	if labelledCountRe.MatchString(s) {
		return true
	}
	m := countRe.FindStringIndex(s)
	if m == nil {
		return false
	}
	rest := strings.TrimSpace(s[m[1]:])
	if rest == "" {
		return false
	}
	r := []rune(rest)[0]
	return unicode.IsLower(r) || unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

// trackCount reads "100 songs" as 100, or 0 if s doesn't count songs.
func trackCount(s string) int {
	m := tracksRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, m[1]))
	return n
}

// parseYear reads "2017" or "2017年" as 2017.
func parseYear(s string) (int, bool) {
	m := yearRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	y, _ := strconv.Atoi(m[1])
	return y, true
}

func yearTime(y int) int64 {
	return time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
}

// bestThumb returns the largest of the thumbnails under v.
func bestThumb(v any) string {
	best, size := "", -1.0
	for _, t := range arr(find(v, "thumbnails")) {
		w, _ := num(t, "width")
		h, _ := num(t, "height")
		if u := str(t, "url"); u != "" && w*h >= size {
			best, size = u, w*h
		}
	}
	if strings.HasPrefix(best, "//") {
		best = "https:" + best
	}
	return best
}

func isSep(s string) bool {
	switch strings.TrimSpace(s) {
	case "", "•", "·", "&", ",", "、", "and", "和", "與", "y", "e", "et", "und":
		return true
	}
	return false
}

// groups splits runs at their " • " separators.
func groups(runs []any) [][]any {
	var gs [][]any
	var g []any
	for _, r := range runs {
		t := strings.TrimSpace(str(r, "text"))
		if t == "•" || t == "·" {
			if len(g) > 0 {
				gs = append(gs, g)
			}
			g = nil
			continue
		}
		g = append(g, r)
	}
	if len(g) > 0 {
		gs = append(gs, g)
	}
	return gs
}

// browseOf returns where a run leads: a browse ID and the kind of its page.
func browseOf(r any) (id, page string) {
	be := obj(r, "navigationEndpoint", "browseEndpoint")
	if be == nil {
		return "", ""
	}
	return str(be, "browseId"), str(be, "browseEndpointContextSupportedConfigs", "browseEndpointContextMusicConfig", "pageType")
}

func isArtistPage(id, page string) bool {
	return page == pageArtist || page == pageChannel || (page == "" && strings.HasPrefix(id, "UC"))
}

func isAlbumPage(id, page string) bool {
	return page == pageAlbum || strings.HasPrefix(id, "MPRE")
}

func groupHasArtist(g []any) bool {
	for _, r := range g {
		if isArtistPage(browseOf(r)) {
			return true
		}
	}
	return false
}

// labelled reports whether the first group of a byline names what the item
// is ("Song • Luis Fonsi • …"): one word that leads nowhere, before an
// artist who leads to their page.
func labelled(gs [][]any) bool {
	if len(gs) < 2 || len(gs[0]) != 1 || obj(gs[0][0], "navigationEndpoint") != nil {
		return false
	}
	for _, g := range gs[1:] {
		if groupHasArtist(g) {
			return true
		}
	}
	return false
}

// fillRuns reads a line of a track's credits: its artists, album, length
// and plays. byline says the line starts with the artists; label that
// it starts with what the item is.
func (t *Track) fillRuns(runs []any, byline, label bool) {
	gs := groups(runs)
	if byline && len(gs) > 1 && (label || labelled(gs)) {
		gs = gs[1:]
	}
	for gi, g := range gs {
		for _, r := range g {
			s := str(r, "text")
			if isSep(s) {
				continue
			}
			if id, page := browseOf(r); id != "" {
				switch {
				case isAlbumPage(id, page):
					t.Album.ID, t.Album.Name = id, s
				case isArtistPage(id, page):
					t.Artists = append(t.Artists, Artist{ID: id, Name: s})
				}
				continue
			}
			if obj(r, "navigationEndpoint") != nil {
				continue
			}
			s = strings.TrimSpace(s)
			switch {
			case durationRe.MatchString(s):
				t.Duration = parseDuration(s)
			case yearRe.MatchString(s):
			case looksLikeCount(s):
				if t.Views == "" {
					t.Views = s
				}
			case byline && gi == 0:
				t.Artists = append(t.Artists, Artist{Name: s})
			}
		}
	}
}

// columns returns the text objects of a list item's flex columns, then of
// its fixed columns.
func columns(r any) (flex, fixed []any) {
	for _, c := range arr(r, "flexColumns") {
		flex = append(flex, dig(c, "musicResponsiveListItemFlexColumnRenderer", "text"))
	}
	for _, c := range arr(r, "fixedColumns") {
		fixed = append(fixed, dig(c, "musicResponsiveListItemFixedColumnRenderer", "text"))
	}
	return flex, fixed
}

func videoTypeOf(v any) string {
	s, _ := find(v, "musicVideoType").(string)
	return s
}

// responsiveTrack reads a song or video from a musicResponsiveListItemRenderer,
// as search, playlists, albums and artists list them. label says its byline
// starts with what it is, as in suggestions.
func responsiveTrack(r any, label bool) Track {
	flex, fixed := columns(r)
	var t Track
	var we any
	if len(flex) > 0 {
		t.Name = text(flex[0])
		we = dig(flex[0], "runs", 0, "navigationEndpoint", "watchEndpoint")
	}
	t.ID = str(r, "playlistItemData", "videoId")
	if t.ID == "" {
		t.ID = str(we, "videoId")
	}
	overlay := dig(r, "overlay", "musicItemThumbnailOverlayRenderer", "content", "musicPlayButtonRenderer", "playNavigationEndpoint", "watchEndpoint")
	if t.ID == "" {
		t.ID = str(overlay, "videoId")
	}
	if t.ID == "" {
		t.ID = str(r, "navigationEndpoint", "watchEndpoint", "videoId")
	}
	t.VideoType = videoTypeOf(we)
	if t.VideoType == "" {
		t.VideoType = videoTypeOf(overlay)
	}
	if t.VideoType == "" {
		t.VideoType = videoTypeOf(dig(r, "navigationEndpoint"))
	}
	for i, c := range flex {
		if i > 0 {
			t.fillRuns(arr(c, "runs"), i == 1, label && i == 1)
		}
	}
	for _, c := range fixed {
		t.fillRuns(arr(c, "runs"), false, false)
	}
	t.Album.PicURL = bestThumb(dig(r, "thumbnail"))
	t.Unplayable = t.ID == "" || str(r, "musicItemRendererDisplayPolicy") == greyOut
	// An album lists a song by its music video where it has one, and
	// credits the song's own audio (MPTC+its ID): that plays, the video
	// shows.
	if t.VideoType == VideoOMV {
		if song := creditsID(r); song != "" && song != t.ID {
			t.ID, t.VideoID, t.VideoType = song, t.ID, VideoATV
		}
	}
	return t
}

// creditsID returns the video whose credits a list item's menu leads to.
func creditsID(r any) string {
	for _, be := range findAll(dig(r, "menu"), "browseEndpoint", nil) {
		if id := str(be, "browseId"); strings.HasPrefix(id, "MPTC") && len(id) == 4+11 {
			return id[4:]
		}
	}
	return ""
}

// panelTrack reads a track of a queue: a playlistPanelVideoRenderer, or a
// wrapper of one with its counterpart, a song's music video.
func panelTrack(item any) (Track, bool) {
	kind, v := only(item)
	switch kind {
	case "playlistPanelVideoRenderer":
		return panelVideo(v), true
	case "playlistPanelVideoWrapperRenderer":
		t := panelVideo(dig(v, "primaryRenderer", "playlistPanelVideoRenderer"))
		other := dig(v, "counterpart", 0, "counterpartRenderer", "playlistPanelVideoRenderer")
		if id := str(other, "videoId"); id != "" && id != t.ID && t.Video() == "" {
			if vt := videoTypeOf(dig(other, "navigationEndpoint")); vt != VideoATV {
				t.VideoID = id
			}
		}
		return t, t.ID != "" || t.Name != ""
	}
	return Track{}, false
}

func panelVideo(p any) Track {
	t := Track{
		ID:        str(p, "videoId"),
		Name:      text(dig(p, "title")),
		Duration:  parseDuration(text(dig(p, "lengthText"))),
		VideoType: videoTypeOf(dig(p, "navigationEndpoint")),
	}
	if t.ID == "" {
		t.ID = str(p, "navigationEndpoint", "watchEndpoint", "videoId")
	}
	t.fillRuns(arr(p, "longBylineText", "runs"), true, false)
	if len(t.Artists) == 0 {
		t.fillRuns(arr(p, "shortBylineText", "runs"), true, false)
	}
	t.Album.PicURL = bestThumb(dig(p, "thumbnail"))
	t.Unplayable = t.ID == "" || dig(p, "unplayableText") != nil
	return t
}

// twoRowTrack reads a song or video from a musicTwoRowItemRenderer, as the
// home page's carousels show them.
func twoRowTrack(r any) Track {
	we := dig(r, "navigationEndpoint", "watchEndpoint")
	t := Track{
		ID:        str(we, "videoId"),
		Name:      text(dig(r, "title")),
		VideoType: videoTypeOf(we),
	}
	t.fillRuns(arr(r, "subtitle", "runs"), true, false)
	t.Album.PicURL = bestThumb(dig(r, "thumbnailRenderer"))
	t.Unplayable = t.ID == ""
	return t
}

// browsablePlaylist reports whether a playlist ID browses as a playlist:
// mixes made on the fly (RDAMPL…, RDEM…) don't.
func browsablePlaylist(id string) bool {
	if id == "" {
		return false
	}
	if strings.HasPrefix(id, "RD") {
		return strings.HasPrefix(id, "RDCLAK")
	}
	return true
}

// twoRowPlaylist reads a playlist from a musicTwoRowItemRenderer: a
// playlist, or an album as the playlist it plays as.
func twoRowPlaylist(r any) (Playlist, bool) {
	id, page := browseOf(r)
	pid := ""
	switch {
	case strings.HasPrefix(id, "VL"):
		pid = id[2:]
	case page == pagePlaylist:
		pid = id
	case id == "":
		pid = str(r, "navigationEndpoint", "watchPlaylistEndpoint", "playlistId")
	case isAlbumPage(id, page):
		pid = str(find(dig(r, "thumbnailOverlay"), "watchPlaylistEndpoint"), "playlistId")
	}
	if !browsablePlaylist(pid) {
		return Playlist{}, false
	}
	p := Playlist{ID: pid, Name: text(dig(r, "title")), CoverURL: bestThumb(dig(r, "thumbnailRenderer"))}
	p.Subtitle = text(dig(r, "subtitle"))
	p.fillRuns(arr(r, "subtitle", "runs"))
	return p, true
}

// fillRuns reads a playlist's subtitle: who made it, how many songs it
// has, and how often it was played.
func (p *Playlist) fillRuns(runs []any) {
	for _, r := range runs {
		s := strings.TrimSpace(str(r, "text"))
		if isSep(s) {
			continue
		}
		if id, page := browseOf(r); id != "" {
			if isArtistPage(id, page) && p.Creator.Nickname == "" {
				p.Creator.UserID, p.Creator.Nickname = id, s
			}
			continue
		}
		if n := trackCount(s); n > 0 {
			p.TrackCount = n
		} else if looksLikeCount(s) {
			p.PlayCount = parseCount(s)
		}
	}
}

// responsivePlaylist reads a playlist from a musicResponsiveListItemRenderer,
// as search lists them: "Author • 1.2M views" or "YouTube Music • 50 songs".
func responsivePlaylist(r any) (Playlist, bool) {
	id, page := browseOf(r)
	if !strings.HasPrefix(id, "VL") && page != pagePlaylist {
		return Playlist{}, false
	}
	flex, _ := columns(r)
	p := Playlist{ID: strings.TrimPrefix(id, "VL"), CoverURL: bestThumb(dig(r, "thumbnail"))}
	if len(flex) > 0 {
		p.Name = text(flex[0])
	}
	if len(flex) > 1 {
		p.Subtitle = text(flex[1])
		runs := arr(flex[1], "runs")
		p.fillRuns(runs)
		if p.Creator.Nickname == "" {
			if gs := groups(runs); len(gs) > 0 {
				first := strings.TrimSpace(text(map[string]any{"runs": gs[0]}))
				if trackCount(first) == 0 && !looksLikeCount(first) {
					p.Creator.Nickname = first
				}
			}
		}
	}
	return p, p.ID != ""
}

// fillRuns reads an album's subtitle: what it is, who made it, and the year
// it came out ("Single • Luis Fonsi • 2017", or "2024" on its artist's page).
func (al *AlbumProfile) fillRuns(runs []any) {
	gs := groups(runs)
	for gi, g := range gs {
		joined := strings.TrimSpace(text(map[string]any{"runs": g}))
		switch {
		case yearRe.MatchString(joined):
			y, _ := parseYear(joined)
			al.PublishTime = yearTime(y)
		case groupHasArtist(g):
			for _, r := range g {
				s := strings.TrimSpace(str(r, "text"))
				if isSep(s) {
					continue
				}
				id, page := browseOf(r)
				if id == "" || isArtistPage(id, page) {
					al.Artists = append(al.Artists, Artist{ID: id, Name: s})
				}
			}
		case trackCount(joined) > 0:
			al.Size = trackCount(joined)
		case gi == 0 && len(gs) > 1 && al.Kind == "":
			al.Kind = joined
		case looksLikeCount(joined) || durationRe.MatchString(joined):
		case joined != "":
			al.Artists = append(al.Artists, Artist{Name: joined})
		}
	}
}

// twoRowAlbum reads an album from a musicTwoRowItemRenderer.
func twoRowAlbum(r any) (AlbumProfile, bool) {
	id, page := browseOf(r)
	if !isAlbumPage(id, page) {
		return AlbumProfile{}, false
	}
	al := AlbumProfile{Album: Album{ID: id, Name: text(dig(r, "title")), PicURL: bestThumb(dig(r, "thumbnailRenderer"))}}
	al.fillRuns(arr(r, "subtitle", "runs"))
	return al, true
}

// responsiveAlbum reads an album from a musicResponsiveListItemRenderer, as
// search lists them.
func responsiveAlbum(r any) (AlbumProfile, bool) {
	id, page := browseOf(r)
	if !isAlbumPage(id, page) {
		return AlbumProfile{}, false
	}
	flex, _ := columns(r)
	al := AlbumProfile{Album: Album{ID: id, PicURL: bestThumb(dig(r, "thumbnail"))}}
	if len(flex) > 0 {
		al.Name = text(flex[0])
	}
	if len(flex) > 1 {
		al.fillRuns(arr(flex[1], "runs"))
	}
	return al, true
}

// responsiveArtist reads an artist from a musicResponsiveListItemRenderer.
func responsiveArtist(r any) (ArtistProfile, bool) {
	id, page := browseOf(r)
	if !isArtistPage(id, page) {
		return ArtistProfile{}, false
	}
	flex, _ := columns(r)
	a := ArtistProfile{Artist: Artist{ID: id}, Avatar: bestThumb(dig(r, "thumbnail"))}
	if len(flex) > 0 {
		a.Name = text(flex[0])
	}
	return a, true
}

func twoRowArtist(r any) (ArtistProfile, bool) {
	id, page := browseOf(r)
	if !isArtistPage(id, page) {
		return ArtistProfile{}, false
	}
	return ArtistProfile{Artist: Artist{ID: id, Name: text(dig(r, "title"))}, Avatar: bestThumb(dig(r, "thumbnailRenderer"))}, true
}

// shelf is a section of a page: its title, the browse its title or "more"
// leads to, its items (one-key renderer objects) and its continuation.
type shelf struct {
	Title    string
	More     any // a browseEndpoint
	Items    []any
	Continue string
}

// shelves returns the shelves of a sectionListRenderer's contents.
func shelves(contents []any) []shelf {
	var out []shelf
	for _, c := range contents {
		kind, v := only(c)
		var s shelf
		switch kind {
		case "musicCarouselShelfRenderer":
			h := dig(v, "header", "musicCarouselShelfBasicHeaderRenderer")
			s.Title = text(dig(h, "title"))
			s.More = dig(h, "title", "runs", 0, "navigationEndpoint", "browseEndpoint")
			if s.More == nil {
				s.More = dig(h, "moreContentButton", "buttonRenderer", "navigationEndpoint", "browseEndpoint")
			}
			s.Items = arr(v, "contents")
		case "musicShelfRenderer", "musicPlaylistShelfRenderer":
			s.Title = text(dig(v, "title"))
			s.More = dig(v, "title", "runs", 0, "navigationEndpoint", "browseEndpoint")
			if s.More == nil {
				s.More = dig(v, "bottomEndpoint", "browseEndpoint")
			}
			s.Items = arr(v, "contents")
			s.Continue = continuation(v)
		case "gridRenderer":
			s.Title = text(dig(v, "header", "gridHeaderRenderer", "title"))
			s.Items = arr(v, "items")
			s.Continue = continuation(v)
		default:
			continue
		}
		out = append(out, s)
	}
	return out
}

// continuation returns the token for what comes after a shelf or a list:
// in its continuations, or a continuationItemRenderer last in its items.
func continuation(v any) string {
	for _, k := range []string{"nextContinuationData", "nextRadioContinuationData", "reloadContinuationData"} {
		if t := str(v, "continuations", 0, k, "continuation"); t != "" {
			return t
		}
	}
	for _, key := range []string{"contents", "items", "continuationItems"} {
		if t := str(v, key, -1, "continuationItemRenderer", "continuationEndpoint", "continuationCommand", "token"); t != "" {
			return t
		}
	}
	return ""
}

// continued returns the items and the next token of a continuation's
// answer, in either of the shapes InnerTube answers with.
func continued(resp any) (items []any, next string) {
	if cc := obj(resp, "continuationContents"); cc != nil {
		for _, v := range cc {
			items = arr(v, "contents")
			if items == nil {
				items = arr(v, "items")
			}
			return items, continuation(v)
		}
	}
	for _, key := range []string{"onResponseReceivedActions", "onResponseReceivedCommands"} {
		for _, a := range arr(resp, key) {
			if ci := dig(a, "appendContinuationItemsAction"); ci != nil {
				items = append(items, arr(ci, "continuationItems")...)
				next = continuation(ci)
			}
		}
	}
	return items, next
}

// tracksOf reads the songs and videos among items.
func tracksOf(items []any) []Track {
	var out []Track
	for _, it := range items {
		kind, v := only(it)
		switch kind {
		case "musicResponsiveListItemRenderer":
			if id, _ := browseOf(v); id != "" && str(v, "playlistItemData", "videoId") == "" {
				continue // an album, artist or playlist
			}
			t := responsiveTrack(v, false)
			if t.Name != "" {
				out = append(out, t)
			}
		case "musicTwoRowItemRenderer":
			if dig(v, "navigationEndpoint", "watchEndpoint") != nil {
				out = append(out, twoRowTrack(v))
			}
		case "playlistPanelVideoRenderer", "playlistPanelVideoWrapperRenderer":
			if t, ok := panelTrack(it); ok {
				out = append(out, t)
			}
		}
	}
	return out
}

// sectionContents returns the sections of a browse: of its single column,
// or the tab and the secondary column of its two.
func sectionContents(resp any) (tab, secondary []any) {
	if sc := dig(resp, "contents", "singleColumnBrowseResultsRenderer", "tabs", 0, "tabRenderer", "content", "sectionListRenderer"); sc != nil {
		return arr(sc, "contents"), nil
	}
	tc := dig(resp, "contents", "twoColumnBrowseResultsRenderer")
	return arr(tc, "tabs", 0, "tabRenderer", "content", "sectionListRenderer", "contents"),
		arr(tc, "secondaryContents", "sectionListRenderer", "contents")
}
