package api

import (
	"net/http"
	"slices"
)

// cors serves cross-origin clients: orbit dev on another port, and later
// the Electron (file://) and Capacitor (capacitor://localhost) shells
// (docs/frontend.md §6.4). Auth is bearer tokens, never cookies, so no
// Allow-Credentials — a configured wildcard stays safe.
func cors(next http.Handler, origins []string) http.Handler {
	wildcard := slices.Contains(origins, "*")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			switch {
			case wildcard:
				w.Header().Set("Access-Control-Allow-Origin", "*")
			case slices.Contains(origins, origin):
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "86400")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
