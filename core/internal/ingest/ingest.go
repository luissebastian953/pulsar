// Package ingest normalizes uploaded images: decode (PNG, JPEG or still
// WebP) → scale-and-crop to the nearest SDXL bucket → re-encode PNG, which
// drops EXIF, XMP, text chunks and any pixel-LSB payload
// (.claude/skills/core-job). It is the one ingest path: every format an
// upload may arrive in is decoded here, and the caller runs CheckUpload on
// the normalized PNG — never on the bytes as uploaded.
package ingest

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"math"

	"golang.org/x/image/draw"
	// WebP is what NovelAI (and most hosts) hand out; decode only — the
	// output is always PNG. Animated WebP does not decode and is refused.
	_ "golang.org/x/image/webp"

	"pulsar/core/internal/assembly"
)

const MaxBytes = 20 << 20

// MaxPixels bounds the decoded size, checked from the header before any
// pixel is decoded: a few KiB of lossless WebP or PNG can declare a
// 16383×16383 canvas (1 GiB as RGBA).
const MaxPixels = 50_000_000

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
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if format != "png" && format != "jpeg" && format != "webp" {
		return nil, fmt.Errorf("unsupported format %q (png, jpeg or webp)", format)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return nil, fmt.Errorf("image is %dx%d; the limit is %d pixels", cfg.Width, cfg.Height, MaxPixels)
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	sb := src.Bounds()
	ar := float64(sb.Dx()) / float64(sb.Dy())
	best := assembly.Buckets[0]
	bestDiff := math.Inf(1)
	for _, b := range assembly.Buckets {
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
