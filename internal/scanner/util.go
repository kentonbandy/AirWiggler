package scanner

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

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

// isAudioFile reports whether the filename has a recognised audio extension.
func isAudioFile(lower string) bool {
	switch filepath.Ext(lower) {
	case ".flac", ".mp3", ".wav", ".m4a", ".aac", ".ogg", ".opus", ".aif", ".aiff":
		return true
	}
	return false
}
