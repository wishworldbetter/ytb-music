package browsercookies

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// safariApps is where Safari's app is: in a cryptex on newer macOS, which
// /Applications links to.
var safariApps = []string{"/Applications/Safari.app", "/System/Cryptexes/App/System/Applications/Safari.app"}

// safariFiles are where Safari keeps its cookies: in its container since
// macOS 10.14, before in the user's Library.
func safariFiles() []string {
	return []string{
		filepath.Join(home, "Library", "Containers", "com.apple.Safari", "Data", "Library", "Cookies", "Cookies.binarycookies"),
		filepath.Join(home, "Library", "Cookies", "Cookies.binarycookies"),
	}
}

// safariProfiles returns Safari, if it is signed in to YouTube, or if the
// app may not read its cookies, which macOS keeps from apps without Full
// Disk Access.
func safariProfiles() []Profile {
	app := installed(safariApps...)
	if app == "" {
		return nil
	}
	cookies, err := safariCookies()
	switch {
	case errors.Is(err, ErrNoAccess):
		return []Profile{{Browser: "Safari", App: app, Dir: "default", NeedsAccess: true}}
	case err != nil || !signedIn(cookies):
		return []Profile{{Browser: "Safari", App: app, Dir: "default", SignedOut: true}}
	}
	return []Profile{{Browser: "Safari", App: app, Dir: "default"}}
}

// safariCookies returns Safari's YouTube cookies.
func safariCookies() ([]Cookie, error) {
	var data []byte
	denied := false
	for _, f := range safariFiles() {
		b, err := os.ReadFile(f)
		if err == nil {
			data = b
			break
		}
		if errors.Is(err, os.ErrPermission) {
			denied = true
		}
	}
	if data == nil {
		if denied {
			return nil, ErrNoAccess
		}
		return nil, ErrNotFound
	}
	all, err := parseBinaryCookies(data)
	if err != nil {
		return nil, err
	}
	var out []Cookie
	for _, c := range all {
		if youtubeHost(c.Domain) {
			out = append(out, c)
		}
	}
	return out, nil
}

// macEpoch is when Safari's times count from.
var macEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

var errBinaryCookies = errors.New("browsercookies: not Safari's cookies")

// parseBinaryCookies reads Safari's Cookies.binarycookies: "cook", the
// number of pages and their sizes, big-endian, then the pages, each a
// header, the number of its cookies and their offsets, little-endian, then
// the cookies, each its size, flags, the offsets of its strings within it,
// and when it expires.
func parseBinaryCookies(b []byte) ([]Cookie, error) {
	if len(b) < 8 || string(b[:4]) != "cook" {
		return nil, errBinaryCookies
	}
	pages := int(binary.BigEndian.Uint32(b[4:8]))
	if pages < 0 || 8+4*pages > len(b) {
		return nil, errBinaryCookies
	}
	sizes := make([]int, pages)
	for i := range sizes {
		sizes[i] = int(binary.BigEndian.Uint32(b[8+4*i:]))
	}
	at := 8 + 4*pages
	var out []Cookie
	for _, size := range sizes {
		if size < 8 || at+size > len(b) {
			return nil, errBinaryCookies
		}
		page := b[at : at+size]
		at += size
		n := int(binary.LittleEndian.Uint32(page[4:8]))
		if n < 0 || 8+4*n > len(page) {
			return nil, errBinaryCookies
		}
		for i := range n {
			off := int(binary.LittleEndian.Uint32(page[8+4*i:]))
			if c, ok := parseBinaryCookie(page, off); ok {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

// parseBinaryCookie reads the cookie at off in page.
func parseBinaryCookie(page []byte, off int) (Cookie, bool) {
	if off < 0 || off+48 > len(page) {
		return Cookie{}, false
	}
	size := int(binary.LittleEndian.Uint32(page[off:]))
	if size < 48 || off+size > len(page) {
		return Cookie{}, false
	}
	rec := page[off : off+size]
	u32 := func(at int) int { return int(binary.LittleEndian.Uint32(rec[at:])) }
	str := func(at int) string {
		if at <= 0 || at >= len(rec) {
			return ""
		}
		end := at
		for end < len(rec) && rec[end] != 0 {
			end++
		}
		return string(rec[at:end])
	}
	flags := u32(8)
	c := Cookie{
		Domain:   str(u32(16)),
		Name:     str(u32(20)),
		Path:     str(u32(24)),
		Value:    str(u32(28)),
		Secure:   flags&1 != 0,
		HTTPOnly: flags&4 != 0,
	}
	if s := math.Float64frombits(binary.LittleEndian.Uint64(rec[40:])); s > 0 {
		c.Expires = macEpoch.Add(time.Duration(s * float64(time.Second)))
	}
	c.Domain = strings.TrimSpace(c.Domain)
	return c, c.Name != ""
}
