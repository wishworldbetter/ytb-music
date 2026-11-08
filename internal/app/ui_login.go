package app

import (
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"ytb-music/internal/browsercookies"
	"ytb-music/internal/ytm"
)

// Signing in asks first how: with the sign-in to YouTube of a browser on
// the Mac, which the app reads as the browser keeps it, one click and the
// system's leave; with Google's own sign-in page, in a window of the app's,
// which takes the account's password and second step; or, failing those,
// with the Cookie header of a request to YouTube Music, pasted. Google
// lets no other app sign in to YouTube Music on its own, as a browser's
// authorization would: what a browser holds is the way in.

// loginState is the dialog that asks how to sign in.
type loginState struct {
	open bool
	gen  int // counts the times it opened, for what a closed one started
	// The browsers' profiles signed in to YouTube, as last looked for, and
	// their apps' icons, by the app's path.
	scanning, scanned bool
	profiles          []browsercookies.Profile
	icons             map[string]*ui.Bitmap
	// busy is the profile signing in, by its Key, or "paste"; errs says why
	// those that failed did, by Key.
	busy string
	errs map[string]string
	// The Cookie header pasted, its field shown.
	pasting  bool
	paste    string
	pasteErr string
}

// openInApp opens url in app, or with the default app.
func openInApp(app, url string) error {
	if app == "" {
		return mygoOpenURL(url)
	}
	return exec.Command("/usr/bin/open", "-a", app, url).Start()
}

// mygoOpenURL opens a URL with the default app.
func mygoOpenURL(u string) error { return mygo.Shell.OpenExternal(u) }

// fullDiskAccessURL opens System Settings at Full Disk Access, which an app
// needs to read Safari's cookies.
const fullDiskAccessURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles"

// browserProfiles, browserCookies and browserIcon read the browsers' and
// their apps' icons, openURL opens System Settings and openInBrowser a page
// in a browser; tests replace them.
var (
	openInBrowser   = openInApp
	openURL         = mygoOpenURL
	browserProfiles = browsercookies.Profiles
	browserCookies  = browsercookies.YouTube
	browserIcon     = func(app string) *ui.Bitmap {
		b, err := mygo.Shell.FileIcon(app, 64)
		if err != nil {
			return nil
		}
		bmp, err := ui.DecodeBitmap(b)
		if err != nil {
			return nil
		}
		return bmp
	}
)

// openLogin asks how to sign in, looking again for the browsers signed in
// to YouTube.
func (a *app) openLogin() {
	l := &a.login
	l.open, l.busy, l.pasteErr = true, "", ""
	l.gen++
	l.errs = map[string]string{}
	a.scanBrowsers()
}

// scanBrowsers looks for the browsers' profiles signed in to YouTube.
func (a *app) scanBrowsers() {
	l := &a.login
	if l.scanning {
		return
	}
	l.scanning = true
	go func() {
		ps, _ := browserProfiles()
		icons := map[string]*ui.Bitmap{}
		for _, p := range ps {
			if _, ok := icons[p.App]; !ok && p.App != "" {
				icons[p.App] = browserIcon(p.App)
			}
		}
		a.post(func() {
			l.scanning, l.scanned, l.profiles = false, true, ps
			if l.icons == nil {
				l.icons = map[string]*ui.Bitmap{}
			}
			for app, bmp := range icons {
				l.icons[app] = bmp
			}
		})
	}()
}

