package ytm

// What needs a YouTube account. The app plays without one for now: these
// answer as for nobody signed in, so the views that use them show their
// signed-out state, until a sign-in through the web view brings cookies.

// QR codes are NetEase's way to sign in: YouTube's is a sign-in to Google
// in a web view (LoginURL).
func QRKey() (string, error) { return "", ErrSignIn }
func QRCheck(key string) (code int, msg, nickname, avatar string, err error) {
	return QRExpired, "", "", "", ErrSignIn
}

func Events(uid string, limit int, before int64) ([]Event, int64, bool, error) {
	return nil, 0, false, nil
}

func ThreadComments(thread string, offset, limit int) (*Comments, error) {
	return nil, ErrUnsupported
}
func LikeComment(thread string, id string, like bool) error { return ErrUnsupported }

// Podcasts: YouTube Music has them, the app doesn't show them yet.
func PodcastDetail(id string) (*Podcast, error) { return nil, ErrUnsupported }
func PodcastPrograms(id string, limit, offset int) ([]Program, bool, error) {
	return nil, false, ErrUnsupported
}
func RecommendedPodcasts() ([]Podcast, error)                { return nil, ErrUnsupported }
func HotPodcasts(limit, offset int) ([]Podcast, bool, error) { return nil, false, ErrUnsupported }
func SubscribePodcast(id string, subscribe bool) error       { return ErrUnsupported }
func SubscribedPodcasts() ([]Podcast, error)                 { return nil, ErrSignIn }
func LikeProgram(id string, like bool) error                 { return ErrUnsupported }
func ProgramsLiked(ids []string) (map[string]bool, error)    { return nil, nil }
