package player

import (
	"context"
	"errors"
	"io"
	"math"
	"math/cmplx"
	"net/http"
	"os"
	"sync"

	"github.com/egoist/mygo/audio"

	"ytb-music/internal/ytm"
)

// A download fetches a song as it plays: readers wait for the bytes they
// need, so playback starts with the first ones. The song comes into a file
// of the cache, which keeps it once it has all come in, and is read from
// there: the system holds what of it was read lately, rather than the
// app's memory, as much again as the song. Songs the cache would not keep
// come into memory.
type download struct {
	mu     sync.Mutex
	cond   sync.Cond
	file   *os.File // where the song is, or nil: then data
	data   []byte
	n      int64 // the bytes in so far
	size   int64 // expected, or 0 if unknown
	done   bool
	err    error
	cancel context.CancelFunc
	// part is the file's path until the cache keeps it, which keep does
	// once the song has all come in: the path it is at then.
	part string
	keep func(part string, n int64) (string, bool)
	// users counts what reads or writes the song: the stream decoding it,
	// until it closes, and the goroutine fetching it. The last to go
	// closes the file, and removes it unless the cache kept it.
	users  int
	closed bool
}

// startDownload fetches url, of size bytes if known, into part, a file
// that keep keeps once it has all come in: or into memory, without one.
func startDownload(url string, size int64, part *os.File, keep func(string, int64) (string, bool)) *download {
	ctx, cancel := context.WithCancel(context.Background())
	d := &download{size: size, cancel: cancel, users: 2}
	d.cond.L = &d.mu
	switch {
	case part != nil:
		d.file, d.part, d.keep = part, part.Name(), keep
	case size > 0:
		d.data = make([]byte, 0, prealloc(size))
	}
	go d.run(ctx, url)
	return d
}

// maxPrealloc is the most memory set aside for a song before its bytes
// come in: the size it is said to be is the API's word, which a song that
// turns out longer only outgrows.
const maxPrealloc = 32 << 20

func prealloc(size int64) int64 { return min(size, maxPrealloc) }

// downloaded returns a download that has all of b in already.
func downloaded(b []byte) *download {
	d := &download{data: b, n: int64(len(b)), size: int64(len(b)), done: true, cancel: func() {}, users: 1}
	d.cond.L = &d.mu
	return d
}

// opened returns a download that has all of the file f in already, of
// size bytes, as a song from the cache.
func opened(f *os.File, size int64) *download {
	d := &download{file: f, n: size, size: size, done: true, cancel: func() {}, users: 1}
	d.cond.L = &d.mu
	return d
}

func (d *download) run(ctx context.Context, url string) {
	defer d.release()
	finish := func(err error) {
		d.mu.Lock()
		d.done, d.err = true, err
		d.cond.Broadcast()
		d.mu.Unlock()
	}
	if d.size > 0 && ranged(url) {
		err := d.fetchRanges(ctx, url)
		if err == nil && d.part != "" && d.n == d.size {
			d.hand()
		}
		finish(err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		finish(err)
		return
	}
	req.Header.Set("User-Agent", ytm.UserAgent)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		finish(err)
		return
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		finish(errors.New(res.Status))
		return
	}
	buf := make([]byte, 64<<10)
	for {
		n, err := res.Body.Read(buf)
		if n > 0 {
			d.write(buf[:n])
		}
		if err == io.EOF {
			// Whole if as long as promised: the cache's.
			if d.part != "" && (d.size <= 0 || d.n == d.size) {
				d.hand()
			}
			finish(nil)
			return
		}
		if err != nil {
			finish(err)
			return
		}
	}
}

// write adds b to what is in. Only run writes, so it reads the file and
// what is in without the lock, which guards them from readers.
func (d *download) write(b []byte) {
	if d.file != nil {
		if _, err := d.file.Write(b); err != nil {
			d.toMemory()
		}
	}
	d.mu.Lock()
	if d.file == nil {
		d.data = append(d.data, b...)
	}
	d.n += int64(len(b))
	d.cond.Broadcast()
	d.mu.Unlock()
}

