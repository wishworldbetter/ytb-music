package ytm

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// signInFromFile sets the cookies of the file YTM_COOKIE_FILE names, as the
// app writes them with YTM_COOKIE_DUMP, or skips the test.
func signInFromFile(t *testing.T) {
	t.Helper()
	path := os.Getenv("YTM_COOKIE_FILE")
	if os.Getenv("LIVE") == "" || path == "" {
		t.Skip("needs LIVE and YTM_COOKIE_FILE")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !SetCookies(strings.TrimSpace(string(b))) {
		t.Fatal("the cookies are nobody's")
	}
	t.Cleanup(ClearSession)
}

// Signed in, the library's pages list the listener's playlists, the songs
// they like, what they played and whom they follow, and the supermix
// plays on.
func TestLiveAccount(t *testing.T) {
	signInFromFile(t)
	p, err := Account()
	if err != nil || p == nil {
		t.Fatalf("account: %+v, %v", p, err)
	}
	t.Logf("account: %q, id %q, avatar %v", p.Nickname, p.UserID, p.AvatarURL != "")

	made, saved, err := UserPlaylists(p.UserID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("playlists: %d made, %d saved", len(made), len(saved))
	for i, pl := range made[:min(len(made), 4)] {
		t.Logf("  made %d: %s %q special=%d creator=%q", i, pl.ID, pl.Name, pl.SpecialType, pl.Creator.Nickname)
	}
	for i, pl := range saved[:min(len(saved), 3)] {
		t.Logf("  saved %d: %s %q creator=%q", i, pl.ID, pl.Name, pl.Creator.Nickname)
	}

	liked, err := LikedIDs(p.UserID)
	t.Logf("liked: %d ids, %v", len(liked), err)

	plays, err := RecentPlays(20)
	t.Logf("history: %d, %v", len(plays), err)
	for _, pl := range plays[:min(len(plays), 3)] {
		t.Logf("  %s %q %s", pl.Track.ID, pl.Track.Name, pl.At.Format("01-02 15:04"))
	}

	artists, err := FollowedArtists()
	t.Logf("followed artists: %d, %v", len(artists), err)

	fm, err := PersonalFM(FMDefault)
	t.Logf("supermix: %d, %v", len(fm), err)
	more, err := PersonalFM(FMDefault)
	t.Logf("supermix more: %d, %v", len(more), err)

	daily, err := DailySongs()
	t.Logf("quick picks: %d, %v", len(daily), err)
	for _, s := range daily[:min(len(daily), 3)] {
		t.Logf("  %q — %s", s.Name, s.ArtistNames())
	}
}

// Signed in with Premium, a song asked for at "exhigh" streams AAC at 256
// kbps, its URL unscrambled, from the player worked out once and kept.
func TestLivePremiumStream(t *testing.T) {
	signInFromFile(t)
	PlayerCacheDir = t.TempDir()
	defer func() { PlayerCacheDir = "" }()
	for i, id := range []string{"kJQP7kiw5Fk", "FXovf5dsRTw"} {
		start := time.Now()
		s, err := SongURL(id, "exhigh")
		if err != nil {
			t.Fatal(err)
		}
		if s.Level != "exhigh" {
			t.Skip("the account has no Premium")
		}
		t.Logf("%s: %d bytes, found in %v", id, s.Size, time.Since(start))
		for _, r := range []string{"0-65535", fmt.Sprintf("%d-%d", s.Size/2, s.Size/2+65535)} {
			body, code, err := get(s.URL+"&range="+r, UserAgent)
			if err != nil || code != 200 || len(body) != 65536 {
				t.Fatalf("song %d, range %s: %d, %d bytes, %v", i, r, code, len(body), err)
			}
		}
	}
	// Opened again, the player comes from the cache, not worked out anew.
	solvers.mu.Lock()
	solvers.cur.ctx.Close()
	solvers.cur, solvers.checked = nil, time.Time{}
	solvers.mu.Unlock()
	start := time.Now()
	if _, err := currentSolver(); err != nil {
		t.Fatal(err)
	}
	t.Logf("player from the cache in %v", time.Since(start))
}
