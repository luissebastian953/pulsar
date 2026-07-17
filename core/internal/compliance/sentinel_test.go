package compliance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pulsar/core/internal/assembly"
)

const forbidden = "loli, shota, child, toddler, aged down"

func fakeSentinel(t *testing.T, rating, tags map[string]float64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"rating": rating, "tags": tags})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOutputPolicy(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		rating map[string]float64
		tags   map[string]float64
		allow  bool
	}{
		{"clean adult", map[string]float64{"general": 0.9}, map[string]float64{"1girl": 0.99}, true},
		{"age tag over gate", map[string]float64{"general": 0.8}, map[string]float64{"loli": 0.5}, false},
		{"faint noise under both gates", map[string]float64{"general": 0.8}, map[string]float64{"child": 0.02}, true},
		// the calibration case: seed-7 pose output — individually faint age
		// tags whose sum clears the sum gate (real scores from that image)
		{"sum rule catches faint ensemble", map[string]float64{"sensitive": 0.92},
			map[string]float64{"loli": 0.0331, "child": 0.0541, "aged_down": 0.0056}, false},
		// explicit content halves both gates — false negatives are catastrophic
		{"explicit halves gate", map[string]float64{"explicit": 0.7}, map[string]float64{"loli": 0.2}, false},
		{"explicit halves sum gate", map[string]float64{"explicit": 0.7}, map[string]float64{"child": 0.015, "loli": 0.015}, false},
		{"explicit but clean", map[string]float64{"explicit": 0.9}, map[string]float64{"1girl": 0.99}, true},
		{"aged down normalized", map[string]float64{"general": 0.9}, map[string]float64{"aged_down": 0.6}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSentinel(fakeSentinel(t, tc.rating, tc.tags).URL, forbidden, 0.35, 0.05)
			dec, err := s.CheckOutput(ctx, []byte("img"))
			if err != nil {
				t.Fatal(err)
			}
			if dec.Allowed != tc.allow {
				t.Errorf("allowed = %v (%s), want %v", dec.Allowed, dec.Reason, tc.allow)
			}
		})
	}
}

func TestThresholdClamp(t *testing.T) {
	// env can tune the gate but never disable it
	s := NewSentinel(fakeSentinel(t, nil, map[string]float64{"loli": 0.7}).URL, forbidden, 99, 99)
	dec, err := s.CheckOutput(context.Background(), []byte("img"))
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allowed {
		t.Error("threshold 99 must clamp, not disable the gate")
	}
}

func TestPromptRejection(t *testing.T) {
	s := NewSentinel("http://unused", forbidden, 0, 0)
	dec, err := s.CheckPrompt(context.Background(), assembly.Spec{Prompt: "masterpiece, 1girl, Loli, smile"})
	if err != nil || dec.Allowed {
		t.Errorf("forbidden tag in prompt must reject: %+v, %v", dec, err)
	}
	dec, err = s.CheckPrompt(context.Background(), assembly.Spec{Prompt: "masterpiece, 1girl, lolita_fashion"})
	if err != nil || !dec.Allowed {
		t.Errorf("substring must not reject: %+v, %v", dec, err)
	}
}

func TestSentinelDownFailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	s := NewSentinel(srv.URL, forbidden, 0, 0)
	if _, err := s.CheckOutput(context.Background(), []byte("img")); err == nil {
		t.Error("sentinel failure must return an error (callers block on error)")
	}
}
