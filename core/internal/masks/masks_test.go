package masks

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestColumns(t *testing.T) {
	ms, err := Columns(2, 832, 1216, 0.06)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 {
		t.Fatalf("got %d masks", len(ms))
	}
	imgs := make([]image.Image, 2)
	for i, m := range ms {
		img, err := png.Decode(bytes.NewReader(m))
		if err != nil {
			t.Fatal(err)
		}
		if img.Bounds().Dx() != 832 || img.Bounds().Dy() != 1216 {
			t.Fatalf("mask %d dims %v", i, img.Bounds())
		}
		imgs[i] = img
	}
	lum := func(i int, x int) uint32 { r, _, _, _ := imgs[i].At(x, 600).RGBA(); return r >> 8 }
	// left mask: white on its side, black on the other; feather at the seam
	if lum(0, 100) < 250 || lum(0, 700) > 5 {
		t.Errorf("mask A sides: %d %d", lum(0, 100), lum(0, 700))
	}
	if lum(1, 100) > 5 || lum(1, 700) < 250 {
		t.Errorf("mask B sides: %d %d", lum(1, 100), lum(1, 700))
	}
	if s := lum(0, 416); s < 60 || s > 200 {
		t.Errorf("seam should be feathered, got %d", s)
	}
	if _, err := Columns(5, 100, 100, 0.06); err == nil {
		t.Error("5 columns must be rejected")
	}
}
