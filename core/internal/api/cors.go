package api

import (
	"net/http"
	"slices"
)

// cors serves orbit dev and the shells; bearer auth, so no
// Allow-Credentials (docs/frontend.md §6.4).
//
// Cache safety matters here more than it looks: /images responses are
// cached for a year (immutable), and a no-cors <img> hit stores whatever
// headers this middleware emitted for a request WITHOUT an Origin. If the
// allow-origin header is only present on Origin-bearing requests, the
// cached copy poisons every later CORS fetch of the same URL — the silent
// "Download does nothing" bug. So: wildcard emits the header on every
// response, and named-origin lists emit Vary: Origin on every response.
func cors(next http.Handler, origins []string) http.Handler {
	wildcard := slices.Contains(origins, "*")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wildcard {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			w.Header().Add("Vary", "Origin")
			if origin := r.Header.Get("Origin"); origin != "" && slices.Contains(origins, origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
