package scanner

import (
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/dhowden/tag"
	flaclib "github.com/mewkiz/flac"
	"github.com/tcolgate/mp3"

	"github.com/kento/airwiggler/internal/model"
)

var qualityFolders = []string{"high", "medium"}

// rootArtNames are checked in order in the album root directory.
var rootArtNames = []string{"cover.jpg", "folder.jpg", "cover.png"}

// Scan walks musicDir and builds a Library. Albums with no recognized quality
// folders are silently skipped.
func Scan(musicDir string) (*model.Library, error) {
	entries, err := os.ReadDir(musicDir)
	if err != nil {
		return nil, err
	}

	lib := &model.Library{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		album, err := scanAlbum(musicDir, entry.Name())
		if err != nil {
			log.Printf("scanner: skipping album %q: %v", entry.Name(), err)
			continue
		}
		if len(album.Qualities) == 0 {
			continue
		}
		lib.Albums = append(lib.Albums, *album)
	}
	return lib, nil
}

func scanAlbum(musicDir, folderName string) (*model.Album, error) {
	albumDir := filepath.Join(musicDir, folderName)
	album := &model.Album{
		ID:        slugify(folderName),
		Title:     folderName,
		Qualities: make(map[string]model.Quality),
	}

	var firstAudioPath string
	for _, q := range qualityFolders {
		qDir := filepath.Join(albumDir, q)
		if _, err := os.Stat(qDir); os.IsNotExist(err) {
			continue
		}
		tracks, first, err := scanTracks(musicDir, qDir)
		if err != nil {
			log.Printf("scanner: skipping quality %q in %q: %v", q, folderName, err)
			continue
		}
		album.Qualities[q] = model.Quality{Tracks: tracks}
		if firstAudioPath == "" {
			firstAudioPath = first
		}
	}

	// Cover art: file-based resolution first, then embedded tags.
	if artPath := resolveArtFile(albumDir); artPath != "" {
		album.Art = fileURL(musicDir, artPath)
	} else if firstAudioPath != "" {
		if pic := embeddedPicture(firstAudioPath); pic != nil {
			album.ArtBytes = pic.Data
			album.ArtMIME = pic.MIMEType
			album.Art = "/api/art/" + album.ID
		}
	}

	// Additional images in the album root (cover.* first, then all others).
	album.Images = collectAllImages(musicDir, albumDir)

	return album, nil
}

func scanTracks(musicDir, qualityDir string) (tracks []model.Track, firstPath string, err error) {
	entries, err := os.ReadDir(qualityDir)
	if err != nil {
		return nil, "", err
	}

	var filenames []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		lower := strings.ToLower(e.Name())
		if strings.HasSuffix(lower, ".flac") || strings.HasSuffix(lower, ".mp3") {
			filenames = append(filenames, e.Name())
		}
	}
	sort.Strings(filenames)

	for _, f := range filenames {
		absPath := filepath.Join(qualityDir, f)
		if firstPath == "" {
			firstPath = absPath
		}
		title, duration := readMetadata(absPath)
		if title == "" {
			title = trimExtension(f)
		}
		tracks = append(tracks, model.Track{
			Title:    title,
			URL:      fileURL(musicDir, absPath),
			Duration: duration,
		})
	}
	return tracks, firstPath, nil
}

func readMetadata(path string) (title string, duration float64) {
	// Title via dhowden/tag (handles both Vorbis comments and ID3).
	if f, err := os.Open(path); err == nil {
		if m, err := tag.ReadFrom(f); err == nil {
			title = m.Title()
		}
		f.Close()
	}

	// Duration via format-specific parser.
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		duration = flacDuration(path)
	case ".mp3":
		duration = mp3Duration(path)
	}
	return title, duration
}

func flacDuration(path string) float64 {
	stream, err := flaclib.Open(path)
	if err != nil {
		return 0
	}
	defer stream.Close()
	info := stream.Info
	if info.SampleRate == 0 {
		return 0
	}
	return float64(info.NSamples) / float64(info.SampleRate)
}

func mp3Duration(path string) float64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	d := mp3.NewDecoder(f)
	var frame mp3.Frame
	skipped := 0
	var total time.Duration
	for {
		if err := d.Decode(&frame, &skipped); err != nil {
			if err == io.EOF {
				break
			}
			break
		}
		total += frame.Duration()
	}
	return total.Seconds()
}

// resolveArtFile checks for image files in the album root and quality subdirs.
func resolveArtFile(albumDir string) string {
	// 1-3: root art files
	for _, name := range rootArtNames {
		p := filepath.Join(albumDir, name)
		if fileExists(p) {
			return p
		}
	}
	// 4-5: quality subdir cover files
	imageExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp"}
	for _, q := range qualityFolders {
		qDir := filepath.Join(albumDir, q)
		for _, ext := range imageExts {
			if p := filepath.Join(qDir, "cover"+ext); fileExists(p) {
				return p
			}
		}
	}
	return ""
}

// collectAllImages returns URLs for all image files in the album root,
// sorted with cover.* files first, then all others alphabetically.
func collectAllImages(musicDir, albumDir string) []string {
	imageExts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}
	entries, err := os.ReadDir(albumDir)
	if err != nil {
		return nil
	}
	var covers, others []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !imageExts[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		u := fileURL(musicDir, filepath.Join(albumDir, e.Name()))
		base := strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		if base == "cover" {
			covers = append(covers, u)
		} else {
			others = append(others, u)
		}
	}
	sort.Strings(covers)
	sort.Strings(others)
	return append(covers, others...)
}

func embeddedPicture(audioPath string) *tag.Picture {
	f, err := os.Open(audioPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil {
		return nil
	}
	return m.Picture()
}

// fileURL converts an absolute filesystem path to a URL path rooted at /music/.
func fileURL(musicDir, absPath string) string {
	rel, err := filepath.Rel(musicDir, absPath)
	if err != nil {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	encoded := make([]string, len(parts))
	for i, p := range parts {
		encoded[i] = url.PathEscape(p)
	}
	return "/music/" + strings.Join(encoded, "/")
}

// slugify converts a folder name to a URL-safe identifier.
func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case unicode.IsSpace(r) || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	result := b.String()
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	return strings.Trim(result, "-")
}

func trimExtension(filename string) string {
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
