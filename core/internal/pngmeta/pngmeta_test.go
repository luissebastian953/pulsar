package pngmeta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// raw frames one chunk by hand, independently of appendChunk.
func raw(typ string, data []byte) []byte {
	var b bytes.Buffer
	binary.Write(&b, binary.BigEndian, uint32(len(data)))
	b.WriteString(typ)
	b.Write(data)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(typ), data...)))
	return b.Bytes()
}

func basePNG(t testing.TB) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{uint8(40 * x), uint8(70 * y), 200, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// ihdrEnd is where the first post-IHDR chunk starts: 8 signature + 25 IHDR.
const ihdrEnd = 8 + 12 + ihdrLen

// inject splices pre-framed chunks in right after IHDR.
func inject(src []byte, chunks ...[]byte) []byte {
	out := append([]byte{}, src[:ihdrEnd]...)
	for _, c := range chunks {
		out = append(out, c...)
	}
	return append(out, src[ihdrEnd:]...)
}

func types(t testing.TB, data []byte) []string {
	t.Helper()
	cs, err := Chunks(data)
	if err != nil {
		t.Fatalf("Chunks: %v", err)
	}
	var out []string
	for _, c := range cs {
		out = append(out, c.Type)
	}
	return out
}

func idat(t testing.TB, data []byte) []byte {
	t.Helper()
	cs, err := Chunks(data)
	if err != nil {
		t.Fatalf("Chunks: %v", err)
	}
	var out []byte
	for _, c := range cs {
		if c.Type == "IDAT" {
			out = append(out, c.Data...)
		}
	}
	return out
}

func pixels(t testing.TB, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return img
}

func samePixels(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if color.RGBAModel.Convert(a.At(x, y)) != color.RGBAModel.Convert(b.At(x, y)) {
				return false
			}
		}
	}
	return true
}

func TestStripDropsMetadataKeepsColour(t *testing.T) {
	base := basePNG(t)
	secret := []byte("prompt\x00{\"8\":{\"class_type\":\"LoadImage\"}}")
	for _, typ := range []string{"tEXt", "zTXt", "iTXt", "eXIf", "tIME", "comf", "caBX", "acTL", "zzZz"} {
		t.Run("drops_"+typ, func(t *testing.T) {
			in := inject(base, raw(typ, secret))
			out, err := Strip(in)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out, base) {
				t.Errorf("Strip left %v, want the bare image", types(t, out))
			}
			if bytes.Contains(out, secret) {
				t.Error("payload survived the strip")
			}
		})
	}
	// gAMA 1/2.2, sRGB perceptual, 72 dpi: real values so image/png accepts them
	colour := map[string][]byte{
		"gAMA": {0, 0, 0xb1, 0x8f},
		"cHRM": make([]byte, 32),
		"sRGB": {0},
		"iCCP": []byte("p\x00\x00x"),
		"cICP": {1, 13, 0, 1},
		"mDCV": make([]byte, 24),
		"cLLI": make([]byte, 8),
		"sBIT": {8, 8, 8, 8},
		"pHYs": {0, 0, 0x0b, 0x13, 0, 0, 0x0b, 0x13, 1},
	}
	for typ, data := range colour {
		t.Run("keeps_"+typ, func(t *testing.T) {
			in := inject(base, raw(typ, data), raw("tEXt", secret))
			out, err := Strip(in)
			if err != nil {
				t.Fatal(err)
			}
			if want := inject(base, raw(typ, data)); !bytes.Equal(out, want) {
				t.Errorf("chunks = %v", types(t, out))
			}
		})
	}
	for _, typ := range []string{"IHDR", "PLTE", "tRNS", "IDAT", "IEND"} {
		if !Allowed(typ) {
			t.Errorf("critical chunk %s is not allow-listed", typ)
		}
	}
}

