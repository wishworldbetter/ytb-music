package ytm

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

// The catalogue's pages: home, charts, playlists, albums and artists, each
// a browse of its ID.

func browse(id, params string) (any, error) {
	body := map[string]any{"browseId": id}
	if params != "" {
		body["params"] = params
	}
	return call("browse", body)
}

// browseNext fetches what comes after a page, by its continuation token.
func browseNext(token string) (any, error) {
	return call("browse", map[string]any{"continuation": token})
}

var homeCache = newCache[[]shelf](5*time.Minute, 4)

// homePage is the first page of the home feed: its shelves, and the token
// for the rest.
type homePage struct {
	shelves []shelf
	token   string
}

var homeFirstCache = newCache[homePage](5*time.Minute, 4)

// homeFirst returns the first page of the home feed, which has the quick
// picks: what needs no more than those comes without waiting for the rest.
func homeFirst() (homePage, error) {
	return homeFirstCache.get(Language+"/"+Region, func() (homePage, error) {
		resp, err := browse("FEmusic_home", "")
		if err != nil {
			return homePage{}, err
		}
		tab, _ := sectionContents(resp)
		token := continuation(dig(resp, "contents", "singleColumnBrowseResultsRenderer", "tabs", 0, "tabRenderer", "content", "sectionListRenderer"))
		return homePage{shelves(tab), token}, nil
	})
}

// homeShelves returns the shelves of the home page, its first pages of
// them, kept for a few minutes as YouTube says they keep.
func homeShelves() ([]shelf, error) {
	return homeCache.get(Language+"/"+Region, func() ([]shelf, error) {
		first, err := homeFirst()
		if err != nil {
			return nil, err
		}
		out := append([]shelf(nil), first.shelves...)
		token := first.token
		for i := 0; i < 3 && token != ""; i++ {
			more, err := browseNext(token)
			if err != nil {
				break
			}
			items, next := continued(more)
			out = append(out, shelves(items)...)
			token = next
		}
		if len(out) == 0 {
			return nil, errors.New("ytm: the home page shows nothing")
		}
		return out, nil
	})
}

// FeaturedPlaylists returns up to limit playlists YouTube Music features
// on its home page (FEmusic_home) for anyone, mixes and moods included.
func FeaturedPlaylists(limit int) ([]Playlist, error) {
	shs, err := homeShelves()
	if err != nil {
		return nil, err
	}
	return playlistsOf(shs, limit), nil
}

// FeaturedPlaylistsFirst returns up to limit of the playlists on the first
// page of the home feed: what FeaturedPlaylists starts with, which shows
// while the rest of the feed comes.
func FeaturedPlaylistsFirst(limit int) ([]Playlist, error) {
	first, err := homeFirst()
	if err != nil {
		return nil, err
	}
	return playlistsOf(first.shelves, limit), nil
}

// playlistsOf returns up to limit of the playlists on shelves, once each.
func playlistsOf(shs []shelf, limit int) []Playlist {
	var out []Playlist
	seen := map[string]bool{}
	for _, s := range shs {
		for _, it := range s.Items {
			kind, v := only(it)
			if kind != "musicTwoRowItemRenderer" {
				continue
			}
			p, ok := twoRowPlaylist(v)
			if !ok || seen[p.ID] {
				continue
			}
			seen[p.ID] = true
			out = append(out, p)
			if limit > 0 && len(out) >= limit {
				return out
			}
		}
	}
	return out
}

// RecommendedPlaylists returns the playlists recommended to the listener:
// without an account, the home page's.
func RecommendedPlaylists() ([]Playlist, error) { return FeaturedPlaylists(0) }

// DailySongs returns the songs of the home page's "Quick picks" shelf, or
// its first shelf of songs, each with Reason empty.
func DailySongs() ([]Track, error) {
	// The quick picks lead the home feed's first page.
	if first, err := homeFirst(); err == nil {
		for _, s := range first.shelves {
			if len(s.Items) == 0 {
				continue
			}
			if kind, _ := only(s.Items[0]); kind != "musicResponsiveListItemRenderer" {
				continue
			}
			if ts := playable(tracksOf(s.Items)); len(ts) > 0 {
				fillDurations(ts)
				return ts, nil
			}
		}
	}
	shs, err := homeShelves()
	if err == nil {
		// Quick picks: the first shelf listing songs a row each.
		for _, s := range shs {
			if len(s.Items) == 0 {
				continue
			}
			if kind, _ := only(s.Items[0]); kind != "musicResponsiveListItemRenderer" {
				continue
			}
			if ts := playable(tracksOf(s.Items)); len(ts) > 0 {
				fillDurations(ts)
				return ts, nil
			}
		}
		for _, s := range shs {
			if ts := playable(tracksOf(s.Items)); len(ts) > 0 {
				fillDurations(ts)
				return ts, nil
			}
		}
	}
	charts, cerr := chartList()
	if cerr != nil || len(charts) == 0 {
		if err == nil {
			err = cerr
		}
		if err == nil {
			err = errors.New("ytm: no songs to pick")
		}
		return nil, err
	}
	p, _, err := playlistPage(charts[0].ID)
	if err != nil {
		return nil, err
	}
	return p.Tracks, nil
}

