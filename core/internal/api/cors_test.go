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
