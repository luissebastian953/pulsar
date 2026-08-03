package ingest

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func encode(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestNormalizeToNearestBucket(t *testing.T) {
	cases := []struct{ w, h, wantW, wantH int }{
		{100, 150, 832, 1216},  // portrait → portrait bucket
		{500, 500, 1024, 1024}, // square
		{1920, 1080, 1344, 768},
		{4000, 2400, 1344, 768}, // 1.67 ar sits nearer the 1.75 bucket than 1.46
	}
	for _, c := range cases {
		res, err := Normalize(encode(t, c.w, c.h))
		if err != nil {
			t.Fatal(err)
		}
		if res.Width != c.wantW || res.Height != c.wantH {
			t.Errorf("%dx%d → %dx%d, want %dx%d", c.w, c.h, res.Width, res.Height, c.wantW, c.wantH)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(res.PNG))
		if err != nil || cfg.Width != c.wantW {
			t.Errorf("re-decode: %v %d", err, cfg.Width)
		}
	}
}

func TestNormalizeRejects(t *testing.T) {
	if _, err := Normalize([]byte("not an image")); err == nil {
		t.Error("garbage must not decode")
	}
	if _, err := Normalize(make([]byte, MaxBytes+1)); err == nil {
		t.Error("oversize must be rejected")
	}
}

// TestBustCrop: tall images keep only the top portion; squarish pass through.
func TestBustCrop(t *testing.T) {
	tall := encode(t, 400, 800)
	out, err := BustCrop(tall)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil || cfg.Width != 400 || cfg.Height != 360 {
		t.Errorf("tall crop = %dx%d (%v), want 400x360", cfg.Width, cfg.Height, err)
	}

	// square canvases crop too — the CALLER gates by framing tags, since
	// canvas aspect is not a reliable proxy for subject framing
	square := encode(t, 800, 800)
	out, err = BustCrop(square)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err = image.DecodeConfig(bytes.NewReader(out))
	if err != nil || cfg.Height != 360 {
		t.Errorf("square crop height = %d (%v), want 360", cfg.Height, err)
	}
}
