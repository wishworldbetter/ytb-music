package app

import (
	"fmt"
	"os"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"ytb-music/internal/ytm"
)

// The music video Now Playing shows for a song: a video view of MyGo's,
// the system's player (AVFoundation), playing the video's pictures alone,
// kept in time with the song the app plays. The system decodes the video
// with the hardware's decoders and draws it straight to the screen; the
// app's own player stays what plays, so the queue, the media keys, the
// notch and the visualizers go on as with the cover. The video shows in
// the cover's place, or over the whole view in the theater; the view is
// made as it first shows and goes once nothing shows it.

type videoState struct {
	view *mygo.VideoView
	// unsupported says the system has no video views for the app: MyGo
	// plays videos on macOS alone, for now.
	unsupported bool
	id          string // the video shown or loading
	url         string // its stream, once known
	err         string
	// loaded is the stream the view plays, and ready says it has a frame
	// to show, aspect wide for each unit high; checked is when it was last
	// kept in time, and seeked when it last moved there, which it is given
	// a moment to land on. asked is when the video was asked for, for the
	// log of how long it took.
	loaded  string
	ready   bool
	aspect  float32
	checked time.Time
	seeked  time.Time
	asked   time.Time
	// The video is cued at target, paused, while holding: there it waits
	// for the song to come, having been moved there lead ahead of the song,
	// as long as moving there took last, at cued.
	target   time.Duration
	holding  bool
	lead     time.Duration
	cued     time.Time
	urls     map[string]videoURL // streams found, by video ID
	fetching map[string]bool     // streams being found
	stage    ui.Rect             // where the theater was laid out last
}

// videoURL is a video's stream and when it was found: YouTube's streams
// expire after six hours.
type videoURL struct {
	url string
	at  time.Time
}

const videoURLLife = 5 * time.Hour

// debugVideo logs how long the video takes to show, and how it keeps in
// time, with YTM_DEBUG_VIDEO.
var debugVideo = os.Getenv("YTM_DEBUG_VIDEO") != ""

func videoLog(what string, since time.Time) {
	if debugVideo {
		fmt.Fprintf(os.Stderr, "video: %s after %.2fs\n", what, time.Since(since).Seconds())
	}
}

// videoURLFor finds the stream of a video's pictures, at most 1080p:
// tests replace it.
var videoURLFor = func(id string) (string, error) { return ytm.VideoStream(id, 1080) }

// How the video keeps in time with the song: looked at every videoCheck,
// a little faster or slower while it is off by more than videoNudge, moved
// to where the song is once it is off by more than videoJump, and left
// videoLand to land after a move, as the stream buffers there.
// How far ahead of the song the video is cued: a stream loading for the
// first time, a stream moving within what it loaded, and the margin over
// what loading took last.
const (
	videoLoadLead   = 4300 * time.Millisecond
	videoSeekLead   = 2500 * time.Millisecond
	videoLeadMargin = 800 * time.Millisecond
)

const (
	videoPoll  = 100 * time.Millisecond
	videoCheck = 500 * time.Millisecond
	videoNudge = 60 * time.Millisecond
	videoJump  = time.Second
	videoLand  = 2 * time.Second
)

// videoShown says Now Playing shows the video of tr, in place of its
// cover: tr is a video, and the listener chose to see videos. A song
// paired with a video shows its cover until the switch swaps them, as the
// video's sound is not the song's.
func (a *app) videoShown(tr *ytm.Track) bool {
	return a.npVideo && tr.HasVideo() && tr.Video() == tr.ID
}

// chooseVideo shows the video of the song playing in its cover's place,
// or the cover again. For a song paired with a video, the video plays in
// its place, and back.
func (a *app) chooseVideo(on bool) {
	tr := a.pl.Current()
	if tr == nil {
		return
	}
	a.npVideo = on
	if on {
		a.swapToVideo(tr)
		return
	}
	a.npTheater = false
	if song, ok := a.songOf[tr.ID]; ok {
		delete(a.songOf, tr.ID)
		a.pl.Swap(song)
		a.toast = "已切换到歌曲"
	}
}

