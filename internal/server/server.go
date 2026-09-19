package server

import (
	"bytes"
	"encoding/json"
	"html"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kento/airwiggler/internal/model"
	"github.com/kento/airwiggler/internal/scanner"
)

// Server holds app state and HTTP handlers.
type Server struct {
	musicDir        string
	webFS           fs.FS
	siteTitle       string
	defaultQuality  string
	version         string
	accessToken     string
	albumTokens     map[string]string
	authProxyHeader string
	cookieSecure    bool

	notifier          *notifier
	bruteForceCounter *eventCounter
	authGrantCounter  *eventCounter

	bruteForceThreshold int
	authGrantThreshold  int

	mu       sync.Mutex
	scanning bool
	library  *model.Library
	artIndex map[string]*model.Album // album ID → album (for embedded art)
}

// Config holds runtime configuration from environment variables.
type Config struct {
	Port                  string
	SiteTitle             string
	DefaultQuality        string
	Version               string
	MusicDir              string
	AccessToken           string
	AlbumTokens           map[string]string
	AuthProxyHeader       string
	NotifyURL             string
	BruteForceThreshold   int
	AuthGrantThreshold    int
	RescanOnStart         bool
	RescanIntervalMinutes int
	CookieSecure          bool
}

// New creates a Server and performs an initial scan if configured.
func New(cfg Config, webFS fs.FS) *Server {
	s := &Server{
		musicDir:            cfg.MusicDir,
		webFS:               webFS,
		siteTitle:           cfg.SiteTitle,
		defaultQuality:      cfg.DefaultQuality,
		version:             cfg.Version,
		accessToken:         cfg.AccessToken,
		albumTokens:         cfg.AlbumTokens,
		authProxyHeader:     cfg.AuthProxyHeader,
		cookieSecure:        cfg.CookieSecure,
		notifier:            newNotifier(cfg.NotifyURL),
		bruteForceCounter:   newEventCounter(time.Minute),
		authGrantCounter:    newEventCounter(time.Hour),
		bruteForceThreshold: cfg.BruteForceThreshold,
		authGrantThreshold:  cfg.AuthGrantThreshold,
		library:             &model.Library{},
		artIndex:            make(map[string]*model.Album),
	}

	if cfg.RescanOnStart {
		s.runScan()
	}

	if cfg.RescanIntervalMinutes > 0 {
		go func() {
			t := time.NewTicker(time.Duration(cfg.RescanIntervalMinutes) * time.Minute)
			defer t.Stop()
			for range t.C {
				s.runScan()
			}
		}()
	}

	return s
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/library", s.handleLibrary)
	mux.HandleFunc("/api/rescan", s.handleRescan)
	mux.HandleFunc("/api/share", s.handleShare)
	mux.HandleFunc("/api/art/", s.handleArt)
	mux.HandleFunc("/album/", s.handleAlbumRoute)

	// Serve audio files and file-based cover art directly from the music dir.
	mux.HandleFunc("/music/", s.handleMusic)

	// Serve embedded frontend. Force revalidation on every request so that
	// browsers never serve stale JS/CSS after a container update.
	// index.html is served via a dedicated handler that injects versioned
	// asset URLs so CDNs (e.g. Cloudflare) treat each release as new URLs.
	mux.HandleFunc("/", s.handleIndex)
	mux.Handle("/style.css", noCacheFS(http.FileServer(http.FS(s.webFS))))
	mux.Handle("/app.js", noCacheFS(http.FileServer(http.FS(s.webFS))))

	var authHandler http.Handler
	switch {
	case s.authProxyHeader != "":
		if s.accessToken != "" {
			log.Println("WARNING: both AUTH_PROXY_HEADER and ACCESS_TOKEN are set; AUTH_PROXY_HEADER takes priority")
		}
		authHandler = headerAuthMiddleware(s.authProxyHeader, mux)
	default:
		authHandler = tokenMiddleware(s.accessToken, s.albumTokens, s.cookieSecure, s.notifier, s.bruteForceCounter, s.authGrantCounter, s.bruteForceThreshold, s.authGrantThreshold, mux)
	}
	return securityHeaders(authHandler)
}

// handleIndex serves index.html with versioned asset URLs injected so that
// CDNs cache-bust automatically on each new release.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		// Fall through to the embedded filesystem for other static assets.
		noCacheFS(http.FileServer(http.FS(s.webFS))).ServeHTTP(w, r)
		return
	}
	f, err := s.webFS.Open("index.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	v := s.version
	body = bytes.ReplaceAll(body, []byte(`href="style.css"`), []byte(`href="style.css?v=`+v+`"`))
	body = bytes.ReplaceAll(body, []byte(`src="app.js"`), []byte(`src="app.js?v=`+v+`"`))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(body)
}

// The browser still sends a conditional request (ETag/Last-Modified), so unchanged
// files are served as 304 Not Modified — no wasted bandwidth.
func noCacheFS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		h.ServeHTTP(w, r)
	})
}

