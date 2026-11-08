package browsercookies

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// firefoxApps is where Firefox's app is.
var firefoxApps = []string{"/Applications/Firefox.app"}

// firefoxProfiles returns Firefox's profiles signed in to YouTube, its
// default first, as profiles.ini lists them.
func firefoxProfiles() []Profile {
	root := appSupport("Firefox")
	app := installed(firefoxApps...)
	f, err := os.Open(filepath.Join(root, "profiles.ini"))
	switch {
	case denied(err):
		return []Profile{{Browser: "Firefox", App: app, Dir: "access", NeedsAccess: true}}
	case err != nil:
		if app != "" {
			return []Profile{{Browser: "Firefox", App: app, Dir: "none", SignedOut: true}}
		}
		return nil
	}
	defer f.Close()
	type entry struct {
		name, path string
		relative   bool
		def        bool
	}
	var entries []entry
	var defaults []string // the profiles installs default to
	var cur *entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			cur = nil
			if strings.HasPrefix(line, "[Profile") {
				entries = append(entries, entry{relative: true})
				cur = &entries[len(entries)-1]
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch {
		case cur == nil && k == "Default":
			defaults = append(defaults, v) // of an [Install…] section
		case cur == nil:
		case k == "Name":
			cur.name = v
		case k == "Path":
			cur.path = v
		case k == "IsRelative":
			cur.relative = v != "0"
		case k == "Default":
			cur.def = v == "1"
		}
	}
	var first, rest []Profile
	for _, e := range entries {
		if e.path == "" {
			continue
		}
		dir := e.path
		if !e.relative {
			rel, err := filepath.Rel(root, e.path)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			dir = rel
		}
		cookies, err := firefoxCookies(dir)
		if err != nil || !signedIn(cookies) {
			continue
		}
		p := Profile{Browser: "Firefox", App: app, Dir: dir, Name: e.name}
		isDefault := e.def
		for _, d := range defaults {
			isDefault = isDefault || d == e.path
		}
		if isDefault {
			first = append(first, p)
		} else {
			rest = append(rest, p)
		}
	}
	if len(first)+len(rest) == 0 && app != "" {
		return []Profile{{Browser: "Firefox", App: app, Dir: "none", SignedOut: true}}
	}
	return append(first, rest...)
}

// firefoxCookies returns the YouTube cookies of Firefox's profile dir, in
// its own container, not those of other containers. Firefox writes its
// newest cookies to the database's log beside it until it checkpoints, so
// both are read, from a copy: Firefox holds the database locked.
func firefoxCookies(dir string) ([]Cookie, error) {
	if dir == "" || strings.Contains(dir, "..") {
		return nil, ErrNotFound
	}
	db := filepath.Join(appSupport("Firefox"), dir, "cookies.sqlite")
	if _, err := os.Stat(db); err != nil {
		return nil, ErrNotFound
	}
	tmp, err := os.MkdirTemp("", "ytb-firefox")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	for _, suffix := range []string{"", "-wal"} {
		b, err := os.ReadFile(db + suffix)
		if err != nil {
			if suffix == "" {
				return nil, err
			}
			continue
		}
		if err := os.WriteFile(filepath.Join(tmp, "cookies.sqlite"+suffix), b, 0o600); err != nil {
			return nil, err
		}
	}
	rows, err := queryRW(filepath.Join(tmp, "cookies.sqlite"),
		`select host, name, path, isSecure, isHttpOnly, sameSite, expiry, hex(value) from moz_cookies where host in `+youtubeHostsSQL+` and originAttributes = ''`)
	if err != nil {
		return nil, err
	}
	var out []Cookie
	for _, line := range strings.Split(strings.TrimRight(rows, "\n"), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			continue
		}
		c := Cookie{Domain: f[0], Name: f[1], Path: f[2], Secure: f[3] == "1", HTTPOnly: f[4] == "1", SameSite: sameSite(f[5])}
		if exp, err := strconv.ParseInt(f[6], 10, 64); err == nil && exp > 0 {
			// Seconds, or milliseconds since Firefox 125.
			if exp > 1e11 {
				c.Expires = time.UnixMilli(exp)
			} else {
				c.Expires = time.Unix(exp, 0)
			}
		}
		v, _ := hexString(f[7])
		c.Value = v
		if c.Name != "" {
			out = append(out, c)
		}
	}
	return out, nil
}
