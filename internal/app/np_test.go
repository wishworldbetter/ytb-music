package app

import (
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"ytb-music/internal/ytm"
)

func TestNowPlayingClosesAsOneLayer(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	tr := ytm.Track{ID: "1", Name: "宝船", Artists: []ytm.Artist{{Name: "DOUDOU"}}, Duration: 200000}
	a.pl.Cue([]ytm.Track{tr})
	a.pl.SetLyrics(nil)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.SetDark(false)
	a.nowPlaying = true
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if !tt.HasText("待播清单") {
		t.Fatal("now playing did not open")
	}
	// Mid-way out, it still covers the page and the sidebar alike.
	a.nowPlaying = false
	tt.Frame()
	tt.Advance(60 * time.Millisecond)
	tt.Frame()
	img := tt.Image()
	sx, sy := img.Bounds().Dx(), img.Bounds().Dy()
	dark := func(x, y float32) bool {
		r, g, b, _ := img.At(int(x*float32(sx)/1200), int(y*float32(sy)/800)).RGBA()
		return (r+g+b)/3 < 0x9000
	}
	if !dark(900, 400) || !dark(100, 400) {
		t.Errorf("page %v, sidebar %v: the closing layer should cover both", dark(900, 400), dark(100, 400))
	}
	tt.Advance(2 * time.Second)
	tt.Frame()
	if tt.HasText("待播清单") {
		t.Error("now playing still shows after closing")
	}
}

func TestQueueStaysAsATrackIsPlayed(t *testing.T) {
	a := newTestApp()
	var tracks []ytm.Track
	for i := range 12 {
		tracks = append(tracks, ytm.Track{ID: fmt.Sprint(i + 1), Name: fmt.Sprintf("歌%02d", i), Duration: 200000})
	}
	a.pl.Cue(tracks)
	a.nowPlaying, a.npPane = true, paneQueue
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Frame()
	if err := tt.Click("歌05"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if got := a.pl.Current().Name; got != "歌05" {
		t.Fatalf("playing %q, want 歌05", got)
	}
	// The tracks before it are still listed, as they were.
	for _, s := range []string{"歌01", "歌04", "歌06", "歌09"} {
		if !tt.HasText(s) {
			t.Errorf("missing %q in %q", s, tt.Texts())
		}
	}
}

// The app chooses Now Playing's pane before it makes the player: it
// crashed on start when the choice asked the player what played.
func TestLyricsPaneBeforePlayer(t *testing.T) {
	a := &app{settings: settings{LyricsStage: true}}
	if pane := a.lyricsPaneChosen(); pane != paneStage {
		t.Errorf("pane %d", pane)
	}
}

// 上一首 plays the song before, however far this one has played: a few
// seconds in, it went back to this one's start, and took a second click.
func TestPrevGoesBackASong(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]ytm.Track{{ID: "1", Name: "一", Duration: 200000}, {ID: "2", Name: "二", Duration: 200000}})
	a.pl.SetLyrics(nil)
	a.pl.Hold(30 * time.Second)
	a.nowPlaying = true
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if err := tt.Click("上一首"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if id := a.pl.Current().ID; id != "2" {
		t.Errorf("playing %s, want the one before, 2", id)
	}
}

// Now Playing holds still as the quality's badge goes, while the next song
// loads, and comes back: the row of times grew with it, moving the column.
func TestNowPlayingHoldsStillWithoutBadge(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]ytm.Track{{ID: "1", Name: "一", Duration: 200000}})
	a.pl.SetLyrics(nil)
	a.nowPlaying = true
	tt := ui.NewTester(a.view, 1200, 800)
	var at []float32
	for _, st := range []*ytm.Stream{{Level: "lossless"}, nil} {
		a.pl.SetStream(st)
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		r, ok := tt.Find("专辑封面")
		if !ok {
			t.Fatal("no cover")
		}
		at = append(at, r.Y)
	}
	if at[0] != at[1] {
		t.Errorf("the cover moved from %v to %v as the badge went", at[0], at[1])
	}
}