// handleConfig serves GET /api/config — exposes runtime config to the frontend.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		SiteTitle      string `json:"siteTitle"`
		DefaultQuality string `json:"defaultQuality"`
		Version        string `json:"version"`
	}{
		SiteTitle:      s.siteTitle,
		DefaultQuality: s.defaultQuality,
		Version:        s.version,
	})
}

// handleLibrary serves GET /api/library.
func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	lib := s.library
	s.mu.Unlock()

	if albumID := albumScope(r); albumID != "" {
		filtered := &model.Library{}
		for _, album := range lib.Albums {
			if album.ID == albumID {
				filtered.Albums = append(filtered.Albums, album)
				break
			}
		}
		lib = filtered
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(lib); err != nil {
		log.Printf("server: encode library: %v", err)
	}
}

// handleShare serves GET /api/share?album=<id>. Only full-library sessions may
// generate share links.
func (s *Server) handleShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if albumScope(r) != "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	albumID := r.URL.Query().Get("album")
	if albumID == "" {
		http.Error(w, "missing album", http.StatusBadRequest)
		return
	}

	base := externalBaseURL(r)
	albumPath := "/album/" + url.PathEscape(albumID)
	albumOnlyLink := ""
	if token, ok := albumTokenFor(albumID, s.accessToken, s.albumTokens); ok {
		albumOnlyLink = base + albumPath + "?token=" + url.QueryEscape(token)
	}

	fullAccessLink := base + albumPath
	if s.accessToken != "" {
		fullAccessLink = base + albumPath + "?scope=full&token=" + url.QueryEscape(s.accessToken)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		AlbumOnlyLink  string `json:"albumOnlyLink,omitempty"`
		FullAccessLink string `json:"fullAccessLink"`
	}{
		AlbumOnlyLink:  albumOnlyLink,
		FullAccessLink: fullAccessLink,
	})
}

func externalBaseURL(r *http.Request) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host
}

// handleAlbumRoute serves album-specific HTML with Open Graph metadata, and
// token-validated album art for social previews.
func (s *Server) handleAlbumRoute(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/art") {
		s.handleAlbumPreviewArt(w, r)
		return
	}
	s.handleAlbumPage(w, r)
}

func (s *Server) handleAlbumPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	albumID := albumIDFromPath(r.URL.Path)
	if albumID == "" {
		http.NotFound(w, r)
		return
	}
	album, ok := s.findAlbum(albumID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if scopedAlbum := albumScope(r); scopedAlbum != "" && scopedAlbum != albumID {
		http.NotFound(w, r)
		return
	}

	f, err := s.webFS.Open("index.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	v := s.version
	body = bytes.ReplaceAll(body, []byte(`href="style.css"`), []byte(`href="/style.css?v=`+v+`"`))
	body = bytes.ReplaceAll(body, []byte(`src="app.js"`), []byte(`src="/app.js?v=`+v+`"`))
	body = bytes.ReplaceAll(body, []byte(`href="style.css?v=`), []byte(`href="/style.css?v=`))
	body = bytes.ReplaceAll(body, []byte(`src="app.js?v=`), []byte(`src="/app.js?v=`))

	pageURL := externalBaseURL(r) + r.URL.RequestURI()
	artURL := ""
	if album.Art != "" || len(album.ArtBytes) > 0 {
		artURL = externalBaseURL(r) + "/album/" + url.PathEscape(albumID) + "/art"
		if r.URL.RawQuery != "" {
			artURL += "?" + r.URL.RawQuery
		}
	}
	description := album.Title
	if len(album.QualityOrder) > 0 {
		description += " — " + strings.Join(album.QualityOrder, ", ")
	}
	meta := albumOpenGraphMeta(s.siteTitle, album.Title, description, pageURL, artURL)
	body = bytes.Replace(body, []byte("<head>"), []byte("<head>\n"+meta), 1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(body)
}

func albumOpenGraphMeta(siteTitle, title, description, pageURL, artURL string) string {
	lines := []string{
		`<meta property="og:site_name" content="` + html.EscapeString(siteTitle) + `">`,
		`<meta property="og:type" content="music.album">`,
		`<meta property="og:title" content="` + html.EscapeString(title) + `">`,
		`<meta property="og:description" content="` + html.EscapeString(description) + `">`,
		`<meta property="og:url" content="` + html.EscapeString(pageURL) + `">`,
	}
	if artURL != "" {
		lines = append(lines, `<meta property="og:image" content="`+html.EscapeString(artURL)+`">`)
	}
	lines = append(lines, `<meta name="twitter:card" content="summary_large_image">`)
	return strings.Join(lines, "\n") + "\n"
}

func (s *Server) handleAlbumPreviewArt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	albumID := albumIDFromPath(r.URL.Path)
	if albumID == "" {
		http.NotFound(w, r)
		return
	}
	album, ok := s.findAlbum(albumID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if scopedAlbum := albumScope(r); scopedAlbum != "" && scopedAlbum != albumID {
		http.NotFound(w, r)
		return
	}

	if len(album.ArtBytes) > 0 {
		mimeType := album.ArtMIME
		if mimeType == "" {
			mimeType = imageContentType("", album.ArtBytes)
		}
		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		if r.Method != http.MethodHead {
			w.Write(album.ArtBytes)
		}
		return
	}

	if !strings.HasPrefix(album.Art, "/music/") {
		http.NotFound(w, r)
		return
	}
	relURL := strings.TrimPrefix(album.Art, "/music/")
	rel, err := url.PathUnescape(relURL)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	path := filepath.Clean(filepath.Join(s.musicDir, filepath.FromSlash(rel)))
	root := filepath.Clean(s.musicDir)
	if path != root && !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", imageContentType(path, data))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if r.Method != http.MethodHead {
		w.Write(data)
	}
}

func imageContentType(path string, data []byte) string {
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	if path != "" {
		if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); t != "" {
			return t
		}
	}
	return http.DetectContentType(data)
}

