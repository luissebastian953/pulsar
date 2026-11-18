package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Prompt chunks: named snippets a prompt refers to as `!macro:Name!`
// (NovelAI's typeable form). Expansion is a pure pre-parse pass over one
// surface: the reference is replaced by the chunk's content, recursively,
// and every expanded rune remembers the reference it came from, so the
// scanner's issues and tag spans land on the token the user typed
// (reports/NovelAI prompt chunks and image inspector.md, Phase 1).
//
// Chunks are request data — the caller sends the ones it references; core
// stores none. Expansion never runs on a config or safety surface.
const (
	// ChunkPrefix opens a reference; the name runs to the next `!`.
	ChunkPrefix = "!macro:"
	// MaxChunkName bounds a chunk name, in runes.
	MaxChunkName = 64
	// MaxChunkBytes bounds one chunk's content.
	MaxChunkBytes = 4 << 10
	// MaxChunks bounds the chunks one request may carry.
	MaxChunks = 64
	// MaxChunkDepth bounds nesting: a reference in the prompt is depth 1.
	MaxChunkDepth = 8
)

// Chunk is one named snippet, as sent with a request.
type Chunk struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// ChunkRef is a tag's provenance: the chunk the user referenced and a hash
// of what it expanded to (HashChunk), so history can tell whether today's
// chunk of that name still reproduces the stored tags.
type ChunkRef struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// ChunkSpan is one reference in a surface: where the token sits (UTF-16
// offsets, piece index) and the text it resolved to, nested references
// included.
type ChunkSpan struct {
	ChunkRef
	Span
	Text string
}

// Origin maps one rune of an expanded surface back to the source: the
// UTF-16 span and piece index of the rune itself, or — for a rune that came
// out of a chunk — of the whole top-level reference. Ref indexes
// Expansion.Refs; -1 means the rune was typed.
type Origin struct {
	Start, End, Piece int
	Ref               int
}

// Expansion is a surface with its references resolved.
type Expansion struct {
	Text   string
	Origin []Origin // one per rune of Text
	Refs   []ChunkSpan
	// SrcLen and SrcPieces are the source's UTF-16 length and last piece
	// index: where an end-of-text issue points.
	SrcLen, SrcPieces int
}

// Error codes for references.
const (
	CodeUnknownChunk  = "unknown_chunk"   // no chunk of that name was sent
	CodeChunkCycle    = "chunk_cycle"     // a chunk reaches itself
	CodeChunkDepth    = "chunk_depth"     // nesting deeper than MaxChunkDepth
	CodeChunkSyntax   = "chunk_syntax"    // `!macro:` without a name or closing `!`
	CodeChunkInConfig = "chunk_in_config" // a reference in a config/safety surface
)

// HashChunk is the provenance hash: the first 16 hex digits of the sha256
// of a chunk's resolved text (nested references expanded), so editing a
// nested chunk changes the hash of every chunk that includes it.
func HashChunk(resolved string) string {
	sum := sha256.Sum256([]byte(resolved))
	return hex.EncodeToString(sum[:8])
}

// nameForbidden are the runes a chunk name may not contain: the token's own
// delimiter, the piece separators and the weighting operators.
const nameForbidden = "!,|{}[]"

// CheckChunkName validates a chunk name (decision D4): trimmed, non-empty,
// at most MaxChunkName runes, no `! , | { } [ ]`, no `::`, no newline or
// control character.
func CheckChunkName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("chunk name is required")
	case !utf8.ValidString(name):
		return fmt.Errorf("chunk name is not valid UTF-8")
	case strings.TrimSpace(name) != name:
		return fmt.Errorf("chunk name %q has leading or trailing whitespace", name)
	case utf8.RuneCountInString(name) > MaxChunkName:
		return fmt.Errorf("chunk name is longer than %d characters", MaxChunkName)
	case strings.Contains(name, "::"):
		return fmt.Errorf("chunk name %q may not contain `::`", name)
	}
	for _, r := range name {
		if strings.ContainsRune(nameForbidden, r) || unicode.IsControl(r) || r == unicode.ReplacementChar {
			return fmt.Errorf("chunk name %q may not contain %q", name, r)
		}
	}
	return nil
}

