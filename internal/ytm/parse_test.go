package ytm

import (
	"encoding/json"
	"os"
	"testing"
)

// fixture loads a response recorded from YouTube Music, trimmed, from
// testdata.
func fixture(t *testing.T, name string) any {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestParseDuration reads lengths as YouTube writes them.
func TestParseDuration(t *testing.T) {
	for in, want := range map[string]int64{
		"3:45": 225000, "0:07": 7000, "1:02:03": 3723000, "12:00": 720000,
		"": 0, "2017": 0, "3:4": 0, "10B plays": 0,
	} {
		if got := parseDuration(in); got != want {
			t.Errorf("parseDuration(%q) = %d, want %d", in, got, want)
		}
	}
}

// TestCounts tells plays and song counts from artists and lengths, in
// English and Chinese.
func TestCounts(t *testing.T) {
	for _, c := range []struct {
		in     string
		count  bool
		n      float64
		tracks int
	}{
		{"10B plays", true, 10e9, 0},
		{"1.3M views", true, 1.3e6, 0},
		{"12,345 plays", true, 12345, 0},
		{"播放次数：2亿", true, 2e8, 0},
		{"1395万次观看", true, 13950000, 0},
		{"100 songs", true, 100, 100},
		{"1,234 songs", true, 1234, 1234},
		{"15 首歌曲", true, 15, 15},
		{"50 Cent", false, 50, 0},
		{"3:49", false, 3, 0},
		{"2017", false, 2017, 0},
		{"Luis Fonsi", false, 0, 0},
	} {
		if got := looksLikeCount(c.in); got != c.count {
			t.Errorf("looksLikeCount(%q) = %v", c.in, got)
		}
		if got := parseCount(c.in); got != c.n {
			t.Errorf("parseCount(%q) = %v, want %v", c.in, got, c.n)
		}
		if got := trackCount(c.in); got != c.tracks {
			t.Errorf("trackCount(%q) = %d, want %d", c.in, got, c.tracks)
		}
	}
	if y, ok := parseYear("2019年"); !ok || y != 2019 {
		t.Errorf("parseYear(2019年) = %d, %v", y, ok)
	}
}

// TestSized asks each CDN for a picture of the size wanted, keeping a crop
// the picture had.
func TestSized(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		want string
	}{
		{"", 300, ""},
		{"https://lh3.googleusercontent.com/abc=w60-h60-l90-rj", 300, "https://lh3.googleusercontent.com/abc=w300-h300-l90-rj"},
		{"https://yt3.googleusercontent.com/abc=s192", 544, "https://yt3.googleusercontent.com/abc=w544-h544-l90-rj"},
		{"https://yt3.googleusercontent.com/abc=w120-h120-p-l90-rj", 400, "https://yt3.googleusercontent.com/abc=w400-h400-p-l90-rj"},
		{"https://yt3.ggpht.com/abc=s48-c-k-c0x00000000-no-cc-rj-rp", 96, "https://yt3.ggpht.com/abc=w96-h96-l90-rj"},
		{"https://lh3.googleusercontent.com/abc", 100, "https://lh3.googleusercontent.com/abc=w100-h100-l90-rj"},
		{"https://i.ytimg.com/vi/kJQP7kiw5Fk/hq720.jpg?sqp=xyz", 100, "https://i.ytimg.com/vi/kJQP7kiw5Fk/default.jpg"},
		{"https://i.ytimg.com/vi/kJQP7kiw5Fk/hqdefault.jpg", 300, "https://i.ytimg.com/vi/kJQP7kiw5Fk/mqdefault.jpg"},
		{"https://i.ytimg.com/vi_webp/kJQP7kiw5Fk/maxresdefault.webp", 480, "https://i.ytimg.com/vi/kJQP7kiw5Fk/hqdefault.jpg"},
		{"https://i.ytimg.com/vi/kJQP7kiw5Fk/default.jpg", 1000, "https://i.ytimg.com/vi/kJQP7kiw5Fk/sddefault.jpg"},
		{"https://i.ytimg.com/pl_c/PL123/studio_square_thumbnail.jpg", 300, "https://i.ytimg.com/pl_c/PL123/studio_square_thumbnail.jpg"},
	} {
		if got := Sized(c.in, c.n); got != c.want {
			t.Errorf("Sized(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

// TestLyricsQuery names a song as LRCLib does: a video's title without
// what the video adds, a channel without " - Topic" or "VEVO".
func TestLyricsQuery(t *testing.T) {
	for _, c := range []struct {
		t             Track
		title, artist string
	}{
		{Track{Name: "Despacito", Artists: []Artist{{Name: "Luis Fonsi"}}, VideoType: VideoATV}, "Despacito", "Luis Fonsi"},
		{Track{Name: "Song - Remastered 2011", Artists: []Artist{{Name: "Band - Topic"}}, VideoType: VideoATV}, "Song - Remastered 2011", "Band"},
		{Track{Name: "Tarantino (Official Music Video)", Artists: []Artist{{Name: "LISA"}}, VideoType: VideoOMV}, "Tarantino", "LISA"},
		{Track{Name: "Luis Fonsi - Despacito ft. Daddy Yankee", Artists: []Artist{{Name: "LuisFonsiVEVO"}}, VideoType: VideoOMV}, "Despacito", "Luis Fonsi"},
		{Track{Name: "Justin Bieber - Despacito (Lyrics / Letra) ft. Luis Fonsi & Daddy Yankee", Artists: []Artist{{Name: "7clouds"}}, VideoType: VideoUGC}, "Despacito", "Justin Bieber"},
		{Track{Name: "晴天【MV】", Artists: []Artist{{Name: "周杰倫"}}, VideoType: VideoOMV}, "晴天", "周杰倫"},
		{Track{Name: "Housewife [Official Video]", Artists: []Artist{{Name: "Tate McRae"}}}, "Housewife", "Tate McRae"},
		{Track{Name: "Imposible (feat. Ozuna)", Artists: []Artist{{Name: "Luis Fonsi"}}, VideoType: VideoATV}, "Imposible (feat. Ozuna)", "Luis Fonsi"},
	} {
		title, artist := lyricsQuery(&c.t)
		if title != c.title || artist != c.artist {
			t.Errorf("lyricsQuery(%q by %q) = %q by %q, want %q by %q", c.t.Name, c.t.ArtistNames(), title, artist, c.title, c.artist)
		}
	}
	if got := stripFeat("Imposible (feat. Ozuna)"); got != "Imposible" {
		t.Errorf("stripFeat = %q", got)
	}
}

// TestBestRecord picks the lyrics of the song as long as the track, timed
// ones first, and none of a song much longer or shorter.
func TestBestRecord(t *testing.T) {
	rs := []lrclibRecord{
		{TrackName: "remix", Duration: 260, SyncedLyrics: "[00:01.00] remix"},
		{TrackName: "plain", Duration: 229, PlainLyrics: "plain"},
		{TrackName: "timed", Duration: 231, SyncedLyrics: "[00:01.00] timed"},
		{TrackName: "empty", Duration: 229},
	}
	if r, ok := bestRecord(rs, 229, false); !ok || r.TrackName != "timed" {
		t.Errorf("for 229s: %q, %v; want timed", r.TrackName, ok)
	}
	if r, ok := bestRecord(rs, 245, false); ok {
		t.Errorf("for 245s: %q; want none", r.TrackName)
	}
	if r, ok := bestRecord(rs, 250, true); !ok || r.TrackName != "remix" {
		t.Errorf("a video of 250s: %q, %v; want remix", r.TrackName, ok)
	}
}

// TestSearchSongs reads a search's songs with their artists' and album's
// IDs, length and plays, and the token for the next page.
func TestSearchSongs(t *testing.T) {
	items, next := searchShelf(fixture(t, "search_songs.json"))
	ts := tracksOf(items)
	if len(ts) != 2 || next == "" {
		t.Fatalf("%d songs, next %q", len(ts), next)
	}
	s := ts[0]
	if s.ID != "FXovf5dsRTw" || s.Name != "Despacito" || s.VideoType != VideoATV || s.Duration != 229000 || s.Views != "10B plays" {
		t.Errorf("song: %+v", s)
	}
	if len(s.Artists) != 2 || s.Artists[0] != (Artist{"UCk91oFc2hY2CdHirU93baLg", "Luis Fonsi"}) || s.Artists[1].Name != "Daddy Yankee" {
		t.Errorf("artists: %+v", s.Artists)
	}
	if s.Album.ID != "MPREb_78js9pEDBUZ" || s.Album.Name != "Despacito" || s.Album.PicURL == "" {
		t.Errorf("album: %+v", s.Album)
	}
}

// TestAlbumPage reads an album with its songs, each of the album and under
// its cover, a song listed by its music video playing the song's own audio
// and showing the video.
func TestAlbumPage(t *testing.T) {
	al, _, err := parseAlbum(fixture(t, "album.json"), "MPREb_QqR6FQPQ76x")
	if err != nil {
		t.Fatal(err)
	}
	if al.Name != "VIDA" || al.Kind != "Album" || al.PublishTime != yearTime(2019) || al.Size != 15 || al.ArtistNames() != "Luis Fonsi" || al.Description == "" {
		t.Errorf("album: %q %q %d %d %q", al.Name, al.Kind, al.PublishTime, al.Size, al.ArtistNames())
	}
	if len(al.Songs) != 3 {
		t.Fatalf("%d songs", len(al.Songs))
	}
	sola := al.Songs[0]
	if sola.ID != "Jz2Ot00HHII" || sola.VideoID != "sKlA0A3ufpc" || sola.VideoType != VideoATV || sola.Video() != "sKlA0A3ufpc" {
		t.Errorf("a song listed by its video: %s, video %s, %s", sola.ID, sola.VideoID, sola.VideoType)
	}
	if sola.Album != al.Album || sola.ArtistNames() != "Luis Fonsi" || sola.Duration != 206000 {
		t.Errorf("song: %+v", sola)
	}
	if feat := al.Songs[1]; feat.ArtistNames() != "Luis Fonsi, Ozuna" {
		t.Errorf("a song with a guest: %q", feat.ArtistNames())
	}
	if remix := al.Songs[2]; remix.ID != "FdMG84qN_98" || remix.VideoID != "" {
		t.Errorf("a song without a video: %s, video %s", remix.ID, remix.VideoID)
	}
}

// TestPlaylistPage reads a playlist's header and tracks, a music video
// without a song of its own staying a video.
func TestPlaylistPage(t *testing.T) {
	p, token, err := parsePlaylist(fixture(t, "playlist.json"), "PL4fGSI1pDJn69On1f-8NAvX_CYlx7QyZc")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Top 100 Music Videos United States" || p.Creator.Nickname != "YouTube Charts" || p.TrackCount != 100 || p.CoverURL == "" || p.Description == "" {
		t.Errorf("playlist: %q by %q, %d, %q", p.Name, p.Creator.Nickname, p.TrackCount, p.CoverURL)
	}
	if p.Subtitle != "Chart • 2026 • 100 songs • 6 hours, 7 minutes" {
		t.Errorf("subtitle: %q", p.Subtitle)
	}
	if token != "" || len(p.Tracks) != 2 {
		t.Fatalf("%d tracks, token %q", len(p.Tracks), token)
	}
	v := p.Tracks[0]
	if v.ID != "-5ZXrO_lYSA" || v.Video() != v.ID || v.ArtistNames() != "LISA" || v.Duration != 217000 || v.Album.PicURL == "" {
		t.Errorf("video: %+v", v)
	}
}

// TestDiscography joins an artist's albums and singles the newest first,
// each by them and of its kind, and finds the pages of all of them.
func TestDiscography(t *testing.T) {
	self, parts := albumShelves(fixture(t, "artist.json"), "UCk91oFc2hY2CdHirU93baLg")
	if self.Name != "Luis Fonsi" || len(parts) != 2 || parts[0].more == nil || parts[1].more == nil {
		t.Fatalf("%+v, %d shelves", self, len(parts))
	}
	als := mergeAlbums(parts, self)
	var got []string
	for _, al := range als {
		got = append(got, al.Kind+" "+al.Name)
		if len(al.Artists) != 1 || al.Artists[0] != self || al.PicURL == "" {
			t.Errorf("%q: %+v", al.Name, al)
		}
	}
	want := []string{"Single Me Despido", "Single CAMBIARÉ", "Single Vagabundo", "Album El Viaje", "Album Ley De Gravedad", "Album VIDA"}
	if len(got) != len(want) {
		t.Fatalf("%q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %q, want %q", i, got[i], want[i])
		}
	}
}

// TestSuggestions reads suggested queries and songs, the word saying what
// a suggestion is not taken for its artist, nor a video's views.
func TestSuggestions(t *testing.T) {
	sug := parseSuggestions(fixture(t, "suggestions.json"))
	if len(sug.Queries) != 6 || sug.Queries[0] != "despacito" || len(sug.Songs) != 3 || len(sug.Order) != 1 || sug.Order[0] != "songs" {
		t.Fatalf("%+v", sug)
	}
	if s := sug.Songs[0]; s.ArtistNames() != "Luis Fonsi, Daddy Yankee" || s.Album.Name != "Despacito" || s.Views != "10B plays" {
		t.Errorf("song: %+v", s)
	}
	if v := sug.Songs[2]; len(v.Artists) != 0 || v.Views != "12M views" || v.VideoType != VideoUGC {
		t.Errorf("video: %+v", v)
	}
}

// TestRadio reads a radio, the song it started from first, a song paired
// with its music video showing the video, and the token for more.
func TestRadio(t *testing.T) {
	ts, next := parseRadio(fixture(t, "radio.json"), "FXovf5dsRTw", true)
	if len(ts) != 3 || next == "" {
		t.Fatalf("%d tracks, next %q", len(ts), next)
	}
	if ts[0].ID != "FXovf5dsRTw" || ts[0].Duration != 229000 || ts[0].Album.ID != "MPREb_78js9pEDBUZ" {
		t.Errorf("seed: %+v", ts[0])
	}
	if w := ts[2]; w.ID != "YDswuo2dIvY" || w.VideoID != "TyHvyGVs42U" || w.ArtistNames() != "Luis Fonsi, Demi Lovato" {
		t.Errorf("paired: %+v", w)
	}
	if ts[1].VideoID != "" || ts[1].HasVideo() {
		t.Errorf("a song alone has a video: %+v", ts[1])
	}
	// The seed moves first when YouTube lists it later.
	again, _ := parseRadio(fixture(t, "radio.json"), "2djj_KnGwjk", true)
	if again[0].ID != "2djj_KnGwjk" || again[1].ID != "FXovf5dsRTw" {
		t.Errorf("seed not first: %s, %s", again[0].ID, again[1].ID)
	}
}

// A video kept in time with the song plays the tallest H.264 variant that
// fits, not a VP9 one, nor the lowest the manifest starts from.
func TestPickVariant(t *testing.T) {
	m := "#EXTM3U\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=313238,CODECS=\"avc1.4D4015,mp4a.40.5\",RESOLUTION=426x240,AUDIO=\"233\"\nhttps://m/240\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=2569938,CODECS=\"avc1.4D401F,mp4a.40.2\",RESOLUTION=1280x720,AUDIO=\"234\"\nhttps://m/720\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=4688833,CODECS=\"avc1.640028,mp4a.40.2\",RESOLUTION=1920x1080,AUDIO=\"234\"\nhttps://m/1080\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=5845523,CODECS=\"vp09.00.40.08,mp4a.40.2\",RESOLUTION=1920x1080,AUDIO=\"234\"\nhttps://m/vp9\n"
	for _, c := range []struct {
		h    int
		want string
	}{{1080, "https://m/1080"}, {900, "https://m/720"}, {100, "https://m/240"}, {2160, "https://m/1080"}} {
		if got := pickVariant(m, c.h); got != c.want {
			t.Errorf("at most %d: %s, want %s", c.h, got, c.want)
		}
	}
}

// TestUserChannel reads who a channel is of and its shelf of playlists,
// passing over its shelves of episodes, podcasts and videos.
func TestUserChannel(t *testing.T) {
	resp := fixture(t, "user.json")
	u, ok := channelUser(resp, "UCBR8-60-B28hp2BmDPdntcQ")
	if !ok || u.Nickname != "YouTube" || u.Subscribers != "46.6M subscribers" || u.AvatarURL == "" {
		t.Fatalf("%+v", u)
	}
	items, more := playlistShelf(resp)
	if len(items) != 2 || more == nil {
		t.Fatalf("%d playlists, more %v", len(items), more)
	}
	for _, it := range items {
		_, v := only(it)
		if pl, ok := twoRowPlaylist(v); !ok || pl.Name == "" {
			t.Errorf("%+v", pl)
		}
	}
}
