package app

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"ytb-music/internal/browsercookies"
)

// loginApp is the window signed out, the dialog asking how to sign in
// opened from the account row, with profiles as the browsers' found.
func loginApp(t *testing.T, profiles ...browsercookies.Profile) (*app, *ui.Tester) {
	a := newTestApp()
	a.settings = defaultSettings()
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	if err := tt.Click("登录"); err != nil {
		t.Fatal(err)
	}
	if !a.login.open {
		t.Fatal("登录 did not ask how to sign in")
	}
	// What the look for the browsers found, which lands nowhere without a
	// window.
	a.login.scanning, a.login.scanned, a.login.profiles = false, true, profiles
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	return a, tt
}

// 登录 asks how to sign in, the browsers signed in to YouTube first: a
// click on one signs in with its cookies.
func TestLoginWithBrowser(t *testing.T) {
	asked := make(chan browsercookies.Profile, 1)
	browserCookies = func(p browsercookies.Profile) ([]browsercookies.Cookie, error) {
		asked <- p
		return nil, errors.New("offline")
	}
	defer func() { browserCookies = browsercookies.YouTube }()
	chrome := browsercookies.Profile{Browser: "Chrome", Dir: "Default", Name: "star", Email: "star@example.com"}
	_, tt := loginApp(t, chrome, browsercookies.Profile{Browser: "Arc", Dir: "Default", Name: "Person 1"})
	if !tt.HasText("使用 Google 账号密码登录") {
		t.Error("no way to sign in with Google's page")
	}
	if err := tt.Click("Google Chrome · star"); err != nil {
		t.Fatal(err)
	}
	if got := wait(t, asked, "Chrome's cookies"); got.Key() != chrome.Key() {
		t.Errorf("read %+v, want Chrome's", got)
	}
}

// Safari, which the app may not read without Full Disk Access, opens
// System Settings there rather than reading its cookies.
func TestLoginSafariNeedsAccess(t *testing.T) {
	var opened string
	openURL = func(u string) error { opened = u; return nil }
	defer func() { openURL = mygoOpenURL }()
	browserCookies = func(browsercookies.Profile) ([]browsercookies.Cookie, error) {
		t.Error("read Safari's cookies without access")
		return nil, nil
	}
	defer func() { browserCookies = browsercookies.YouTube }()
	_, tt := loginApp(t, browsercookies.Profile{Browser: "Safari", Dir: "default", NeedsAccess: true})
	if err := tt.Click("Safari"); err != nil {
		t.Fatal(err)
	}
	if opened != fullDiskAccessURL {
		t.Errorf("opened %q, want Full Disk Access", opened)
	}
}

// A Cookie header without a sign-in's says so, asking YouTube nothing.
func TestLoginPasteWithoutSignIn(t *testing.T) {
	a, tt := loginApp(t)
	if err := tt.Click("手动粘贴 Cookie"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.login.paste = "PREF=hl=zh; VISITOR_INFO1_LIVE=x"
	tt.Frame()
	if err := tt.Click("用 Cookie 登录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.login.pasteErr == "" || a.login.busy != "" {
		t.Errorf("error %q, busy %q", a.login.pasteErr, a.login.busy)
	}
}

// A browser signed out of YouTube opens YouTube Music in it, to sign in
// there, rather than reading it.
func TestLoginSignedOutBrowser(t *testing.T) {
	var app, url string
	openInBrowser = func(a, u string) error { app, url = a, u; return nil }
	defer func() { openInBrowser = openInApp }()
	browserCookies = func(browsercookies.Profile) ([]browsercookies.Cookie, error) {
		t.Error("read a browser signed out")
		return nil, nil
	}
	defer func() { browserCookies = browsercookies.YouTube }()
	_, tt := loginApp(t, browsercookies.Profile{Browser: "Chrome", App: "/Applications/Google Chrome.app", Dir: "none", SignedOut: true})
	if err := tt.Click("Google Chrome"); err != nil {
		t.Fatal(err)
	}
	if app != "/Applications/Google Chrome.app" || url != "https://music.youtube.com/" {
		t.Errorf("opened %q in %q", url, app)
	}
}
