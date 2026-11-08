package player

import (
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"ytb-music/internal/ytm"
)

// decodeAll decodes the whole of a song from its start.
func decodeAll(t *testing.T, b []byte) []float32 {
	t.Helper()
	s, err := openStream(downloaded(b), 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []float32
	buf := make([]float32, 4096)
	for {
		n, err := s.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil || n == 0 {
			return out
		}
	}
}

// maxDiff returns how far apart a and b are at most, over the length of
// the shorter.
func maxDiff(a, b []float32) float64 {
	d := 0.0
	for i := range min(len(a), len(b)) {
		d = math.Max(d, math.Abs(float64(a[i]-b[i])))
	}
	return d
}

// A seek lands on the frame asked for: what plays from there is what
// plays there when the song plays through.
func TestM4ASeek(t *testing.T) {
	b, err := os.ReadFile("testdata/seek.m4a")
	if err != nil {
		t.Fatal(err)
	}
	ref := decodeAll(t, b)
	s, err := openStream(downloaded(b), 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.SampleRate() != 44100 || s.Len()*2 != int64(len(ref)) {
		t.Fatalf("%d Hz, %d frames, though %d decode", s.SampleRate(), s.Len(), len(ref)/2)
	}
	buf := make([]float32, 4096)
	for i := 1; i < 20; i++ {
		frame := s.Len() * int64(i) / 21
		if err := s.SetPosition(frame); err != nil {
			t.Fatal(err)
		}
		n, _ := s.Read(buf)
		// AAC's noise substitution and the overlap of the packet before
		// leave it a little off the decode through.
		if d := maxDiff(buf[:n], ref[frame*2:]); d > 0.05 {
			t.Errorf("seek to frame %d: off by up to %.3f", frame, d)
		}
	}
}

// A song from YouTube comes a range at a time, the first small so that it
// plays at once, and a range that breaks off goes on from where it broke.
func TestRangedDownload(t *testing.T) {
	b := make([]byte, firstRange+3*rangeSize+12345)
	for i := range b {
		b[i] = byte(i * 7)
	}
	var asked []string
	broke := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rg := r.URL.Query().Get("range")
		asked = append(asked, rg)
		from, to, ok := strings.Cut(rg, "-")
		if !ok {
			// As googlevideo does for a song asked for whole.
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		f, _ := strconv.Atoi(from)
		e, _ := strconv.Atoi(to)
		part := b[f : e+1]
		w.Header().Set("Content-Length", strconv.Itoa(len(part)))
		if !broke && f > 0 {
			// The first big range breaks off halfway.
			broke = true
			w.Write(part[:len(part)/2])
			return
		}
		w.Write(part)
	}))
	defer srv.Close()
	d := &download{size: int64(len(b)), users: 2, data: make([]byte, 0, len(b))}
	d.cond.L = &d.mu
	if err := d.fetchRanges(t.Context(), srv.URL+"/videoplayback?x=1"); err != nil {
		t.Fatal(err)
	}
	if string(d.data) != string(b) {
		t.Fatalf("got %d bytes, not the song", len(d.data))
	}
	if asked[0] != "0-"+strconv.Itoa(firstRange-1) {
		t.Errorf("asked first for %s", asked[0])
	}
	if len(asked) < 6 {
		t.Errorf("asked for %v: the broken range wasn't asked for again", asked)
	}
}

// A song plays as it streams into a file of the cache, rather than the
// app's memory, and the cache keeps it once it has all come in; one cut
// short leaves no file behind.
func TestStreamIntoCache(t *testing.T) {
	b, err := os.ReadFile("testdata/seek.m4a")
	if err != nil {
		t.Fatal(err)
	}
	half := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cut" {
			w.Header().Set("Content-Length", strconv.Itoa(len(b)))
			w.Write(b[:len(b)/2])
			w.(http.Flusher).Flush()
			<-half
			return
		}
		w.Write(b)
	}))
	defer srv.Close()
	defer close(half)
	dir := t.TempDir()
	c := NewCache(dir, 1<<30)
	ref := decodeAll(t, b)

	st := &ytm.Stream{URL: srv.URL + "/whole", Size: int64(len(b)), Level: "standard", Type: "m4a"}
	s, err := openStream(c.download("1", st), 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []float32
	buf := make([]float32, 4096)
	for {
		n, err := s.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil || n == 0 {
			break
		}
	}
	s.Close()
	if d := maxDiff(got, ref); len(got) != len(ref) || d != 0 {
		t.Errorf("played %d of %d samples, off by up to %.4f", len(got), len(ref), d)
	}
	if dl, _, ok := c.lookup("1", "standard"); !ok {
		t.Error("not kept")
	} else {
		dl.Close()
	}

	cut := c.download("2", &ytm.Stream{URL: srv.URL + "/cut", Size: int64(len(b)), Level: "standard", Type: "m4a"})
	cut.waitFor(int64(len(b) / 2))
	cut.Close()
	for start := time.Now(); time.Since(start) < 2*time.Second; time.Sleep(10 * time.Millisecond) {
		if parts, _ := filepath.Glob(filepath.Join(dir, "*.part")); len(parts) == 0 {
			break
		}
	}
	if parts, _ := filepath.Glob(filepath.Join(dir, "*.part")); len(parts) != 0 {
		t.Errorf("left %v", parts)
	}
	if _, _, ok := c.lookup("2", "standard"); ok {
		t.Error("kept a song cut short")
	}
}