// swapToVideo plays, for a song YouTube Music pairs with a music video,
// the video's sound in the song's place, from where the song is, as
// YouTube Music's switch between song and video does: the video shown is
// then the sound heard.
func (a *app) swapToVideo(tr *ytm.Track) {
	if tr.VideoID == "" || tr.VideoID == tr.ID {
		return
	}
	mv := *tr
	mv.ID, mv.VideoID, mv.VideoType = tr.VideoID, "", ytm.VideoOMV
	if a.songOf == nil {
		a.songOf = map[string]ytm.Track{}
	}
	a.songOf[mv.ID] = *tr
	a.pl.Swap(mv)
	a.toast = "已切换到 MV"
}

// playVideoOf opens Now Playing on the video of tr: playing tr first, unless
// it plays already.
func (a *app) playVideoOf(tr *ytm.Track) {
	if cur := a.pl.Current(); cur == nil || cur.ID != tr.ID {
		a.pl.PlayQueue([]ytm.Track{*tr}, 0)
	}
	a.nowPlaying = true
	a.chooseVideo(true)
}

// videoOnScreen says the video view shows, or may in a moment: Now
// Playing is open on a video.
func (a *app) videoOnScreen() bool {
	tr := a.pl.Current()
	return a.nowPlaying && tr != nil && a.videoShown(tr)
}

// ensureVideo makes the video view, once, and finds the stream of video id.
func (a *app) ensureVideo(id string) {
	v := &a.video
	if v.unsupported {
		v.err = "这台电脑暂不支持播放视频"
		return
	}
	if v.view == nil && a.win != nil {
		view, err := a.win.NewVideoView()
		if err != nil {
			v.unsupported = true
			v.err = "这台电脑暂不支持播放视频"
			return
		}
		// The song plays the sound, and the video starts as the song comes
		// to where it waits, with what it has: it buffered while it waited.
		view.SetMuted(true)
		view.SetWaitToBuffer(false)
		v.view = view
	}
	if v.id == id {
		return
	}
	v.id, v.url, v.loaded, v.err, v.ready, v.aspect = id, "", "", "", false, 0
	v.asked = time.Now()
	if u, ok := v.urls[id]; ok && time.Since(u.at) < videoURLLife {
		v.url = u.url
		return
	}
	a.fetchVideo(id)
}

// fetchVideo finds the stream of video id, unless it is being found, for
// the video shown, or ahead of the switch to it, for the video to come the
// sooner once chosen.
func (a *app) fetchVideo(id string) {
	v := &a.video
	if v.fetching[id] {
		return
	}
	if v.fetching == nil {
		v.fetching = map[string]bool{}
	}
	v.fetching[id] = true
	go func() {
		url, err := videoURLFor(id)
		a.post(func() {
			delete(v.fetching, id)
			if err == nil && url != "" {
				if v.urls == nil {
					v.urls = map[string]videoURL{}
				}
				v.urls[id] = videoURL{url, time.Now()}
			}
			if v.id != id || v.url != "" {
				return
			}
			videoLog("stream found", v.asked)
			if err != nil || url == "" {
				v.err = "视频暂时无法播放"
				return
			}
			v.url = url
		})
	}()
}

// prefetchVideo finds the stream of video id ahead of the switch to it.
func (a *app) prefetchVideo(id string) {
	if u, ok := a.video.urls[id]; ok && time.Since(u.at) < videoURLLife {
		return
	}
	a.fetchVideo(id)
}

// closeVideo lets the video view go, with what it played.
func (a *app) closeVideo() {
	v := &a.video
	if v.view == nil {
		return
	}
	v.view.Destroy()
	v.view, v.id, v.url, v.loaded, v.ready = nil, "", "", "", false
}