func TestStripKeepsPixelsBitExact(t *testing.T) {
	base := basePNG(t)
	in := inject(base,
		raw("gAMA", []byte{0, 0, 0xb1, 0x8f}),
		raw("tEXt", []byte("prompt\x00{}")),
		raw("iTXt", []byte("workflow\x00\x00\x00\x00\x00{}")),
	)
	in = append(in, "trailing bytes after IEND"...)
	out, err := Strip(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(idat(t, out), idat(t, in)) {
		t.Error("IDAT bytes changed")
	}
	if !samePixels(pixels(t, in), pixels(t, out)) {
		t.Error("decoded pixels changed")
	}
	if got := types(t, out); got[len(got)-1] != "IEND" || !bytes.HasSuffix(out, raw("IEND", nil)) {
		t.Errorf("output does not end at IEND: %v", got)
	}
	if bytes.Contains(out, []byte("trailing")) {
		t.Error("bytes after IEND survived")
	}
	again, err := Strip(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again, out) {
		t.Error("Strip is not idempotent")
	}
}

func TestMalformedFailsClosed(t *testing.T) {
	base := basePNG(t)
	badCRC := append([]byte{}, base...)
	badCRC[len(badCRC)-1] ^= 0xff // IEND crc
	badData := inject(base, raw("tEXt", []byte("prompt\x00{}")))
	badData[ihdrEnd+10] ^= 0xff // a byte inside the dropped chunk: still an error
	oversized := append([]byte{}, base...)
	binary.BigEndian.PutUint32(oversized[ihdrEnd:], 0x7fffffff)
	huge := append([]byte{}, base...)
	binary.BigEndian.PutUint32(huge[ihdrEnd:], 0xffffffff)
	shortIHDR := append([]byte(signature), raw("IHDR", make([]byte, 12))...)
	shortIHDR = append(shortIHDR, raw("IEND", nil)...)

	cases := map[string][]byte{
		"empty":           nil,
		"short signature": base[:5],
		"bad signature":   append([]byte("\x89PNG\r\n\x1a\x00"), base[8:]...),
		"signature only":  base[:8],
		"not a png":       []byte("GIF89a, definitely not a png at all"),
		"bad crc":         badCRC,
		"bad crc dropped": badData,
		"truncated chunk": base[:len(base)-20],
		"missing IEND":    base[:len(base)-12],
		"oversized len":   oversized,
		"len over 2^31":   huge,
		"IDAT first":      append([]byte(signature), base[ihdrEnd:]...),
		"short IHDR":      shortIHDR,
		"duplicate IHDR":  inject(base, base[8:ihdrEnd]),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := Strip(in)
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("err = %v, want ErrMalformed", err)
			}
			if out != nil {
				t.Error("returned bytes alongside an error")
			}
		})
	}
}

func TestRewriteAdd(t *testing.T) {
	base := basePNG(t)
	note := Chunk{Type: "iTXt", Data: []byte("pulsar\x00\x00\x00\x00\x00{}")}

	out, err := Rewrite(inject(base, raw("tEXt", []byte("prompt\x00{}"))), Allowed, []Chunk{note})
	if err != nil {
		t.Fatal(err)
	}
	if want := inject(base, raw(note.Type, note.Data)); !bytes.Equal(out, want) {
		t.Errorf("chunks = %v, want the add before IDAT", types(t, out))
	}
	if !samePixels(pixels(t, base), pixels(t, out)) {
		t.Error("decoded pixels changed")
	}

	// no IDAT: the add lands before IEND, exactly once
	bare := append(append([]byte{}, base[:ihdrEnd]...), raw("IEND", nil)...)
	out, err = Rewrite(bare, Allowed, []Chunk{note})
	if err != nil {
		t.Fatal(err)
	}
	if got := types(t, out); len(got) != 3 || got[1] != "iTXt" {
		t.Errorf("chunks = %v", got)
	}

	// keep is the caller's filter, but IHDR and IEND are structural
	out, err = Rewrite(base, func(string) bool { return false }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, bare) {
		t.Errorf("chunks = %v, want IHDR+IEND", types(t, out))
	}

	for _, typ := range []string{"", "abc", "abcde", "IHDR", "IDAT", "IEND", "te t", "tE1t", "t\x00Xt"} {
		if _, err := Rewrite(base, Allowed, []Chunk{{Type: typ}}); err == nil {
			t.Errorf("add type %q accepted", typ)
		}
	}
	if _, err := Rewrite(base[:10], Allowed, []Chunk{note}); !errors.Is(err, ErrMalformed) {
		t.Errorf("malformed input: err = %v", err)
	}
}