func playable(ts []Track) []Track {
	out := ts[:0]
	for _, t := range ts {
		if t.ID != "" {
			out = append(out, t)
		}
	}
	return out
}

// Charts returns the charts YouTube Music shows (FEmusic_charts): top
// songs, top videos, trending, each a playlist with its Preview of the
// top 3 (name, artist) and UpdateFreq saying how often it changes when
// YouTube says.
func Charts() ([]Playlist, error) {
	charts, err := previewCache.get(Language+"/"+Region, chartsWithPreviews)
	if err != nil {
		return nil, err
	}
	out := make([]Playlist, len(charts))
	for i, c := range charts {
		out[i] = c
		out[i].Preview = append([]ChartLine(nil), c.Preview...)
	}
	return out, nil
}

var previewCache = newCache[[]Playlist](30*time.Minute, 4)

// chartsWithPreviews returns the charts, each with its top 3, fetched at
// once.
func chartsWithPreviews() ([]Playlist, error) {
	charts, err := chartList()
	if err != nil {
		return nil, err
	}
	charts = append([]Playlist(nil), charts...)
	var wg sync.WaitGroup
	// All at once, about a dozen: in waves, each waits for the last.
	sem := make(chan struct{}, 16)
	for i := range charts {
		wg.Add(1)
		go func(c *Playlist) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			p, _, err := playlistPage(c.ID)
			if err != nil {
				return
			}
			if c.Description == "" {
				c.Description = p.Description
			}
			c.TrackCount = p.TrackCount
			if c.CoverURL == "" {
				c.CoverURL = p.CoverURL
			}
			for _, t := range p.Tracks {
				if len(c.Preview) == 3 {
					break
				}
				c.Preview = append(c.Preview, ChartLine{First: t.Name, Second: t.ArtistNames()})
			}
		}(&charts[i])
	}
	wg.Wait()
	return charts, nil
}

var chartCache = newCache[[]Playlist](30*time.Minute, 4)

// chartList returns the charts of the charts page, without their previews.
func chartList() ([]Playlist, error) {
	return chartCache.get(Language+"/"+Region, func() ([]Playlist, error) {
		resp, err := call("browse", map[string]any{
			"browseId": "FEmusic_charts",
			"formData": map[string]any{"selectedValues": []string{Region}},
		})
		if err != nil {
			resp, err = browse("FEmusic_charts", "")
		}
		if err != nil {
			return nil, err
		}
		charts := chartsOf(resp, func(string) bool { return true })
		if len(charts) == 0 {
			// The charts YouTube Charts makes, as the explore page shows them.
			if resp, err := browse("FEmusic_explore", ""); err == nil {
				charts = chartsOf(resp, func(id string) bool { return strings.HasPrefix(id, "PL4fGSI1pDJn") })
			}
		}
		if len(charts) == 0 {
			return nil, errors.New("ytm: no charts")
		}
		return charts, nil
	})
}

