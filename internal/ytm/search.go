package ytm

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// What search filters by, as music.youtube.com asks: a playlist search
// goes through YouTube Music's own playlists, then the community's.
var searchParams = map[SearchType][]string{
	SearchSongs:     {"EgWKAQIIAWoMEA4QChADEAQQCRAF"},
	SearchVideos:    {"EgWKAQIQAWoMEA4QChADEAQQCRAF"},
	SearchAlbums:    {"EgWKAQIYAWoMEA4QChADEAQQCRAF"},
	SearchArtists:   {"EgWKAQIgAWoMEA4QChADEAQQCRAF"},
	SearchPlaylists: {"EgeKAQQoADgBagwQDhAKEAMQBBAJEAU=", "EgeKAQQoAEABagwQDhAKEAMQBBAJEAU="},
	SearchPodcasts:  {"EgWKAQJQAWoQEBAQCRAEEAMQBRAKEBUQEQ=="},
}

// search is a search as far as it was fetched: what came, and where each
// of its filters goes on from.
type search struct {
	mu      sync.Mutex
	at      time.Time
	res     SearchResult
	seen    map[string]bool
	streams []searchStream
}

type searchStream struct {
	params  string
	token   string
	started bool
	done    bool
	n       int
}

var searches struct {
	mu sync.Mutex
	m  map[string]*search
}

const searchKeep = 10 * time.Minute

func searchFor(query string, kind SearchType) *search {
	key := Language + "/" + Region + "/" + strconv.Itoa(int(kind)) + "/" + query
	searches.mu.Lock()
	defer searches.mu.Unlock()
	if searches.m == nil {
		searches.m = map[string]*search{}
	}
	if s := searches.m[key]; s != nil && time.Since(s.at) < searchKeep {
		return s
	}
	for k, s := range searches.m {
		if time.Since(s.at) >= searchKeep || len(searches.m) >= 32 {
			delete(searches.m, k)
		}
	}
	s := &search{at: time.Now(), seen: map[string]bool{}}
	for _, p := range searchParams[kind] {
		s.streams = append(s.streams, searchStream{params: p})
	}
	searches.m[key] = s
	return s
}

// SearchPage returns limit of what matches a query, of a kind, from
// offset. YouTube pages by continuation: SearchPage keeps the tokens of
// the pages it fetched, so asking for the page after one it gave goes on
// from there.
func SearchPage(query string, kind SearchType, limit, offset int) (SearchResult, error) {
	query = strings.TrimSpace(query)
	if kind == SearchUsers || query == "" {
		return SearchResult{}, nil
	}
	if _, ok := searchParams[kind]; !ok {
		return SearchResult{}, ErrUnsupported
	}
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	s := searchFor(query, kind)
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	for s.res.Len() < offset+limit {
		st := s.next()
		if st == nil {
			break
		}
		if err = s.fetch(query, kind, st); err != nil {
			break
		}
	}
	if err != nil && s.res.Len() <= offset {
		return SearchResult{}, err
	}
	s.at = time.Now()
	out := s.res.slice(offset, limit)
	out.Total = s.res.Len()
	if s.next() != nil {
		out.Total++
	}
	return out, nil
}

// next returns the filter to fetch more from: of those with more, the one
// that gave fewest so far.
func (s *search) next() *searchStream {
	var best *searchStream
	for i := range s.streams {
		st := &s.streams[i]
		if !st.done && (best == nil || st.n < best.n) {
			best = st
		}
	}
	return best
}

func (s *search) fetch(query string, kind SearchType, st *searchStream) error {
	var items []any
	var next string
	if !st.started {
		resp, err := call("search", map[string]any{"query": query, "params": st.params})
		if err != nil {
			return err
		}
		items, next = searchShelf(resp)
		st.started = true
	} else {
		resp, err := call("search", map[string]any{"continuation": st.token})
		if err != nil {
			return err
		}
		items, next = continued(resp)
	}
	before := s.res.Len()
	s.add(kind, items)
	st.n += s.res.Len() - before
	st.token = next
	if next == "" || len(items) == 0 {
		st.done = true
	}
	return nil
}

// searchShelf returns the items of the first page of a search, and the
// token for the next.
func searchShelf(resp any) (items []any, next string) {
	sections := arr(resp, "contents", "tabbedSearchResultsRenderer", "tabs", 0, "tabRenderer", "content", "sectionListRenderer", "contents")
	for _, c := range sections {
		if k, v := only(c); k == "musicShelfRenderer" {
			return arr(v, "contents"), continuation(v)
		}
	}
	return nil, ""
}