// toMemory moves the song into memory from a file that takes no more, as
// on a full disk: it plays on, but is not kept.
func (d *download) toMemory() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.data = make([]byte, d.n, max(prealloc(d.size), d.n))
	got, _ := d.file.ReadAt(d.data, 0)
	d.data = d.data[:got]
	d.n = int64(got)
	d.file.Close()
	os.Remove(d.part)
	d.file, d.part = nil, ""
}

// hand gives the song, all in, to the cache, and reads it from where it
// is kept. Windows moves no file that is open: it closes the file first,
// and opens it again where it went, while no one reads it.
func (d *download) hand() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.file.Close()
	path, ok := d.keep(d.part, d.n)
	f, err := os.Open(path)
	if err != nil {
		d.file, d.n, d.done, d.err = nil, 0, true, err
		return
	}
	d.file = f
	if ok {
		d.part = ""
	}
}

// Close stops the download and wakes its readers: the stream lets the
// song go.
func (d *download) Close() {
	d.cancel()
	d.mu.Lock()
	if !d.done {
		d.done, d.err = true, context.Canceled
	}
	d.cond.Broadcast()
	closed := d.closed
	d.closed = true
	d.mu.Unlock()
	if !closed {
		d.release()
	}
}

// share counts one more user of the song, as a second stream decoding
// it, unless it is gone already.
func (d *download) share() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.users <= 0 {
		return false
	}
	d.users++
	return true
}

// release lets the song go for one of its users. With none left, nothing
// reads it any more: its file closes, and goes unless the cache kept it.
func (d *download) release() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.users--; d.users > 0 {
		return
	}
	if d.file != nil {
		d.file.Close()
	}
	if d.part != "" {
		os.Remove(d.part)
	}
	d.file, d.data, d.n, d.part = nil, nil, 0, ""
}

// Progress returns the share of the song downloaded so far.
func (d *download) Progress() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.done && d.err == nil {
		return 1
	}
	if d.size <= 0 {
		return 0
	}
	return float64(d.n) / float64(d.size)
}

// waitFor waits until n bytes are in, or the download ended, and returns
// how many there are.
func (d *download) waitFor(n int64) int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	for d.n < n && !d.done {
		d.cond.Wait()
	}
	return d.n
}

// readAt reads into b what is in from off, as much as fits. Its caller
// holds the lock.
func (d *download) readAt(b []byte, off int64) (int, error) {
	n := min(int64(len(b)), d.n-off)
	if n <= 0 {
		return 0, nil
	}
	if d.file == nil {
		return copy(b, d.data[off:d.n]), nil
	}
	return d.file.ReadAt(b[:n], off)
}

// peek returns up to n bytes from off, as far as they are in.
func (d *download) peek(off int64, n int) []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	b := make([]byte, n)
	got, _ := d.readAt(b, off)
	return b[:got]
}

// A stream decodes a download for package audio, and measures the
// spectrum of what it decodes, for the visualizers.
type stream interface {
	audio.Decoder
	Close() error
	// source returns the download decoded, and its spectrum.
	source() (*download, *spectrum)
}

// openStream opens the song dl fetches: AAC in MP4, as YouTube serves it.
// durationMs is what the catalogue says it lasts, which the file's own
// index says better.
func openStream(dl *download, durationMs int64) (stream, error) {
	return openM4AStream(dl)
}

// openShared opens a second stream of what dl fetches, for decoding apart
// from what plays: it measures no spectrum, and closing it lets the song
// go for it alone, leaving the download to the stream that plays.
func openShared(dl *download, durationMs int64) (stream, error) {
	if !dl.share() {
		return nil, errors.New("player: the song is gone")
	}
	s, err := openStream(dl, durationMs)
	if err != nil {
		dl.release()
		return nil, err
	}
	if s, ok := s.(*m4aStream); ok {
		s.spec, s.drop = nil, dl.release
	}
	return s, nil
}

