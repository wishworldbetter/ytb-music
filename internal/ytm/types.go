// Package ytm is YouTube Music, as the app plays it: the catalogue through
// InnerTube, the API music.youtube.com calls, and the streams of songs and
// videos through the clients that serve them whole.
//
// Its types keep the shapes the app's views were written for: IDs are
// YouTube's own strings, a video ID for a track, a browse ID for an album
// (MPREb_…) or an artist (UC…), and a playlist ID (PL…, OLAK5uy_…,
// RDCLAK…) without the VL a browse of it takes.
package ytm

import (
	"errors"
	"strings"
	"time"
)

// UserAgent is what the app's own requests say they are: Safari, as the
// web player's listeners mostly are on a Mac.
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"

var (
	// ErrUnavailable is what a song gives that doesn't stream: taken
	// down, private, or kept from the listener's country.
	ErrUnavailable = errors.New("this song isn't available to stream")
	// ErrSignIn is what needs an account gives, until the app signs in.
	ErrSignIn = errors.New("需要登录 YouTube 账号")
	// ErrUnsupported is what YouTube Music has no counterpart for.
	ErrUnsupported = errors.New("YouTube Music 暂不支持")
	// ErrInPlaylist is what adding a song a playlist has already gives.
	ErrInPlaylist = errors.New("the song is in the playlist already")
)

type Artist struct {
	ID   string `json:"id"` // a channel's browse ID, UC…; empty for a credit with no page
	Name string `json:"name"`
}

type Album struct {
	ID     string `json:"id"` // a browse ID, MPREb_…
	Name   string `json:"name"`
	PicURL string `json:"picUrl"` // the cover, or the track's thumbnail
}

// Video types, as YouTube Music tells songs from videos.
const (
	VideoATV      = "MUSIC_VIDEO_TYPE_ATV" // a song: its audio under its album's cover
	VideoOMV      = "MUSIC_VIDEO_TYPE_OMV" // an official music video
	VideoUGC      = "MUSIC_VIDEO_TYPE_UGC" // a video someone uploaded
	VideoOfficial = "MUSIC_VIDEO_TYPE_OFFICIAL_SOURCE_MUSIC"
	VideoPodcast  = "MUSIC_VIDEO_TYPE_PODCAST_EPISODE"
)

type Track struct {
	ID       string   `json:"id"` // the video ID, which streams
	Name     string   `json:"name"`
	Artists  []Artist `json:"ar"`
	Album    Album    `json:"al"`
	Duration int64    `json:"dt"` // milliseconds
	Alias    []string `json:"alia"`
	// Reason says why a recommendation was picked.
	Reason string `json:"reason"`
	// Unplayable marks a track YouTube Music shows greyed out.
	Unplayable bool `json:"unplayable,omitempty"`
	// VideoType is one of the Video constants, or empty when unknown.
	VideoType string `json:"videoType,omitempty"`
	// VideoID is the video to show for the track: its own for a music
	// video, its counterpart's for a song YouTube Music pairs with one,
	// or empty.
	VideoID string `json:"videoId,omitempty"`
	// Views is how often it was played, as YouTube words it ("1.2B plays").
	Views string `json:"views,omitempty"`
	// A podcast's episode plays as a track of its audio, ID, with the
	// episode's ProgramID, the PodcastID it is in, and what it is about.
	ProgramID   string `json:"programId,omitempty"`
	PodcastID   string `json:"podcastId,omitempty"`
	Description string `json:"description,omitempty"`
}

// ArtistNames joins the track's artists as the credits read.
func (t *Track) ArtistNames() string {
	names := make([]string, len(t.Artists))
	for i, a := range t.Artists {
		names[i] = a.Name
	}
	return strings.Join(names, ", ")
}

// Playable reports whether the track streams, as far as the catalogue
// says: a stream can still be refused.
func (t *Track) Playable() bool { return t.ID != "" && !t.Unplayable }

func (t *Track) Length() time.Duration { return time.Duration(t.Duration) * time.Millisecond }

// HasVideo reports whether the track has a video to show, its own or its
// counterpart's.
func (t *Track) HasVideo() bool { return t.Video() != "" }

// Video is the ID of the video to show for the track, or empty.
func (t *Track) Video() string {
	if t.VideoID != "" {
		return t.VideoID
	}
	switch t.VideoType {
	case VideoOMV, VideoUGC, VideoOfficial:
		return t.ID
	}
	return ""
}

// URL is the track's page on YouTube Music.
func (t *Track) URL() string { return "https://music.youtube.com/watch?v=" + t.ID }

// Thread names the track's comments, and what likes go to.
func (t *Track) Thread() string { return "video:" + t.ID }

