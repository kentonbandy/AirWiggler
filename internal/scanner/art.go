package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dhowden/tag"
)

// rootArtNames are checked in order in the album root directory.
var rootArtNames = []string{"cover.jpg", "folder.jpg", "cover.png"}

// resolveArtFile checks for image files in the album root and quality subdirs.
func resolveArtFile(albumDir string, subDirs []string) string {
	for _, name := range rootArtNames {
		if p := filepath.Join(albumDir, name); fileExists(p) {
			return p
		}
	}
	imageExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp"}
	for _, dir := range subDirs {
		qDir := filepath.Join(albumDir, dir)
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
