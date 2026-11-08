package ytm

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// The listener's library, signed in: their playlists and those they saved,
// the songs they like, what they played, and the artists they follow, as
// YouTube Music's library pages list them; and what changes it.

// me is the profile Account found last, which tells the listener's own
// playlists from those they saved.
var me struct {
	mu sync.Mutex
	p  *Profile
}

func rememberMe(p *Profile) {
	me.mu.Lock()
	me.p = p
	me.mu.Unlock()
}

func myProfile() *Profile {
	me.mu.Lock()
	defer me.mu.Unlock()
	return me.p
}

// likedID is the playlist of the songs the listener likes.
const likedID = "LM"

func needSignIn() error {
	if !SignedIn() {
		return ErrSignIn
	}
	return nil
}

// libraryItems returns the items of a library page, as many as max,
// following its continuations: a grid of cards or a shelf of rows.
func libraryItems(id string, max int) ([]any, error) {
	if err := needSignIn(); err != nil {
		return nil, err
	}
	resp, err := browse(id, "")
	if err != nil {
		return nil, err
	}
	tab, _ := sectionContents(resp)
	var items []any
	var token string
	for _, s := range shelves(tab) {
		items = append(items, s.Items...)
		if s.Continue != "" {
			token = s.Continue
		}
	}
	for i := 0; i < 20 && token != "" && len(items) < max; i++ {
		more, err := browseNext(token)
		if err != nil {
			break
		}
		its, next := continued(more)
		if len(its) == 0 {
			break
		}
		items = append(items, its...)
		token = next
	}
	return items, nil
}

// UserPlaylists returns the listener's playlists: those they made, their
// liked songs first, and those they saved. Of anyone else, it returns the
// playlists their channel shows, as made.
func UserPlaylists(uid string) (made, saved []Playlist, err error) {
	if p := myProfile(); (p == nil || p.UserID != uid) && strings.HasPrefix(uid, "UC") {
		made, err = channelPlaylists(uid)
		return made, nil, err
	}
	items, err := libraryItems("FEmusic_liked_playlists", 500)
	if err != nil {
		return nil, nil, err
	}
	p := myProfile()
	for _, it := range items {
		kind, v := only(it)
		var pl Playlist
		var ok bool
		switch kind {
		case "musicTwoRowItemRenderer":
			pl, ok = twoRowPlaylist(v)
		case "musicResponsiveListItemRenderer":
			pl, ok = responsivePlaylist(v)
		}
		if !ok || strings.HasPrefix(pl.ID, "RDCLAK") && pl.Creator.Nickname == "" {
			continue
		}
		switch {
		case pl.ID == likedID:
			pl.SpecialType = 5
			pl.Name = "我喜欢的音乐"
			mine(&pl, p)
			made = append([]Playlist{pl}, made...)
		case p != nil && (pl.Creator.Nickname == p.Nickname || pl.Creator.UserID == p.UserID ||
			pl.Creator.Nickname == "" && strings.Contains(pl.Subtitle, p.Nickname)):
			mine(&pl, p)
			made = append(made, pl)
		default:
			pl.Subscribed = true
			saved = append(saved, pl)
		}
	}
	return made, saved, nil
}

// mine marks a playlist the listener's, as the app tells their own.
func mine(pl *Playlist, p *Profile) {
	if p != nil {
		pl.Creator.UserID, pl.Creator.Nickname = p.UserID, p.Nickname
	}
}

// LikedIDs returns the video IDs of the songs the listener likes, up to
// a thousand.
func LikedIDs(uid string) ([]string, error) {
	if err := needSignIn(); err != nil {
		return nil, err
	}
	pl, err := PlaylistDetail(likedID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(pl.Tracks)*2)
	for _, t := range pl.Tracks {
		ids = append(ids, t.ID)
		if t.VideoID != "" && t.VideoID != t.ID {
			ids = append(ids, t.VideoID)
		}
	}
	return ids, nil
}