// FuzzRewrite: arbitrary bytes never panic, and whatever Strip accepts is a
// fixed point carrying only allow-listed chunks.
func FuzzRewrite(f *testing.F) {
	base := basePNG(f)
	f.Add(base)
	f.Add(inject(base, raw("tEXt", []byte("prompt\x00{}")), raw("gAMA", []byte{0, 0, 0xb1, 0x8f})))
	f.Add(append(append([]byte{}, base...), "tail"...))
	f.Add(base[:len(base)-12])
	f.Add([]byte(signature))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := Strip(data)
		if err != nil {
			if out != nil {
				t.Fatal("bytes returned alongside an error")
			}
			return
		}
		cs, err := Chunks(out)
		if err != nil {
			t.Fatalf("Strip output does not parse: %v", err)
		}
		for _, c := range cs {
			if !Allowed(c.Type) {
				t.Fatalf("chunk %q survived", c.Type)
			}
		}
		if cs[len(cs)-1].Type != "IEND" || len(out) > len(data) {
			t.Fatalf("output not bounded by IEND / input size")
		}
		again, err := Strip(out)
		if err != nil || !bytes.Equal(again, out) {
			t.Fatalf("not idempotent: %v", err)
		}
		if _, err := Rewrite(data, func(string) bool { return true }, []Chunk{{Type: "iTXt", Data: data}}); err != nil {
			t.Fatalf("Rewrite rejected input Strip accepted: %v", err)
		}
	})
}

func TestITXt(t *testing.T) {
	base := basePNG(t)
	text := "猫 <b>&\"quoted\"</b>\nline two — ünïcödé"
	c, err := ITXt("pulsar", text)
	if err != nil {
		t.Fatal(err)
	}
	if c.Type != "iTXt" || !bytes.Equal(c.Data, append([]byte("pulsar\x00\x00\x00\x00\x00"), text...)) {
		t.Errorf("chunk = %q %q", c.Type, c.Data)
	}
	key, got, ok := ITXtText(c)
	if !ok || key != "pulsar" || got != text {
		t.Errorf("ITXtText = %q %q %v", key, got, ok)
	}

	// it rides through Rewrite ahead of the pixel data and survives a re-parse
	out, err := Rewrite(base, Allowed, []Chunk{c})
	if err != nil {
		t.Fatal(err)
	}
	cs, err := Chunks(out)
	if err != nil {
		t.Fatal(err)
	}
	at, idat := -1, -1
	for i, ch := range cs {
		if ch.Type == "iTXt" {
			at = i
		}
		if ch.Type == "IDAT" && idat < 0 {
			idat = i
		}
	}
	if at < 1 || at > idat {
		t.Errorf("iTXt at %d, first IDAT at %d", at, idat)
	}
	if _, got, ok := ITXtText(cs[at]); !ok || got != text {
		t.Errorf("round trip = %q", got)
	}
	// and Strip takes it out again, restoring the input exactly
	if back, err := Strip(out); err != nil || !bytes.Equal(back, base) {
		t.Errorf("Strip(Rewrite(+iTXt)) != input: %v", err)
	}
	// the pixels are untouched
	a, _ := png.Decode(bytes.NewReader(base))
	b, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			if a.At(x, y) != b.At(x, y) {
				t.Fatalf("pixel (%d,%d) changed", x, y)
			}
		}
	}

	for name, kw := range map[string]string{
		"empty": "", "too long": string(bytes.Repeat([]byte("k"), 80)), "leading space": " k", "trailing space": "k ",
		"double space": "a  b", "nul": "a\x00b", "newline": "a\nb", "non-ascii": "clé",
	} {
		if _, err := ITXt(kw, "x"); err == nil {
			t.Errorf("keyword %s accepted", name)
		}
	}
	if _, err := ITXt(string(bytes.Repeat([]byte("k"), 79)), ""); err != nil {
		t.Errorf("79-byte keyword and empty text: %v", err)
	}
	for name, bad := range map[string]string{"nul": "a\x00b", "invalid utf-8": "a\xffb"} {
		if _, err := ITXt("k", bad); err == nil {
			t.Errorf("text with %s accepted", name)
		}
	}

	// ITXtText refuses what ITXt does not write
	for name, ch := range map[string]Chunk{
		"tEXt":       {Type: "tEXt", Data: []byte("k\x00v")},
		"compressed": {Type: "iTXt", Data: []byte("k\x00\x01\x00\x00\x00v")},
		"no keyword": {Type: "iTXt", Data: []byte("\x00\x00\x00\x00\x00v")},
		"truncated":  {Type: "iTXt", Data: []byte("k\x00\x00")},
		"no lang":    {Type: "iTXt", Data: []byte("k\x00\x00\x00lang")},
		"empty":      {Type: "iTXt"},
	} {
		if _, _, ok := ITXtText(ch); ok {
			t.Errorf("ITXtText accepted %s", name)
		}
	}
	// language tag and translated keyword are skipped, not required empty
	if k, v, ok := ITXtText(Chunk{Type: "iTXt", Data: []byte("k\x00\x00\x00en\x00clef\x00v")}); !ok || k != "k" || v != "v" {
		t.Errorf("with language tag: %q %q %v", k, v, ok)
	}
}
