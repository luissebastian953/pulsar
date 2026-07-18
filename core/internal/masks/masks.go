// Package masks generates region masks for multi-character generation.
// The SAME masks drive regional conditioning and per-character IP-Adapter
// attn_mask — one implementation, shared by design (CLAUDE.md invariants).
package masks

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

// Columns splits w×h into n vertical bands with feathered boundaries
// (feather is the ramp width as a fraction of w) and returns one white-on-
// black PNG per band, left to right.
func Columns(n, w, h int, feather float64) ([][]byte, error) {
	if n < 1 || n > 4 {
		return nil, fmt.Errorf("columns must be 1..4, got %d", n)
	}
	fw := feather * float64(w)
	out := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		x0, x1 := float64(i*w)/float64(n), float64((i+1)*w)/float64(n)
		img := image.NewGray(image.Rect(0, 0, w, h))
		row := make([]uint8, w)
		for x := 0; x < w; x++ {
			v := 1.0
			fx := float64(x) + 0.5
			if i > 0 { // feather the left boundary
				v = min(v, clamp01((fx-(x0-fw/2))/fw))
			}
			if i < n-1 { // feather the right boundary
				v = min(v, clamp01(((x1+fw/2)-fx)/fw))
			}
			row[x] = uint8(v*254.0 + 0.5)
		}
		for y := 0; y < h; y++ {
			copy(img.Pix[y*img.Stride:], row)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
		out = append(out, buf.Bytes())
	}
	return out, nil
}

func clamp01(v float64) float64 { return min(max(v, 0), 1) }

var _ = color.Gray{} // keep image/color for future soft shapes