// syncVideo keeps the video where the song is. A stream takes seconds to
// load, and to move in, so the video is cued where the song will be once
// it has: paused there until the song comes, then played. It plays and
// pauses with the song, and catches up or waits by playing a little
// faster or slower, cued again when far off.
func (a *app) syncVideo(c *ui.Context) {
	v := &a.video
	if v.view == nil || v.url == "" {
		return
	}
	now := time.Now()
	pos := a.pl.Position()
	playing := a.pl.Playing() && !a.pl.Loading()
	if playing {
		c.After(videoCheck)
	}
	if v.loaded != v.url {
		v.loaded, v.ready = v.url, false
		v.view.Load(v.url)
		if v.lead == 0 {
			v.lead = videoLoadLead
		}
		v.cue(pos+v.lead, now)
		c.After(videoPoll)
		return
	}
	if !v.ready {
		// Looked at often until its first frame, playing or not, for the
		// cover to give way to it at once.
		c.After(videoPoll)
		st := v.view.State()
		if st.Width > 0 && st.Height > 0 {
			v.aspect = float32(st.Width) / float32(st.Height)
		}
		if !st.HasFrame || st.Width == 0 {
			if st.Err != "" {
				v.err = "视频暂时无法播放"
			}
			return
		}
		v.ready = true
		// The next stream is cued as far ahead as this one took, and a
		// little more.
		v.lead = min(max(now.Sub(v.cued)+videoLeadMargin, videoSeekLead), 8*time.Second)
		videoLog(fmt.Sprintf("first frame, %.2fs ahead of the song", (st.Position-pos).Seconds()), v.asked)
	}
	if v.holding {
		if !playing {
			return
		}
		switch wait := v.target - pos; {
		case wait > 0 && wait < 10*time.Second:
			c.After(min(wait, videoCheck))
		case wait > -videoJump:
			// The song came: the video plays on with it.
			v.holding = false
			v.view.SetRate(1)
			v.view.Play()
			v.seeked, v.checked = now, now
			videoLog("playing with the song", v.asked)
		default:
			// The song moved away while the video waited.
			v.cue(pos+videoSeekLead, now)
		}
		return
	}
	if now.Sub(v.checked) < videoCheck {
		return
	}
	v.checked = now
	st := v.view.State()
	if debugVideo {
		fmt.Fprintf(os.Stderr, "video: song %.2fs video %.2fs rate=%.2f waiting=%v\n", pos.Seconds(), st.Position.Seconds(), st.Rate, st.Waiting)
	}
	if st.Err != "" {
		v.err = "视频暂时无法播放"
		return
	}
	off := st.Position - pos
	going := st.Rate > 0 || st.Waiting
	switch {
	case playing && !going:
		// The song plays again: the video with it, from where it paused
		// if that is near, else cued again.
		if off > -videoJump && off < videoJump {
			v.view.SetRate(1)
			v.view.Play()
			v.seeked = now
		} else {
			v.cue(pos+videoSeekLead, now)
		}
		return
	case !playing && going:
		v.view.Pause()
		return
	}
	if !st.Ready || st.Waiting || now.Sub(v.seeked) < videoLand {
		return
	}
	switch {
	case off > videoJump || off < -videoJump:
		v.cue(pos+videoSeekLead, now)
	case off > videoNudge:
		v.view.SetRate(0.92)
	case off < -videoNudge:
		v.view.SetRate(1.08)
	default:
		v.view.SetRate(1)
	}
}

// cue moves the video to at, paused, to wait there for the song.
func (v *videoState) cue(at time.Duration, now time.Time) {
	v.view.Pause()
	v.view.Seek(at)
	v.target, v.holding, v.cued = at, true, now
}

// videoSize is how large the video shows in a box w by h: as large as
// fits, as wide for its height as it is.
func (v *videoState) videoSize(w, h float32) (float32, float32) {
	aspect := v.aspect
	if aspect <= 0 {
		aspect = 16.0 / 9
	}
	if w/aspect <= h {
		return w, w / aspect
	}
	return h * aspect, h
}

// videoAspect is how wide the video is for each unit high: 16:9 until
// known.
func (a *app) videoAspect() float32 {
	if a.video.aspect > 0 {
		return a.video.aspect
	}
	return 16.0 / 9
}