// The spectrum of a song, measured in chunks of fftSize frames as it
// decodes, a second ahead of what plays: the visualizers look up the
// chunk playing.

const (
	fftSize = 1024
	Bands   = 24
)

type spectrum struct {
	mu     sync.RWMutex
	rate   int
	chunks [][Bands]float32
	have   []bool
	window [fftSize]float32
	acc    []float32 // mono samples of the chunk being filled
	accAt  int64     // its first frame
	edges  [Bands + 1]int
	fft    []complex128
}

func newSpectrum(rate int, frames int64) *spectrum {
	n := int(frames/fftSize) + 2
	s := &spectrum{rate: rate, chunks: make([][Bands]float32, n), have: make([]bool, n), accAt: -1}
	for i := range s.window {
		s.window[i] = float32(0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(fftSize-1)))
	}
	// Bands spaced evenly in pitch, from 40 Hz to 14 kHz.
	lo, hi := math.Log(40), math.Log(14000)
	for i := range s.edges {
		f := math.Exp(lo + (hi-lo)*float64(i)/Bands)
		s.edges[i] = max(1, min(fftSize/2-1, int(f*fftSize/float64(rate))))
	}
	for i := 1; i < len(s.edges); i++ {
		s.edges[i] = max(s.edges[i], s.edges[i-1]+1)
	}
	s.fft = make([]complex128, fftSize)
	return s
}

// feed takes stereo samples decoded from frame at, unless the stream
// measures no spectrum.
func (s *spectrum) feed(at int64, stereo []float32) {
	if s == nil {
		return
	}
	if s.accAt < 0 || at != s.accAt+int64(len(s.acc)) {
		// The start, or a seek: begin a chunk at a chunk's boundary.
		s.acc = s.acc[:0]
		skip := (fftSize - at%fftSize) % fftSize
		if int(skip)*2 >= len(stereo) {
			s.accAt = -1
			return
		}
		stereo = stereo[skip*2:]
		at += skip
		s.accAt = at
	}
	for i := 0; i+1 < len(stereo); i += 2 {
		s.acc = append(s.acc, (stereo[i]+stereo[i+1])/2)
		if len(s.acc) == fftSize {
			s.analyze(s.accAt / fftSize)
			s.accAt += fftSize
			s.acc = s.acc[:0]
		}
	}
}

func (s *spectrum) analyze(chunk int64) {
	if chunk < 0 || chunk >= int64(len(s.chunks)) {
		return
	}
	for i, v := range s.acc {
		s.fft[i] = complex(float64(v*s.window[i]), 0)
	}
	fft(s.fft)
	var bands [Bands]float32
	for b := range Bands {
		var sum float64
		for k := s.edges[b]; k < s.edges[b+1]; k++ {
			sum += cmplx.Abs(s.fft[k])
		}
		mag := sum / float64(s.edges[b+1]-s.edges[b]) / (fftSize / 4)
		// Higher bands carry less energy: tilt them up, as the ear hears.
		mag *= 1 + float64(b)/6
		db := 20 * math.Log10(mag+1e-9)
		bands[b] = float32(math.Max(0, math.Min(1, (db+60)/52)))
	}
	s.mu.Lock()
	s.chunks[chunk] = bands
	s.have[chunk] = true
	s.mu.Unlock()
}

// at returns the bands of frame, or false if not measured.
func (s *spectrum) at(frame int64) ([Bands]float32, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := frame / fftSize
	if c < 0 || c >= int64(len(s.chunks)) || !s.have[c] {
		return [Bands]float32{}, false
	}
	return s.chunks[c], true
}

// fft transforms x in place; len(x) is a power of two.
func fft(x []complex128) {
	n := len(x)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			wk := complex(1, 0)
			for k := range size / 2 {
				a, b := x[start+k], x[start+k+size/2]*wk
				x[start+k], x[start+k+size/2] = a+b, a-b
				wk *= w
			}
		}
	}
}
