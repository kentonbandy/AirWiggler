package server

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kento/airwiggler/internal/model"
	"github.com/kento/airwiggler/internal/scanner"
)

// Server holds app state and HTTP handlers.
type Server struct {
	musicDir       string
	webFS          fs.FS
	siteTitle      string
	defaultQuality string
	accessToken    string
	cookieSecure   bool

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
	MusicDir              string
	AccessToken           string
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
		accessToken:         cfg.AccessToken,
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
	mux.HandleFunc("/api/art/", s.handleArt)

	// Serve audio files and file-based cover art directly from the music dir.
	mux.Handle("/music/", http.StripPrefix("/music/", http.FileServer(http.Dir(s.musicDir))))

	// Serve embedded frontend. Force revalidation on every request so that
	// browsers never serve stale JS/CSS after a container update.
	mux.Handle("/", noCacheFS(http.FileServer(http.FS(s.webFS))))

	return securityHeaders(tokenMiddleware(s.accessToken, s.cookieSecure, s.notifier, s.bruteForceCounter, s.authGrantCounter, s.bruteForceThreshold, s.authGrantThreshold, mux))
}

// noCacheFS wraps a handler and sets Cache-Control: no-cache on every response.
// The browser still sends a conditional request (ETag/Last-Modified), so unchanged
// files are served as 304 Not Modified — no wasted bandwidth.
func noCacheFS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		h.ServeHTTP(w, r)
	})
}

	return securityHeaders(tokenMiddleware(s.accessToken, s.cookieSecure, s.notifier, s.bruteForceCounter, s.authGrantCounter, s.bruteForceThreshold, s.authGrantThreshold, mux))
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
	}{
		SiteTitle:      s.siteTitle,
		DefaultQuality: s.defaultQuality,
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

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(lib); err != nil {
		log.Printf("server: encode library: %v", err)
	}
}

// handleRescan serves POST /api/rescan.
func (s *Server) handleRescan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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

// handleArt serves GET /api/art/<id> for albums with embedded cover art.
func (s *Server) handleArt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/art/")
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
