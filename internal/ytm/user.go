package ytm

import (
	"errors"
	"strings"
	"time"
)

var userCache = newCache[any](3*time.Minute, 16)

// userPage is the page of a user's channel (browse its UC ID).
func userPage(id string) (any, error) {
	return userCache.get(Language+"/"+Region+"/"+id, func() (any, error) {
		return browse(id, "")
	})
}

// UserDetail returns a user, from their channel: their name, picture and
// subscribers. The listener's own may have no channel to show, or no
// channel ID at all, so theirs is what their account says, with what the
// channel adds.
func UserDetail(id string) (*User, error) {
	var u *User
	if p := myProfile(); p != nil && p.UserID == id {
		u = &User{UserID: id, Nickname: p.Nickname, AvatarURL: p.AvatarURL}
	}
	if !strings.HasPrefix(id, "UC") {
		if u != nil {
			return u, nil
		}
		return nil, ErrUnsupported
	}
	resp, err := userPage(id)
	if err != nil {
		if u != nil {
			return u, nil
		}
		return nil, err
	}
	if c, ok := channelUser(resp, id); ok {
		if u == nil {
			return &c, nil
		}
		u.Subscribers = c.Subscribers
		if u.AvatarURL == "" {
			u.AvatarURL = c.AvatarURL
		}
	}
	if u == nil {
		return nil, errors.New("ytm: no such user")
	}
	return u, nil
}

// channelUser reads who a channel's page is of from its header.
func channelUser(resp any, id string) (User, bool) {
	_, h := only(dig(resp, "header"))
	name := text(dig(h, "title"))
	if name == "" {
		return User{}, false
	}
	u := User{UserID: id, Nickname: name, Signature: text(dig(h, "description"))}
	if fg := dig(h, "foregroundThumbnail"); fg != nil {
		u.AvatarURL = bestThumb(fg)
	} else {
		u.AvatarURL = bestThumb(dig(h, "thumbnail"))
	}
	sub := dig(h, "subscriptionButton", "subscribeButtonRenderer")
	if u.Subscribers = text(dig(sub, "longSubscriberCountText")); u.Subscribers == "" {
		u.Subscribers = text(dig(sub, "subscriberCountText"))
	}
	return u, true
}

// channelPlaylists returns the playlists a user's channel shows, all of
// them where its shelf leads to more, as the user's.
func channelPlaylists(id string) ([]Playlist, error) {
	resp, err := userPage(id)
	if err != nil {
		return nil, err
	}
	u, _ := channelUser(resp, id)
	items, more := playlistShelf(resp)
	if more != nil {
		if all, err := shelfPage(more); err == nil && len(all) >= len(items) {
			items = all
		}
	}
	var out []Playlist
	for _, it := range items {
		kind, v := only(it)
		if kind != "musicTwoRowItemRenderer" {
			continue
		}
		if pl, ok := twoRowPlaylist(v); ok {
			pl.Creator.UserID, pl.Creator.Nickname = id, u.Nickname
			out = append(out, pl)
		}
	}
	return out, nil
}

// playlistShelf returns the items of a channel's shelf of playlists, not
// those of its videos or podcasts, and the page of all of them it leads
// to, if any.
func playlistShelf(resp any) (items []any, more any) {
	tab, _ := sectionContents(resp)
	for _, s := range shelves(tab) {
		if len(s.Items) == 0 {
			continue
		}
		if kind, v := only(s.Items[0]); kind == "musicTwoRowItemRenderer" {
			if _, ok := twoRowPlaylist(v); ok {
				return s.Items, s.More
			}
		}
	}
	return nil, nil
}

// shelfPage returns the items of the page a shelf leads to, with those
// its continuations bring.
func shelfPage(be any) ([]any, error) {
	resp, err := browse(str(be, "browseId"), str(be, "params"))
	if err != nil {
		return nil, err
	}
	tab, _ := sectionContents(resp)
	var items []any
	for _, s := range shelves(tab) {
		items = append(items, s.Items...)
		token := s.Continue
		for i := 0; i < 5 && token != ""; i++ {
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
	}
	return items, nil
}
