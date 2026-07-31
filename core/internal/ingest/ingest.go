// Package ingest normalizes uploaded images: decode (PNG/JPEG only) →
// scale-and-crop to the nearest SDXL bucket → re-encode PNG, which drops
// EXIF and every other metadata chunk (.claude/skills/core-job).
package ingest

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"math"

	"golang.org/x/image/draw"
)

var buckets = [][2]int{{1024, 1024}, {896, 1152}, {832, 1216}, {1216, 832}, {1344, 768}}

const MaxBytes = 20 << 20

type Result struct {
	PNG    []byte
	Width  int
	Height int
}

// Normalize decodes, fits to the aspect-nearest bucket (cover + center
// crop), and re-encodes. Off-bucket inputs degrade SDXL coherence, so the
// server owns this rather than trusting clients.
func Normalize(data []byte) (*Result, error) {
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("image exceeds %d bytes", MaxBytes)
	}
	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if format != "png" && format != "jpeg" {
		return nil, fmt.Errorf("unsupported format %q (png or jpeg)", format)
	}

	sb := src.Bounds()
	ar := float64(sb.Dx()) / float64(sb.Dy())
	best := buckets[0]
	bestDiff := math.Inf(1)
	for _, b := range buckets {
		if d := math.Abs(math.Log(ar / (float64(b[0]) / float64(b[1])))); d < bestDiff {
			best, bestDiff = b, d
		}
	}
	w, h := best[0], best[1]

	// cover: scale so both dimensions reach the bucket, then center-crop
	scale := max(float64(w)/float64(sb.Dx()), float64(h)/float64(sb.Dy()))
	sw, sh := int(math.Round(float64(sb.Dx())*scale)), int(math.Round(float64(sb.Dy())*scale))
	scaled := image.NewRGBA(image.Rect(0, 0, sw, sh))
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), src, sb, draw.Over, nil)

	out := image.NewRGBA(image.Rect(0, 0, w, h))
	off := image.Pt((sw-w)/2, (sh-h)/2)
	draw.Draw(out, out.Bounds(), scaled, off, draw.Src)

	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return &Result{PNG: buf.Bytes(), Width: w, Height: h}, nil
}

// BustCrop keeps the top 45% of the image — for reference conditioning,
// identity lives in the face; a full-body gown imports its fabric texture
// into everything the adapter touches (verified live: cropping the
// exemplar to the bust eliminated the veil artifact). The caller gates on
// the exemplar's framing TAGS — canvas aspect is not a reliable proxy (a
// square official render holds a full body with margins).
func BustCrop(data []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	b := src.Bounds()
	crop := image.Rect(b.Min.X, b.Min.Y, b.Max.X, b.Min.Y+b.Dy()*45/100)
	dst := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	draw.Copy(dst, image.Point{}, src, crop, draw.Src, nil)
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