// importProfile signs in with a browser profile's cookies of YouTube,
// which the web view's store then keeps, as a sign-in in the app's window
// does. For a browser of Chromium's, macOS asks the user first whether the
// app may read the browser's key; Safari's cookies take Full Disk Access,
// which System Settings gives.
func (a *app) importProfile(p browsercookies.Profile) {
	l := &a.login
	if l.busy != "" {
		return
	}
	switch {
	case p.NeedsAccess:
		openURL(fullDiskAccessURL)
		return
	case p.SignedOut:
		// Signed in there, the list finds it as the app is back.
		openInBrowser(p.App, "https://music.youtube.com/")
		return
	}
	key, gen := p.Key(), l.gen
	l.busy = key
	delete(l.errs, key)
	go func() {
		cookies, err := browserCookies(p)
		var prof *ytm.Profile
		header := browsercookies.Header(cookies)
		if err == nil && ytm.SetCookies(header) {
			prof, err = ytm.Account()
		}
		if prof != nil {
			for _, c := range cookies {
				mygo.Cookies.Set(mygo.Cookie{
					Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path,
					Expires: c.Expires, Secure: c.Secure, HTTPOnly: c.HTTPOnly,
					SameSite: mygo.CookieSameSite(strings.ToLower(c.SameSite)),
				})
			}
		} else {
			ytm.ClearSession()
		}
		a.post(func() {
			if gen != l.gen {
				return
			}
			l.busy = ""
			switch {
			case errors.Is(err, browsercookies.ErrKeychain):
				l.errs[key] = "没有拿到钥匙串的允许：再试一次，在系统弹窗里点「允许」"
			case errors.Is(err, browsercookies.ErrNoAccess):
				l.errs[key] = "需要先在系统设置里允许「完全磁盘访问权限」"
			case errors.Is(err, browsercookies.ErrSignedOut), err == nil && prof == nil:
				l.errs[key] = "这里的 YouTube 登录已失效，请先在浏览器里重新登录 music.youtube.com"
			case err != nil:
				l.errs[key] = "读取失败，请稍后再试"
			default:
				a.loggedIn(prof, header)
			}
		})
	}()
}

// pasteCookies signs in with a Cookie header pasted, as a browser's
// developer tools show a request to YouTube Music sending it.
func (a *app) pasteCookies() {
	l := &a.login
	if l.busy != "" {
		return
	}
	header := strings.TrimSpace(l.paste)
	header = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(header, "Cookie:"), "cookie:"))
	if !ytm.SetCookies(header) {
		l.pasteErr = "没有找到登录用的 SAPISID：请复制 music.youtube.com 请求里完整的 Cookie"
		return
	}
	gen := l.gen
	l.busy, l.pasteErr = "paste", ""
	go func() {
		prof, err := ytm.Account()
		if prof != nil {
			expires := time.Now().AddDate(1, 0, 0)
			for _, part := range strings.Split(header, ";") {
				name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
				if !ok || name == "" {
					continue
				}
				mygo.Cookies.Set(mygo.Cookie{Name: name, Value: value, Domain: ".youtube.com", Path: "/", Expires: expires, Secure: true})
			}
		} else {
			ytm.ClearSession()
		}
		a.post(func() {
			if gen != l.gen {
				return
			}
			l.busy = ""
			switch {
			case err != nil:
				l.pasteErr = "连不上 YouTube，请稍后再试"
			case prof == nil:
				l.pasteErr = "这份 Cookie 的登录已失效"
			default:
				a.loggedIn(prof, header)
			}
		})
	}()
}

// loggedIn takes prof as who listens, signed in with the cookies of header,
// and puts the dialog away.
func (a *app) loggedIn(prof *ytm.Profile, header string) {
	dumpCookies(header)
	a.login.open = false
	a.signedIn(prof)
	a.pickUp(prof.UserID)
	a.toast = "已登录：" + prof.Nickname
}

// profileTitle is how a browser profile reads in the dialog: the browser,
// and the profile's name where it has one worth showing.
func profileTitle(p browsercookies.Profile) string {
	switch p.Name {
	case "", "default", "default-release":
		return p.Title()
	}
	return p.Title() + " · " + p.Name
}

