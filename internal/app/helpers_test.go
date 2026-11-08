package app

import (
	"errors"
	"github.com/egoist/mygo/ui"
	"ytb-music/internal/browsercookies"
	"ytb-music/internal/player"
	"ytb-music/internal/ytm"
)

func newTestApp() *app {
	a := &app{
		playlists: map[string]*ytm.Playlist{}, loadingPl: map[string]bool{}, plErr: map[string]string{},
		artists: map[string]*artistData{}, albums: map[string]*albumData{}, users: map[string]*userData{},
		podcasts: map[string]*podcastData{}, podcastTab: &podcastTab{}, searches: map[route]*searchData{},
		lyricAt: -1, queueAt: -1,
	}
	a.art = newArtCache(func(func()) {})
	a.pl = player.New(func(func()) {})
	a.pl.SetVolume(0.5)
	return a
}

// Tests never read the Chrome of the machine they run on.
func init() {
	browserProfiles = func() ([]browsercookies.Profile, error) { return nil, nil }
	browserIcon = func(string) *ui.Bitmap { return nil }
	videoURLFor = func(string, int, int) (string, error) { return "", errors.New("offline") }
}