// CheckChunks validates the chunks a request carries: at most MaxChunks,
// valid unique names, each content at most MaxChunkBytes of valid UTF-8.
// Content syntax is not checked here — it is parsed where it is referenced.
func CheckChunks(chunks []Chunk) error {
	if len(chunks) > MaxChunks {
		return fmt.Errorf("too many chunks (%d; the limit is %d per request)", len(chunks), MaxChunks)
	}
	seen := make(map[string]bool, len(chunks))
	for _, c := range chunks {
		if err := CheckChunkName(c.Name); err != nil {
			return err
		}
		if seen[c.Name] {
			return fmt.Errorf("chunk %q is sent twice", c.Name)
		}
		seen[c.Name] = true
		if len(c.Content) > MaxChunkBytes {
			return fmt.Errorf("chunk %q is too long (%d bytes; the limit is %d)", c.Name, len(c.Content), MaxChunkBytes)
		}
		if !utf8.ValidString(c.Content) {
			return fmt.Errorf("chunk %q is not valid UTF-8", c.Name)
		}
	}
	return nil
}

// Expand resolves every `!macro:Name!` in src against chunks and returns
// the first error, if any. In a config mode any reference is an error
// (chunk_in_config): user data never lands in a deployment surface.
func Expand(src string, chunks []Chunk, m Mode) (Expansion, error) {
	exp, errs := expand(src, chunks, m)
	if len(errs) > 0 {
		return Expansion{}, &Error{errs[0]}
	}
	return exp, nil
}

// chunkFail is a failed resolution, before it is located on a reference.
type chunkFail struct {
	code, msg, hint string
	// depth failures depend on where the chunk is used; the rest fail
	// everywhere and are remembered
	contextual bool
}

type resolved struct {
	text   string
	height int // nesting levels below this chunk, itself included
	fail   *chunkFail
}

type expander struct {
	chunks map[string]string
	memo   map[string]resolved
}

// readRef reads a reference token at rs[i:] (which starts with
// ChunkPrefix). It returns the trimmed name and the index just past the
// closing `!`; ok=false means the token is malformed and end is where it
// stops being one.
func readRef(rs []rune, i int) (name string, end int, ok bool) {
	j := i + len(ChunkPrefix)
	for ; j < len(rs); j++ {
		r := rs[j]
		if r == '!' {
			name = strings.TrimSpace(string(rs[i+len(ChunkPrefix) : j]))
			if name == "" || utf8.RuneCountInString(name) > MaxChunkName || strings.Contains(name, "::") {
				return "", j + 1, false
			}
			return name, j + 1, true
		}
		if strings.ContainsRune(nameForbidden, r) || r == '\n' || r == '\r' || unicode.IsControl(r) {
			return "", j, false
		}
	}
	return "", j, false
}

func hasPrefixAt(rs []rune, i int, p string) bool {
	if len(rs)-i < len(p) {
		return false
	}
	for k := 0; k < len(p); k++ {
		if rs[i+k] != rune(p[k]) {
			return false
		}
	}
	return true
}

var syntaxFail = &chunkFail{code: CodeChunkSyntax, msg: "`!macro:` needs a chunk name and a closing `!`", hint: "Write `!macro:Name!`"}

// resolve returns the fully expanded content of a chunk. stack is the chain
// of chunks being expanded above it (the per-branch visited set). The first
// failure aborts the branch; successes are memoised, so the work is linear
// in the total chunk size however often a chunk is referenced.
func (x *expander) resolve(name string, stack []string) resolved {
	chain := func() string { return strings.Join(append(append([]string(nil), stack...), name), " → ") }
	for _, s := range stack {
		if s == name {
			return resolved{fail: &chunkFail{code: CodeChunkCycle,
				msg: fmt.Sprintf("chunk `%s` includes itself (%s)", name, chain()), hint: "Remove one of the references"}}
		}
	}
	if len(stack) >= MaxChunkDepth {
		return resolved{fail: &chunkFail{code: CodeChunkDepth, contextual: true,
			msg:  fmt.Sprintf("chunks nest deeper than %d levels (%s)", MaxChunkDepth, chain()),
			hint: "Flatten the chunks"}}
	}
	if r, ok := x.memo[name]; ok {
		if r.fail != nil || len(stack)+r.height <= MaxChunkDepth {
			return r
		}
		// fits elsewhere, too deep here: fall through to report the chain
	}
	content, ok := x.chunks[name]
	if !ok {
		msg := fmt.Sprintf("no chunk named `%s`", name)
		if len(stack) > 0 {
			msg = fmt.Sprintf("chunk `%s` uses `%s%s!`, but no chunk has that name", stack[len(stack)-1], ChunkPrefix, name)
		}
		r := resolved{fail: &chunkFail{code: CodeUnknownChunk, msg: msg, hint: "Create it in Prompt Chunks, or remove the reference"}}
		x.memo[name] = r
		return r
	}
	rs := []rune(content)
	var b strings.Builder
	height := 1
	inner := append(stack, name)
	for i := 0; i < len(rs); i++ {
		if rs[i] != '!' || !hasPrefixAt(rs, i, ChunkPrefix) {
			b.WriteRune(rs[i])
			continue
		}
		child, end, ok := readRef(rs, i)
		var r resolved
		if !ok {
			r = resolved{fail: &chunkFail{code: CodeChunkSyntax,
				msg: fmt.Sprintf("chunk `%s`: %s", name, syntaxFail.msg), hint: syntaxFail.hint}}
		} else {
			r = x.resolve(child, inner)
		}
		if r.fail != nil {
			if !r.fail.contextual {
				x.memo[name] = r
			}
			return r
		}
		b.WriteString(r.text)
		height = max(height, r.height+1)
		if b.Len() > MaxBytes {
			r := resolved{fail: &chunkFail{code: CodeTooLong,
				msg: fmt.Sprintf("chunk `%s` expands past the prompt limit (%d bytes)", name, MaxBytes), hint: "Shorten the chunk"}}
			x.memo[name] = r
			return r
		}
		i = end - 1
	}
	r := resolved{text: b.String(), height: height}
	x.memo[name] = r
	return r
}

