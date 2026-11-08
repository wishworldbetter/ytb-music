package browsercookies

import (
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// binaryCookie encodes a cookie as Safari's Cookies.binarycookies keeps one.
func binaryCookie(domain, name, path, value string, flags uint32, expires time.Time) []byte {
	head := make([]byte, 56)
	strs := []string{domain, name, path, value}
	off := len(head)
	var body []byte
	offsets := make([]uint32, 4)
	for i, s := range strs {
		offsets[i] = uint32(off + len(body))
		body = append(body, append([]byte(s), 0)...)
	}
	binary.LittleEndian.PutUint32(head[0:], uint32(len(head)+len(body)))
	binary.LittleEndian.PutUint32(head[8:], flags)
	for i, o := range offsets {
		binary.LittleEndian.PutUint32(head[16+4*i:], o)
	}
	binary.LittleEndian.PutUint64(head[40:], math.Float64bits(expires.Sub(macEpoch).Seconds()))
	return append(head, body...)
}

// Safari's cookies read from its own format: YouTube's, with their flags
// and when they expire, and not other sites'.
func TestSafariCookies(t *testing.T) {
	exp := time.Date(2027, 3, 1, 12, 0, 0, 0, time.UTC)
	cookies := [][]byte{
		binaryCookie(".youtube.com", "SAPISID", "/", "abc/def", 1, exp),
		binaryCookie(".example.com", "id", "/", "x", 0, exp),
		binaryCookie("music.youtube.com", "PREF", "/", "hl=zh", 4, exp),
	}
	page := make([]byte, 8+4*len(cookies)+4)
	binary.BigEndian.PutUint32(page[0:], 0x100)
	binary.LittleEndian.PutUint32(page[4:], uint32(len(cookies)))
	off := len(page)
	for i, c := range cookies {
		binary.LittleEndian.PutUint32(page[8+4*i:], uint32(off))
		off += len(c)
	}
	for _, c := range cookies {
		page = append(page, c...)
	}
	file := []byte("cook")
	file = binary.BigEndian.AppendUint32(file, 1)
	file = binary.BigEndian.AppendUint32(file, uint32(len(page)))
	file = append(file, page...)

	home = t.TempDir()
	defer func() { home = os.Getenv("HOME") }()
	dir := filepath.Join(home, "Library", "Containers", "com.apple.Safari", "Data", "Library", "Cookies")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "Cookies.binarycookies"), file, 0o600)

	got, err := safariCookies()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "SAPISID" || got[0].Value != "abc/def" || !got[0].Secure || !got[0].Expires.Equal(exp) ||
		got[1].Name != "PREF" || !got[1].HTTPOnly || !signedIn(got) {
		t.Errorf("cookies %+v", got)
	}
}

// Firefox's default profile signed in to YouTube lists, and gives the
// cookies of its own container, not another container's sign-in.
func TestFirefoxCookies(t *testing.T) {
	home = t.TempDir()
	defer func() { home = os.Getenv("HOME") }()
	root := appSupport("Firefox")
	prof := filepath.Join(root, "Profiles", "abc.default-release")
	os.MkdirAll(prof, 0o700)
	os.WriteFile(filepath.Join(root, "profiles.ini"), []byte("[Install4F96D1932A9F858E]\nDefault=Profiles/abc.default-release\n\n[Profile0]\nName=default-release\nIsRelative=1\nPath=Profiles/abc.default-release\n"), 0o600)
	sql := `create table moz_cookies (id integer primary key, originAttributes text not null default '', name text, value text, host text, path text, expiry integer, lastAccessed integer, creationTime integer, isSecure integer, isHttpOnly integer, inBrowserElement integer default 0, sameSite integer default 0, rawSameSite integer default 0, schemeMap integer default 0);
insert into moz_cookies (originAttributes, name, value, host, path, expiry, isSecure, isHttpOnly, sameSite) values
 ('', 'SAPISID', 'mine', '.youtube.com', '/', 1803600000, 1, 0, 0),
 ('^userContextId=2', 'SAPISID', 'work', '.youtube.com', '/', 1803600000, 1, 0, 0),
 ('', 'SID', 'other', '.example.com', '/', 1803600000, 1, 1, 1);`
	if out, err := exec.Command(sqlite, filepath.Join(prof, "cookies.sqlite"), sql).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v: %s", err, out)
	}
	ps := firefoxProfiles()
	if len(ps) != 1 || ps[0].Dir != "Profiles/abc.default-release" || ps[0].Name != "default-release" {
		t.Fatalf("profiles %+v", ps)
	}
	got, err := firefoxCookies(ps[0].Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Value != "mine" || !got[0].Secure || got[0].Expires.Year() != 2027 {
		t.Errorf("cookies %+v", got)
	}
}
