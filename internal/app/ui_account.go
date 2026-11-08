package app

import (
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"ytb-music/internal/ytm"
)

// boot signs in with the cookies the web view's store kept from a sign-in
// to YouTube, if any, then loads the home page for whoever is listening.
func (a *app) boot() {
	a.homeLoading = true
	go func() {
		var prof *ytm.Profile
		header := youtubeCookies()
		cookies := ytm.SetCookies(header)
		// What the home page showed last time shows at once, and the new
		// loads beside the account's check, with its cookies.
		a.post(func() {
			a.loadHomeCache(cookies)
			a.loadHome()
		})
		if cookies && ytm.RestoreSession() {
			dumpCookies(header)
			p, err := ytm.Account()
			switch {
			case err == nil && p == nil:
				ytm.ClearSession() // expired
			case err == nil:
				prof = p
			}
		}
		a.post(func() { a.signedIn(prof) })
	}()
}

// preparePremium works out YouTube's player ahead of the first song, for
// the streams of Premium, when the settings ask for them.
func (a *app) preparePremium() {
	if a.profile != nil && a.settings.Quality == "exhigh" {
		ytm.PreparePlayer()
	}
}

// signedIn shows what is personal to prof, or to nobody.
func (a *app) signedIn(prof *ytm.Profile) {
	a.profile = prof
	a.preparePremium()
	// Playback picks up where it was left, once the session that lets
	// the listener play their songs is back.
	a.restoreSession()
	a.mine, a.saved, a.recPlaylists, a.liked = nil, nil, nil, nil
	a.likedPrograms, a.askedPrograms = nil, nil
	// What the listener may play, and follows, changed: load the pages
	// again.
	a.playlists = map[string]*ytm.Playlist{}
	a.plErr = map[string]string{}
	a.artists, a.albums, a.users, a.podcasts = map[string]*artistData{}, map[string]*albumData{}, map[string]*userData{}, map[string]*podcastData{}
	a.podcastTab = &podcastTab{}
	a.searches = map[route]*searchData{}
	a.load(a.route)
	// Loaded for whoever listens already, as boot does, it stays.
	if a.homeSignedIn != ytm.SignedIn() || !a.homeLoading && a.homeAt.IsZero() {
		a.loadHome()
	}
	if prof == nil {
		return
	}
	a.loadLiked(prof.UserID)
	a.loadMine(prof.UserID)
}

// openLoginWindow opens a window to sign in to Google, on Google's own
// page, which ends on YouTube Music: once there, signed in, the app takes
// the cookies of the sign-in, which the web view's store keeps, and the
// window closes.
func (a *app) openLoginWindow() {
	if w := a.loginWin; w != nil && !w.IsDestroyed() {
		w.Show()
		w.Focus()
		return
	}
	w := mygo.NewWindow(mygo.WindowOptions{
		Title:     "登录 YouTube",
		Width:     500,
		Height:    720,
		MinWidth:  400,
		MinHeight: 560,
		URL:       ytm.LoginURL,
		// Safari's, which Google lets sign in: a bare web view's names no
		// browser.
		Page: mygo.PageOptions{UserAgent: ytm.UserAgent},
	})
	a.loginWin = w
	w.Page().OnDidNavigate(func(u string) {
		if strings.HasPrefix(u, "https://music.youtube.com") {
			a.finishLogin(w)
		}
	})
	w.OnClosed(func() {
		if a.loginWin == w {
			a.loginWin = nil
		}
	})
}

// finishLogin takes the cookies of a sign-in that reached YouTube Music,
// and closes its window, unless they are nobody's yet.
func (a *app) finishLogin(w *mygo.Window) {
	go func() {
		header := youtubeCookies()
		var prof *ytm.Profile
		if ytm.SetCookies(header) {
			prof, _ = ytm.Account()
		}
		if prof != nil {
			dumpCookies(header)
		}
		a.post(func() {
			if prof == nil {
				return // not signed in yet: the page stays
			}
			a.login.open = false
			a.signedIn(prof)
			a.pickUp(prof.UserID)
			a.toast = "已登录：" + prof.Nickname
			w.Close()
			a.showMain()
		})
	}()
}