type Playlist struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	CoverURL    string  `json:"coverImgUrl"`
	PicURL      string  `json:"picUrl"`
	Description string  `json:"description"`
	PlayCount   float64 `json:"playCount"`
	TrackCount  int     `json:"trackCount"`
	UpdateFreq  string  `json:"updateFrequency"`
	SpecialType int     `json:"specialType"` // 5: the listener's liked songs
	Privacy     int     `json:"privacy"`     // 10: only its creator sees it
	Subscribed  bool    `json:"subscribed"`  // the listener saved it
	Creator     struct {
		UserID   string `json:"userId"`
		Nickname string `json:"nickname"`
	} `json:"creator"`
	Tracks   []Track `json:"tracks"`
	TrackIDs []struct {
		ID string `json:"id"`
	} `json:"trackIds"`
	// Preview holds a chart's top songs, as its card shows them.
	Preview []ChartLine `json:"-"`
	// Subtitle is the line YouTube Music shows under its name, as
	// "Playlist • YouTube Music • 50 songs".
	Subtitle string `json:"subtitle,omitempty"`
}

// ChartLine is a song at the top of a chart: its name and its artist.
type ChartLine struct {
	First  string `json:"first"`
	Second string `json:"second"`
}

func (p *Playlist) Cover() string {
	if p.CoverURL != "" {
		return p.CoverURL
	}
	return p.PicURL
}

// Stream is where a track streams from, and what it is.
type Stream struct {
	URL  string `json:"-"`
	Size int64  `json:"size"` // in bytes
	// Type is the format: m4a, AAC in MP4.
	Type string `json:"type"`
	// Level is the level served.
	Level string `json:"level"`
	// Gain is how many dB to change the track by to sound as loud as
	// others, and Peak its loudest sample, 1 at full scale, or 0 if
	// unknown.
	Gain float64 `json:"gain"`
	Peak float64 `json:"peak"`
}

// Levels are the stream levels, from the lowest. YouTube Music serves
// AAC at 128 kbps to anyone (itag 140, "standard"), and at 256 kbps to
// Premium (itag 141, "exhigh"); its other formats are Opus, or HE-AAC,
// which package audio doesn't decode.
var Levels = []string{"standard", "exhigh"}

// Lossless reports whether level streams FLAC: none does.
func Lossless(level string) bool { return false }

// SearchType is what a search looks for.
type SearchType int

const (
	SearchSongs     SearchType = 1
	SearchAlbums    SearchType = 10
	SearchArtists   SearchType = 100
	SearchPlaylists SearchType = 1000
	SearchUsers     SearchType = 1002
	SearchPodcasts  SearchType = 1009
	SearchVideos    SearchType = 1014
)

// SearchResult is a page of what a search found, of the one type it
// looked for, and how many it found in all.
type SearchResult struct {
	Songs     []Track
	Videos    []Track
	Albums    []AlbumProfile
	Artists   []ArtistProfile
	Playlists []Playlist
	Users     []User
	Podcasts  []Podcast
	// Total is how many there are in all, as far as known: YouTube says
	// no count, so it is what came so far, and one more while more come.
	Total int
}

// Len is how many the page has.
func (r *SearchResult) Len() int {
	return len(r.Songs) + len(r.Videos) + len(r.Albums) + len(r.Artists) + len(r.Playlists) + len(r.Users) + len(r.Podcasts)
}

// Append adds the next page of a search to r.
func (r *SearchResult) Append(next SearchResult) {
	r.Songs = append(r.Songs, next.Songs...)
	r.Videos = append(r.Videos, next.Videos...)
	r.Albums = append(r.Albums, next.Albums...)
	r.Artists = append(r.Artists, next.Artists...)
	r.Playlists = append(r.Playlists, next.Playlists...)
	r.Users = append(r.Users, next.Users...)
	r.Podcasts = append(r.Podcasts, next.Podcasts...)
	r.Total = next.Total
}

// Suggestions are what a search may be after, as its query is typed.
type Suggestions struct {
	// Queries are searches to complete the query with.
	Queries   []string
	Songs     []Track
	Artists   []Artist
	Albums    []AlbumProfile
	Playlists []Playlist
	Order     []string // "songs", "artists", "albums" and "playlists"
}

// AlbumProfile is an album as its page shows it.
type AlbumProfile struct {
	Album
	Artists     []Artist `json:"artists"`
	PublishTime int64    `json:"publishTime"` // Unix milliseconds: YouTube says the year, so its first day
	Size        int      `json:"size"`        // how many songs it has
	Description string   `json:"description"`
	// Kind is what YouTube Music calls it: Album, Single, EP.
	Kind  string  `json:"kind,omitempty"`
	Songs []Track `json:"-"`
}

// ArtistNames joins the album's artists' names, as its credits read.
func (al *AlbumProfile) ArtistNames() string {
	return (&Track{Artists: al.Artists}).ArtistNames()
}

