package server

import (
	"crypto/subtle"
	"net/http"
)

const cookieName = "aw_session"

// tokenMiddleware wraps h with shared-token access control.
// When token is empty it returns h unchanged (no-op).
func tokenMiddleware(token string, cookieSecure bool, n *notifier, bfCounter, agCounter *eventCounter, bfThreshold, agThreshold int, h http.Handler) http.Handler {
	if token == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Token exchange: /?token=<value> — validate, set cookie, redirect.
		if r.URL.Path == "/" {
			if provided := r.URL.Query().Get("token"); provided != "" {
				if !secureEqual(provided, token) {
					// Failed token attempt — track for brute-force detection.
					bfCounter.record()
					if bfCounter.count() >= bfThreshold {
						n.send("AirWiggler: Possible brute-force detected",
							"20+ failed token attempts in the last minute.\nConsider rotating ACCESS_TOKEN if this persists.")
					}
					http.Error(w, "invalid token", http.StatusForbidden)
					return
				}
				http.SetCookie(w, &http.Cookie{
					Name:     cookieName,
					Value:    token,
					Path:     "/",
					MaxAge:   60 * 60 * 24 * 30, // 30 days
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
					Secure:   cookieSecure,
				})
				// New session granted — track for link-circulating detection.
				agCounter.record()
				if agCounter.count() >= agThreshold {
					n.send("AirWiggler: Unusual authentication activity",
						"10+ new sessions granted in the last hour.\nYour share link may be circulating beyond intended recipients.\nConsider rotating ACCESS_TOKEN and re-sharing selectively.")
				}
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
		}

		// All other requests: require a valid session cookie.
		cookie, err := r.Cookie(cookieName)
		if err != nil || !secureEqual(cookie.Value, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		h.ServeHTTP(w, r)
	})
}

func secureEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
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
