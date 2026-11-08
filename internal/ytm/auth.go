package ytm

import (
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Signed in, requests carry the cookies of a YouTube account, as a browser
// signed in to music.youtube.com sends them, and the SAPISIDHASH its web
// player signs each request with: a hash of the time, the SAPISID cookie
// and the origin. The app gets the cookies from a sign-in to Google in a
// web view, whose cookie store keeps them from one run to the next.

var auth struct {
	mu     sync.RWMutex
	header string // the Cookie header
	// The cookies the Authorization header hashes, as the web player
	// does: SAPISID, and the first- and third-party ones beside it.
	sapisid, sapisid1p, sapisid3p string
}

// LoginURL is where a sign-in to YouTube Music starts, in a web view: it
// ends on music.youtube.com, signed in.
const LoginURL = "https://accounts.google.com/ServiceLogin?ltmpl=music&service=youtube&passive=true&continue=" +
	"https%3A%2F%2Fwww.youtube.com%2Fsignin%3Faction_handle_signin%3Dtrue%26next%3Dhttps%253A%252F%252Fmusic.youtube.com%252F"

// SetCookies makes requests carry cookies, a Cookie header as a request to
// music.youtube.com sends them ("SID=…; SAPISID=…"), and reports whether
// they are an account's: those of nobody signed in, it leaves requests
// anonymous.
func SetCookies(header string) bool {
	jar := parseCookies(header)
	sapisid := jar["SAPISID"]
	if sapisid == "" {
		sapisid = jar["__Secure-3PAPISID"]
	}
	auth.mu.Lock()
	defer auth.mu.Unlock()
	if sapisid == "" {
		auth.header, auth.sapisid, auth.sapisid1p, auth.sapisid3p = "", "", "", ""
		return false
	}
	changed := auth.header != header
	auth.header = header
	auth.sapisid, auth.sapisid1p, auth.sapisid3p = sapisid, jar["__Secure-1PAPISID"], jar["__Secure-3PAPISID"]
	if changed {
		go clearCaches()
	}
	return true
}

// SignedIn reports whether requests carry an account's cookies.
func SignedIn() bool {
	auth.mu.RLock()
	defer auth.mu.RUnlock()
	return auth.header != ""
}

func parseCookies(header string) map[string]string {
	jar := map[string]string{}
	for _, part := range strings.Split(header, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && name != "" {
			jar[name] = value
		}
	}
	return jar
}

// signRequest adds the account's cookies and signature to h, for a
// request from origin, when signed in.
func signRequest(h http.Header, origin string) {
	auth.mu.RLock()
	header, sapisid, p1, p3 := auth.header, auth.sapisid, auth.sapisid1p, auth.sapisid3p
	auth.mu.RUnlock()
	if header == "" {
		return
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	hash := func(cookie string) string {
		sum := sha1.Sum([]byte(ts + " " + cookie + " " + origin))
		return ts + "_" + hex.EncodeToString(sum[:])
	}
	authz := "SAPISIDHASH " + hash(sapisid)
	if p1 != "" {
		authz += " SAPISID1PHASH " + hash(p1)
	}
	if p3 != "" {
		authz += " SAPISID3PHASH " + hash(p3)
	}
	h.Set("Cookie", header)
	h.Set("Authorization", authz)
	h.Set("X-Goog-AuthUser", "0")
	h.Set("X-Origin", origin)
}

// Account returns who is signed in, or nil when the cookies are nobody's
// (signed out, or expired).
func Account() (*Profile, error) {
	if !SignedIn() {
		return nil, nil
	}
	v, err := call("account/account_menu", map[string]any{})
	if err != nil {
		return nil, err
	}
	var header any
	for _, action := range arr(v, "actions") {
		header = dig(action, "openPopupAction", "popup", "multiPageMenuRenderer", "header", "activeAccountHeaderRenderer")
		if header != nil {
			break
		}
	}
	if header == nil {
		return nil, nil
	}
	p := &Profile{
		Nickname:  text(dig(header, "accountName")),
		AvatarURL: bestThumb(dig(header, "accountPhoto")),
	}
	// Their channel's ID, which credits their playlists, from the menu's
	// link to it; else their handle.
	for _, be := range findAll(dig(v, "actions"), "browseEndpoint", nil) {
		if id := str(be, "browseId"); strings.HasPrefix(id, "UC") {
			p.UserID = id
			break
		}
	}
	if p.UserID == "" {
		p.UserID = text(dig(header, "channelHandle"))
	}
	if p.UserID == "" {
		p.UserID = p.Nickname
	}
	rememberMe(p)
	return p, nil
}

// RestoreSession reports whether the cookies set are an account's: the
// app sets them from its cookie store before asking.
func RestoreSession() bool { return SignedIn() }

// SaveSession does nothing: the web view's cookie store keeps the cookies.
func SaveSession() error { return nil }

// ClearSession forgets the account's cookies.
func ClearSession() {
	rememberMe(nil)
	fm.mu.Lock()
	fm.token = ""
	fm.mu.Unlock()
	auth.mu.Lock()
	auth.header, auth.sapisid, auth.sapisid1p, auth.sapisid3p = "", "", "", ""
	auth.mu.Unlock()
	go clearCaches()
}

// Logout signs out: requests go anonymous. The app clears the web view's
// cookies.
func Logout() { ClearSession() }