// ArtistProfile is who an artist is, as their page shows them, with the
// songs they are best known for.
type ArtistProfile struct {
	Artist
	Avatar    string   `json:"img1v1Url"` // square
	PicURL    string   `json:"picUrl"`    // wide
	Alias     []string `json:"alias"`
	Trans     string   `json:"trans"`
	BriefDesc string   `json:"briefDesc"`
	MusicSize int      `json:"musicSize"`
	AlbumSize int      `json:"albumSize"`
	Followed  bool     `json:"followed"` // by the listener signed in
	// Subscribers is how many follow the channel, as YouTube words it.
	Subscribers string `json:"subscribers,omitempty"`
	// AccountID is the artist's own account, which posts their events, or empty.
	AccountID string  `json:"accountId"`
	Songs     []Track `json:"-"`
	// SongsID is the playlist of all their top songs, of which Songs are
	// the first few, or empty.
	SongsID string `json:"songsId,omitempty"`
}

// User is someone on YouTube, as their page shows them.
type User struct {
	UserID    string `json:"userId"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
	Signature string `json:"signature"`
	Followeds int    `json:"followeds"` // their followers
	Follows   int    `json:"follows"`
	// Subscribers is how many subscribe to their channel, as YouTube
	// says it ("1.2K subscribers"), if it says.
	Subscribers string `json:"subscribers"`
}

// Profile is who is signed in.
type Profile struct {
	UserID    string `json:"userId"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
}

// Play is a song the listener played.
type Play struct {
	Track  Track
	At     time.Time
	Device string
}

// Podcast is a show of episodes.
type Podcast struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	PicURL       string  `json:"picUrl"`
	Description  string  `json:"desc"`
	Category     string  `json:"category"`
	DJ           User    `json:"dj"`
	ProgramCount int     `json:"programCount"`
	SubCount     int64   `json:"subCount"`
	PlayCount    float64 `json:"playCount"`
	Subed        bool    `json:"subed"`
	RcmdText     string  `json:"rcmdtext"`
	LastProgram  string  `json:"lastProgramName"`
}

// Program is an episode of a podcast.
type Program struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CoverURL    string `json:"coverUrl"`
	Duration    int64  `json:"duration"`   // milliseconds
	CreateTime  int64  `json:"createTime"` // Unix milliseconds
	Description string `json:"description"`
	// AudioID is the video ID its audio streams from.
	AudioID string `json:"audioId"`
}

// Track is the episode as it plays, in a podcast.
func (p *Program) Track(pod *Podcast) Track {
	cover := p.CoverURL
	if cover == "" {
		cover = pod.PicURL
	}
	t := Track{
		ID: p.AudioID, Name: p.Name, Duration: p.Duration,
		Album:     Album{Name: pod.Name, PicURL: cover},
		ProgramID: p.ID, PodcastID: pod.ID, Description: p.Description,
		VideoType: VideoPodcast,
	}
	if pod.DJ.Nickname != "" {
		t.Artists = []Artist{{Name: pod.DJ.Nickname}}
	}
	return t
}

// Comment is what someone wrote under a video.
type Comment struct {
	ID   string `json:"commentId"`
	User struct {
		ID        string `json:"userId"`
		Nickname  string `json:"nickname"`
		AvatarURL string `json:"avatarUrl"`
	} `json:"user"`
	Content    string `json:"content"`
	Time       int64  `json:"time"` // Unix milliseconds
	LikedCount int    `json:"likedCount"`
	Liked      bool   `json:"liked"`
	IPLocation struct {
		Location string `json:"location"`
	} `json:"ipLocation"`
	BeReplied []struct {
		User struct {
			Nickname string `json:"nickname"`
		} `json:"user"`
		Content string `json:"content"`
	} `json:"beReplied"`
}

// Comments are a page of a video's comments.
type Comments struct {
	Hot    []Comment `json:"hotComments"`
	Latest []Comment `json:"comments"`
	Total  int       `json:"total"`
	More   bool      `json:"more"`
}

// Event is something posted to a channel's feed.
type Event struct {
	ID       string `json:"id"`
	Time     int64  `json:"eventTime"` // Unix milliseconds
	User     User   `json:"user"`
	Pictures []struct {
		SquareURL string `json:"squareUrl"`
	} `json:"pics"`
	Info struct {
		LikedCount   int `json:"likedCount"`
		CommentCount int `json:"commentCount"`
		ShareCount   int `json:"shareCount"`
	} `json:"info"`
	Content EventContent `json:"-"`
}

// EventContent is what an event says and shares.
type EventContent struct {
	Msg       string
	Song      *Track
	Album     *AlbumProfile
	Artist    *ArtistProfile
	Playlist  *Playlist
	Video     bool
	Forwarded string
}

// FM modes: what the radio picks songs by.
const (
	FMDefault  = "DEFAULT"
	FMFamiliar = "FAMILIAR"
	FMExplore  = "EXPLORE"
)

// The states of a sign-in by QR code.
const (
	QRExpired   = 800
	QRWaiting   = 801
	QRScanned   = 802
	QRConfirmed = 803
)