func (s *Server) findAlbum(id string) (*model.Album, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.library.Albums {
		if s.library.Albums[i].ID == id {
			return &s.library.Albums[i], true
		}
	}
	return nil, false
}

// handleRescan serves POST /api/rescan.
func (s *Server) handleRescan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if albumScope(r) != "" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	s.mu.Lock()
	if s.scanning {
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"status":"already_running"}`))
		return
	}
	s.scanning = true
	s.mu.Unlock()

	go func() {
		s.runScan()
	}()

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"started"}`))
}

// handleMusic serves files under /music/. Album-scoped sessions may only fetch
// files belonging to their album.
func (s *Server) handleMusic(w http.ResponseWriter, r *http.Request) {
	if scopedAlbum := albumScope(r); scopedAlbum != "" {
		s.mu.Lock()
		var allowedPrefix string
		for i := range s.library.Albums {
			if s.library.Albums[i].ID == scopedAlbum {
				allowedPrefix = albumMusicPrefix(&s.library.Albums[i])
				break
			}
		}
		s.mu.Unlock()

		if allowedPrefix == "" || !pathWithinPrefix(r.URL.EscapedPath(), allowedPrefix) {
			http.NotFound(w, r)
			return
		}
	}

	http.StripPrefix("/music/", http.FileServer(http.Dir(s.musicDir))).ServeHTTP(w, r)
}

func albumMusicPrefix(album *model.Album) string {
	for _, q := range album.QualityOrder {
		for _, t := range album.Qualities[q].Tracks {
			if prefix := musicAlbumPrefix(t.URL); prefix != "" {
				return prefix
			}
		}
	}
	for _, img := range album.Images {
		if prefix := musicAlbumPrefix(img); prefix != "" {
			return prefix
		}
	}
	if prefix := musicAlbumPrefix(album.Art); prefix != "" {
		return prefix
	}
	return ""
}

func musicAlbumPrefix(path string) string {
	if !strings.HasPrefix(path, "/music/") {
		return ""
	}
	rest := strings.TrimPrefix(path, "/music/")
	albumPart, _, _ := strings.Cut(rest, "/")
	if albumPart == "" {
		return ""
	}
	return "/music/" + albumPart
}

func pathWithinPrefix(path, prefix string) bool {
	decodedPath, err := url.PathUnescape(path)
	if err == nil {
		path = fileURLPathEscape(decodedPath)
	}
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func fileURLPathEscape(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// handleArt serves GET /api/art/<id> for albums with embedded cover art.
func (s *Server) handleArt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/art/")
	if scopedAlbum := albumScope(r); scopedAlbum != "" && scopedAlbum != id {
		http.NotFound(w, r)
		return
	}
	if id == "" {
		http.NotFound(w, r)
		return
	}

	s.mu.Lock()
	album, ok := s.artIndex[id]
	s.mu.Unlock()

	if !ok || len(album.ArtBytes) == 0 {
		http.NotFound(w, r)
		return
	}

	mime := album.ArtMIME
	if mime == "" {
		mime = "image/jpeg"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(album.ArtBytes)
}

// runScan performs a full scan and replaces the in-memory library.
func (s *Server) runScan() {
	s.mu.Lock()
	s.scanning = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.scanning = false
		s.mu.Unlock()
	}()

	lib, err := scanner.Scan(s.musicDir)
	if err != nil {
		log.Printf("server: scan failed: %v", err)
		return
	}

	// Rebuild art index.
	idx := make(map[string]*model.Album, len(lib.Albums))
	for i := range lib.Albums {
		if len(lib.Albums[i].ArtBytes) > 0 {
			idx[lib.Albums[i].ID] = &lib.Albums[i]
		}
	}

	s.mu.Lock()
	s.library = lib
	s.artIndex = idx
	s.mu.Unlock()

	log.Printf("server: scan complete, %d albums", len(lib.Albums))
}