// videoCard shows the video in the cover's place, filling it, coming in as
// the cover goes. Until its first frame, the cover shows in it, dimmed.
// The button in its corner gives it the whole view.
func (a *app) videoCard(c *ui.Context, art *artwork) {
	card := ui.Box(c.Key("video-card")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(14).
		Background(ui.RGB(0, 0, 0)).Shadow(0, 30, 60, -12, ui.RGBA(0, 0, 0, 0.55)).Label("视频画面")
	card.Transition(ui.ElementTransition{Animation: &coverSlide, Enter: &ui.Motion{Scale: 0.9}, Exit: &ui.Motion{Scale: 0.9}})
	over := card.Hovered()
	card.Children(func() {
		a.videoContent(c, art, 14)
		reveal(ui.Box(c.Key("theater-reveal")).Absolute().Right(10).Bottom(10), over).Children(func() {
			b := ui.ButtonBase(c.Key("theater")).Size(30, 30).Radius(999).Center().FocusRing(false).
				Label("影院模式").Tooltip("影院模式")
			bg := ui.RGBA(0, 0, 0, 0.45)
			if b.Hovered() {
				bg = ui.RGBA(0, 0, 0, 0.7)
			}
			b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
			b.Children(func() { ui.Icon(c, iconExpand).Size(14, 14).TextColor(white) })
			if b.Clicked() {
				a.npTheater = true
			}
		})
	})
}

// videoContent fills a box with the video: its view once its pictures
// come, and the cover, dimmed, under what it waits for or why it cannot
// play until then.
func (a *app) videoContent(c *ui.Context, art *artwork, radius float32) {
	v := &a.video
	// Not while Now Playing comes up or goes down: the view, under the
	// window's content, stays where the frame before put it as the sheet
	// moves in frames that only paint.
	if v.view != nil && v.ready && v.err == "" && a.npAtRest {
		ui.VideoView(c, v.view).Fill().Radius(radius)
		return
	}
	artImage(c, bitmap(art), radius, 1)
	msg := v.err
	if msg == "" {
		msg = "正在载入视频…"
	}
	ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Radius(radius).Background(ui.RGBA(0, 0, 0, 0.55)).Children(func() {
		ui.Text(c, msg).FontSize(13).FontWeight(600).TextColor(white.Alpha(0.7))
	})
}

// theater gives the video the whole of Now Playing, as large as fits, the
// slim bar of controls under it.
func (a *app) theater(c *ui.Context, tr *ytm.Track, art *artwork, accent ui.Color, w float32, pointerIn bool) {
	padX := npSideX(w)
	reveal(ui.Row(c.Key("theater-bar")).Absolute().Top(topBar(c)).Left(padX).Height(npTabsHeight).Gap(8).AlignItems(ui.Center), pointerIn).Children(func() {
		if pillButton(c.Key("collapse"), iconShrink, "收起", white.Alpha(0.12), white).Clicked() {
			a.npTheater = false
		}
		full := a.win != nil && a.win.IsFullScreen()
		label := "全屏"
		if full {
			label = "退出全屏"
		}
		if pillButton(c.Key("fullscreen"), iconScreen, label, white.Alpha(0.12), white).Clicked() && a.win != nil {
			a.win.SetFullScreen(!full)
		}
	})
	stage := ui.Box(c.Key("theater")).Grow(1).Margin(npTabsHeight+16, padX, 8, padX)
	// The video fits where the stage was laid out last: a frame more once
	// that changed, as the window resized, for it to fit there.
	b := stage.LayoutBounds()
	if b != a.video.stage {
		a.video.stage = b
		c.AnimationFrame()
	}
	vw, vh := a.video.videoSize(b.W, b.H)
	stage.Children(func() {
		frame := ui.Box(c.Key("video")).Absolute().Top((b.H-vh)/2).Left((b.W-vw)/2).Size(vw, vh).Radius(14).
			Background(ui.RGB(0, 0, 0)).Shadow(0, 18, 48, -12, ui.RGBA(0, 0, 0, 0.47))
		frame.Children(func() { a.videoContent(c, art, 14) })
	})
	a.stageBar(c, tr, art, accent)
}

// mediaSwitch switches, over the cover, between a song's cover and its
// video, as YouTube Music's switch between song and video does.
func (a *app) mediaSwitch(c *ui.Context, tr *ytm.Track) {
	on := a.videoShown(tr)
	ui.Row(c.Key("media-seg")).Padding(2).Gap(2).Radius(999).Background(white.Alpha(0.08)).Children(func() {
		for _, opt := range []struct {
			label string
			video bool
		}{{"歌曲", false}, {"视频", true}} {
			b := ui.ButtonBase(c.Key(opt.label)).Padding(4, 14).Radius(999).FocusRing(false).Label(opt.label)
			bg, fg := ui.Transparent, white.Alpha(0.55)
			switch {
			case opt.video == on:
				bg, fg = white.Alpha(0.16), white
			case b.Hovered():
				fg = white.Alpha(0.85)
			}
			b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
			b.Children(func() { ui.Text(c, opt.label).FontSize(12.5).FontWeight(600).TextColor(fg) })
			if b.Clicked() && opt.video != on {
				a.chooseVideo(opt.video)
			}
		}
	})
}
