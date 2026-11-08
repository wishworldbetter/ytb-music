package ytm

// The catalogue, as anyone may browse it, over InnerTube: pages in
// browse.go, search in search.go, streams, queues and radios in stream.go,
// lyrics in lyrics.go.

// Search returns the songs matching a query.
func Search(query string, limit int) ([]Track, error) {
	res, err := SearchPage(query, SearchSongs, limit, 0)
	return res.Songs, err
}

// HeartMode returns songs like song, to go on with: its radio.
func HeartMode(song, start, pid string) ([]Track, error) {
	tracks, _, err := Radio(song, "")
	return tracks, err
}