// loginDialog asks how to sign in: with a browser's sign-in, first, as
// most will; then with Google's page, or a Cookie header pasted.
func (a *app) loginDialog(c *ui.Context) {
	p := a.pal
	l := &a.login
	ui.DialogBase(c.Key("login"), &l.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.35))
		panel.Width(460).Padding(26, 26, 22, 26).Radius(20).Gap(0).
			Background(p.bg).Border(1, p.hairline).Shadow(0, 24, 60, -12, ui.RGBA(0, 0, 0, 0.4)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.94}, Exit: &ui.Motion{Scale: 0.96}})

		ui.Row(c).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(6).Children(func() {
				ui.Text(c, "登录 YouTube Music").FontSize(19).FontWeight(700)
				ui.Text(c, "登录后可同步你的歌单、喜欢的音乐和播放记录，Premium 会员可听更高音质。").
					FontSize(12.5).TextColor(p.muted).LineHeight(1.45)
			})
			if iconButton(c.Key("close"), iconX, 16, p.muted, "关闭").Margin(-4, -6, 0, 8).Clicked() {
				l.open = false
			}
		})

		ui.Text(c, "使用浏览器的登录（推荐）").FontSize(12).FontWeight(600).TextColor(p.muted).Margin(20, 0, 8, 2)
		a.browserList(c)
		a.loginHints(c)

		// Or Google's own page.
		ui.Row(c).Gap(12).AlignItems(ui.Center).Margin(18, 0, 14, 0).Children(func() {
			ui.Box(c).Height(0.5).Grow(1).Background(p.hairline)
			ui.Text(c, "或").FontSize(11.5).TextColor(p.faint)
			ui.Box(c).Height(0.5).Grow(1).Background(p.hairline)
		})
		window := pillButton(c.Key("google"), nil, "使用 Google 账号密码登录", p.hover.Over(p.bg), p.text).Justify(ui.Center)
		if window.Clicked() {
			a.openLoginWindow()
		}
		ui.Text(c, "在窗口里打开 Google 官方登录页（系统 Safari 内核），支持两步验证").
			FontSize(11).TextColor(p.faint).TextAlign(ui.Center).Margin(6, 0, 0, 0)

		a.pasteSection(c)
	})
}

// browserList shows the browsers signed in to YouTube, each a row that
// signs in with its sign-in, or what to do without any.
func (a *app) browserList(c *ui.Context) {
	p := a.pal
	l := &a.login
	list := ui.Column(c.Key("browsers")).Radius(12).Background(p.hover).Padding(4).Gap(2)
	list.Children(func() {
		if len(l.profiles) == 0 {
			ui.Column(c).Padding(14, 12).Gap(6).AlignItems(ui.Center).Children(func() {
				if !l.scanned {
					ui.Text(c, "正在查找浏览器里的登录…").FontSize(12.5).TextColor(p.muted)
					return
				}
				ui.Text(c, "没有找到登录了 YouTube 的浏览器").FontSize(13).FontWeight(600)
				ui.Text(c, "先在 Chrome、Safari、Edge、Arc 或 Firefox 里登录 music.youtube.com，再回来").
					FontSize(11.5).TextColor(p.muted).TextAlign(ui.Center)
				if ui.Button(c.Key("rescan"), "重新查找").Margin(4, 0, 0, 0).Clicked() {
					a.scanBrowsers()
				}
			})
			return
		}
		for _, pr := range l.profiles {
			a.browserRow(c, pr)
		}
	})
	if len(l.profiles) > 0 {
		again := ui.ButtonBase(c.Key("rescan-link")).AlignSelf(ui.End).Margin(6, 4, 0, 0).FocusRing(false).Label("重新查找")
		fg := p.faint
		if again.Hovered() {
			fg = p.muted
		}
		again.Children(func() {
			text := "重新查找"
			if l.scanning {
				text = "正在查找…"
			}
			ui.Text(c, text).FontSize(11.5).TextColor(fg)
		})
		if again.Clicked() {
			a.scanBrowsers()
		}
	}
}

