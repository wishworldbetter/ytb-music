package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"ytb-music/internal/browsercookies"
	"ytb-music/internal/player"
	"ytb-music/internal/ytm"
)

type pageKind int

const (
	pageHome pageKind = iota
	pageDaily
	pageCharts
	pagePlaylist
	pageSearch
	pageFM
	pageArtist
	pageAlbum
	pageUser
	pagePodcast
	pagePodcasts
)

type route struct {
	page pageKind
	// id is the playlist, artist, album, user or podcast the page shows.
	id string
	// query and kind are what a search page searched for.
	query string
	kind  ytm.SearchType
}

// app is the state both windows show. Everything in it is read and changed
// on the main thread: by the views, and by goroutines through post.
type app struct {
	win   *mygo.Window
	notch *notch
	tray  *mygo.Tray // the icon in the menu bar or notification area, while it shows
	art   *artCache
	pl    *player.Player
	pal   palette

	route   route
	history []route
	forward []route // the pages gone back from, the last first to go to

	profile      *ytm.Profile
	mine, saved  []ytm.Playlist  // the listener's playlists: made, and saved
	savingPl     map[string]bool // the playlists whose saving or its end is on its way
	recPlaylists []ytm.Playlist  // the day's playlists picked for the listener
	login        loginState
	accountOpen  bool            // the dropdown of the account row shows
	naming       nameDialog      // the name of a playlist asked for
	deleting     deleteDialog    // whether to delete a playlist, asked
	liked        map[string]bool // the songs the listener likes
	toast        string          // a message to show once
	picked       pickedUp        // the song a login picked up, waiting to play

	// The episodes the listener likes, and doesn't, as far as NetEase has
	// answered, and those asked about: see ui_like.go.
	likedPrograms, askedPrograms map[string]bool

	daily       []ytm.Track
	dailySpin   spinState // the record on the daily card
	dailyBars   barState  // the bars on its play button
	featured    []ytm.Playlist
	charts      []ytm.Playlist
	homeErr     string
	homeLoading bool
	// homeGen counts loads of the home page, to drop what an earlier one
	// brings; homeSignedIn is whether the last was for someone signed in.
	homeGen      int
	homeSignedIn bool
	homeAt       time.Time // when what the home page shows was asked for

	playlists  map[string]*ytm.Playlist
	loadingPl  map[string]bool
	plErr      map[string]string
	artists    map[string]*artistData
	artistTab  int // what an artist's page lists: artistHot, artistLatest, artistAlbums or artistEvents
	albums     map[string]*albumData
	users      map[string]*userData
	podcasts   map[string]*podcastData
	podcastTab *podcastTab // the podcasts tab's lists
	trackList  ui.ListState
	// Where the page and the sidebar are scrolled, kept while Now Playing
	// covers them, unbuilt.
	pageScroll ui.ScrollState
	navScroll  ui.ScrollState
	scrolled   route // the page trackList and pageScroll are of

	mineCollapsed, savedCollapsed bool

	query       string
	searchBox   ui.Handle
	searches    map[route]*searchData // the results of each search, by its page
	searched    route                 // the search last shown, which the sidebar goes back to
	suggestions suggestState          // what the search field's query may be after

	nowPlaying  bool
	npOpen      bool       // Now Playing as it last showed: open or not
	npChangedAt time.Time  // when it last opened or closed
	npPane      int        // paneLyrics, paneQueue, paneStage, paneComments, paneProgram or paneCaptions
	video       videoState // the music video Now Playing shows
	// npVideo says the listener chose a song's video over its cover, which
	// Now Playing then shows in the cover's place, and npTheater that the
	// video takes the whole view.
	npVideo bool
	// npAtRest says Now Playing is open, and has come to rest.
	npAtRest  bool
	npTheater bool
	// songOf is the song each music video played in place of, as the
	// switch to the video swapped them, to swap back to.
	songOf      map[string]ytm.Track
	loginWin    *mygo.Window // the window signing in to YouTube, while open
	npPaneTop   float32      // how far the cover beside the pane starts below its top
	lyricScroll ui.ScrollState
	lyricGlide  lyricGlide
	lyricAt     int
	lyricTrack  string
	queueList   ui.ListState
	queueAt     int // the row of the queue pane last scrolled to, -1 while it is hidden
	comments    commentState
	tabCount    rollState // the count on the comments' tab, as it rolls

	fmMode        int               // the FM mode chosen, in fmModes
	radioStarting string            // the radio asked for its first songs, or ""
	radioNext     map[string]string // where songs' radios go on from, by radio

	scrub      float64
	scrubbing  bool
	volume     float64
	volumeOpen bool // the volume popover of the player bar shows
	mutedFrom  float64

	rowBars barState
	// underTop and underBottom say whether the page has content under its
	// bars, which show their scroll edges then.
	underTop, underBottom bool
	barScrub              scrubState
	npScrub               scrubState
	backdropCache         backdropCache // the backdrop of Now Playing, as last painted
	npArt                 *artwork      // the cover whose colors Now Playing shows
	npCoverState          coverState    // the cover of Now Playing, as it is tilted and flicked
	wallState             wallState     // the wall of the queue's covers, as it is dragged
	quiet                 quietState    // the quiet parts of the song playing, for its hot comments
	captions              captionState  // the episode playing's captions, and the model that writes them
	sig                   playerSig
	quitting              bool

	settings    settings
	settingsWin *mygo.Window
	cacheUsed   int64 // the bytes the song cache takes, -1 until measured
	updates     bool  // the update plugin is used (useUpdater)

	restored     bool   // playback picked up where it was left
	savedSession string // the session as last kept
	sessionDue   bool   // a save of the session is due
}