// expand is Expand returning every reference error it found (capped), for
// live validation. With errors the Expansion still carries the references
// that did resolve, so an editor can keep marking them.
func expand(src string, chunks []Chunk, m Mode) (Expansion, []Issue) {
	rs := []rune(src)
	// source geometry: UTF-16 offset and piece index of every rune
	u16 := make([]int, len(rs)+1)
	piece := make([]int, len(rs)+1)
	off, p := 0, 0
	for i, r := range rs {
		u16[i], piece[i] = off, p
		if r >= 0x10000 {
			off += 2
		} else {
			off++
		}
		if r == ',' || r == '\n' {
			p++
		}
	}
	u16[len(rs)], piece[len(rs)] = off, p

	exp := Expansion{SrcLen: off, SrcPieces: p, Origin: make([]Origin, 0, len(rs))}
	var errs []Issue
	fail := func(f *chunkFail, start, end int) {
		if len(errs) < maxIssues {
			errs = append(errs, Issue{Code: f.code, Message: f.msg, Hint: f.hint, Start: u16[start], End: u16[end], Tag: piece[start]})
		}
	}
	// modeRaw is FormatNAI's own round-trip check: text only, no references
	if m == modeRaw || !strings.Contains(src, ChunkPrefix) {
		for i := range rs {
			exp.Origin = append(exp.Origin, Origin{Start: u16[i], End: u16[i+1], Piece: piece[i], Ref: -1})
		}
		exp.Text = src
		return exp, nil
	}

	x := &expander{chunks: make(map[string]string, len(chunks)), memo: map[string]resolved{}}
	for _, c := range chunks {
		x.chunks[c.Name] = c.Content
	}
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		if rs[i] != '!' || !hasPrefixAt(rs, i, ChunkPrefix) {
			b.WriteRune(rs[i])
			exp.Origin = append(exp.Origin, Origin{Start: u16[i], End: u16[i+1], Piece: piece[i], Ref: -1})
			continue
		}
		if m.config() {
			fail(&chunkFail{code: CodeChunkInConfig, msg: "chunk references aren't allowed in deployment prompt config",
				hint: "Write the tags out"}, i, i+len(ChunkPrefix))
			i += len(ChunkPrefix) - 1
			continue
		}
		name, end, ok := readRef(rs, i)
		if !ok {
			fail(syntaxFail, i, max(end, i+len(ChunkPrefix)))
			i = max(end, i+len(ChunkPrefix)) - 1
			continue
		}
		if len(errs) >= maxIssues {
			break // enough to show; stop resolving
		}
		r := x.resolve(name, nil)
		if r.fail != nil {
			fail(r.fail, i, end)
			i = end - 1
			continue
		}
		ref := len(exp.Refs)
		sp := Span{Start: u16[i], End: u16[end], Index: piece[i]}
		exp.Refs = append(exp.Refs, ChunkSpan{ChunkRef: ChunkRef{Name: name, Hash: HashChunk(r.text)}, Span: sp, Text: r.text})
		b.WriteString(r.text)
		for range r.text {
			exp.Origin = append(exp.Origin, Origin{Start: sp.Start, End: sp.End, Piece: sp.Index, Ref: ref})
		}
		if b.Len() > MaxBytes {
			// attributed to the field (the limit applies after expansion)
			return exp, append(errs, Issue{Code: CodeTooLong, Hint: "Shorten the prompt or its chunks", Start: 0, End: off,
				Message: fmt.Sprintf("prompt is too long once its chunks are expanded (over %d bytes)", MaxBytes)})
		}
		i = end - 1
	}
	exp.Text = b.String()
	return exp, errs
}
