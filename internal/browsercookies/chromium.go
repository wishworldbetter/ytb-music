package browsercookies

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// chromium is a browser made of Chromium: where it keeps its profiles, in
// Application Support, the Keychain item of the password it encrypts
// cookies with, and where its app is.
type chromium struct {
	name, dir, keychain string
	apps                []string
}

var chromiums = []chromium{
	{"Chrome", "Google/Chrome", "Chrome Safe Storage", []string{"/Applications/Google Chrome.app"}},
	{"Edge", "Microsoft Edge", "Microsoft Edge Safe Storage", []string{"/Applications/Microsoft Edge.app"}},
	{"Arc", "Arc/User Data", "Arc Safe Storage", []string{"/Applications/Arc.app"}},
	{"Brave", "BraveSoftware/Brave-Browser", "Brave Safe Storage", []string{"/Applications/Brave Browser.app"}},
	{"Vivaldi", "Vivaldi", "Vivaldi Safe Storage", []string{"/Applications/Vivaldi.app"}},
	{"Chromium", "Chromium", "Chromium Safe Storage", []string{"/Applications/Chromium.app"}},
}

func chromiumNamed(name string) (chromium, bool) {
	for _, b := range chromiums {
		if b.name == name {
			return b, true
		}
	}
	return chromium{}, false
}

const sqlite = "/usr/bin/sqlite3"

// chromiumProfiles returns b's profiles that are signed in to YouTube, the
// one used last first.
func chromiumProfiles(b chromium) []Profile {
	root := appSupport(b.dir)
	app := installed(b.apps...)
	data, err := os.ReadFile(filepath.Join(root, "Local State"))
	switch {
	case denied(err):
		logf("%s: %v", b.name, err)
		return []Profile{{Browser: b.name, App: app, Dir: "access", NeedsAccess: true}}
	case err != nil:
		logf("%s: %v", b.name, err)
		if app != "" {
			return []Profile{{Browser: b.name, App: app, Dir: "none", SignedOut: true}}
		}
		return nil
	}
	var state struct {
		Profile struct {
			LastUsed  string `json:"last_used"`
			InfoCache map[string]struct {
				Name     string `json:"name"`
				UserName string `json:"user_name"`
			} `json:"info_cache"`
		} `json:"profile"`
	}
	if json.Unmarshal(data, &state) != nil {
		return nil
	}
	var out []Profile
	for dir, info := range state.Profile.InfoCache {
		db, err := cookieDB(root, dir)
		if err != nil {
			logf("%s %s: %v", b.name, dir, err)
			continue
		}
		n, err := query(db, `select count(*) from cookies where host_key in `+youtubeHostsSQL+` and name in ('SAPISID','__Secure-3PAPISID')`)
		if err != nil || strings.TrimSpace(n) == "0" {
			logf("%s %s: %q %v", b.name, dir, strings.TrimSpace(n), err)
			continue
		}
		out = append(out, Profile{Browser: b.name, App: app, Dir: dir, Name: info.Name, Email: info.UserName})
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Dir == state.Profile.LastUsed) != (out[j].Dir == state.Profile.LastUsed) {
			return out[i].Dir == state.Profile.LastUsed
		}
		return out[i].Name < out[j].Name
	})
	if len(out) == 0 && app != "" {
		return []Profile{{Browser: b.name, App: app, Dir: "none", SignedOut: true}}
	}
	return out
}

