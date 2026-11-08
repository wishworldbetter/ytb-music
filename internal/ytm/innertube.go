package ytm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"
)

// Language and Region are what YouTube Music answers in and for: the
// language of its words ("en", "zh-CN") and the country whose catalogue
// and charts it shows ("US"). Set them before the first request.
var (
	Language = "en"
	Region   = "US"
)

const (
	musicAPI   = "https://music.youtube.com/youtubei/v1/"
	playerAPI  = "https://www.youtube.com/youtubei/v1/player?prettyPrint=false"
	remixName  = "WEB_REMIX"
	remixID    = "67"
	remixVer   = "1.20251020.01.00"
	visionName = "VISIONOS"
	visionID   = "101"
	visionVer  = "0.1"
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

// visitor is the visitorData that anonymous requests carry, as a browser
// that has been to YouTube before: fetched once, shared, and fetched again
// when it gets old or YouTube starts refusing it.
var visitor struct {
	mu         sync.Mutex
	data       string
	at         time.Time
	refreshing bool
}

var visitorRe = regexp.MustCompile(`"(Cg[A-Za-z0-9_%-]{10,})"`)

func visitorData() string {
	visitor.mu.Lock()
	if visitor.data != "" {
		// Old, it serves on while another is fetched: no request waits
		// for one but the first.
		if time.Since(visitor.at) >= 6*time.Hour && !visitor.refreshing {
			visitor.refreshing = true
			go refreshVisitorData()
		}
		d := visitor.data
		visitor.mu.Unlock()
		return d
	}
	defer visitor.mu.Unlock()
	if d, err := fetchVisitorData(); err == nil {
		visitor.data, visitor.at = d, time.Now()
	}
	return visitor.data
}

func refreshVisitorData() {
	d, err := fetchVisitorData()
	visitor.mu.Lock()
	if err == nil {
		visitor.data, visitor.at = d, time.Now()
	}
	visitor.refreshing = false
	visitor.mu.Unlock()
}

// VisitorData returns the visitorData requests carry and when it was
// fetched, for the app to keep for its next run, or "" before the first
// request.
func VisitorData() (string, time.Time) {
	visitor.mu.Lock()
	defer visitor.mu.Unlock()
	return visitor.data, visitor.at
}

// SetVisitorData has requests carry data, the visitorData an earlier run
// kept, found at, rather than fetch one before the first request, which
// costs a round trip; once old, another is fetched meanwhile.
func SetVisitorData(data string, at time.Time) {
	if data == "" {
		return
	}
	visitor.mu.Lock()
	if visitor.data == "" {
		visitor.data, visitor.at = data, at
	}
	visitor.mu.Unlock()
}

// dropVisitorData forgets data, if it is still the visitorData in use, so
// the next request fetches another.
func dropVisitorData(data string) {
	visitor.mu.Lock()
	if visitor.data == data {
		visitor.data = ""
	}
	visitor.mu.Unlock()
}

// resetVisitorData forgets the visitorData in use.
func resetVisitorData() {
	visitor.mu.Lock()
	visitor.data = ""
	visitor.mu.Unlock()
}

func fetchVisitorData() (string, error) {
	req, err := http.NewRequest("GET", "https://www.youtube.com/sw.js_data", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("ytm: visitor data: %s", resp.Status)
	}
	m := visitorRe.FindSubmatch(body)
	if m == nil {
		return "", errors.New("ytm: no visitor data")
	}
	return string(m[1]), nil
}

// call posts body to a WEB_REMIX endpoint of music.youtube.com, as its
// web player does, and returns the JSON it answers.
func call(endpoint string, body map[string]any) (any, error) {
	return post(musicAPI+endpoint+"?prettyPrint=false", body, func(vd string) (map[string]any, http.Header) {
		client := map[string]any{"clientName": remixName, "clientVersion": remixVer, "hl": Language, "gl": Region}
		if vd != "" {
			client["visitorData"] = vd
		}
		h := http.Header{}
		h.Set("Origin", "https://music.youtube.com")
		h.Set("Referer", "https://music.youtube.com/")
		h.Set("X-YouTube-Client-Name", remixID)
		h.Set("X-YouTube-Client-Version", remixVer)
		signRequest(h, "https://music.youtube.com")
		return client, h
	})
}

// callPlayer asks the visionOS client for a video's streams: its formats
// come as plain URLs that serve whole.
func callPlayer(id string) (any, error) {
	body := map[string]any{"videoId": id, "contentCheckOk": true, "racyCheckOk": true}
	return post(playerAPI, body, func(vd string) (map[string]any, http.Header) {
		client := map[string]any{
			"clientName": visionName, "clientVersion": visionVer, "hl": Language, "gl": Region,
			"deviceMake": "Apple", "deviceModel": "RealityDevice14,1",
			"osName": "visionOS", "osVersion": "1.3.21O771",
		}
		if vd != "" {
			client["visitorData"] = vd
		}
		h := http.Header{}
		h.Set("X-YouTube-Client-Name", visionID)
		h.Set("X-YouTube-Client-Version", visionVer)
		return client, h
	})
}

// post sends body with the context client makes, and tries once more with
// fresh visitorData if YouTube refuses the one it had.
func post(url string, body map[string]any, client func(vd string) (map[string]any, http.Header)) (any, error) {
	var err error
	for try := 0; try < 2; try++ {
		vd := visitorData()
		c, h := client(vd)
		payload := make(map[string]any, len(body)+1)
		for k, v := range body {
			payload[k] = v
		}
		payload["context"] = map[string]any{"client": c}
		var v any
		var status int
		v, status, err = postJSON(url, payload, h, vd)
		if err == nil {
			return v, nil
		}
		if status != 401 && status != 403 {
			break
		}
		dropVisitorData(vd)
	}
	return nil, err
}

func postJSON(url string, payload any, h http.Header, vd string) (any, int, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		return nil, 0, err
	}
	for k, vs := range h {
		req.Header[k] = vs
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	if vd != "" {
		req.Header.Set("X-Goog-Visitor-Id", vd)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	var v any
	jerr := json.Unmarshal(data, &v)
	if resp.StatusCode != 200 {
		if msg := str(v, "error", "message"); msg != "" {
			return nil, resp.StatusCode, fmt.Errorf("ytm: %s: %s", resp.Status, msg)
		}
		return nil, resp.StatusCode, fmt.Errorf("ytm: %s", resp.Status)
	}
	if jerr != nil {
		return nil, resp.StatusCode, fmt.Errorf("ytm: %w", jerr)
	}
	return v, resp.StatusCode, nil
}

// get fetches url with the app's User-Agent and returns its body, which
// must come with status 200.
func get(url string, ua string) ([]byte, int, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", ua)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode != 200 {
		return nil, resp.StatusCode, fmt.Errorf("ytm: GET %s: %s", url, resp.Status)
	}
	return body, resp.StatusCode, nil
}

// ttlCache keeps values for a while, up to max of them, sharing one fetch
// among the callers that ask for the same key at once.
type ttlCache[V any] struct {
	mu      sync.Mutex
	ttl     time.Duration
	max     int
	entries map[string]*cacheEntry[V]
}

type cacheEntry[V any] struct {
	mu  sync.Mutex
	at  time.Time
	ok  bool
	val V
}

func newCache[V any](ttl time.Duration, max int) *ttlCache[V] {
	return &ttlCache[V]{ttl: ttl, max: max, entries: map[string]*cacheEntry[V]{}}
}

// get returns the value of key, fetching it if it is missing or old. A
// failed fetch isn't kept.
func (c *ttlCache[V]) get(key string, fetch func() (V, error)) (V, error) {
	c.mu.Lock()
	e := c.entries[key]
	if e == nil {
		if len(c.entries) >= c.max {
			c.evict()
		}
		e = &cacheEntry[V]{}
		c.entries[key] = e
	}
	c.mu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ok && time.Since(e.at) < c.ttl {
		return e.val, nil
	}
	v, err := fetch()
	if err != nil {
		var zero V
		return zero, err
	}
	e.val, e.at, e.ok = v, time.Now(), true
	return v, nil
}

// evict drops the oldest entry, or an unfilled one; c.mu is held.
func (c *ttlCache[V]) evict() {
	var oldest string
	var at time.Time
	for k, e := range c.entries {
		if !e.mu.TryLock() {
			continue
		}
		t, ok := e.at, e.ok
		e.mu.Unlock()
		if !ok {
			t = time.Time{}
		}
		if oldest == "" || t.Before(at) {
			oldest, at = k, t
		}
	}
	if oldest != "" {
		delete(c.entries, oldest)
	}
}

// clear drops every value kept.
func (c *ttlCache[V]) clear() {
	c.mu.Lock()
	c.entries = map[string]*cacheEntry[V]{}
	c.mu.Unlock()
}

// clearCaches drops what was kept of the catalogue: signed in or out, the
// home page, the charts and the artists' pages are another listener's.
func clearCaches() {
	homeCache.clear()
	homeFirstCache.clear()
	previewCache.clear()
	chartCache.clear()
	artistCache.clear()
	discoCache.clear()
	premiumCache.clear()
	forgetPremium()
	searches.mu.Lock()
	searches.m = nil
	searches.mu.Unlock()
}
