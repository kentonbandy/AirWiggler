package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/kento/airwiggler/internal/server"
)

//go:embed web
var webFiles embed.FS

func main() {
	cfg := server.Config{
		Port:                  envOr("APP_PORT", "8080"),
		SiteTitle:             envOr("SITE_TITLE", "My Music Library"),
		DefaultQuality:        envOr("DEFAULT_QUALITY", "medium"),
		MusicDir:              envOr("MUSIC_DIR", "/music"),
		AccessToken:           os.Getenv("ACCESS_TOKEN"),
		NotifyURL:             os.Getenv("NOTIFY_URL"),
		BruteForceThreshold:   envInt("NOTIFY_BRUTE_FORCE_THRESHOLD", 20),
		AuthGrantThreshold:    envInt("NOTIFY_AUTH_GRANT_THRESHOLD", 10),
		RescanOnStart:         envBool("RESCAN_ON_START", true),
		RescanIntervalMinutes: envInt("RESCAN_INTERVAL_MINUTES", 15),
	}

	// Serve files from the web/ subdirectory at the root URL.
	webRoot, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatalf("failed to create web sub-filesystem: %v", err)
	}

	srv := server.New(cfg, webRoot)

	addr := ":" + cfg.Port
	log.Printf("AirWiggler listening on %s (music: %s)", addr, cfg.MusicDir)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}
