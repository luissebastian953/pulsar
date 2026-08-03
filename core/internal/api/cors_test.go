package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"pulsar/core/internal/api"
)

func corsServer(origins ...string) http.Handler {
	return (&api.Server{Cfg: api.Config{CORSOrigins: origins}}).Handler()
}

func TestCORSPreflight(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/generate", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	corsServer("*").ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("allow-origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, Authorization" {
		t.Errorf("allow-headers = %q", got)
	}
}

func TestCORSExplicitOriginList(t *testing.T) {
	h := corsServer("capacitor://localhost", "https://pulsar.example")

	req := httptest.NewRequest(http.MethodOptions, "/generate", nil)
	req.Header.Set("Origin", "capacitor://localhost")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "capacitor://localhost" {
		t.Errorf("allowed origin echoed = %q", got)
	}

	req = httptest.NewRequest(http.MethodOptions, "/generate", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed origin got header %q", got)
	}
}

func TestCORSHeaderOnActualRequest(t *testing.T) {
	// a 404 route exercises the middleware without touching the DB
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	corsServer("*").ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("allow-origin on actual request = %q", got)
	}
}

func TestCORSDisabledWhenUnconfigured(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	corsServer().ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("CORS headers present with no config: %q", got)
	}
}

// TestCORSWildcardCacheSafe pins the poisoned-cache download fix: /images
// responses are cached for a year (immutable), and a no-cors <img> request
// carries no Origin header. If the wildcard allow-origin were only emitted
// for Origin-bearing requests, the cached copy would fail every later CORS
// fetch of the same URL — the silent "Download does nothing" bug. Wildcard
// must be unconditional.
func TestCORSWildcardCacheSafe(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil) // no Origin
	rec := httptest.NewRecorder()
	corsServer("*").ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("allow-origin without Origin header = %q, want *", got)
	}
}

// TestCORSNamedOriginVaries: when allow-origin depends on the request
// origin, every response must carry Vary: Origin — including responses to
// requests with no Origin at all — or shared caches mix them up.
func TestCORSNamedOriginVaries(t *testing.T) {
	h := corsServer("capacitor://localhost")

	for _, origin := range []string{"", "capacitor://localhost", "https://evil.example"} {
		req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("Vary"); got != "Origin" {
			t.Errorf("origin=%q: Vary = %q, want Origin", origin, got)
		}
	}
}