// cookieDB returns the path of the cookie database of profile dir in root.
func cookieDB(root, dir string) (string, error) {
	if dir == "" || strings.Contains(dir, "/") || strings.Contains(dir, "..") {
		return "", errors.New("browsercookies: bad profile")
	}
	for _, rel := range []string{"Cookies", filepath.Join("Network", "Cookies")} {
		p := filepath.Join(root, dir, rel)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", ErrNotFound
}

// query runs sql on a database a browser may hold open, read only: as an
// immutable file, which takes no lock.
func query(db, sql string) (string, error) {
	cmd := exec.Command(sqlite, "-readonly", "-separator", "\t", "file:"+db+"?immutable=1", sql)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("browsercookies: sqlite3: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

// chromiumCookies returns the YouTube cookies of b's profile dir,
// decrypted, with the key it asks the Keychain for.
func chromiumCookies(b chromium, dir string) ([]Cookie, error) {
	db, err := cookieDB(appSupport(b.dir), dir)
	if err != nil {
		return nil, err
	}
	version := 0
	if v, err := query(db, `select value from meta where key='version'`); err == nil {
		version, _ = strconv.Atoi(strings.TrimSpace(v))
	}
	rows, err := query(db, `select host_key, name, path, is_secure, is_httponly, samesite, expires_utc, hex(encrypted_value), hex(value) from cookies where host_key in `+youtubeHostsSQL)
	if err != nil {
		return nil, err
	}
	key, err := chromiumKey(b.keychain)
	if err != nil {
		return nil, err
	}
	var out []Cookie
	for _, line := range strings.Split(strings.TrimRight(rows, "\n"), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			continue
		}
		c := Cookie{Domain: f[0], Name: f[1], Path: f[2], Secure: f[3] == "1", HTTPOnly: f[4] == "1", SameSite: sameSite(f[5])}
		if us, err := strconv.ParseInt(f[6], 10, 64); err == nil && us > 0 {
			c.Expires = chromeTime(us)
		}
		enc, _ := hex.DecodeString(f[7])
		plain, _ := hex.DecodeString(f[8])
		if len(enc) > 0 {
			v, err := decrypt(key, enc, version)
			if err != nil {
				continue
			}
			plain = v
		}
		c.Value = string(plain)
		if c.Name != "" {
			out = append(out, c)
		}
	}
	return out, nil
}

// sameSite reads a SameSite stored as Chromium and Firefox store it.
func sameSite(v string) string {
	switch v {
	case "0":
		return "None"
	case "1":
		return "Lax"
	case "2":
		return "Strict"
	}
	return ""
}

// chromeTime is a time as Chromium keeps it, in microseconds since 1601:
// more than a time.Duration holds, which overflows past 292 years.
func chromeTime(us int64) time.Time {
	const since1601 = 11644473600 * 1_000_000 // to 1970, in microseconds
	return time.UnixMicro(us - since1601)
}

// chromiumKey derives the key a Chromium browser encrypts cookies with from
// the password it keeps in the Keychain, as item service.
func chromiumKey(service string) ([]byte, error) {
	cmd := exec.Command("/usr/bin/security", "find-generic-password", "-w", "-s", service)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrKeychain, strings.TrimSpace(stderr.String()))
	}
	password := strings.TrimSpace(out.String())
	return pbkdf2.Key(sha1.New, password, []byte("saltysalt"), 1003, 16)
}

// decrypt decrypts a value Chrome encrypted ("v10", AES-128-CBC with an IV
// of spaces). From version 24 of the database, the value starts with the
// SHA-256 of its domain, which it drops.
func decrypt(key, enc []byte, version int) ([]byte, error) {
	if !bytes.HasPrefix(enc, []byte("v10")) {
		return nil, errors.New("browsercookies: unknown encryption")
	}
	enc = enc[3:]
	if len(enc) == 0 || len(enc)%aes.BlockSize != 0 {
		return nil, errors.New("browsercookies: bad value")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	iv := bytes.Repeat([]byte(" "), aes.BlockSize)
	plain := make([]byte, len(enc))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, enc)
	pad := int(plain[len(plain)-1])
	if pad < 1 || pad > aes.BlockSize || pad > len(plain) {
		return nil, errors.New("browsercookies: bad padding")
	}
	plain = plain[:len(plain)-pad]
	if version >= 24 {
		if len(plain) < 32 {
			return nil, errors.New("browsercookies: bad value")
		}
		plain = plain[32:]
	}
	return plain, nil
}

// queryRW runs sql on a database of the app's own, a copy, which sqlite3
// may write the log beside it into.
func queryRW(db, sql string) (string, error) {
	cmd := exec.Command(sqlite, "-separator", "\t", db, sql)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("browsercookies: sqlite3: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

// hexString decodes what sqlite's hex() gave.
func hexString(h string) (string, error) {
	b, err := hex.DecodeString(h)
	return string(b), err
}