// youtubeCookies returns the cookies the web view's store sends to YouTube
// Music, as a Cookie header.
func youtubeCookies() string {
	list, err := mygo.Cookies.List(mygo.CookieFilter{URL: "https://music.youtube.com/"})
	if err != nil {
		return ""
	}
	parts := make([]string, 0, len(list))
	for _, c := range list {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// dumpCookies writes the cookies of the account to the file
// YTM_COOKIE_DUMP names, if set, for the live tests of package ytm.
func dumpCookies(header string) {
	if path := os.Getenv("YTM_COOKIE_DUMP"); path != "" {
		os.WriteFile(path, []byte(header), 0o600)
	}
}

func (a *app) signOut() {
	a.login.gen++
	// Anonymous before the pages load again, then without the cookies.
	ytm.Logout()
	a.signedIn(nil)
	go mygo.Cookies.Clear()
}

func (a *app) profileName() string {
	if a.profile == nil {
		return ""
	}
	return a.profile.Nickname
}

// accountRow sits at the foot of the sidebar: who is signed in, with a
// dropdown to open their page or the settings or sign out, or a way to
// sign in.
// accountRow is who listens, at the foot of the sidebar: their picture and
// name, or a picture standing for them and the way to sign in. As tall as
// the player bar beside it, level with it, the two make one band.
func (a *app) accountRow(c *ui.Context) {
	p := a.pal
	b := ui.ButtonBase(c.Key("account")).Height(playerBarHeight - 28).PaddingX(8).Gap(10).Radius(12).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	bg := ui.Transparent
	if b.Hovered() || a.accountOpen && a.profile != nil {
		bg = p.hover
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	lines := func(name, sub string) {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, name).FontSize(13).FontWeight(600).TextColor(p.text).SingleLine()
			ui.Text(c, sub).FontSize(11.5).TextColor(p.muted).SingleLine()
		})
	}
	if a.profile == nil {
		a.accountOpen = false
		b.Label("登录").Children(func() {
			defaultAvatar(c, accountAvatar, p)
			lines("登录", "同步你的歌单和收藏")
		})
		if b.Clicked() {
			a.openLogin()
		}
		return
	}
	prof := a.profile
	b.Label(prof.Nickname).Expanded(a.accountOpen).Children(func() {
		if prof.AvatarURL == "" {
			defaultAvatar(c, accountAvatar, p)
		} else {
			a.avatar(c, prof.AvatarURL, accountAvatar, p.press)
		}
		lines(prof.Nickname, "YouTube Music")
		ui.Icon(c, iconUpDown).Size(13, 13).TextColor(p.faint)
	})
	if b.Clicked() {
		a.accountOpen = !a.accountOpen
	}
	a.accountMenu(c, b)
}

// accountAvatar is how large the listener's picture is in the sidebar.
const accountAvatar = 34

// defaultAvatar is a picture standing for someone without one: a figure
// on a gray disc, size across, as the system draws an account's.
func defaultAvatar(c *ui.Context, size float32, p palette) {
	from, to := ui.Hex("#c7c7cc"), ui.Hex("#9a9aa0")
	if p.dark {
		from, to = ui.Hex("#6e6e73"), ui.Hex("#48484a")
	}
	ui.Box(c).Size(size, size).Radius(size/2).Shrink(0).Gradient(from, to, 180).Clip().Children(func() {
		// The figure, its shoulders cut by the disc's edge.
		ui.Icon(c, iconFigure).Size(size*0.82, size*0.82).TextColor(white.Alpha(0.95)).
			Absolute().Left(size * 0.09).Top(size * 0.2)
	})
}