// Like likes a song for the listener, or takes the like back.
func Like(id string, like bool) error {
	if err := needSignIn(); err != nil {
		return err
	}
	endpoint := "like/like"
	if !like {
		endpoint = "like/removelike"
	}
	_, err := call(endpoint, map[string]any{"target": map[string]any{"videoId": id}})
	return err
}

// SubscribePlaylist saves a playlist to the listener's library, or takes
// it out.
func SubscribePlaylist(id string, subscribe bool) error {
	if err := needSignIn(); err != nil {
		return err
	}
	endpoint := "like/like"
	if !subscribe {
		endpoint = "like/removelike"
	}
	_, err := call(endpoint, map[string]any{"target": map[string]any{"playlistId": strings.TrimPrefix(id, "VL")}})
	return err
}

// FollowArtist subscribes the listener to an artist's channel, or
// unsubscribes them.
func FollowArtist(id string, follow bool) error {
	if err := needSignIn(); err != nil {
		return err
	}
	endpoint := "subscription/subscribe"
	if !follow {
		endpoint = "subscription/unsubscribe"
	}
	_, err := call(endpoint, map[string]any{"channelIds": []string{id}})
	return err
}

// FollowedArtists returns the artists the listener subscribes to.
func FollowedArtists() ([]ArtistProfile, error) {
	items, err := libraryItems("FEmusic_library_corpus_artists", 500)
	if err != nil {
		return nil, err
	}
	var out []ArtistProfile
	for _, it := range items {
		kind, v := only(it)
		var ar ArtistProfile
		var ok bool
		switch kind {
		case "musicResponsiveListItemRenderer":
			ar, ok = responsiveArtist(v)
		case "musicTwoRowItemRenderer":
			ar, ok = twoRowArtist(v)
		}
		if ok {
			ar.Followed = true
			out = append(out, ar)
		}
	}
	return out, nil
}

