package ytm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLivePageLoad measures, against YouTube, how long each page takes to
// load as the app loads it, and prints when each request ran, to see
// those that wait for one another: LIVE=1 go test -run TestLivePageLoad -v.
// It checks nothing; it is for looking at.

type reqLog struct {
	mu   sync.Mutex
	t0   time.Time
	rows []reqRow
}

type reqRow struct {
	start, end time.Duration
	what       string
}

type logTransport struct {
	base http.RoundTripper
	log  *reqLog
}

func (lt logTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	what := r.URL.Host + r.URL.Path
	if r.Body != nil && r.Method == "POST" {
		b, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(b))
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			for _, k := range []string{"browseId", "videoId", "query", "input", "continuation"} {
				if v, ok := m[k].(string); ok {
					if len(v) > 24 {
						v = v[:24] + "…"
					}
					what += " " + k + "=" + v
				}
			}
		}
		if c := r.URL.Query().Get("continuation"); c != "" {
			what += " cont"
		}
	} else if r.URL.RawQuery != "" && strings.Contains(r.URL.Host, "lrclib") {
		what += "?" + r.URL.RawQuery
	}
	lt.log.mu.Lock()
	start := time.Since(lt.log.t0)
	lt.log.mu.Unlock()
	resp, err := lt.base.RoundTrip(r)
	if resp != nil && resp.Body != nil {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(b))
	}
	lt.log.mu.Lock()
	lt.log.rows = append(lt.log.rows, reqRow{start, time.Since(lt.log.t0), what})
	lt.log.mu.Unlock()
	return resp, err
}

func (l *reqLog) reset() {
	l.mu.Lock()
	l.t0, l.rows = time.Now(), nil
	l.mu.Unlock()
}

func (l *reqLog) print(name string, total time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	sort.Slice(l.rows, func(i, j int) bool { return l.rows[i].start < l.rows[j].start })
	fmt.Printf("\n## %s: %.2fs, %d requests\n", name, total.Seconds(), len(l.rows))
	for _, r := range l.rows {
		bar := strings.Repeat(" ", int(r.start/(100*time.Millisecond))) + strings.Repeat("█", max(1, int((r.end-r.start)/(100*time.Millisecond))))
		if len(bar) > 70 {
			bar = bar[:70]
		}
		fmt.Printf("  %5.2f-%5.2f %-70s %s\n", r.start.Seconds(), r.end.Seconds(), bar, r.what)
	}
}

func TestLivePageLoad(t *testing.T) {
	if os.Getenv("LIVE") == "" {
		t.Skip()
	}
	log := &reqLog{}
	httpClient.Transport = logTransport{http.DefaultTransport, log}
	defer func() { httpClient.Transport = nil }()
	// The app keeps its visitorData; so does this.
	visitorData()
	page := func(name string, fn func() error) {
		clearCaches()
		playerCache.clear()
		log.reset()
		t0 := time.Now()
		err := fn()
		log.print(name, time.Since(t0))
		if err != nil {
			fmt.Println("  error:", err)
		}
	}
	par := func(fns ...func() error) error {
		var wg sync.WaitGroup
		errs := make([]error, len(fns))
		t0 := time.Now()
		for i, fn := range fns {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs[i] = fn()
				fmt.Printf("  part %d ready at %.2fs\n", i, time.Since(t0).Seconds())
			}()
		}
		wg.Wait()
		for _, e := range errs {
			if e != nil {
				return e
			}
		}
		return nil
	}

	page("home (featured+charts+quick picks)", func() error {
		return par(
			func() error { _, err := FeaturedPlaylistsFirst(30); return err },
			func() error { _, err := FeaturedPlaylists(30); return err },
			func() error { _, err := Charts(); return err },
			func() error { _, err := DailySongs(); return err },
		)
	})
	var artistID, albumID, plID, longPl string
	for _, k := range []SearchType{SearchSongs, SearchVideos, SearchArtists, SearchAlbums, SearchPlaylists} {
		page(fmt.Sprintf("search despacito kind %d", k), func() error {
			res, err := SearchPage("despacito", k, 20, 0)
			if k == SearchArtists && len(res.Artists) > 0 {
				artistID = res.Artists[0].ID
			}
			if k == SearchAlbums && len(res.Albums) > 0 {
				albumID = res.Albums[0].ID
			}
			return err
		})
	}
	page("search suggest", func() error { _, err := SearchSuggest("despa"); return err })
	page("artist page (detail + latest songs)", func() error {
		return par(
			func() error { _, err := ArtistDetail(artistID); return err },
			func() error { _, err := ArtistLatestSongs(artistID, 100); return err },
		)
	})
	page("artist albums tab", func() error { _, _, err := ArtistAlbums(artistID, 30, 0); return err })
	page("album page", func() error { _, err := AlbumDetail(albumID); return err })
	pls, _ := FeaturedPlaylists(30)
	if len(pls) > 0 {
		plID = pls[0].ID
	}
	page("featured playlist first page", func() error { p, _, err := PlaylistStart(plID); fmt.Println("  tracks", len(p.Tracks)); return err })
	res, _ := SearchPage("top 1000 songs of all time", SearchPlaylists, 20, 0)
	best := 0
	for _, p := range res.Playlists {
		if p.TrackCount > best {
			best, longPl = p.TrackCount, p.ID
		}
	}
	page(fmt.Sprintf("long playlist first page (%d songs)", best), func() error { p, _, err := PlaylistStart(longPl); fmt.Println("  tracks", len(p.Tracks)); return err })
	charts, _ := Charts()
	if len(charts) > 0 {
		page("chart playlist page", func() error { _, err := PlaylistDetail(charts[0].ID); return err })
	}
	tr := Track{ID: "kJQP7kiw5Fk", Name: "Luis Fonsi - Despacito ft. Daddy Yankee", Artists: []Artist{{Name: "Luis Fonsi"}}, Duration: 282000, VideoType: VideoOMV}
	page("song start (stream + lyrics)", func() error {
		return par(
			func() error { _, err := playerOf(tr.ID); return err },
			func() error { _, _, _, err := Lyrics(&tr); return err },
		)
	})
	page("song radio (next)", func() error { _, _, err := Radio(tr.ID, ""); return err })
}
