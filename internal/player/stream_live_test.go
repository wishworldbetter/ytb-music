package player

import (
	"os"
	"testing"
	"time"

	"ytb-music/internal/ytm"
)

// A song from YouTube plays from its first range, and a seek far into it
// plays as soon as the bytes there have come.
func TestStreamLive(t *testing.T) {
	if os.Getenv("LIVE") == "" {
		t.Skip()
	}
	st, err := ytm.SongURL("kJQP7kiw5Fk", "standard")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	dl := startDownload(st.URL, st.Size, nil, nil)
	s, err := openStream(dl, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("opened in", time.Since(start), s.SampleRate(), s.Len())
	buf := make([]float32, 4096)
	if n, err := s.Read(buf); n == 0 || err != nil {
		t.Fatal("read", n, err)
	}
	t.Log("first samples in", time.Since(start))
	if err := s.SetPosition(int64(s.SampleRate()) * 120); err != nil {
		t.Fatal(err)
	}
	total := 0
	for range 200 {
		n, err := s.Read(buf)
		total += n
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Log("after seek read", total, "in", time.Since(start), "progress", dl.Progress())
	s.Close()
}