// accountMenu drops up from the account row, as wide as it, in the colors
// of the volume popover: the listener's own page and the settings, then
// signing out.
func (a *app) accountMenu(c *ui.Context, row ui.Element) {
	pop := popoverColors(a.pal.dark)
	ui.PopoverBase(c.Key("account-menu"), row, &a.accountOpen, func(panel ui.Element) {
		panel.AttachTo(row, ui.AnchorTopStart, ui.AnchorBottomStart).Margin(0, 0, 6, 0).Padding(5).Gap(1).
			Radius(12).Background(pop.fill).Border(0.5, pop.edge).Shadow(0, 8, 24, -4, ui.RGBA(0, 0, 0, 0.2)).
			Role(ui.RoleMenu).FocusGroup(ui.Vertical).Label("账户").TransformOrigin(0.5, 1).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.96, Y: 4}, Exit: &ui.Motion{Scale: 0.98}})
		if w := row.Bounds().W; w > 0 {
			panel.Width(w)
		}
		shortcut := "Ctrl+,"
		if runtime.GOOS == "darwin" {
			shortcut = "⌘,"
		}
		if a.menuItem(c, iconUser, "我的主页", "").AutoFocus().Clicked() {
			a.accountOpen = false
			a.open(route{page: pageUser, id: a.profile.UserID})
		}
		if a.menuItem(c, iconSettings, "设置…", shortcut).Clicked() {
			a.accountOpen = false
			a.openSettings()
		}
		ui.Box(c).Height(0.5).Margin(4, 8).Background(pop.edge)
		if a.menuItem(c, iconSignOut, "退出登录", "").Clicked() {
			a.accountOpen = false
			a.signOut()
		}
	})
}

// menuItem is an item of a dropdown: an icon, its label, and the shortcut
// that does the same, if any.
func (a *app) menuItem(c *ui.Context, icon *ui.SVG, label, shortcut string) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c.Key(label)).Role(ui.RoleMenuItem).Label(label).Height(30).PaddingX(8).Gap(9).Radius(7).
		AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	if b.Hovered() || b.FocusVisible() {
		b.Background(p.press)
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(15, 15).TextColor(p.muted)
		ui.Text(c, label).FontSize(13).Grow(1)
		if shortcut != "" {
			ui.Text(c, shortcut).FontSize(12).TextColor(p.faint)
		}
	})
	return b
}

// myPlaylists lists the listener's playlists under a header that opens
// and closes them: those they made, if mine, with a button to make one
// and a menu to rename them, or else with a menu to unsave them.
func (a *app) myPlaylists(c *ui.Context, title string, lists []ytm.Playlist, mine bool, collapsed *bool) {
	if len(lists) == 0 {
		return
	}
	p := a.pal
	ui.Row(c.Key(title)).AlignItems(ui.Center).Margin(18, 4, 6, 6).Children(func() {
		b := ui.ButtonBase(c.Key("toggle")).Grow(1).Height(26).PaddingX(4).Gap(6).Radius(6).
			AlignItems(ui.Center).Justify(ui.Start).Role(ui.RoleDisclosure).Label(title)
		if b.Clicked() {
			*collapsed = !*collapsed
		}
		b.Expanded(!*collapsed)
		if b.Hovered() {
			b.Background(p.hover)
		}
		b.Children(func() {
			ui.Text(c, title).FontSize(11.5).FontWeight(600).TextColor(p.faint).Grow(1)
			arrow := iconDown
			if *collapsed {
				arrow = iconForward
			}
			ui.Icon(c, arrow).Size(12, 12).TextColor(p.faint)
		})
		if mine && iconButton(c.Key("new"), iconPlus, 13, p.faint, "新建歌单").Size(26, 26).Radius(6).Clicked() {
			a.newPlaylist(nil)
		}
	})
	if *collapsed {
		return
	}
	for i := range lists {
		pl := &lists[i]
		selected := a.route.page == pagePlaylist && a.route.id == pl.ID && !a.nowPlaying
		b := ui.ButtonBase(c.Key(pl.ID)).Height(34).PaddingX(8).Gap(10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Label(pl.Name)
		switch {
		case selected:
			b.Background(p.press)
		case b.Hovered():
			b.Background(p.hover)
		}
		b.Children(func() {
			a.cover(c, pl.Cover(), 80, 22, 5)
			ui.Text(c, pl.Name).FontSize(13).SingleLine().Shrink(1).MinWidth(0)
		})
		switch {
		case mine && pl.SpecialType != 5:
			b.ContextMenu(func(m *ui.Menu) {
				if m.Item("重命名…").Chosen() {
					a.startRename(pl)
				}
				m.Separator()
				if m.Item("删除歌单…").Chosen() {
					a.confirmDelete(pl)
				}
			})
		case !mine:
			b.ContextMenu(func(m *ui.Menu) {
				if m.Item("取消收藏").Chosen() {
					a.toggleSaved(*pl)
				}
			})
		}
		if b.Clicked() {
			a.open(route{page: pagePlaylist, id: pl.ID})
		}
	}
}