func chartsOf(resp any, keep func(id string) bool) []Playlist {
	tab, _ := sectionContents(resp)
	var out []Playlist
	seen := map[string]bool{}
	for _, s := range shelves(tab) {
		for _, it := range s.Items {
			kind, v := only(it)
			if kind != "musicTwoRowItemRenderer" {
				continue
			}
			if id, page := browseOf(v); !strings.HasPrefix(id, "VL") && page != pagePlaylist {
				continue
			}
			p, ok := twoRowPlaylist(v)
			if ok && keep(p.ID) && !seen[p.ID] {
				seen[p.ID] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// PlaylistDetail returns a playlist (browse "VL"+id) with its tracks, all
// of them up to 1000, following continuations, and TrackCount.
// PlaylistStart returns a playlist with the tracks of its first page, and
// the token for the rest, which PlaylistMore fetches a page at a time: a
// long playlist can show as soon as its first page comes.
func PlaylistStart(id string) (*Playlist, string, error) {
	return playlistPage(strings.TrimPrefix(id, "VL"))
}

// PlaylistMore returns the tracks of the page of a playlist that token
// names, and the token for the page after it, empty at its end.
func PlaylistMore(token string) ([]Track, string, error) {
	resp, err := browseNext(token)
	if err != nil {
		return nil, "", err
	}
	items, next := continued(resp)
	ts := tracksOf(items)
	if len(ts) == 0 {
		next = ""
	}
	return ts, next, nil
}

// PlaylistLimit is how many of a playlist's tracks are fetched, at most.
const PlaylistLimit = 1000

func PlaylistDetail(id string) (*Playlist, error) {
	p, token, err := PlaylistStart(id)
	if err != nil {
		return nil, err
	}
	for i := 0; i < 20 && token != "" && len(p.Tracks) < PlaylistLimit; i++ {
		ts, next, err := PlaylistMore(token)
		if err != nil {
			break
		}
		p.Tracks = append(p.Tracks, ts...)
		token = next
	}
	if len(p.Tracks) > PlaylistLimit {
		p.Tracks = p.Tracks[:PlaylistLimit]
	}
	if p.TrackCount < len(p.Tracks) {
		p.TrackCount = len(p.Tracks)
	}
	p.TrackIDs = make([]struct {
		ID string `json:"id"`
	}, len(p.Tracks))
	for i, t := range p.Tracks {
		p.TrackIDs[i].ID = t.ID
	}
	return p, nil
}

// playlistPage returns a playlist with the tracks of its first page, and
// the token for the rest.
func playlistPage(id string) (*Playlist, string, error) {
	resp, err := browse("VL"+id, "")
	if err != nil {
		return nil, "", err
	}
	return parsePlaylist(resp, id)
}

// parsePlaylist reads a playlist's page: what it is, its first tracks, and
// the token for the rest.
func parsePlaylist(resp any, id string) (*Playlist, string, error) {
	tab, sec := sectionContents(resp)
	p := &Playlist{ID: id}
	h := detailHeader(resp, tab)
	if h == nil {
		return nil, "", errors.New("ytm: no such playlist")
	}
	p.Name = text(dig(h, "title"))
	p.Description = headerDescription(h)
	p.CoverURL = bestThumb(dig(h, "thumbnail"))
	var subs []string
	for _, k := range []string{"subtitle", "secondSubtitle"} {
		if s := text(dig(h, k)); s != "" {
			subs = append(subs, s)
		}
		p.fillRuns(arr(h, k, "runs"))
	}
	p.Subtitle = strings.Join(subs, " • ")
	if p.Creator.Nickname == "" {
		if s := strings.TrimSpace(text(dig(h, "straplineTextOne"))); s != "" {
			p.Creator.Nickname = s
			p.Creator.UserID, _ = browseOf(dig(h, "straplineTextOne", "runs", 0))
		} else if s := str(h, "facepile", "avatarStackViewModel", "text", "content"); s != "" {
			p.Creator.Nickname = s
		}
	}
	items, token := trackShelf(append(sec, tab...))
	p.Tracks = tracksOf(items)
	return p, token, nil
}

// detailHeader returns the header of a playlist's or an album's page, in
// its two-column layout or its older one.
func detailHeader(resp any, tab []any) any {
	for _, c := range tab {
		if h := find(c, "musicResponsiveHeaderRenderer"); h != nil {
			return h
		}
	}
	for _, k := range []string{"musicResponsiveHeaderRenderer", "musicDetailHeaderRenderer", "musicImmersiveHeaderRenderer", "musicVisualHeaderRenderer"} {
		if h := find(dig(resp, "header"), k); h != nil {
			return h
		}
	}
	return nil
}

func headerDescription(h any) string {
	if d := text(dig(h, "description", "musicDescriptionShelfRenderer", "description")); d != "" {
		return d
	}
	return text(dig(h, "description"))
}

// trackShelf returns the items of the first shelf of tracks among
// sections, and the token for more of them.
func trackShelf(sections []any) ([]any, string) {
	for _, c := range sections {
		kind, v := only(c)
		if kind == "musicPlaylistShelfRenderer" || kind == "musicShelfRenderer" {
			return arr(v, "contents"), continuation(v)
		}
	}
	return nil, ""
}

// AlbumDetail returns an album (browse its MPREb_ ID) with its songs, each
// with the album's cover as Album.PicURL.
func AlbumDetail(id string) (*AlbumProfile, error) {
	resp, err := browse(id, "")
	if err != nil {
		return nil, err
	}
	al, token, err := parseAlbum(resp, id)
	if err != nil {
		return nil, err
	}
	for i := 0; i < 5 && token != ""; i++ {
		more, err := browseNext(token)
		if err != nil {
			break
		}
		items, next := continued(more)
		ts := tracksOf(items)
		if len(ts) == 0 {
			break
		}
		al.addSongs(ts)
		token = next
	}
	return al, nil
}

// parseAlbum reads an album's page: what it is, its songs, and the token
// for more of them.
func parseAlbum(resp any, id string) (*AlbumProfile, string, error) {
	tab, sec := sectionContents(resp)
	h := detailHeader(resp, tab)
	if h == nil {
		return nil, "", errors.New("ytm: no such album")
	}
	al := &AlbumProfile{Album: Album{ID: id, Name: text(dig(h, "title")), PicURL: bestThumb(dig(h, "thumbnail"))}}
	al.fillRuns(arr(h, "subtitle", "runs"))
	if strap := arr(h, "straplineTextOne", "runs"); len(strap) > 0 {
		al.Artists = nil
		for _, r := range strap {
			s := strings.TrimSpace(str(r, "text"))
			if isSep(s) {
				continue
			}
			aid, _ := browseOf(r)
			al.Artists = append(al.Artists, Artist{ID: aid, Name: s})
		}
	}
	al.Description = headerDescription(h)
	for _, r := range arr(h, "secondSubtitle", "runs") {
		if n := trackCount(str(r, "text")); n > 0 {
			al.Size = n
		}
	}
	items, token := trackShelf(append(sec, tab...))
	al.addSongs(tracksOf(items))
	return al, token, nil
}

// addSongs adds songs to the album, each of it and, uncredited, by its
// artists.
func (al *AlbumProfile) addSongs(songs []Track) {
	for i := range songs {
		t := &songs[i]
		t.Album = al.Album
		if len(t.Artists) == 0 {
			t.Artists = append([]Artist(nil), al.Artists...)
		}
	}
	al.Songs = append(al.Songs, songs...)
	if al.Size < len(al.Songs) {
		al.Size = len(al.Songs)
	}
}

var artistCache = newCache[any](3*time.Minute, 16)

// artistPage returns the browse of an artist's channel, kept a little for
// the calls that read it one after another.
func artistPage(id string) (any, error) {
	return artistCache.get(Language+"/"+Region+"/"+id, func() (any, error) {
		resp, err := browse(id, "")
		if err != nil {
			return nil, err
		}
		if dig(resp, "header") == nil {
			return nil, errors.New("ytm: no such artist")
		}
		return resp, nil
	})
}

// ArtistDetail returns an artist (browse their channel ID) with their top
// songs, as many as the page shows, and their name, avatar, banner,
// description and subscribers.
func ArtistDetail(id string) (*ArtistProfile, error) {
	resp, err := artistPage(id)
	if err != nil {
		return nil, err
	}
	_, h := only(dig(resp, "header"))
	a := &ArtistProfile{Artist: Artist{ID: id, Name: text(dig(h, "title"))}}
	a.PicURL = bestThumb(dig(h, "thumbnail"))
	if fg := dig(h, "foregroundThumbnail"); fg != nil {
		a.Avatar = bestThumb(fg)
	} else {
		a.Avatar = resize(a.PicURL, 544, 544, true)
	}
	if a.PicURL == "" {
		a.PicURL = a.Avatar
	}
	a.BriefDesc = text(dig(h, "description"))
	a.Subscribers = text(dig(h, "subscriptionButton", "subscribeButtonRenderer", "subscriberCountText"))
	tab, _ := sectionContents(resp)
	for _, c := range tab {
		if kind, v := only(c); kind == "musicDescriptionShelfRenderer" && a.BriefDesc == "" {
			a.BriefDesc = text(dig(v, "description"))
		}
	}
	for _, s := range shelves(tab) {
		if len(s.Items) == 0 {
			continue
		}
		if kind, _ := only(s.Items[0]); kind != "musicResponsiveListItemRenderer" {
			continue
		}
		if ts := tracksOf(s.Items); len(ts) > 0 {
			fillDurations(ts)
			a.Songs = ts
			if id := str(s.More, "browseId"); strings.HasPrefix(id, "VL") {
				a.SongsID = id[2:]
			}
			break
		}
	}
	return a, nil
}

var discoCache = newCache[[]AlbumProfile](10*time.Minute, 16)

// discography returns all of an artist's albums, singles and EPs, the
// newest first: the shelves of their page, and the pages those lead to
// for the ones the shelves don't show.
func discography(id string) ([]AlbumProfile, error) {
	return discoCache.get(Language+"/"+Region+"/"+id, func() ([]AlbumProfile, error) {
		resp, err := artistPage(id)
		if err != nil {
			return nil, err
		}
		self, parts := albumShelves(resp, id)
		var wg sync.WaitGroup
		for i := range parts {
			if parts[i].more == nil {
				continue
			}
			wg.Add(1)
			go func(p *albumShelf) {
				defer wg.Done()
				if all, err := discographyPage(p.more); err == nil && len(all) >= len(p.albums) {
					p.albums = all
				}
			}(&parts[i])
		}
		wg.Wait()
		return mergeAlbums(parts, self), nil
	})
}

// albumShelf is a shelf of an artist's albums, or singles and EPs, and
// the page of all of them it leads to.
type albumShelf struct {
	albums []AlbumProfile
	more   any
}

// albumShelves returns who an artist is and their page's shelves of albums.
func albumShelves(resp any, id string) (Artist, []albumShelf) {
	_, h := only(dig(resp, "header"))
	self := Artist{ID: id, Name: text(dig(h, "title"))}
	tab, _ := sectionContents(resp)
	var parts []albumShelf
	for _, s := range shelves(tab) {
		var als []AlbumProfile
		for _, it := range s.Items {
			if kind, v := only(it); kind == "musicTwoRowItemRenderer" {
				if al, ok := twoRowAlbum(v); ok {
					als = append(als, al)
				}
			}
		}
		if len(als) == 0 {
			continue
		}
		var more any
		if bid, page := str(s.More, "browseId"), str(s.More, "browseEndpointContextSupportedConfigs", "browseEndpointContextMusicConfig", "pageType"); page == pageDisco || strings.HasPrefix(bid, "MPAD") {
			more = s.More
		}
		parts = append(parts, albumShelf{als, more})
	}
	return self, parts
}

// mergeAlbums joins an artist's shelves of albums, the newest first, each
// by them unless credited otherwise.
func mergeAlbums(parts []albumShelf, self Artist) []AlbumProfile {
	var out []AlbumProfile
	seen := map[string]bool{}
	for _, p := range parts {
		for _, al := range p.albums {
			if seen[al.ID] {
				continue
			}
			seen[al.ID] = true
			if al.Kind == "" {
				al.Kind = "Album"
			}
			if len(al.Artists) == 0 {
				al.Artists = []Artist{self}
			}
			out = append(out, al)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].PublishTime > out[j].PublishTime })
	return out
}

// discographyPage returns the albums of the page an artist's shelf of
// albums or singles leads to.
func discographyPage(be any) ([]AlbumProfile, error) {
	items, err := shelfPage(be)
	if err != nil {
		return nil, err
	}
	var out []AlbumProfile
	for _, it := range items {
		if kind, v := only(it); kind == "musicTwoRowItemRenderer" {
			if al, ok := twoRowAlbum(v); ok {
				out = append(out, al)
			}
		}
	}
	return out, nil
}

// ArtistAlbums returns limit of an artist's albums, singles and EPs from
// offset, the newest first, and whether they have more.
func ArtistAlbums(id string, limit, offset int) ([]AlbumProfile, bool, error) {
	all, err := discography(id)
	if err != nil {
		return nil, false, err
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(all) {
		return nil, false, nil
	}
	end := len(all)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return append([]AlbumProfile(nil), all[offset:end]...), end < len(all), nil
}

// ArtistLatestSongs returns up to limit of an artist's songs, the newest
// first, as far as YouTube Music orders them: their latest singles' and
// albums' songs.
func ArtistLatestSongs(id string, limit int) ([]Track, error) {
	all, err := discography(id)
	if err != nil {
		return nil, err
	}
	if len(all) > 3 {
		all = all[:3]
	}
	if len(all) == 0 {
		return nil, nil
	}
	// All at once, rather than the next only if the first had too few:
	// a request more beats a round trip more.
	albums := make([]*AlbumProfile, len(all))
	errs := make([]error, len(all))
	var wg sync.WaitGroup
	for i := range albums {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			albums[i], errs[i] = AlbumDetail(all[i].ID)
		}(i)
	}
	wg.Wait()
	if errs[0] != nil {
		return nil, errs[0]
	}
	var songs []Track
	for _, al := range albums {
		if al != nil && (limit <= 0 || len(songs) < limit) {
			songs = append(songs, al.Songs...)
		}
	}
	if limit > 0 && len(songs) > limit {
		songs = songs[:limit]
	}
	return songs, nil
}
