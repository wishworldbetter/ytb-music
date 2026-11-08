package ytm

import "testing"

// The listener's own page is who their account says they are, without a
// channel to browse: it failed to load, the app asking YouTube Music for
// a user as it asked NetEase.
func TestOwnUserDetail(t *testing.T) {
	rememberMe(&Profile{UserID: "@star", Nickname: "star", AvatarURL: "https://yt3.ggpht.com/a"})
	defer rememberMe(nil)
	u, err := UserDetail("@star")
	if err != nil || u.Nickname != "star" || u.AvatarURL == "" {
		t.Fatalf("%+v, %v", u, err)
	}
	if _, err := UserDetail("@someone"); err == nil {
		t.Fatal("someone else's handle loaded")
	}
}
