package main

import (
	"bufio"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/kento/airwiggler/internal/server"
)

//go:embed web
var webFiles embed.FS

// version is stamped at build time via -ldflags "-X main.version=<tag>".
var version = "dev"

func main() {
	loadDotEnv(".env")

	cfg := server.Config{
		Port:                  envOr("APP_PORT", "8080"),
		SiteTitle:             envOr("SITE_TITLE", "My Music Library"),
		DefaultQuality:        envOr("DEFAULT_QUALITY", "high"),
		MusicDir:              envOr("MUSIC_DIR", "/music"),
		Version:               version,
		AccessToken:           os.Getenv("ACCESS_TOKEN"),
		AlbumTokens:           envMap("ALBUM_ACCESS_TOKENS"),
		AuthProxyHeader:       os.Getenv("AUTH_PROXY_HEADER"),
		NotifyURL:             os.Getenv("NOTIFY_URL"),
		BruteForceThreshold:   envInt("NOTIFY_BRUTE_FORCE_THRESHOLD", 20),
		AuthGrantThreshold:    envInt("NOTIFY_AUTH_GRANT_THRESHOLD", 10),
		RescanOnStart:         envBool("RESCAN_ON_START", true),
		RescanIntervalMinutes: envInt("RESCAN_INTERVAL_MINUTES", 15),
		CookieSecure:          envBool("COOKIE_SECURE", true),
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

// envMap parses comma-separated key=value pairs from an environment variable.
func envMap(key string) map[string]string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	m := make(map[string]string)
	for _, pair := range strings.Split(v, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		k, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		value = strings.TrimSpace(value)
		if k != "" && value != "" {
			m[k] = value
		}
	}
	return m
}

// loadDotEnv reads KEY=VALUE pairs from path and sets them in the environment,
// silently skipping the file if it does not exist. Existing env vars are never
// overwritten, so real environment variables always take precedence.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // file absent — no-op
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		// Strip optional surrounding quotes.
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
}
