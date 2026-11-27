package ingest

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"strings"
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

// 12x8 WebP fixtures, left half red (200,30,30), right half blue
// (30,30,200): `cwebp -lossless` and `cwebp -q 80` of the same PNG.
const (
	webpLossless = "UklGRigAAABXRUJQVlA4TBwAAAAvC8ABAA8wHvdQvRXy/EcLBAJJZvuTrRDR/8gB"
	webpLossy    = "UklGRlgAAABXRUJQVlA4IEwAAACwAgCdASoMAAgAAUAmJagCdEcAfwDKAP0ABSqOTrgA/vV7++RMPEmA///cV1xH//cZJ//sV1xH+4R/9Xp/zOi2Xpb//L0/5nRbJ0AA"
)

func unb64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestNormalizeWebP: a WebP upload goes down the same path as PNG and JPEG
// and comes out as a bucket-sized PNG with nothing but pixels in it.
func TestNormalizeWebP(t *testing.T) {
	for name, tol := range map[string]int{webpLossless: 0, webpLossy: 40} {
		res, err := Normalize(unb64(t, name))
		if err != nil {
			t.Fatal(err)
		}
		// 12:8 is nearest the 1216x832 bucket
		if res.Width != 1216 || res.Height != 832 {
			t.Errorf("webp → %dx%d, want 1216x832", res.Width, res.Height)
		}
		if !bytes.HasPrefix(res.PNG, []byte("\x89PNG\r\n\x1a\n")) || bytes.Contains(res.PNG, []byte("WEBP")) {
			t.Fatal("output is not a PNG")
		}
		img, format, err := image.Decode(bytes.NewReader(res.PNG))
		if err != nil || format != "png" {
			t.Fatalf("re-decode: %v %s", err, format)
		}
		near := func(got color.Color, r, g, b int) bool {
			c := color.NRGBAModel.Convert(got).(color.NRGBA)
			d := func(a uint8, w int) bool { x := int(a) - w; return x >= -tol && x <= tol }
			return d(c.R, r) && d(c.G, g) && d(c.B, b) && c.A == 255
		}
		if !near(img.At(200, 400), 200, 30, 30) || !near(img.At(1000, 400), 30, 30, 200) {
			t.Errorf("pixels: left %v right %v", img.At(200, 400), img.At(1000, 400))
		}
		// only critical chunks: nothing of the source container survives
		for _, typ := range chunkTypes(t, res.PNG) {
			if typ != "IHDR" && typ != "IDAT" && typ != "IEND" {
				t.Errorf("normalized PNG carries a %s chunk", typ)
			}
		}
	}
}

// Metadata in a WebP (EXIF, XMP — where NovelAI and ComfyUI put prompts)
// never reaches the stored blob.
func TestNormalizeWebPDropsMetadata(t *testing.T) {
	const secret = "prompt-in-exif-SECRET"
	src := unb64(t, webpLossless)
	// RIFF 'WEBP' VP8L… → append EXIF and XMP chunks and fix the RIFF size
	chunk := func(fourcc, body string) []byte {
		out := append([]byte(fourcc), 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(out[4:], uint32(len(body)))
		out = append(out, body...)
		if len(body)%2 == 1 {
			out = append(out, 0)
		}
		return out
	}
	withMeta := append([]byte{}, src...)
	withMeta = append(withMeta, chunk("EXIF", "Exif\x00\x00"+secret)...)
	withMeta = append(withMeta, chunk("XMP ", "<x:xmpmeta>"+secret+"</x:xmpmeta>")...)
	binary.LittleEndian.PutUint32(withMeta[4:], uint32(len(withMeta)-8))
	if !bytes.Contains(withMeta, []byte(secret)) {
		t.Fatal("fixture lost its metadata")
	}
	res, err := Normalize(withMeta)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(res.PNG, []byte(secret)) || bytes.Contains(res.PNG, []byte("xmpmeta")) {
		t.Error("WebP metadata survived normalization")
	}
}

func TestNormalizeRejectsHostileWebP(t *testing.T) {
	// a VP8L header declaring 16383x16383 over almost no data
	huge := unb64(t, webpLossless)
	// VP8L: after the 0x2f signature byte, 14 bits width-1 then 14 bits height-1
	binary.LittleEndian.PutUint32(huge[21:], 0x3fff|0x3fff<<14)
	if _, err := Normalize(huge); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("16383x16383 WebP: err = %v, want the pixel limit", err)
	}
	// a PNG that only claims to be huge is refused before decoding too
	big := encode(t, 2, 2)
	binary.BigEndian.PutUint32(big[16:], 60000)
	binary.BigEndian.PutUint32(big[20:], 60000)
	binary.BigEndian.PutUint32(big[29:], crc32.ChecksumIEEE(big[12:29]))
	if _, err := Normalize(big); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("60000x60000 PNG: err = %v, want the pixel limit", err)
	}
	for name, data := range map[string][]byte{
		"truncated":     unb64(t, webpLossless)[:30],
		"riff not webp": append([]byte("RIFF\x10\x00\x00\x00WAVE"), make([]byte, 12)...),
		"animated":      append([]byte("RIFF\x1e\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02\x00\x00\x00\x0b\x00\x00\x07\x00\x00ANIM"), make([]byte, 8)...),
		"gif":           []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"),
	} {
		if _, err := Normalize(data); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}

func chunkTypes(t *testing.T, data []byte) []string {
	t.Helper()
	var out []string
	for p := 8; p+8 <= len(data); {
		n := int(binary.BigEndian.Uint32(data[p:]))
		out = append(out, string(data[p+4:p+8]))
		p += 12 + n
	}
	return out
}
