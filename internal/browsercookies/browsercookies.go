// Package browsercookies reads the YouTube sign-in of the browsers on a
// Mac, as yt-dlp's --cookies-from-browser does, for an app to sign in as
// the browser is: Chromium's (Chrome, Edge, Brave, Arc, Vivaldi and
// Chromium itself), whose cookie databases it reads with the system's
// sqlite3 and decrypts with the key each keeps in the login Keychain
// ("Chrome Safe Storage" and the like), which macOS asks the user before
// handing over; Firefox's, which it keeps plain; and Safari's, in a file
// of its own format, which macOS lets an app read only once it is allowed
// Full Disk Access.
package browsercookies

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Profile is a browser's profile signed in to YouTube, or a browser that
// is there without one: one the app may not read yet, or signed out.
type Profile struct {
	Browser string // the browser, as it names itself: "Chrome", "Safari"…
	App     string // where the browser's app is, for its icon
	Dir     string // the profile's directory in the browser's
	Name    string // what the browser calls the profile, if it has several
	Email   string // the Google account the browser is signed in to, if known
	// NeedsAccess says the app may not read the browser's profiles until
	// it is allowed Full Disk Access, as macOS keeps Safari's, Chrome's and
	// others' from other apps: whether one is signed in to YouTube is
	// unknown till then.
	NeedsAccess bool
	// SignedOut says the browser is there, read, and none of its profiles
	// signed in to YouTube.
	SignedOut bool
}

// Title is the browser's name as people know it.
func (p Profile) Title() string {
	switch p.Browser {
	case "Chrome":
		return "Google Chrome"
	case "Edge":
		return "Microsoft Edge"
	}
	return p.Browser
}

// Key tells profiles apart.
func (p Profile) Key() string { return p.Browser + "/" + p.Dir }

// Cookie is a cookie of YouTube's, decrypted.
type Cookie struct {
	Name, Value, Domain, Path string
	Secure, HTTPOnly          bool
	SameSite                  string // "", "None", "Lax" or "Strict"
	Expires                   time.Time
}

var (
	// ErrUnsupported is what anywhere but macOS gives.
	ErrUnsupported = errors.New("browsercookies: only macOS is supported")
	// ErrNotFound is what a profile no longer there gives.
	ErrNotFound = errors.New("browsercookies: the profile isn't there")
	// ErrNoAccess is what Safari's cookies give until the app is allowed
	// Full Disk Access.
	ErrNoAccess = errors.New("browsercookies: Full Disk Access is needed")
	// ErrKeychain is what a Chromium browser's cookies give when the
	// Keychain does not hand over its key, as when the user denies it.
	ErrKeychain = errors.New("browsercookies: the Keychain refused the browser's key")
	// ErrSignedOut is what a profile without YouTube's sign-in gives.
	ErrSignedOut = errors.New("browsercookies: not signed in to YouTube")
)

// Logf, if set, logs why profiles are passed over, to debug.
var Logf func(format string, args ...any)

func logf(format string, args ...any) {
	if Logf != nil {
		Logf(format, args...)
	}
}

// home is the user's home folder; tests change it.
var home = os.Getenv("HOME")

func appSupport(rel string) string {
	return filepath.Join(home, "Library", "Application Support", rel)
}

// Profiles returns the browsers' profiles signed in to YouTube: Chromium's
// and Firefox's, the one used last first in each, and Safari's, which
// shows needing access until the app is allowed it. It reads which
// cookies there are, not their values, so macOS asks nothing.
func Profiles() ([]Profile, error) {
	if runtime.GOOS != "darwin" {
		return nil, ErrUnsupported
	}
	var out []Profile
	for _, b := range chromiums {
		out = append(out, chromiumProfiles(b)...)
	}
	out = append(out, firefoxProfiles()...)
	out = append(out, safariProfiles()...)
	// The browsers most use first, and of each, those signed in.
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank(out[i].Browser), rank(out[j].Browser); ri != rj {
			return ri < rj
		}
		return !out[i].SignedOut && out[j].SignedOut
	})
	return out, nil
}

// browserOrder is the browsers as many use them, most first.
var browserOrder = []string{"Chrome", "Safari", "Edge", "Arc", "Brave", "Firefox", "Vivaldi", "Chromium"}

func rank(browser string) int {
	for i, b := range browserOrder {
		if b == browser {
			return i
		}
	}
	return len(browserOrder)
}

// denied says err is macOS keeping another app's files from this one.
func denied(err error) bool { return errors.Is(err, os.ErrPermission) }

// YouTube returns the YouTube cookies of a profile, decrypted. For a
// Chromium browser it asks the Keychain for the browser's key, which makes
// macOS ask the user.
func YouTube(p Profile) ([]Cookie, error) {
	if runtime.GOOS != "darwin" {
		return nil, ErrUnsupported
	}
	var cookies []Cookie
	var err error
	switch {
	case p.Browser == "Firefox":
		cookies, err = firefoxCookies(p.Dir)
	case p.Browser == "Safari":
		cookies, err = safariCookies()
	default:
		b, ok := chromiumNamed(p.Browser)
		if !ok {
			return nil, ErrNotFound
		}
		cookies, err = chromiumCookies(b, p.Dir)
	}
	if err != nil {
		return nil, err
	}
	if !signedIn(cookies) {
		return nil, ErrSignedOut
	}
	return cookies, nil
}

// Header joins cookies as a request's Cookie header.
func Header(cookies []Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// youtubeHosts are the hosts whose cookies a request to music.youtube.com
// sends.
var youtubeHosts = []string{".youtube.com", "youtube.com", "www.youtube.com", "music.youtube.com", ".music.youtube.com"}

// youtubeHostsSQL is youtubeHosts, as a list for SQL's in.
var youtubeHostsSQL = "('" + strings.Join(youtubeHosts, "','") + "')"

func youtubeHost(h string) bool {
	for _, y := range youtubeHosts {
		if h == y {
			return true
		}
	}
	return false
}

// signInCookies are the cookies of a Google sign-in that a request signs
// itself with: a profile without them is signed out.
var signInCookies = []string{"SAPISID", "__Secure-3PAPISID"}

func signedIn(cookies []Cookie) bool {
	for _, c := range cookies {
		for _, n := range signInCookies {
			if c.Name == n && c.Value != "" {
				return true
			}
		}
	}
	return false
}

// installed returns the first of the apps that is there.
func installed(apps ...string) string {
	for _, a := range apps {
		if _, err := os.Stat(a); err == nil {
			return a
		}
	}
	return ""
}