// add appends what items are, of kind, but those it has.
func (s *search) add(kind SearchType, items []any) {
	for _, it := range items {
		k, v := only(it)
		if k != "musicResponsiveListItemRenderer" {
			continue
		}
		switch kind {
		case SearchSongs, SearchVideos:
			t := responsiveTrack(v, false)
			if t.ID == "" || s.seen[t.ID] {
				continue
			}
			s.seen[t.ID] = true
			if kind == SearchSongs {
				s.res.Songs = append(s.res.Songs, t)
			} else {
				s.res.Videos = append(s.res.Videos, t)
			}
		case SearchAlbums:
			if al, ok := responsiveAlbum(v); ok && !s.seen[al.ID] {
				s.seen[al.ID] = true
				s.res.Albums = append(s.res.Albums, al)
			}
		case SearchArtists:
			if a, ok := responsiveArtist(v); ok && !s.seen[a.ID] {
				s.seen[a.ID] = true
				s.res.Artists = append(s.res.Artists, a)
			}
		case SearchPlaylists:
			if p, ok := responsivePlaylist(v); ok && !s.seen[p.ID] {
				s.seen[p.ID] = true
				s.res.Playlists = append(s.res.Playlists, p)
			}
		case SearchPodcasts:
			id, page := browseOf(v)
			if page != pagePodcast && !strings.HasPrefix(id, "MPSP") || s.seen[id] {
				continue
			}
			s.seen[id] = true
			flex, _ := columns(v)
			pod := Podcast{ID: id, PicURL: bestThumb(dig(v, "thumbnail"))}
			if len(flex) > 0 {
				pod.Name = text(flex[0])
			}
			if len(flex) > 1 {
				pod.DJ.Nickname = text(flex[1])
				pod.DJ.UserID, _ = browseOf(dig(flex[1], "runs", 0))
			}
			s.res.Podcasts = append(s.res.Podcasts, pod)
		}
	}
}

// slice returns limit of r from offset, of whichever kind it holds.
func (r *SearchResult) slice(offset, limit int) SearchResult {
	var out SearchResult
	out.Songs = window(r.Songs, offset, limit)
	out.Videos = window(r.Videos, offset, limit)
	out.Albums = window(r.Albums, offset, limit)
	out.Artists = window(r.Artists, offset, limit)
	out.Playlists = window(r.Playlists, offset, limit)
	out.Users = window(r.Users, offset, limit)
	out.Podcasts = window(r.Podcasts, offset, limit)
	return out
}

func window[T any](s []T, offset, limit int) []T {
	if offset >= len(s) {
		return nil
	}
	end := min(offset+limit, len(s))
	return append([]T(nil), s[offset:end]...)
}

// SearchSuggest returns what a search for query may be after.
func SearchSuggest(query string) (Suggestions, error) {
	var sug Suggestions
	if strings.TrimSpace(query) == "" {
		return sug, nil
	}
	resp, err := call("music/get_search_suggestions", map[string]any{"input": query})
	if err != nil {
		return sug, err
	}
	return parseSuggestions(resp), nil
}

// parseSuggestions reads what get_search_suggestions answers: queries, and
// songs, artists, albums and playlists, in the order it shows them.
func parseSuggestions(resp any) Suggestions {
	var sug Suggestions
	order := func(kind string) {
		for _, k := range sug.Order {
			if k == kind {
				return
			}
		}
		sug.Order = append(sug.Order, kind)
	}
	for _, sec := range arr(resp, "contents") {
		for _, c := range arr(sec, "searchSuggestionsSectionRenderer", "contents") {
			kind, v := only(c)
			switch kind {
			case "searchSuggestionRenderer":
				q := str(v, "navigationEndpoint", "searchEndpoint", "query")
				if q == "" {
					q = text(dig(v, "suggestion"))
				}
				if q != "" {
					sug.Queries = append(sug.Queries, q)
				}
			case "musicResponsiveListItemRenderer":
				if dig(v, "navigationEndpoint", "watchEndpoint") != nil {
					if t := responsiveTrack(v, true); t.ID != "" {
						sug.Songs = append(sug.Songs, t)
						order("songs")
					}
					continue
				}
				id, page := browseOf(v)
				switch {
				case isArtistPage(id, page):
					if a, ok := responsiveArtist(v); ok {
						sug.Artists = append(sug.Artists, a.Artist)
						order("artists")
					}
				case isAlbumPage(id, page):
					if al, ok := responsiveAlbum(v); ok {
						sug.Albums = append(sug.Albums, al)
						order("albums")
					}
				case strings.HasPrefix(id, "VL") || page == pagePlaylist:
					if p, ok := responsivePlaylist(v); ok {
						sug.Playlists = append(sug.Playlists, p)
						order("playlists")
					}
				}
			}
		}
	}
	return sug
}