// ⌘ and a number choose Now Playing's tab in that place, for a song and
// for an episode, whose tabs are others: its last is the comments, wherever
// the captions it may have put it.
func TestCmdNumberChoosesTab(t *testing.T) {
	for _, c := range []struct {
		track ytm.Track
		keys  []int // from 1, 0 for the last tab
		panes []int
	}{
		{ytm.Track{ID: "1", Name: "宝船", Duration: 200000}, []int{4, 2, 1}, []int{paneWall, paneStage, paneLyrics}},
		{ytm.Track{ID: "2", Name: "节目", ProgramID: "9", Duration: 200000}, []int{0, 1}, []int{paneQueue, paneProgram}},
	} {
		a := newTestApp()
		a.settings = defaultSettings()
		a.pl.Cue([]ytm.Track{c.track})
		a.pl.SetLyrics(nil)
		a.nowPlaying = true
		tt := ui.NewTester(a.view, 1200, 800)
		tt.Frame()
		for i, n := range c.keys {
			if n == 0 {
				n = len(a.npTabs())
			}
			tt.Key(ui.Cmd, ui.Key1+ui.Key(n-1))
			tt.Frame()
			if a.npPane != c.panes[i] {
				t.Errorf("%s: ⌘%d chose pane %d, want %d", c.track.Name, n, a.npPane, c.panes[i])
			}
		}
	}
}

// Now Playing's tabs stay where they are going from one pane to another,
// and as the comments' count changes: the lyrics, the queue and the
// comments had them over the pane, and the stage and the wall at the top
// on the right, where a longer count pushed the others along.
func TestNowPlayingTabsStayPut(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]ytm.Track{{ID: "1", Name: "一", Duration: 200000}})
	a.pl.SetLyrics(nil)
	a.nowPlaying = true
	tt := ui.NewTester(a.view, 1200, 800)
	settle := func() {
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
	}
	settle()
	first, ok := tt.Find("歌词")
	if !ok {
		t.Fatal("no 歌词 tab")
	}
	for _, tab := range []string{"律动", "待播清单", "选歌", "歌词"} {
		tt.Click(tab)
		settle()
		if a.npPane == paneLyrics && tab != "歌词" {
			t.Fatalf("%s was not chosen", tab)
		}
		if r, _ := tt.Find("歌词"); r.X != first.X || r.Y != first.Y {
			t.Errorf("on %s the 歌词 tab is at %v,%v, was at %v,%v", tab, r.X, r.Y, first.X, first.Y)
		}
	}
	for _, pane := range []int{paneStage, paneLyrics} {
		a.npPane = pane
		a.comments.loaded, a.comments.total = true, 0
		settle()
		a.comments.total = 12345678
		settle()
		if r, _ := tt.Find("歌词"); r.X != first.X || r.Y != first.Y {
			t.Errorf("in pane %d, with the count, the 歌词 tab is at %v,%v, was at %v,%v", pane, r.X, r.Y, first.X, first.Y)
		}
	}
}

// npSettleFrames builds Now Playing, lets what moves come to rest and builds it
// again.
func npSettleFrames(tt *ui.Tester) {
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
}

// 律动 sets the lyrics on the stage in the pane beside the cover, which
// stays where it is: the stage took the whole view, the cover gone.
func TestStageKeepsCover(t *testing.T) {
	a, tt, _ := playingApp()
	a.nowPlaying = true
	npSettleFrames(tt)
	before, ok := tt.Find("专辑封面")
	if !ok {
		t.Fatal("no cover")
	}
	if err := tt.Click("律动"); err != nil {
		t.Fatal(err)
	}
	npSettleFrames(tt)
	if a.npPane != paneStage {
		t.Fatalf("pane %d, want the stage", a.npPane)
	}
	if after, ok := tt.Find("专辑封面"); !ok || math.Abs(float64(after.X-before.X)) > 0.5 || math.Abs(float64(after.Y-before.Y)) > 0.5 || after.W != before.W {
		t.Errorf("on the stage the cover is at %v (%v), was at %v", after, ok, before)
	}
}