// browserRow is a browser's profile: a click signs in with it, or, for a
// browser the app may not read yet, opens where the system allows it.
func (a *app) browserRow(c *ui.Context, pr browsercookies.Profile) {
	p := a.pal
	l := &a.login
	key := pr.Key()
	title := profileTitle(pr)
	row := ui.ButtonBase(c.Key(key)).Padding(8, 10).Gap(12).Radius(9).AlignItems(ui.Center).FocusRing(false).Label(title)
	busy := l.busy == key
	if l.busy != "" && !busy {
		row.Disabled(true)
	}
	bg := ui.Transparent
	if row.Hovered() || busy {
		bg = p.bg.Mix(p.hover, 0.4)
	}
	row.Background(row.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	row.Children(func() {
		if bmp := l.icons[pr.App]; bmp != nil {
			ui.Image(c, bmp).Size(30, 30).Shrink(0)
		} else {
			ui.Box(c).Size(30, 30).Radius(8).Background(p.press).Center().Shrink(0).Children(func() {
				ui.Text(c, pr.Browser[:1]).FontSize(14).FontWeight(700).TextColor(p.muted)
			})
		}
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(13).FontWeight(600).SingleLine()
			sub, color := pr.Email, p.muted
			switch {
			case l.errs[key] != "":
				sub, color = l.errs[key], p.accent
			case pr.NeedsAccess:
				sub = "需要先授权才能读取"
			case pr.SignedOut:
				sub, color = "没有登录 YouTube", p.faint
			case sub == "":
				sub = "已登录 YouTube"
			}
			ui.Text(c, sub).FontSize(11.5).TextColor(color).MaxLines(2)
		})
		action := "导入"
		switch {
		case busy:
			action = "正在导入…"
		case pr.NeedsAccess:
			action = "去授权"
		case pr.SignedOut:
			action = "去登录"
		}
		ui.Text(c, action).FontSize(12).FontWeight(600).TextColor(p.accent).Padding(4, 10).Radius(999).
			Background(p.accent.Alpha(0.1)).Shrink(0)
	})
	if row.Clicked() {
		a.importProfile(pr)
	}
}

// loginHints says what the system asks as a browser's sign-in is read.
func (a *app) loginHints(c *ui.Context) {
	p := a.pal
	ui.Row(c).Gap(8).AlignItems(ui.Start).Margin(10, 2, 0, 2).Children(func() {
		ui.Icon(c, iconInfo).Size(13, 13).TextColor(p.faint).Margin(2, 0, 0, 0).Shrink(0)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, "Chrome、Safari 等的登录数据受 macOS 保护：点「去授权」，在「完全磁盘访问权限」里打开 YTB Music（列表里没有就点「+」添加），按提示重新打开 App，只需一次。").
				FontSize(11.5).TextColor(p.muted).LineHeight(1.45)
			ui.Text(c, "导入 Chrome、Edge、Arc 时，macOS 还会问能否读取钥匙串里的「… Safe Storage」，点「允许」即可。").
				FontSize(11.5).TextColor(p.muted).LineHeight(1.45)
		})
	})
}

// pasteSection takes a Cookie header pasted, behind a link: for when no
// browser's sign-in reads.
func (a *app) pasteSection(c *ui.Context) {
	p := a.pal
	l := &a.login
	if !l.pasting {
		link := ui.ButtonBase(c.Key("paste-open")).AlignSelf(ui.Center).Margin(12, 0, 0, 0).FocusRing(false).Label("手动粘贴 Cookie")
		fg := p.faint
		if link.Hovered() {
			fg = p.muted
		}
		link.Children(func() { ui.Text(c, "手动粘贴 Cookie…").FontSize(11.5).TextColor(fg) })
		if link.Clicked() {
			l.pasting = true
		}
		return
	}
	ui.Column(c.Key("paste")).Gap(8).Margin(16, 0, 0, 0).Children(func() {
		ui.Text(c, "在浏览器打开 music.youtube.com，开发者工具 › 网络里任选一个请求，复制「Cookie」请求头粘贴到这里：").
			FontSize(11.5).TextColor(p.muted).LineHeight(1.45)
		ui.TextArea(c.Key("cookie"), &l.paste).Placeholder("SAPISID=…; __Secure-3PAPISID=…; …").Label("Cookie").
			FontSize(12).Height(72).AutoFocus()
		if l.pasteErr != "" {
			ui.Text(c, l.pasteErr).FontSize(11.5).TextColor(p.accent)
		}
		ui.Row(c).Justify(ui.End).Children(func() {
			label := "用 Cookie 登录"
			if l.busy == "paste" {
				label = "正在登录…"
			}
			ok := pillButton(c.Key("paste-ok"), nil, label, p.accent, p.onAccent)
			if strings.TrimSpace(l.paste) == "" || l.busy != "" {
				ok.Disabled(true).Opacity(0.5)
			}
			if ok.Clicked() {
				a.pasteCookies()
			}
		})
	})
}
