package model

// Track represents a single audio file.
type Track struct {
	Title    string  `json:"title"`
	URL      string  `json:"url"`
	Duration float64 `json:"duration"` // seconds
}

// Quality holds the tracks for one quality tier.
type Quality struct {
	Tracks []Track `json:"tracks"`
}

// Album represents a single album/collection.
type Album struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	Art       string             `json:"art,omitempty"`
	Images    []string           `json:"images,omitempty"` // additional images in album root
	Qualities map[string]Quality `json:"qualities"`

	// ArtBytes and ArtMIME hold embedded cover art extracted from audio tags.
	// Not serialized to JSON — served by the HTTP server at /api/art/<id>.
	ArtBytes []byte `json:"-"`
	ArtMIME  string `json:"-"`
}

// Library is the full in-memory music library.
type Library struct {
	Albums []Album `json:"albums"`
}
