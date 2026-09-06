package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
)

const cookieName = "aw_session"

type albumScopeContextKey struct{}

// albumScope returns the album ID this request is restricted to, or empty for
// full-library access.
func albumScope(r *http.Request) string {
	v, _ := r.Context().Value(albumScopeContextKey{}).(string)
	return v
}

// tokenMiddleware wraps h with full-library and album-scoped token access control.
// When no tokens are configured it returns h unchanged (no-op).
func tokenMiddleware(token string, albumTokens map[string]string, cookieSecure bool, n *notifier, bfCounter, agCounter *eventCounter, bfThreshold, agThreshold int, h http.Handler) http.Handler {
	if token == "" && len(albumTokens) == 0 {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Token exchange:
		//   /?token=<value>                  grants full-library access
		//   /?album=<id>&token=<value>       grants access to one album
		if r.URL.Path == "/" {
			if provided := r.URL.Query().Get("token"); provided != "" {
				albumID := r.URL.Query().Get("album")
				if albumID != "" && r.URL.Query().Get("scope") != "full" {
					if expected, ok := albumTokenFor(albumID, token, albumTokens); ok && secureEqual(provided, expected) {
						setSessionCookie(w, scopedAlbumCookieValue(albumID, provided), cookieSecure)
						recordAuthGrant(n, agCounter, agThreshold)
						http.Redirect(w, r, "/#album/"+url.PathEscape(albumID), http.StatusFound)
						return
					}
					invalidToken(w, n, bfCounter, bfThreshold)
					return
				}

				if token != "" && secureEqual(provided, token) {
					setSessionCookie(w, token, cookieSecure)
					recordAuthGrant(n, agCounter, agThreshold)
					redirectTo := "/"
					if albumID != "" {
						redirectTo = "/#album/" + url.PathEscape(albumID)
					}
					http.Redirect(w, r, redirectTo, http.StatusFound)
					return
				}

				invalidToken(w, n, bfCounter, bfThreshold)
				return
			}
		}

		// All other requests: require a valid session cookie.
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		if token != "" && secureEqual(cookie.Value, token) {
			h.ServeHTTP(w, r)
			return
		}

		if albumID, ok := validScopedAlbumCookie(cookie.Value, token, albumTokens); ok {
			ctx := context.WithValue(r.Context(), albumScopeContextKey{}, albumID)
			h.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func setSessionCookie(w http.ResponseWriter, value string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   60 * 60 * 24 * 30, // 30 days
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

func invalidToken(w http.ResponseWriter, n *notifier, bfCounter *eventCounter, bfThreshold int) {
	// Failed token attempt — track for brute-force detection.
	bfCounter.record()
	if bfCounter.count() >= bfThreshold {
		n.send("AirWiggler: Possible brute-force detected",
			"20+ failed token attempts in the last minute.\nConsider rotating ACCESS_TOKEN if this persists.")
	}
	http.Error(w, "invalid token", http.StatusForbidden)
}

func recordAuthGrant(n *notifier, agCounter *eventCounter, agThreshold int) {
	// New session granted — track for link-circulating detection.
	agCounter.record()
	if agCounter.count() >= agThreshold {
		n.send("AirWiggler: Unusual authentication activity",
			"10+ new sessions granted in the last hour.\nYour share link may be circulating beyond intended recipients.\nConsider rotating ACCESS_TOKEN and re-sharing selectively.")
	}
}

func scopedAlbumCookieValue(albumID, token string) string {
	return "album:" + url.QueryEscape(albumID) + ":" + url.QueryEscape(token)
}

func validScopedAlbumCookie(value, fullAccessToken string, albumTokens map[string]string) (string, bool) {
	if !strings.HasPrefix(value, "album:") {
		return "", false
	}
	value = strings.TrimPrefix(value, "album:")
	if value == "" {
		return "", false
	}
	albumPart, tokenPart, ok := strings.Cut(value, ":")
	if !ok {
		return "", false
	}
	albumID, err := url.QueryUnescape(albumPart)
	if err != nil {
		return "", false
	}
	provided, err := url.QueryUnescape(tokenPart)
	if err != nil {
		return "", false
	}
	expected, ok := albumTokenFor(albumID, fullAccessToken, albumTokens)
	if !ok || !secureEqual(provided, expected) {
		return "", false
	}
	return albumID, true
}

func albumTokenFor(albumID, fullAccessToken string, albumTokens map[string]string) (string, bool) {
	if token, ok := albumTokens[albumID]; ok && token != "" {
		return token, true
	}
	if fullAccessToken == "" {
		return "", false
	}
	mac := hmac.New(sha256.New, []byte(fullAccessToken))
	mac.Write([]byte("album:" + albumID))
	return hex.EncodeToString(mac.Sum(nil)), true
}

func secureEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// headerAuthMiddleware trusts a header set by an upstream reverse proxy (e.g.
// Authelia, Authentik) to signal that a request is authenticated. When the
// named header is present and non-empty the request is allowed through;
// otherwise a 401 is returned. When header is empty the middleware is a no-op.
//
// IMPORTANT: the container port must NOT be reachable directly from the
// internet — only from the trusted reverse proxy — otherwise this header can
// be spoofed.
func headerAuthMiddleware(header string, h http.Handler) http.Handler {
	if header == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(header) == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// securityHeaders adds standard security response headers to every response.
func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; media-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:")
		h.ServeHTTP(w, r)
	})
}
