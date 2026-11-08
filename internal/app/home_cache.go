package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"ytb-music/internal/ytm"
)

// The home page shows at once as the app opens: what it showed last time,
// kept in home.json, while what is new loads behind it. Beside it, the
// visitorData YouTube knows the app by, which would otherwise cost a round
// trip before the first request.

type homeCache struct {
	At       time.Time      `json:"at"`
	SignedIn bool           `json:"signedIn"`
	Featured []ytm.Playlist `json:"featured"`
	Charts   []cachedChart  `json:"charts"`
	Daily    []ytm.Track    `json:"daily"`
	Visitor  string         `json:"visitor,omitempty"`
	// VisitorAt is when the visitorData was fetched.
	VisitorAt time.Time `json:"visitorAt"`
}

// cachedChart is a chart with its preview, which Playlist leaves out of
// its JSON.
type cachedChart struct {
	ytm.Playlist
	Preview []ytm.ChartLine `json:"preview"`
}

// homeCacheLife is how old what the home page showed may be to show again:
// older, the page waits for what is new, as on a first run.
const homeCacheLife = 7 * 24 * time.Hour

func homeCachePath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "home.json"), nil
}

// loadHomeCache shows what the home page showed last time, for whoever
// listens as it was, and has requests carry the visitorData kept.
func (a *app) loadHomeCache(signedIn bool) {
	path, err := homeCachePath()
	if err != nil {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var hc homeCache
	if json.Unmarshal(b, &hc) != nil {
		return
	}
	ytm.SetVisitorData(hc.Visitor, hc.VisitorAt)
	if time.Since(hc.At) > homeCacheLife || hc.SignedIn != signedIn {
		return
	}
	a.featured, a.daily = hc.Featured, hc.Daily
	a.charts = make([]ytm.Playlist, len(hc.Charts))
	for i, c := range hc.Charts {
		a.charts[i] = c.Playlist
		a.charts[i].Preview = c.Preview
	}
}

// saveHomeCache keeps what the home page shows, on a goroutine.
func (a *app) saveHomeCache() {
	hc := homeCache{At: a.homeAt, SignedIn: a.homeSignedIn, Featured: a.featured, Daily: a.daily}
	hc.Visitor, hc.VisitorAt = ytm.VisitorData()
	hc.Charts = make([]cachedChart, len(a.charts))
	for i, c := range a.charts {
		hc.Charts[i] = cachedChart{Playlist: c, Preview: c.Preview}
	}
	go func() {
		b, err := json.Marshal(hc)
		if err != nil {
			return
		}
		path, err := homeCachePath()
		if err != nil {
			return
		}
		if os.WriteFile(path+".part", b, 0o600) == nil {
			os.Rename(path+".part", path)
		}
	}()
}
