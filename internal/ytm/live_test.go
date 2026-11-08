package ytm

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestLive goes through the catalogue as the app does, against YouTube
// itself: a search, the song's album, artist, radio, stream and lyrics,
// the charts and the home page. It runs only when LIVE is set.
func TestLive(t *testing.T) {
	if os.Getenv("LIVE") == "" {
		t.Skip("set LIVE to call YouTube")
	}

	songs, err := Search("Despacito", 10)
	if err != nil || len(songs) == 0 {
		t.Fatalf("Search: %d songs, %v", len(songs), err)
	}
	song := songs[0]
	t.Logf("Search → %d songs, first %s %q by %s on %s %q, %v, %s, %s", len(songs), song.ID, song.Name, describeArtists(song.Artists), song.Album.ID, song.Album.Name, song.Length(), song.VideoType, song.Views)
	if song.ID == "" || len(song.Artists) == 0 || song.Artists[0].ID == "" || song.Album.ID == "" || song.Duration == 0 || song.Album.PicURL == "" {
		t.Errorf("Search: first song is missing what it should have: %+v", song)
	}

	t.Run("SearchPage", func(t *testing.T) {
		p2, err := SearchPage("Despacito", SearchSongs, 20, 20)
		if err != nil || len(p2.Songs) == 0 || p2.Total <= 20 {
			t.Errorf("songs page 2: %d, total %d, %v", len(p2.Songs), p2.Total, err)
		} else {
			t.Logf("songs page 2 → %d, total %d, first %q", len(p2.Songs), p2.Total, p2.Songs[0].Name)
		}
		for _, kind := range []SearchType{SearchVideos, SearchAlbums, SearchArtists, SearchPlaylists, SearchPodcasts, SearchUsers} {
			res, err := SearchPage("Despacito", kind, 20, 0)
			if err != nil {
				t.Errorf("kind %d: %v", kind, err)
				continue
			}
			first := ""
			switch {
			case len(res.Videos) > 0:
				v := res.Videos[0]
				first = fmt.Sprintf("%s %q by %s, %s, %v, video %s", v.ID, v.Name, v.ArtistNames(), v.Views, v.Length(), v.Video())
			case len(res.Albums) > 0:
				a := res.Albums[0]
				first = fmt.Sprintf("%s %q %s by %s, %d", a.ID, a.Name, a.Kind, a.ArtistNames(), a.PublishTime)
			case len(res.Artists) > 0:
				first = fmt.Sprintf("%s %q %s", res.Artists[0].ID, res.Artists[0].Name, res.Artists[0].Avatar)
			case len(res.Playlists) > 0:
				p := res.Playlists[0]
				first = fmt.Sprintf("%s %q by %q, %d songs, %.0f plays", p.ID, p.Name, p.Creator.Nickname, p.TrackCount, p.PlayCount)
			case len(res.Podcasts) > 0:
				first = fmt.Sprintf("%s %q by %q", res.Podcasts[0].ID, res.Podcasts[0].Name, res.Podcasts[0].DJ.Nickname)
			}
			t.Logf("kind %d → %d, total %d, first %s", kind, res.Len(), res.Total, first)
			if kind != SearchUsers && res.Len() == 0 {
				t.Errorf("kind %d found nothing", kind)
			}
		}
	})

	t.Run("SearchSuggest", func(t *testing.T) {
		sug, err := SearchSuggest("despa")
		if err != nil || len(sug.Queries) == 0 {
			t.Fatalf("%+v, %v", sug, err)
		}
		t.Logf("→ queries %q, %d songs, order %v", sug.Queries, len(sug.Songs), sug.Order)
	})

	t.Run("AlbumDetail", func(t *testing.T) {
		al, err := AlbumDetail(song.Album.ID)
		if err != nil || len(al.Songs) == 0 || al.Name == "" || al.PicURL == "" {
			t.Fatalf("%+v, %v", al, err)
		}
		t.Logf("→ %q %s by %s, %d, %d songs, first %s %q by %s", al.Name, al.Kind, al.ArtistNames(), al.PublishTime, al.Size, al.Songs[0].ID, al.Songs[0].Name, al.Songs[0].ArtistNames())
		if al.Songs[0].Album.PicURL != al.PicURL || al.Songs[0].Album.ID != al.ID || al.PublishTime == 0 {
			t.Errorf("album songs don't carry the album: %+v", al.Songs[0].Album)
		}
	})

	artistID := song.Artists[0].ID
	t.Run("ArtistDetail", func(t *testing.T) {
		a, err := ArtistDetail(artistID)
		if err != nil || a.Name == "" || len(a.Songs) == 0 || a.Avatar == "" {
			t.Fatalf("%+v, %v", a, err)
		}
		t.Logf("→ %q, %s subscribers, %d songs (all in %s), avatar %s, banner %s, %d chars about", a.Name, a.Subscribers, len(a.Songs), a.SongsID, a.Avatar, a.PicURL, len(a.BriefDesc))
	})

	t.Run("ArtistAlbums", func(t *testing.T) {
		als, more, err := ArtistAlbums(artistID, 5, 0)
		if err != nil || len(als) == 0 {
			t.Fatalf("%d, %v", len(als), err)
		}
		var names []string
		for _, al := range als {
			names = append(names, fmt.Sprintf("%s %q %d", al.Kind, al.Name, al.PublishTime))
		}
		t.Logf("→ %v, more %v", names, more)
		rest, _, err := ArtistAlbums(artistID, 100, 5)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("→ %d more after the first 5", len(rest))
		for i := 1; i < len(als); i++ {
			if als[i].PublishTime > als[i-1].PublishTime {
				t.Errorf("not newest first: %v", names)
			}
		}
	})

	t.Run("ArtistLatestSongs", func(t *testing.T) {
		ts, err := ArtistLatestSongs(artistID, 5)
		if err != nil || len(ts) == 0 {
			t.Fatalf("%d, %v", len(ts), err)
		}
		t.Logf("→ %d, first %q on %q", len(ts), ts[0].Name, ts[0].Album.Name)
	})

	var charts []Playlist
	t.Run("Charts", func(t *testing.T) {
		charts, err = Charts()
		if err != nil || len(charts) == 0 {
			t.Fatalf("%d, %v", len(charts), err)
		}
		for _, c := range charts {
			t.Logf("→ %s %q, %d songs, preview %v", c.ID, c.Name, c.TrackCount, c.Preview)
		}
		if len(charts[0].Preview) != 3 {
			t.Errorf("no preview of the first chart")
		}
	})

	t.Run("PlaylistDetail", func(t *testing.T) {
		if len(charts) == 0 {
			t.Skip("no charts")
		}
		p, err := PlaylistDetail(charts[0].ID)
		if err != nil || len(p.Tracks) == 0 {
			t.Fatalf("%v", err)
		}
		t.Logf("→ %q by %q, %d of %d tracks, %q, cover %s", p.Name, p.Creator.Nickname, len(p.Tracks), p.TrackCount, p.Subtitle, p.CoverURL)
		// A playlist longer than a page, to follow its continuations.
		long, err := PlaylistDetail("RDCLAK5uy_nBFCpRPNv8_ECZ8004kq7UqW6BPE6vmNU")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("→ %q, %d of %d tracks", long.Name, len(long.Tracks), long.TrackCount)
		if len(long.Tracks) <= 100 {
			t.Errorf("didn't follow continuations: %d tracks", len(long.Tracks))
		}
	})

	t.Run("Home", func(t *testing.T) {
		ps, err := FeaturedPlaylists(10)
		if err != nil || len(ps) == 0 {
			t.Fatalf("FeaturedPlaylists: %d, %v", len(ps), err)
		}
		t.Logf("FeaturedPlaylists → %d, first %s %q %q", len(ps), ps[0].ID, ps[0].Name, ps[0].Subtitle)
		rec, err := RecommendedPlaylists()
		if err != nil || len(rec) == 0 {
			t.Errorf("RecommendedPlaylists: %d, %v", len(rec), err)
		}
		t.Logf("RecommendedPlaylists → %d", len(rec))
		daily, err := DailySongs()
		if err != nil || len(daily) == 0 {
			t.Fatalf("DailySongs: %d, %v", len(daily), err)
		}
		t.Logf("DailySongs → %d, first %q by %s, %v", len(daily), daily[0].Name, daily[0].ArtistNames(), daily[0].Length())
	})

	t.Run("Radio", func(t *testing.T) {
		ts, next, err := Radio(song.ID, "")
		if err != nil || len(ts) < 2 {
			t.Fatalf("%d, %v", len(ts), err)
		}
		if ts[0].ID != song.ID {
			t.Errorf("radio starts with %s %q, not the song", ts[0].ID, ts[0].Name)
		}
		t.Logf("→ %d, then %q by %s; next %v", len(ts), ts[1].Name, ts[1].ArtistNames(), next != "")
		if next != "" {
			more, _, err := Radio(song.ID, next)
			if err != nil || len(more) == 0 {
				t.Errorf("more: %d, %v", len(more), err)
			}
			t.Logf("→ %d more, first %q", len(more), first(more))
		}
	})

	t.Run("SongDetails", func(t *testing.T) {
		ids := []string{songs[1].ID, song.ID, "kJQP7kiw5Fk"}
		ts, err := SongDetails(ids)
		if err != nil || len(ts) != len(ids) {
			t.Fatalf("%d, %v", len(ts), err)
		}
		for i, tr := range ts {
			if tr.ID != ids[i] {
				t.Errorf("%d: %s, want %s", i, tr.ID, ids[i])
			}
		}
		t.Logf("→ %q, %q, %q (%s, video %s)", ts[0].Name, ts[1].Name, ts[2].Name, ts[2].VideoType, ts[2].Video())
	})

	t.Run("SongURL", func(t *testing.T) {
		s, err := SongURL(song.ID, "standard")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("→ %s %s, %d bytes, gain %.2f dB", s.Type, s.Level, s.Size, s.Gain)
		resp, err := http.Get(s.URL + "&range=0-65535")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 || resp.ContentLength != 65536 {
			t.Errorf("first 64KB: %s, %d bytes", resp.Status, resp.ContentLength)
		}
		v, err := VideoURL("kJQP7kiw5Fk")
		if err != nil || !strings.HasPrefix(v, "https://") {
			t.Errorf("VideoURL: %q, %v", v, err)
		}
		if _, err := SongURL("aaaaaaaaaaa", "standard"); !errors.Is(err, ErrUnavailable) {
			t.Errorf("a missing video: %v, want ErrUnavailable", err)
		}
	})

	t.Run("Lyrics", func(t *testing.T) {
		lrc, _, _, err := Lyrics(&song)
		if err != nil || lrc == "" {
			t.Fatalf("%q, %v", lrc, err)
		}
		t.Logf("→ %d lines, timed %v: %q", strings.Count(lrc, "\n")+1, strings.HasPrefix(lrc, "["), firstLine(lrc))
	})

	t.Run("Fetch", func(t *testing.T) {
		b, err := Fetch(Sized(song.Album.PicURL, 300))
		if err != nil || len(b) < 1000 {
			t.Fatalf("%d bytes, %v", len(b), err)
		}
	})
}

func describeArtists(as []Artist) string {
	var s []string
	for _, a := range as {
		s = append(s, a.Name+"("+a.ID+")")
	}
	return strings.Join(s, ", ")
}

func first(ts []Track) string {
	if len(ts) == 0 {
		return ""
	}
	return ts[0].Name
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