// A second stream of the song, as transcribing an episode opens, decodes
// it as the one that plays does. Closing it leaves the download to the
// one that plays, and the one that plays closing leaves it what is in.
func TestSharedStream(t *testing.T) {
	b, err := os.ReadFile("testdata/seek.m4a")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(b) }))
	defer srv.Close()
	c := NewCache(t.TempDir(), 1<<30)
	ref := decodeAll(t, b)
	ms := int64(len(ref)/2) * 1000 / 44100
	readAll := func(s stream) []float32 {
		var got []float32
		buf := make([]float32, 4096)
		for {
			n, err := s.Read(buf)
			got = append(got, buf[:n]...)
			if err != nil || n == 0 {
				return got
			}
		}
	}

	dl := c.download("1", &ytm.Stream{URL: srv.URL, Size: int64(len(b)), Level: "standard", Type: "m4a"})
	playing, err := openStream(dl, ms)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := openShared(dl, ms)
	if err != nil {
		t.Fatal(err)
	}
	shared.Close()
	if got := readAll(playing); len(got) != len(ref) || maxDiff(got, ref) != 0 {
		t.Errorf("played %d of %d samples after the second stream closed", len(got), len(ref))
	}
	playing.Close()

	kept, _, ok := c.lookup("1", "standard")
	if !ok {
		t.Fatal("not kept")
	}
	playing, _ = openStream(kept, ms)
	shared, err = openShared(kept, ms)
	if err != nil {
		t.Fatal(err)
	}
	playing.Close()
	if got := readAll(shared); len(got) != len(ref) || maxDiff(got, ref) != 0 {
		t.Errorf("decoded %d of %d samples after the stream playing closed", len(got), len(ref))
	}
	shared.Close()
	if _, err := openShared(kept, ms); err == nil {
		t.Error("shared a song let go")
	}
}

// A stream whose level would lead out of the cache's folder is played but
// not kept. The level named the file, taken as the API sent it, so a level
// of "/../../x" wrote the song and its JSON beside the cache instead.
func TestCacheKeepsOnlyKnownLevels(t *testing.T) {
	b, err := os.ReadFile("testdata/seek.m4a")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(b) }))
	defer srv.Close()
	root := t.TempDir()
	dir := filepath.Join(root, "cache")
	c := NewCache(dir, 1<<30)

	dl := c.download("1", &ytm.Stream{URL: srv.URL, Size: int64(len(b)), Level: "/../../pwned", Type: "m4a"})
	dl.waitFor(int64(len(b)))
	dl.Close()
	for start := time.Now(); time.Since(start) < 2*time.Second; time.Sleep(10 * time.Millisecond) {
		if parts, _ := filepath.Glob(filepath.Join(dir, "*.part")); len(parts) == 0 {
			break
		}
	}
	var outside []string
	filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() && filepath.Dir(path) != dir {
			outside = append(outside, path)
		}
		return nil
	})
	if len(outside) != 0 {
		t.Errorf("wrote %v outside the cache", outside)
	}
	if songs := c.songs(); len(songs) != 0 {
		t.Errorf("kept %v", songs)
	}
}

// A song the cache won't keep sets aside no more memory than maxPrealloc,
// whatever size the API says it is, which it set aside in full before.
func TestStreamSizeIsNotTrusted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("short")) }))
	defer srv.Close()
	dl := startDownload(srv.URL, 1<<40, nil, nil)
	defer dl.Close()
	dl.mu.Lock()
	n := cap(dl.data)
	dl.mu.Unlock()
	if n > maxPrealloc {
		t.Errorf("set aside %d bytes", n)
	}
}

// A song's better stream that won't open, its URL unscrambled wrong once
// YouTube changed its player, gives way to its standard one, which plays.
func TestBetterStreamGivesWay(t *testing.T) {
	b, err := os.ReadFile("testdata/seek.m4a")
	if err != nil {
		t.Fatal(err)
	}
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(b) }))
	defer good.Close()
	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer refused.Close()
	was := songURL
	defer func() { songURL = was }()
	songURL = func(id, level string) (*ytm.Stream, error) {
		if level == "exhigh" {
			return &ytm.Stream{URL: refused.URL, Size: int64(len(b)), Type: "m4a", Level: "exhigh"}, nil
		}
		return &ytm.Stream{URL: good.URL, Size: int64(len(b)), Type: "m4a", Level: "standard"}, nil
	}
	p := &Player{}
	info, s, err := p.open(ytm.Track{ID: "1"}, "exhigh")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if info.Level != "standard" {
		t.Errorf("plays the %s stream", info.Level)
	}
	if n, err := s.Read(make([]float32, 4096)); n == 0 || err != nil {
		t.Errorf("read %d, %v", n, err)
	}
}