// post runs fn on the main thread and redraws both windows. Without a
// window, as in tests, it drops fn.
func (a *app) post(fn func()) {
	if a.win == nil {
		return
	}
	a.win.Update(func() {
		fn()
		a.redraw()
	})
}

func (a *app) redraw() {
	if a.win != nil {
		a.win.Invalidate()
	}
	if a.notch != nil && a.notch.win != nil {
		a.notch.win.Invalidate()
	}
}

// shotHook, when set, drives the app to check its design.
var shotHook func(a *app)

func (a *app) showMain() {
	if a.win != nil {
		if a.win.IsMinimized() {
			a.win.Restore()
		}
		a.win.Show()
		a.win.Focus()
		// From the menu bar alone the app may be inactive.
		mygo.App.Focus()
	}
}

// Run runs the app until it quits.
func Run() error {
	// Opening the app again shows the running one's window: Linux and
	// Windows would start another, with a second tray icon. Asked before
	// anything else, so the second one touches no data on its way out.
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	a := &app{
		playlists:  map[string]*ytm.Playlist{},
		loadingPl:  map[string]bool{},
		plErr:      map[string]string{},
		artists:    map[string]*artistData{},
		albums:     map[string]*albumData{},
		users:      map[string]*userData{},
		podcasts:   map[string]*podcastData{},
		podcastTab: &podcastTab{},
		searches:   map[route]*searchData{},
		lyricAt:    -1,
		queueAt:    -1,
		cacheUsed:  -1,
	}
	mygo.App.WhenReady(func() {
		a.settings = loadSettings()
		a.settings.applyAppearance()
		a.npPane = a.lyricsPaneChosen()
		// Before the window: on Windows it lays out its content as it is
		// made, and the view asks the player what plays.
		a.art = newArtCache(a.post)
		a.pl = player.New(a.post)
		a.pl.Quality = func() string { return a.settings.Quality }
		a.pl.Normalize = func() bool { return a.settings.Normalize }
		a.pl.Cache = newSongCache(a.settings.CacheMB)
		if dir, err := mygo.App.Path(mygo.PathCache); err == nil {
			ytm.PlayerCacheDir = filepath.Join(dir, "player")
		}
		a.pl.Changed = func() {
			a.keepSession()
			a.syncCaptions()
		}
		a.volume = a.settings.Volume
		a.pl.SetVolume(a.volume)
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:         mygo.App.Name(),
			Width:         1180,
			Height:        780,
			MinWidth:      880,
			MinHeight:     580,
			StateKey:      "main",
			TitleBarStyle: mygo.TitleBarHidden,
			// As tall as the edge over a page (Linux, Windows).
			TitleBarHeight: buttonsHeight,
			// Inset from the corner, within the sidebar, as macOS 26's
			// apps with a sidebar place them.
			TrafficLightPosition: &mygo.Point{X: 20, Y: 20},
			Vibrancy:             mygo.VibrancySidebar,
			BackgroundColor:      "light-dark(#ffffff, #141416)",
			Content:              ui.View(a.view),
		})
		a.setMenu()
		a.boot()
		// The notch player is macOS's, over its menu bar: elsewhere it would
		// hang at the top of the screen as a black box.
		if runtime.GOOS == "darwin" {
			a.notch = newNotch(a)
		}
		// From the menu bar alone, the Dock icon shows while the window
		// does.
		a.syncDock()
		a.win.OnShow(a.syncDock)
		a.win.OnHide(a.syncDock)
		// Hidden, it shows no video: the video view goes, and comes back with it.
		a.win.OnHide(a.closeVideo)
		a.win.OnMinimize(a.closeVideo)
		// Shown or switched to the morning after, it shows the new day's.
		a.win.OnFocus(a.freshenHome)
		// Back from System Settings, or from signing in in a browser, the
		// dialog asking how to sign in looks for the browsers again.
		a.win.OnFocus(func() {
			if a.login.open && a.login.busy == "" {
				a.scanBrowsers()
			}
		})
		a.applyTray()
		// YTM_DEBUG_VIDEO plays a video and opens it, to debug video views.
		if os.Getenv("YTM_DEBUG_LOGIN") != "" {
			browsercookies.Logf = func(f string, args ...any) { fmt.Fprintf(os.Stderr, "browsers: "+f+"\n", args...) }
			go func() {
				ps, err := browsercookies.Profiles()
				fmt.Fprintf(os.Stderr, "browsers: %d found, err %v\n", len(ps), err)
				for _, p := range ps {
					fmt.Fprintf(os.Stderr, "browsers:   %s / %s access=%v\n", p.Browser, p.Name, !p.NeedsAccess)
				}
			}()
			a.openLogin()
		}
		if id := os.Getenv("YTM_DEBUG_VIDEO"); id != "" {
			// Plays the video's song, then switches to the video, back to
			// the cover and to the video again, as a listener would.
			go func() {
				step := func(d time.Duration, fn func()) {
					time.Sleep(d)
					a.post(fn)
				}
				step(3*time.Second, func() {
					a.pl.PlayQueue([]ytm.Track{{ID: id, Name: "debug", VideoType: ytm.VideoOMV}}, 0)
					a.nowPlaying = true
				})
				step(8*time.Second, func() { a.chooseVideo(true) })
				step(10*time.Second, func() { a.chooseVideo(false) })
				step(3*time.Second, func() { a.chooseVideo(true) })
			}()
		}
		if shotHook != nil {
			shotHook(a)
		}
		// Closing the window hides it: music plays on, and the Dock icon
		// or the tray's brings it back. A Linux desktop without a tray
		// has neither, so there closing it quits.
		a.win.OnClose(func(e *mygo.CloseEvent) {
			if a.quitting {
				return
			}
			e.PreventDefault()
			if runtime.GOOS == "darwin" || a.tray != nil {
				a.win.Hide()
			} else {
				mygo.App.Quit()
			}
		})
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.quitting = true
		a.settings.Volume = a.volume
		a.settings.save()
		a.saveSession()
		if write := a.transcriptWrite(); write != nil {
			write()
		}
	})
	// Windows leaves an icon in the notification area after its app until
	// the pointer passes over it.
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if a.tray != nil {
			a.tray.Destroy()
		}
	})
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnActivate(func(bool) { a.showMain() })
	mygo.App.OnSecondInstance(func([]string, string) { a.showMain() })
	return mygo.App.Run()
}
