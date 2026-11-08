package ytm

import (
	"net/url"
	"testing"
)

// A web client's format plays from its URL with the signature its cipher
// scrambled put back under the name the cipher says, and its n replaced by
// what the player makes of it; a URL with neither is taken as it is.
func TestStreamURL(t *testing.T) {
	solve := func(sig, n string) (string, string, error) { return "un" + sig, "un" + n, nil }
	cipher := url.Values{"s": {"AB=C"}, "sp": {"sig"}, "url": {"https://rr1.googlevideo.com/videoplayback?itag=141&n=xyz&ip=1"}}
	got, err := streamURL(map[string]any{"signatureCipher": cipher.Encode()}, solve)
	if err != nil {
		t.Fatal(err)
	}
	q := mustQuery(t, got)
	if q.Get("sig") != "unAB=C" || q.Get("n") != "unxyz" || q.Get("itag") != "141" || q.Get("ip") != "1" {
		t.Errorf("%s", got)
	}

	got, err = streamURL(map[string]any{"url": "https://rr1.googlevideo.com/videoplayback?itag=141&n=xyz"}, solve)
	if q := mustQuery(t, got); err != nil || q.Get("n") != "unxyz" || q.Has("sig") || q.Has("signature") {
		t.Errorf("%s, %v", got, err)
	}

	plain := "https://rr1.googlevideo.com/videoplayback?itag=140"
	if got, err := streamURL(map[string]any{"url": plain}, solve); err != nil || got != plain {
		t.Errorf("%s, %v", got, err)
	}
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}
