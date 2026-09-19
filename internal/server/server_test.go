package server

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kento/airwiggler/internal/model"
)

func testServer(t *testing.T) (*Server, string) {
	t.Helper()
	musicDir := t.TempDir()
	albumDir := filepath.Join(musicDir, "Album")
	if err := os.Mkdir(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBP"), []byte("test")...)
	if err := os.WriteFile(filepath.Join(albumDir, "cover.webp"), webp, 0o644); err != nil {
		t.Fatal(err)
	}
	webFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(`<!DOCTYPE html><html><head><title>AirWiggler</title><link rel="stylesheet" href="style.css"><script src="app.js"></script></head><body></body></html>`)},
		"style.css":  &fstest.MapFile{Data: []byte(`body{}`)},
		"app.js":     &fstest.MapFile{Data: []byte(``)},
	}
	s := New(Config{SiteTitle: "Test Music", Version: "test", MusicDir: musicDir, AccessToken: "secret"}, fs.FS(webFS))
	s.library = &model.Library{Albums: []model.Album{{
		ID:           "album-1",
		Title:        `A & B "Album"`,
		Art:          "/music/Album/cover.webp",
		Qualities:    map[string]model.Quality{"lossless": {}},
		QualityOrder: []string{"lossless"},
	}}}
	albumToken, ok := albumTokenFor("album-1", s.accessToken, s.albumTokens)
	if !ok {
		t.Fatal("expected album token")
	}
	return s, albumToken
}

func TestAlbumPageOpenGraphWithToken(t *testing.T) {
	s, token := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/album/album-1?token="+token, nil)
	req.Host = "example.test"
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		`<meta property="og:site_name" content="Test Music">`,
		`<meta property="og:title" content="A &amp; B &#34;Album&#34;">`,
		`<meta property="og:image" content="http://example.test/album/album-1/art?token=` + token + `">`,
		`href="/style.css?v=test"`,
		`src="/app.js?v=test"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %q\n%s", want, body)
		}
	}
	if rr.Header().Get("Set-Cookie") == "" {
		t.Fatal("expected album page token exchange to set a session cookie")
	}
}

func TestAlbumPreviewArtWithToken(t *testing.T) {
	s, token := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/album/album-1/art?token="+token, nil)
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "image/webp" {
		t.Fatalf("Content-Type = %q, want image/webp", got)
	}
	if rr.Header().Get("Set-Cookie") != "" {
		t.Fatal("preview art token validation should not issue a session cookie")
	}
}

func TestAlbumPageRejectsInvalidToken(t *testing.T) {
	s, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/album/album-1?token=wrong", nil)
	rr := httptest.NewRecorder()

	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
}
