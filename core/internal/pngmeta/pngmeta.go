// Package pngmeta filters PNG chunks at the byte level. Pure: no I/O, stdlib
// only. It never decodes or re-encodes pixels — chunks are spliced, so the
// image data of the result is bit-identical to the input's.
//
// It exists because delivered bytes are a compliance surface
// (docs/compliance.md#output-metadata): ComfyUI writes the full patched graph
// — safety negative, model filenames, the user's upload IDs — into a `prompt`
// tEXt chunk, and custom nodes may write anything. Strip is the allow-list
// that every output passes before CheckOutput and again on the serve path.
package pngmeta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
	"unicode/utf8"
)

const (
	signature = "\x89PNG\r\n\x1a\n"
	// PNG caps a chunk length at 2^31-1.
	maxChunkLen = 1<<31 - 1
	ihdrLen     = 13
)

// ErrMalformed wraps every parse failure. Callers fail closed on it.
var ErrMalformed = errors.New("pngmeta: malformed png")

// Chunk is one PNG chunk without its length and CRC framing.
type Chunk struct {
	Type string
	Data []byte
}

// allowed is the delivery allow-list: the critical chunks, the colour-space
// chunks, and pHYs. Everything else — tEXt, zTXt, iTXt, eXIf, tIME, APNG and
// private chunks — is metadata and is dropped.
var allowed = map[string]bool{
	"IHDR": true, "PLTE": true, "tRNS": true, "IDAT": true, "IEND": true,
	"gAMA": true, "cHRM": true, "sRGB": true, "iCCP": true, "cICP": true,
	"mDCV": true, "cLLI": true, "sBIT": true,
	"pHYs": true,
}

// Allowed reports whether a chunk type is on the delivery allow-list.
func Allowed(typ string) bool { return allowed[typ] }

// Strip returns png with only allow-listed chunks and nothing after IEND.
// It fails closed: a bad signature, CRC, length or structure is an error,
// never a best-effort pass-through.
func Strip(png []byte) ([]byte, error) {
	return Rewrite(png, Allowed, nil)
}

// Chunks parses png into its chunks, IHDR first through IEND last, verifying
// every length and CRC. Bytes after IEND are ignored. Data slices alias png.
func Chunks(png []byte) ([]Chunk, error) {
	if len(png) < len(signature) || string(png[:len(signature)]) != signature {
		return nil, fmt.Errorf("%w: bad signature", ErrMalformed)
	}
	rest := png[len(signature):]
	var out []Chunk
	for {
		// 4 length + 4 type + 4 crc is the smallest possible chunk
		if len(rest) < 12 {
			return nil, fmt.Errorf("%w: truncated before IEND", ErrMalformed)
		}
		n := binary.BigEndian.Uint32(rest[:4])
		if n > maxChunkLen || uint64(n) > uint64(len(rest)-12) {
			return nil, fmt.Errorf("%w: chunk length %d exceeds file", ErrMalformed, n)
		}
		body := rest[4 : 8+n] // type ‖ data — what the CRC covers
		if crc32.ChecksumIEEE(body) != binary.BigEndian.Uint32(rest[8+n:12+n]) {
			return nil, fmt.Errorf("%w: bad crc in %q chunk", ErrMalformed, body[:4])
		}
		c := Chunk{Type: string(body[:4]), Data: body[4:]}
		first := len(out) == 0
		switch {
		case first && (c.Type != "IHDR" || n != ihdrLen):
			return nil, fmt.Errorf("%w: first chunk is not a 13-byte IHDR", ErrMalformed)
		case !first && c.Type == "IHDR":
			return nil, fmt.Errorf("%w: duplicate IHDR", ErrMalformed)
		}
		out = append(out, c)
		if c.Type == "IEND" {
			return out, nil
		}
		rest = rest[12+n:]
	}
}