// RecentPlays returns the songs the listener played last, the latest
// first. YouTube says the day, not the time: those of a day are taken a
// few minutes apart, from its end, which keeps today's together.
func RecentPlays(limit int) ([]Play, error) {
	if err := needSignIn(); err != nil {
		return nil, err
	}
	resp, err := browse("FEmusic_history", "")
	if err != nil {
		return nil, err
	}
	tab, _ := sectionContents(resp)
	now := time.Now()
	var out []Play
	for day, s := range shelves(tab) {
		at := now.Add(-time.Duration(day) * 24 * time.Hour)
		if day > 0 {
			at = at.Add(-time.Hour) // a stretch apart from today's
		}
		for i, t := range tracksOf(s.Items) {
			out = append(out, Play{Track: t, At: at.Add(-time.Duration(i) * 4 * time.Minute)})
			if len(out) == limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// CreatePlaylist makes a private playlist of the listener's.
func CreatePlaylist(name string) (*Playlist, error) {
	if err := needSignIn(); err != nil {
		return nil, err
	}
	v, err := call("playlist/create", map[string]any{"title": name, "description": "", "privacyStatus": "PRIVATE"})
	if err != nil {
		return nil, err
	}
	id := str(v, "playlistId")
	if id == "" {
		return nil, errors.New("ytm: the playlist wasn't made")
	}
	pl := &Playlist{ID: id, Name: name, Privacy: 10}
	mine(pl, myProfile())
	return pl, nil
}

// editPlaylist applies actions to one of the listener's playlists, and
// reports whether YouTube says they took.
func editPlaylist(id string, actions []any, dedupe bool) (any, error) {
	if err := needSignIn(); err != nil {
		return nil, err
	}
	body := map[string]any{"playlistId": strings.TrimPrefix(id, "VL"), "actions": actions}
	if dedupe {
		for _, a := range actions {
			a.(map[string]any)["dedupeOption"] = "DEDUPE_OPTION_CHECK"
		}
	}
	return call("browse/edit_playlist", body)
}

// RenamePlaylist renames one of the listener's playlists.
func RenamePlaylist(id string, name string) error {
	_, err := editPlaylist(id, []any{map[string]any{"action": "ACTION_SET_PLAYLIST_NAME", "playlistName": name}}, false)
	return err
}

// AddToPlaylist adds songs to one of the listener's playlists:
// ErrInPlaylist if it has them already.
func AddToPlaylist(pid string, songs ...string) error {
	actions := make([]any, len(songs))
	for i, id := range songs {
		actions[i] = map[string]any{"action": "ACTION_ADD_VIDEO", "addedVideoId": id}
	}
	v, err := editPlaylist(pid, actions, true)
	if err != nil {
		return err
	}
	if str(v, "status") != "STATUS_SUCCEEDED" {
		if find(v, "confirmDialogRenderer") != nil || find(v, "dedupeDialog") != nil {
			return ErrInPlaylist
		}
		return errors.New("ytm: the song wasn't added")
	}
	return nil
}

// RemoveFromPlaylist takes songs out of one of the listener's playlists.
// YouTube removes an entry by its place in the playlist (its
// setVideoId), which the playlist's page says.
func RemoveFromPlaylist(pid string, songs ...string) error {
	entries, err := playlistEntries(pid)
	if err != nil {
		return err
	}
	var actions []any
	for _, id := range songs {
		if set, ok := entries[id]; ok {
			actions = append(actions, map[string]any{"action": "ACTION_REMOVE_VIDEO", "removedVideoId": id, "setVideoId": set})
		}
	}
	if len(actions) == 0 {
		return nil
	}
	_, err = editPlaylist(pid, actions, false)
	return err
}

// playlistEntries returns the setVideoIds of the songs of a playlist, by
// video ID.
func playlistEntries(pid string) (map[string]string, error) {
	if err := needSignIn(); err != nil {
		return nil, err
	}
	resp, err := browse("VL"+strings.TrimPrefix(pid, "VL"), "")
	if err != nil {
		return nil, err
	}
	tab, sec := sectionContents(resp)
	items, token := trackShelf(append(sec, tab...))
	out := map[string]string{}
	for i := 0; ; i++ {
		for _, it := range items {
			d := dig(it, "musicResponsiveListItemRenderer", "playlistItemData")
			if id, set := str(d, "videoId"), str(d, "playlistSetVideoId"); id != "" && set != "" {
				out[id] = set
			}
		}
		if token == "" || i == 20 {
			return out, nil
		}
		more, err := browseNext(token)
		if err != nil {
			return out, nil
		}
		items, token = continued(more)
	}
}

// DeletePlaylist deletes one of the listener's playlists.
func DeletePlaylist(id string) error {
	if err := needSignIn(); err != nil {
		return err
	}
	_, err := call("playlist/delete", map[string]any{"playlistId": strings.TrimPrefix(id, "VL")})
	return err
}

// supermixID is YouTube Music's "My Supermix": endless songs picked for
// the listener, which the personal FM plays.
const supermixID = "RDTMAK5uy_kset8DisdE7LSD4TNjEVvrKRTmG7a56sY"

var fm struct {
	mu    sync.Mutex
	token string
}

// PersonalFM returns the next songs of the listener's supermix: from its
// start the first time, then from where the last ones ended. YouTube has
// the one mix for every mode.
func PersonalFM(mode string) ([]Track, error) {
	if err := needSignIn(); err != nil {
		return nil, err
	}
	fm.mu.Lock()
	token := fm.token
	fm.mu.Unlock()
	body := map[string]any{"isAudioOnly": true, "enablePersistentPlaylistPanel": true}
	if token == "" {
		body["playlistId"] = supermixID
	} else {
		body["continuation"] = token
	}
	resp, err := call("next", body)
	if err != nil {
		return nil, err
	}
	tracks, next := parseRadio(resp, "", token == "")
	fm.mu.Lock()
	fm.token = next
	fm.mu.Unlock()
	if len(tracks) == 0 {
		return nil, errors.New("ytm: the supermix is empty")
	}
	return tracks, nil
}

// FMTrash skips a song of the FM: YouTube Music has no dislike for it to
// learn from but the song's, which the app doesn't send.
func FMTrash(id string) error { return nil }
