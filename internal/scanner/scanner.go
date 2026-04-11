package scanner

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dhowden/tag"

	"github.com/kento/airwiggler/internal/model"
)

// Scan walks musicDir and builds a Library. Albums with no audio are skipped.
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

	entries, err := os.ReadDir(albumDir)
	if err != nil {
		return nil, err
	}

	var subDirs []string
	for _, e := range entries {
		if e.IsDir() {
			subDirs = append(subDirs, e.Name())
		}
	}

	var firstAudioPath string

	// Audio files directly in the album root become the "root" quality.
	rootTracks, rootFirst, err := scanTracks(musicDir, albumDir)
	if err == nil && len(rootTracks) > 0 {
		album.Qualities["root"] = model.Quality{Tracks: rootTracks}
		album.QualityOrder = append(album.QualityOrder, "root")
		firstAudioPath = rootFirst
	}

	// Each subdirectory becomes a named quality (alphabetical order).
	for _, dir := range subDirs {
		qDir := filepath.Join(albumDir, dir)
		tracks, first, scanErr := scanTracks(musicDir, qDir)
		if scanErr != nil {
			log.Printf("scanner: skipping quality %q in %q: %v", dir, folderName, scanErr)
			continue
		}
		if len(tracks) == 0 {
			continue
		}
		album.Qualities[dir] = model.Quality{Tracks: tracks}
		album.QualityOrder = append(album.QualityOrder, dir)
		if firstAudioPath == "" {
			firstAudioPath = first
		}
	}

	// Cover art: file-based resolution first, then embedded tags.
	if artPath := resolveArtFile(albumDir, subDirs); artPath != "" {
		album.Art = fileURL(musicDir, artPath)
	} else if firstAudioPath != "" {
		if pic := embeddedPicture(firstAudioPath); pic != nil {
			album.ArtBytes = pic.Data
			album.ArtMIME = pic.MIMEType
			album.Art = "/api/art/" + album.ID
		}
	}

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
		if isAudioFile(strings.ToLower(e.Name())) {
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
	if f, err := os.Open(path); err == nil {
		if m, err := tag.ReadFrom(f); err == nil {
			title = m.Title()
		}
		f.Close()
	}
	duration = durationFor(path)
	return title, duration
}