// The switch over the cover shows a music video in the cover's place,
// beside the lyrics, and the cover again; the video's button gives it the
// whole view, which Esc gives back. The video was a tab of its own, which
// took the whole view, lyrics and all.
func TestVideoInCoversPlace(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]ytm.Track{{ID: "v1", Name: "MV", VideoType: ytm.VideoOMV, Duration: 200000}})
	a.pl.SetLyrics(nil)
	a.nowPlaying = true
	tt := ui.NewTester(a.view, 1200, 800)
	npSettleFrames(tt)
	cover, ok := tt.Find("专辑封面")
	if !ok {
		t.Fatal("no cover")
	}
	if err := tt.Click("视频"); err != nil {
		t.Fatal(err)
	}
	npSettleFrames(tt)
	card, ok := tt.Find("视频画面")
	if !ok {
		t.Fatal("no video after 视频")
	}
	if card.X != cover.X || card.W <= cover.W || math.Abs(float64(card.W/card.H-16.0/9)) > 0.01 {
		t.Errorf("the video is at %v, not from where the cover was, %v, wider, at 16:9", card, cover)
	}
	if a.npPane != paneLyrics {
		t.Errorf("the video changed the pane to %d", a.npPane)
	}
	if err := tt.Click("影院模式"); err != nil {
		t.Fatal(err)
	}
	npSettleFrames(tt)
	if !a.npTheater {
		t.Fatal("影院模式 did not give the video the whole view")
	}
	tt.Key(0, ui.KeyEscape)
	npSettleFrames(tt)
	if a.npTheater || !a.nowPlaying {
		t.Errorf("after Esc: theater %v, Now Playing open %v; want the video back beside the pane", a.npTheater, a.nowPlaying)
	}
	if err := tt.Click("歌曲"); err != nil {
		t.Fatal(err)
	}
	npSettleFrames(tt)
	if _, ok := tt.Find("专辑封面"); !ok || a.npVideo {
		t.Error("歌曲 did not show the cover again")
	}
}

// With NM_FRAMES set, saves Now Playing on a music video: the lyrics and
// the stage beside its cover, the video in the cover's place, and the
// theater.
func TestNowPlayingFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	a, tt, _ := playingApp()
	q := a.pl.Queue()
	q[0].VideoType = ytm.VideoOMV
	a.pl.Cue(q)
	a.pl.Hold(2 * time.Second)
	a.nowPlaying = true
	for _, s := range []struct {
		name string
		set  func()
	}{
		{"np-lyrics.png", func() { a.choosePane(paneLyrics) }},
		{"np-stage.png", func() { a.choosePane(paneStage) }},
		{"np-video.png", func() { a.choosePane(paneLyrics); a.chooseVideo(true) }},
		{"np-theater.png", func() { a.npTheater = true }},
	} {
		s.set()
		npSettleFrames(tt)
		tt.Move(600, 400)
		npSettleFrames(tt)
		f, _ := os.Create(filepath.Join(dir, s.name))
		png.Encode(f, tt.Image())
		f.Close()
	}
}

// A click on a song in a list plays it and leaves the list showing, the
// song lit in it: it opened Now Playing over the list, which got in the
// way of picking the next.
func TestPlayingStaysInList(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	pl := &ytm.Playlist{ID: "p1", Name: "歌单", Tracks: []ytm.Track{{ID: "1", Name: "一首歌", Duration: 200000}, {ID: "2", Name: "二首歌", Duration: 200000}}}
	a.playlists["p1"] = pl
	a.open(route{page: pagePlaylist, id: "p1"})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("二首歌"); err != nil {
		t.Fatal(err)
	}
	if cur := a.pl.Current(); cur == nil || cur.ID != "2" || a.nowPlaying {
		t.Fatalf("playing %v, Now Playing open %v", cur, a.nowPlaying)
	}
}