// Rewrite returns png keeping only the chunks keep accepts, with add inserted
// before the first IDAT (before IEND if there is none). IHDR and IEND are
// structural and always kept; bytes after IEND are always dropped. Input is
// validated as in Chunks; add chunks need a four-letter type that is not
// IHDR, IDAT or IEND.
func Rewrite(png []byte, keep func(typ string) bool, add []Chunk) ([]byte, error) {
	chunks, err := Chunks(png)
	if err != nil {
		return nil, err
	}
	for _, c := range add {
		if !validAddType(c.Type) || len(c.Data) > maxChunkLen {
			return nil, fmt.Errorf("pngmeta: cannot add chunk %q", c.Type)
		}
	}
	out := make([]byte, 0, len(png))
	out = append(out, signature...)
	added := false
	for _, c := range chunks {
		if !added && (c.Type == "IDAT" || c.Type == "IEND") {
			for _, a := range add {
				out = appendChunk(out, a)
			}
			added = true
		}
		if c.Type == "IHDR" || c.Type == "IEND" || keep(c.Type) {
			out = appendChunk(out, c)
		}
	}
	return out, nil
}

func validAddType(typ string) bool {
	if len(typ) != 4 || typ == "IHDR" || typ == "IDAT" || typ == "IEND" {
		return false
	}
	for i := 0; i < 4; i++ {
		b := typ[i] | 0x20
		if b < 'a' || b > 'z' {
			return false
		}
	}
	return true
}

func appendChunk(dst []byte, c Chunk) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(c.Data)))
	start := len(dst)
	dst = append(dst, c.Type...)
	dst = append(dst, c.Data...)
	return binary.BigEndian.AppendUint32(dst, crc32.ChecksumIEEE(dst[start:]))
}

// ITXt builds an uncompressed iTXt chunk: keyword NUL 0 0 NUL NUL text
// (no language tag, no translated keyword). The keyword follows the PNG
// rules — 1–79 printable Latin-1 bytes, no leading, trailing or doubled
// space; the text must be valid UTF-8 without NUL. It is the only way this
// package lets text back into a delivered file: pass the result to Rewrite.
func ITXt(keyword, text string) (Chunk, error) {
	if n := len(keyword); n < 1 || n > 79 || keyword[0] == ' ' || keyword[n-1] == ' ' || strings.Contains(keyword, "  ") {
		return Chunk{}, fmt.Errorf("pngmeta: bad iTXt keyword %q", keyword)
	}
	for i := 0; i < len(keyword); i++ {
		if b := keyword[i]; b < 0x20 || b > 0x7e {
			// ASCII only: Latin-1 high bytes are legal PNG but nothing here needs them
			return Chunk{}, fmt.Errorf("pngmeta: bad iTXt keyword %q", keyword)
		}
	}
	if !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
		return Chunk{}, fmt.Errorf("pngmeta: iTXt text for %q is not NUL-free UTF-8", keyword)
	}
	data := make([]byte, 0, len(keyword)+5+len(text))
	data = append(data, keyword...)
	data = append(data, 0, 0, 0, 0, 0) // keyword NUL, compression flag, method, language NUL, translated NUL
	data = append(data, text...)
	return Chunk{Type: "iTXt", Data: data}, nil
}

// ITXtText reads back an uncompressed iTXt chunk written by ITXt. ok is
// false for any other chunk, a compressed one, or a malformed one.
func ITXtText(c Chunk) (keyword, text string, ok bool) {
	if c.Type != "iTXt" {
		return "", "", false
	}
	k := bytes.IndexByte(c.Data, 0)
	if k < 1 || len(c.Data) < k+3 || c.Data[k+1] != 0 {
		return "", "", false
	}
	rest := c.Data[k+3:]
	l := bytes.IndexByte(rest, 0)
	if l < 0 {
		return "", "", false
	}
	rest = rest[l+1:]
	t := bytes.IndexByte(rest, 0)
	if t < 0 {
		return "", "", false
	}
	return string(c.Data[:k]), string(rest[t+1:]), true
}
