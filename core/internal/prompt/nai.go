package prompt

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
)

// Bounds (S3). MaxWeight is NovelAI's stated stable range, applied to |w|
// for both signs; config surfaces are written in emitted units and bounded
// tighter because operators tune them per lineage.
const (
	MaxBytes        = 16 << 10
	MaxTags         = 512
	MaxWeight       = 10.0
	MaxConfigWeight = 2.0
	bracketStep     = 1.05
	maxIssues       = 32
)

// Mode selects the surface rules.
type Mode uint8

const (
	// Positive: the user prompt or a character prompt. w < 0 tags are
	// relocated to Undesired Content (decision 1), w = 0 tags dropped.
	Positive Mode = iota
	// Negative: the user negative. w < 0 is rejected — in UC it would steer
	// toward the concept — and w = 0 tags are dropped.
	Negative
	// ConfigPositive / ConfigNegative: QUALITY_TAGS, style rows, DEFAULT_UC.
	// Weights are emitted units in (0, 2]; nothing is relocated or dropped.
	ConfigPositive
	ConfigNegative
	// modeRaw: no sign handling (FormatNAI's round-trip check).
	modeRaw
)

func (m Mode) config() bool { return m == ConfigPositive || m == ConfigNegative }

func (m Mode) limit() float64 {
	if m.config() {
		return MaxConfigWeight
	}
	return MaxWeight
}

// Surface is one parsed prompt box. The running weight starts at 1.0 for
// every surface; nothing carries between surfaces.
type Surface struct {
	Tags []Tag
	// Relocated holds Positive-mode tags whose weight was negative, at |w|,
	// for the caller to append to the user negative.
	Relocated []Tag
	Notices   []Issue
	// Marks locate the typed weighting for an editor's emphasis highlight
	// (user surfaces only; text that came out of a chunk is never marked).
	Marks []Mark
}

// Mark kinds: text weighted above 1, text weighted below 1 (negative and
// zero included), and an operator (`{ } [ ]`, `N::`, `::`, SD parens).
const (
	MarkUp   = "up"
	MarkDown = "down"
	MarkOp   = "op"
)

// markEpsilon: a weight this close to 1 is not highlighted (NovelAI's rule).
const markEpsilon = 0.01

// Mark is one highlighted stretch of a surface: UTF-16 offsets, its kind,
// and the NovelAI-unit weight in effect (for an operator, the weight it
// sets or restores).
type Mark struct {
	Start  int     `json:"start"`
	End    int     `json:"end"`
	Kind   string  `json:"kind"`
	Weight float64 `json:"w"`
}

// Parse runs the scanner and returns the first error, if any. A chunk
// reference (`!macro:Name!`) is an unknown_chunk error here: use ParseWith
// to supply the chunks.
func Parse(text string, m Mode) (Surface, error) {
	return ParseWith(text, m, nil)
}

// ParseWith is Parse with the request's chunks: references are expanded
// first, and every issue and span inside one lands on the reference.
func ParseWith(text string, m Mode, chunks []Chunk) (Surface, error) {
	s, _, errs := AnalyzeWith(text, m, chunks)
	if len(errs) > 0 {
		return Surface{}, &Error{errs[0]}
	}
	return s, nil
}

// Analyze parses one surface and returns every error it found (capped),
// for live validation. With errors the Surface is empty.
func Analyze(text string, m Mode) (Surface, []Issue) {
	s, _, errs := AnalyzeWith(text, m, nil)
	return s, errs
}

// AnalyzeWith is Analyze with chunks. It also returns the references that
// resolved — even when the surface has errors, so an editor keeps marking
// them. MaxBytes and MaxTags apply to the expanded text.
func AnalyzeWith(text string, m Mode, chunks []Chunk) (Surface, []ChunkSpan, []Issue) {
	if len(text) > MaxBytes {
		return Surface{}, nil, []Issue{{
			Code: CodeTooLong, Message: fmt.Sprintf("prompt is too long (%d bytes; the limit is %d)", len(text), MaxBytes),
			Hint: "Shorten the prompt", Start: 0, End: len(utf16.Encode([]rune(text))),
		}}
	}
	exp, errs := expand(text, chunks, m)
	if len(errs) > 0 {
		return Surface{}, exp.Refs, errs
	}
	surf, errs := analyze(exp, m)
	return surf, exp.Refs, errs
}

func analyze(exp Expansion, m Mode) (Surface, []Issue) {
	s := newScanner(exp, m)
	s.lex()
	if len(s.errs) == 0 {
		s.findSD()
	}
	if len(s.errs) == 0 {
		s.scan()
	}
	if len(s.errs) > 0 {
		return Surface{}, s.errs
	}
	surf := s.build()
	if len(s.errs) > 0 {
		return Surface{}, s.errs
	}
	return surf, nil
}

type entry struct {
	r   rune
	w   float64
	src int // source rune index
	sep bool
}

type sdGroup struct {
	open, colon, close int
	w                  float64
}

// frame is one bracket scope: `{` `[` or an SD `(…:w)` group. Its closer
// restores the weight in effect before the opener (decision 4).
type frame struct {
	kind          rune
	at            int
	before        float64
	beforeSrc     [2]int
	sd            *sdGroup
	section       int // last numeric `w::` (w≠1) in this scope; -1 none
	sectionEnd    int
	sectionText   bool
	sectionWeight float64
}

// opSpan is one operator in the scanned text: rune range and the weight in
// effect after it.
type opSpan struct {
	start, end int
	w          float64
}

type scanner struct {
	rs []rune
	// Source geometry per scanned rune. For typed text these are the rune's
	// own offsets; for a rune that came out of a chunk they are the whole
	// reference's, so every issue and span lands on the token.
	u16   []int // UTF-16 start offset of rune i; len(rs)+1 entries
	u16e  []int // UTF-16 end offset of rune i
	piece []int // piece index of rune i; len(rs)+1 entries
	ref   []int // index into refs of the reference rune i came from; -1 typed
	refs  []ChunkSpan
	ops   []opSpan
	mode  Mode

	errs, notes []Issue
	buf         []entry
	stack       []frame
	base        frame
	w           float64
	wSrc        [2]int // rune span of the operator that set w; -1 = default
	reported    map[int]bool
	sd          map[int]*sdGroup
	sdColon     map[int]*sdGroup
	literalWarn map[int]bool
	// numRune[i]: rune i is part of a number right before `::` — scanned
	// as text first, then taken back as the weight
	numRune []bool
}

func newScanner(exp Expansion, m Mode) *scanner {
	rs := []rune(exp.Text)
	s := &scanner{
		rs: rs, mode: m, w: 1,
		u16:      make([]int, len(rs)+1),
		u16e:     make([]int, len(rs)),
		piece:    make([]int, len(rs)+1),
		ref:      make([]int, len(rs)),
		refs:     exp.Refs,
		numRune:  make([]bool, len(rs)+1),
		reported: map[int]bool{},
		sd:       map[int]*sdGroup{},
		sdColon:  map[int]*sdGroup{},
		base:     frame{section: -1},
		wSrc:     [2]int{-1, -1},
	}
	for i, o := range exp.Origin {
		s.u16[i], s.u16e[i], s.piece[i], s.ref[i] = o.Start, o.End, o.Piece, o.Ref
	}
	s.u16[len(rs)] = exp.SrcLen
	s.piece[len(rs)] = exp.SrcPieces
	for i := 0; i+1 < len(rs); i++ {
		if rs[i] == ':' && rs[i+1] == ':' {
			for k := s.numBefore(i); k < i; k++ {
				s.numRune[k] = true
			}
			i++
		}
	}
	return s
}

// endOff is the UTF-16 offset where the rune range [start, end) ends.
func (s *scanner) endOff(start, end int) int {
	if end <= start {
		return s.u16[start]
	}
	return s.u16e[end-1]
}

// chunkAt names the chunk rune i came out of, "" for typed text.
func (s *scanner) chunkAt(i int) string {
	if i >= 0 && i < len(s.ref) && s.ref[i] >= 0 {
		return s.refs[s.ref[i]].Name
	}
	return ""
}

func inChunk(name, msg string) string {
	if name == "" {
		return msg
	}
	return "in chunk `" + name + "`: " + msg
}

func (s *scanner) issue(code, msg, hint string, start, end int) Issue {
	start = max(0, min(start, len(s.rs)))
	end = max(start, min(end, len(s.rs)))
	if code != CodeTooLong {
		// a problem inside a chunk is located on the reference: say which
		// chunk, since the text quoted isn't in the field
		msg = inChunk(s.chunkAt(start), msg)
	}
	return Issue{Code: code, Message: msg, Hint: hint, Start: s.u16[start], End: s.endOff(start, end), Tag: s.piece[start]}
}

func (s *scanner) fail(code, msg, hint string, start, end int) {
	if len(s.errs) < maxIssues {
		s.errs = append(s.errs, s.issue(code, msg, hint, start, end))
	}
}

func (s *scanner) note(code, msg, hint string, start, end int) {
	if len(s.notes) < maxIssues {
		s.notes = append(s.notes, s.issue(code, msg, hint, start, end))
	}
}

func (s *scanner) snippet(start, end int) string {
	start = max(0, start)
	end = min(len(s.rs), end)
	if end-start > 40 {
		return string(s.rs[start:start+37]) + "…"
	}
	return string(s.rs[start:end])
}

// lex rejects what no grammar position can make legal (S3 P006, P007,
// P009, P010, P011).
func (s *scanner) lex() {
	rs := s.rs
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == unicode.ReplacementChar || (unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r'):
			j := i + 1
			for j < len(rs) && (rs[j] == unicode.ReplacementChar || (unicode.IsControl(rs[j]) && rs[j] != '\n' && rs[j] != '\t' && rs[j] != '\r')) {
				j++
			}
			s.fail(CodeControlChar, "control characters aren't allowed in a prompt", "Remove control characters", i, j)
			i = j - 1
		case r == '|':
			s.fail(CodePipe, "`|` has no meaning in pulsar (it's NovelAI's character separator / V3 prompt mixing)", "Use character boxes", i, i+1)
		case r == ':':
			j := i
			for j < len(rs) && rs[j] == ':' {
				j++
			}
			if j-i >= 3 {
				s.fail(CodeTripleColon, "`:::` is ambiguous", "Use `::` or a single `:`", i, j)
			}
			i = j - 1
		case r == '<':
			j := i + 1
			for j < len(rs) && (rs[j] >= 'a' && rs[j] <= 'z' || rs[j] >= 'A' && rs[j] <= 'Z') {
				j++
			}
			if j > i+1 && j < len(rs) && rs[j] == ':' {
				s.fail(CodeExtraNetwork, fmt.Sprintf("`%s` extra-network syntax isn't supported", string(rs[i:j+1])), "Use the LoRA picker", i, j+1)
			}
		case r == 'e' || r == 'E':
			if hasFoldPrefix(rs[i:], "embedding:") {
				s.fail(CodeEmbedding, "embeddings aren't supported", "Remove `embedding:` — embeddings aren't supported", i, i+len("embedding:"))
			}
		}
	}
}

func hasFoldPrefix(rs []rune, p string) bool {
	pr := []rune(p)
	if len(rs) < len(pr) {
		return false
	}
	return strings.EqualFold(string(rs[:len(pr)]), p)
}

// findSD locates SD/ComfyUI `(text:w)` groups (decision 2). A matched,
// unescaped paren pair whose last colon sits past index 0 and whose suffix
// Python's float() accepts is a weight group; everything else in parens is
// literal text, as on NovelAI (`name (series)` relies on it).
func (s *scanner) findSD() {
	rs := s.rs
	type pair struct{ o, c int }
	var opens []int
	var pairs []pair
	for i := 0; i < len(rs); i++ {
		switch rs[i] {
		case '\\':
			if i+1 < len(rs) && (rs[i+1] == '(' || rs[i+1] == ')') {
				i++
			}
		case '(':
			opens = append(opens, i)
		case ')':
			if n := len(opens); n > 0 {
				pairs = append(pairs, pair{opens[n-1], i})
				opens = opens[:n-1]
			}
		}
	}
	inside := func(in, out pair) bool { return in.o > out.o && in.c < out.c }

	var weighted []pair
	for _, p := range pairs {
		inner := rs[p.o+1 : p.c]
		xx := -1
		for k := len(inner) - 1; k >= 0; k-- {
			if inner[k] == ':' {
				xx = k
				break
			}
		}
		if xx <= 0 {
			continue
		}
		suffix := string(inner[xx+1:])
		if _, ok, amb := pyFloat(suffix); !ok && !amb {
			continue // ComfyUI keeps the colon text: a literal group for us
		}
		// ComfyUI would read a weight here: convert it, but only from a
		// plain decimal — nan/inf/exponents/underscores are not a weight a
		// person means to type
		w, err := plainDecimal(suffix)
		if err != nil {
			s.fail(CodeSDWeightSyntax, fmt.Sprintf("`%s` isn't a plain number weight", strings.TrimSpace(suffix)),
				"Use `1.2::text ::`", p.o, p.c+1)
			continue
		}
		if math.Abs(w) > MaxWeight {
			s.fail(CodeWeightTooHigh, fmt.Sprintf("weight %s is outside ±%v", strings.TrimSpace(suffix), MaxWeight),
				"Weights go up to 10", p.o, p.c+1)
			continue
		}
		g := &sdGroup{open: p.o, colon: p.o + 1 + xx, close: p.c, w: w}
		s.sd[p.o] = g
		s.sdColon[g.colon] = g
		weighted = append(weighted, p)
	}
	if len(s.errs) > 0 {
		return
	}
	// nesting: an SD weight group inside any other paren group is
	// ambiguous (ComfyUI overwrites, A1111 multiplies, NovelAI is literal)
	for _, wp := range weighted {
		for _, p := range pairs {
			if inside(wp, p) {
				s.fail(CodeSDWeightSyntax, "nested parentheses around an SD weight are ambiguous",
					"Write one weight per tag with `w::text ::`", p.o, p.c+1)
				return
			}
		}
	}
	// a bare group inside a weight group is literal only in qualifier
	// position (`castorice (honkai: star rail)`); `((x):1.2)` is SD nesting
	for _, p := range pairs {
		if _, ok := s.sd[p.o]; ok {
			continue
		}
		var host *sdGroup
		for _, wp := range weighted {
			if inside(p, wp) {
				host = s.sd[wp.o]
			}
		}
		if host != nil {
			if !s.textBefore(p.o, host.open) {
				s.fail(CodeSDWeightSyntax, "nested parentheses inside an SD weight are ambiguous",
					"Write one weight per tag with `w::text ::`", p.o, p.c+1)
				return
			}
			continue
		}
		// a whole tag wrapped in bare parens is SD-habit emphasis that
		// pulsar reads literally — say so once
		if !s.textBefore(p.o, -1) && !s.textAfter(p.c) && !s.literalWarned(p.o) {
			s.note(NoticeLiteralParens, fmt.Sprintf("`%s` — parentheses without a weight are literal text", s.snippet(p.o, p.c+1)),
				"Use `{x}` or `1.1::x ::` to strengthen", p.o, p.c+1)
		}
	}
}

func (s *scanner) literalWarned(o int) bool {
	if s.literalWarn == nil {
		s.literalWarn = map[int]bool{}
	}
	if s.literalWarn[o] {
		return true
	}
	s.literalWarn[o] = true
	return false
}

// textBefore: is there tag text in the same piece before rune i (stopping
// at stop, exclusive, or the piece start)? Operators don't count as text.
func (s *scanner) textBefore(i, stop int) bool {
	for k := i - 1; k > stop && k >= 0; k-- {
		switch r := s.rs[k]; {
		case r == ',' || r == '\n':
			return false
		case unicode.IsSpace(r) || strings.ContainsRune("{}[]:", r):
			continue
		default:
			return true
		}
	}
	return false
}

func (s *scanner) textAfter(i int) bool {
	for k := i + 1; k < len(s.rs); k++ {
		switch r := s.rs[k]; {
		case r == ',' || r == '\n':
			return false
		case unicode.IsSpace(r) || strings.ContainsRune("{}[]:", r):
			continue
		default:
			return true
		}
	}
	return false
}

func (s *scanner) scope() *frame {
	if n := len(s.stack); n > 0 {
		return &s.stack[n-1]
	}
	return &s.base
}

func (s *scanner) push(kind rune, at int, sd *sdGroup) {
	s.stack = append(s.stack, frame{kind: kind, at: at, before: s.w, beforeSrc: s.wSrc, sd: sd, section: -1})
}

func (s *scanner) pop() frame {
	f := s.stack[len(s.stack)-1]
	s.stack = s.stack[:len(s.stack)-1]
	s.w, s.wSrc = f.before, f.beforeSrc
	return f
}

func closerOf(open rune) rune {
	switch open {
	case '{':
		return '}'
	case '[':
		return ']'
	}
	return ')'
}

// scan is the NovelAI running-weight scanner with scoped brackets
// (decision 4). With balanced input it equals NovelAI's frontend math
// except where a numeric `w::` sits inside brackets: NovelAI's closer then
// divides the numeric weight and leaks it (`{1.8::x}, y` → y at 1.714);
// pulsar's closer restores the weight from before the opener (y at 1).
func (s *scanner) scan() {
	rs := s.rs
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if g := s.sd[i]; g != nil {
			s.push('(', i, g)
			s.w, s.wSrc = g.w, [2]int{g.open, g.close + 1}
			s.ops = append(s.ops, opSpan{g.open, g.open + 1, g.w}, opSpan{g.colon, g.close + 1, g.w})
			s.note(NoticeConvertedSD, fmt.Sprintf("`%s` converted from SD syntax to weight %s", s.snippet(g.open, g.close+1), naiNum(g.w)),
				"pulsar's syntax is `w::text ::`", g.open, g.close+1)
			continue
		}
		if g := s.sdColon[i]; g != nil {
			// close every scope opened inside the group; unclosed ones are errors
			for len(s.stack) > 0 && s.scope().sd != g {
				f := s.pop()
				s.fail(CodeUnclosedBracket, fmt.Sprintf("`%c` is never closed inside `(…:%s)`", f.kind, naiNum(g.w)),
					fmt.Sprintf("Close it with `%c` before `:`", closerOf(f.kind)), f.at, f.at+1)
			}
			if len(s.stack) > 0 {
				s.pop()
			}
			i = g.close
			continue
		}
		switch {
		case r == '\\' && i+1 < len(rs) && (rs[i+1] == '(' || rs[i+1] == ')'):
			i++
			s.text(rs[i], i)
		case r == '{' || r == '[':
			s.push(r, i, nil)
			if r == '{' {
				s.w *= bracketStep
			} else {
				s.w /= bracketStep
			}
			s.wSrc = [2]int{i, i + 1}
			s.ops = append(s.ops, opSpan{i, i + 1, s.w})
		case r == '}' || r == ']':
			if len(s.stack) == 0 {
				s.fail(CodeStrayCloser, fmt.Sprintf("`%c` has no matching `%c`", r, opener(r)),
					fmt.Sprintf("Remove it, or add `%c` before the text it should weight", opener(r)), i, i+1)
				continue
			}
			top := s.scope()
			if closerOf(top.kind) != r {
				if top.sd != nil {
					s.fail(CodeMismatchedBracket, fmt.Sprintf("`%c` closes inside `(…:%s)` that it didn't open in", r, naiNum(top.sd.w)),
						"Keep brackets inside the parentheses balanced", i, i+1)
					continue
				}
				s.fail(CodeMismatchedBracket, fmt.Sprintf("`%c` closes `%c`", r, top.kind),
					fmt.Sprintf("Close `%c` with `%c`", top.kind, closerOf(top.kind)), top.at, i+1)
				s.pop()
				continue
			}
			s.pop()
			s.ops = append(s.ops, opSpan{i, i + 1, s.w})
		case r == ':' && i+1 < len(rs) && rs[i+1] == ':':
			s.colons(i)
			i++
		case r == ',' || r == '\n':
			s.buf = append(s.buf, entry{r: r, w: s.w, src: i, sep: true})
		default:
			s.text(r, i)
		}
	}
	for len(s.stack) > 0 {
		f := s.pop()
		if f.sd != nil {
			continue // matched by construction
		}
		s.fail(CodeUnclosedBracket, fmt.Sprintf("`%c` is never closed", f.kind),
			fmt.Sprintf("Close it with `%c` — in pulsar `::` doesn't close brackets", closerOf(f.kind)), f.at, f.at+1)
	}
	if b := s.base; b.section >= 0 && b.sectionText && len(s.errs) == 0 {
		s.note(NoticeOpenSection, fmt.Sprintf("the `%s` section is never closed, so it runs to the end of the prompt", string(s.rs[b.section:b.sectionEnd])),
			"End it with `::` (e.g. `1.5::rain ::`)", b.section, b.sectionEnd)
	}
}

func opener(closer rune) rune {
	if closer == '}' {
		return '{'
	}
	return '['
}

func (s *scanner) text(r rune, src int) {
	if !unicode.IsSpace(r) && !s.numRune[src] {
		if math.Abs(s.w) > s.mode.limit() && s.wSrc[0] >= 0 && !s.reported[s.wSrc[0]] {
			s.reported[s.wSrc[0]] = true
			hint := fmt.Sprintf("Weights go up to %v", s.mode.limit())
			if !s.mode.config() {
				hint += " (48 nested `{` already reach 10.4)"
			}
			s.fail(CodeWeightTooHigh, fmt.Sprintf("weight %s is outside ±%v", naiNum(round6(s.w)), s.mode.limit()), hint, s.wSrc[0], s.wSrc[1])
		}
		if sc := s.scope(); sc.section >= 0 {
			sc.sectionText = true
		}
	}
	s.buf = append(s.buf, entry{r: r, w: s.w, src: src})
}

func isASCIIDigit(r rune) bool { return r >= '0' && r <= '9' }

// numBefore matches NovelAI's /-?\d*\.?\d*$/ against the source before
// index i, leftmost start; returns the start index.
func (s *scanner) numBefore(i int) int {
	j := i
	for j > 0 && isASCIIDigit(s.rs[j-1]) {
		j--
	}
	if j > 0 && s.rs[j-1] == '.' {
		j--
		for j > 0 && isASCIIDigit(s.rs[j-1]) {
			j--
		}
	}
	if j > 0 && s.rs[j-1] == '-' {
		j--
	}
	return j
}

// colons handles `::` at i: `w::` overwrites the weight in the current
// scope, a bare `::` resets it to 1 there; neither pops a bracket.
func (s *scanner) colons(i int) {
	for _, f := range s.stack {
		if f.sd != nil {
			s.fail(CodeSDWeightSyntax, "`::` inside an SD `(…:w)` group is ambiguous",
				"Use one syntax: `w::text ::`", i, i+2)
			return
		}
	}
	j := s.numBefore(i)
	num := string(s.rs[j:i])
	// the number's runes were scanned as text: take them back
	if n := i - j; n > 0 && n <= len(s.buf) {
		s.buf = s.buf[:len(s.buf)-n]
	}
	sc := s.scope()
	if num == "" {
		s.spacedNumberHint(i)
		s.numberAfterHint(i)
		s.w, s.wSrc = 1, [2]int{i, i + 2}
		s.ops = append(s.ops, opSpan{i, i + 2, 1})
		sc.section = -1
		return
	}
	var w float64
	switch num {
	case "-", ".", "-.":
		w = 0 // NovelAI's frontend special case
	default:
		v, err := strconv.ParseFloat(num, 64)
		if err != nil && !isRangeErr(err) {
			s.fail(CodeWeightTooHigh, fmt.Sprintf("`%s` isn't a weight", num), "Weights go up to 10", j, i+2)
			return
		}
		w = v
	}
	glued := j > 0 && (unicode.IsLetter(s.rs[j-1]) || unicode.IsDigit(s.rs[j-1]) || s.rs[j-1] == '_' || s.rs[j-1] == '.' || s.rs[j-1] == '-')
	inTag := !glued && j > 0 && unicode.IsSpace(s.rs[j-1]) && s.textBefore(j, -1)
	tagWord := strings.TrimSpace(string(s.rs[max(s.pieceStart(j), i-30):i]))
	switch {
	case glued:
		s.fail(CodeGluedNumber, fmt.Sprintf("`%s` right before `::` is read as weight %s, not as part of the tag", num, num),
			fmt.Sprintf("Add a space before `::` (`%s ::`)", tagWord), j, i+2)
		return
	case inTag && math.Abs(w) > s.mode.limit():
		// `year 2000::` — the tag's own digits became a weight
		s.fail(CodeGluedNumber, fmt.Sprintf("`%s::` reads %s as a weight for everything after it", num, num),
			fmt.Sprintf("Add a space before `::` (`%s ::`)", tagWord), j, i+2)
		return
	case inTag:
		s.note(NoticeNumberInTag, fmt.Sprintf("`%s::` sets weight %s for everything after it", num, num),
			fmt.Sprintf("If %s belongs to the tag, add a space before `::`", num), j, i+2)
	}
	s.numberAfterHint(i)
	s.w, s.wSrc = w, [2]int{j, i + 2}
	s.ops = append(s.ops, opSpan{j, i + 2, w})
	if w != 1 {
		sc.section, sc.sectionEnd, sc.sectionText, sc.sectionWeight = j, i+2, false, w
	} else {
		sc.section = -1
	}
}

func isRangeErr(err error) bool {
	ne, ok := err.(*strconv.NumError)
	return ok && ne.Err == strconv.ErrRange
}

func (s *scanner) pieceStart(i int) int {
	for k := i - 1; k >= 0; k-- {
		if s.rs[k] == ',' || s.rs[k] == '\n' {
			return k + 1
		}
	}
	return 0
}

// N3: a bare `::` at weight 1 right after "<num> " — the space kept the
// number from being a weight (NovelAI's emphasisError1).
func (s *scanner) spacedNumberHint(i int) {
	if s.w != 1 || i < 2 || s.rs[i-1] != ' ' {
		return
	}
	j := s.numBefore(i - 1)
	num := string(s.rs[j : i-1])
	if v, err := strconv.ParseFloat(num, 64); err == nil && strings.ContainsAny(num, "0123456789") && math.Abs(v) < 21 {
		s.note(NoticeSpacedNumber, fmt.Sprintf("`%s ::` — the space stops %s from being a weight", num, num),
			fmt.Sprintf("Remove the space (`%s::`) to set a weight", num), j, i+2)
	}
}

// N4: `::` followed by a number and then a space, comma or the end — the
// number was probably meant before the `::` (NovelAI's emphasisError2).
func (s *scanner) numberAfterHint(i int) {
	k := i + 2
	start := k
	if k < len(s.rs) && s.rs[k] == '-' {
		k++
	}
	digits := 0
	for k < len(s.rs) && (isASCIIDigit(s.rs[k]) || s.rs[k] == '.') {
		if isASCIIDigit(s.rs[k]) {
			digits++
		}
		k++
	}
	if digits == 0 {
		return
	}
	if k < len(s.rs) && s.rs[k] != ' ' && s.rs[k] != ',' && s.rs[k] != '\n' {
		return
	}
	num := string(s.rs[start:k])
	if v, err := strconv.ParseFloat(num, 64); err == nil && math.Abs(v) < 21 {
		s.note(NoticeNumberAfter, fmt.Sprintf("`::%s` — a number after `::` is plain text", num),
			fmt.Sprintf("Put the weight before `::` (`%s::text ::`)", num), i, k)
	}
}

// build splits the weighted buffer into tags, applies the sign rules, and
// the tag-count bound.
func (s *scanner) build() Surface {
	var surf Surface
	var piece []entry
	flush := func() {
		if t, ok := s.makeTag(piece); ok {
			s.classify(&surf, t)
		}
		piece = piece[:0]
	}
	for _, e := range s.buf {
		if e.sep {
			flush()
			continue
		}
		piece = append(piece, e)
	}
	flush()
	if n := len(surf.Tags) + len(surf.Relocated); n > MaxTags {
		// counted after expansion, attributed to the whole field
		s.errs = append(s.errs, Issue{Code: CodeTooLong, Message: fmt.Sprintf("too many tags (%d; the limit is %d)", n, MaxTags),
			Hint: "Shorten the prompt", Start: 0, End: s.u16[len(s.rs)]})
	}
	surf.Notices = append(surf.Notices, s.notes...)
	if s.mode == Positive || s.mode == Negative {
		surf.Marks = s.marks()
	}
	return surf
}

// marks locates the typed weighting: every operator, and every stretch of
// tag text whose weight is not 1. Runes that came out of a chunk are
// skipped — a reference is painted as a chunk, never by emphasis.
func (s *scanner) marks() []Mark {
	var out []Mark
	add := func(start, end int, kind string, w float64) {
		if end > start {
			out = append(out, Mark{Start: s.u16[start], End: s.u16e[end-1], Kind: kind, Weight: round6(w)})
		}
	}
	for _, o := range s.ops {
		// an operator is typed as a whole or not at all
		if s.ref[o.start] < 0 {
			add(o.start, o.end, MarkOp, o.w)
		}
	}
	flush := func(run []entry) {
		for len(run) > 0 && unicode.IsSpace(run[0].r) {
			run = run[1:]
		}
		for len(run) > 0 && unicode.IsSpace(run[len(run)-1].r) {
			run = run[:len(run)-1]
		}
		if len(run) == 0 {
			return
		}
		kind := MarkDown
		if run[0].w > 1 {
			kind = MarkUp
		}
		add(run[0].src, run[len(run)-1].src+1, kind, run[0].w)
	}
	var run []entry
	for _, e := range s.buf {
		marked := !e.sep && s.ref[e.src] < 0 && math.Abs(e.w-1) >= markEpsilon
		if n := len(run); n > 0 && (!marked || e.w != run[0].w || e.src != run[n-1].src+1) {
			flush(run)
			run = run[:0]
		}
		if marked {
			run = append(run, e)
		}
	}
	flush(run)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

func (s *scanner) makeTag(es []entry) (Tag, bool) {
	a, b := 0, len(es)
	for a < b && unicode.IsSpace(es[a].r) {
		a++
	}
	for b > a && unicode.IsSpace(es[b-1].r) {
		b--
	}
	if a == b {
		return Tag{}, false
	}
	es = es[a:b]
	// collapse whitespace; a space takes the weight of the rune before it,
	// so whitespace never forms a run of its own weight
	flat := make([]entry, 0, len(es))
	for _, e := range es {
		if unicode.IsSpace(e.r) {
			if flat[len(flat)-1].r == ' ' {
				continue
			}
			e.r, e.w = ' ', flat[len(flat)-1].w
		}
		flat = append(flat, e)
	}
	var t Tag
	var sb, tb strings.Builder
	cur, first := flat[0].w, 0
	flush := func(end int) {
		a, b := flat[first].src, flat[end-1].src
		t.Runs = append(t.Runs, Run{Text: sb.String(), Weight: cur,
			Span: Span{Start: s.u16[a], End: s.u16e[b], Index: s.piece[a]}})
	}
	for i, e := range flat {
		if e.w != cur {
			flush(i)
			sb.Reset()
			cur, first = e.w, i
		}
		sb.WriteRune(e.r)
		tb.WriteRune(e.r)
	}
	flush(len(flat))
	t.Text = tb.String()
	t.Name = Normalize(t.Text)
	t.Span = Span{Start: s.u16[es[0].src], End: s.u16e[es[len(es)-1].src], Index: s.piece[es[0].src]}
	// provenance: a tag every rune of which came out of one reference
	if ref := s.ref[es[0].src]; ref >= 0 {
		whole := true
		for _, e := range es {
			if s.ref[e.src] != ref {
				whole = false
				break
			}
		}
		if whole {
			c := s.refs[ref].ChunkRef
			t.Chunk = &c
		}
	}
	return t, true
}

func (s *scanner) tagIssue(t Tag, code, msg, hint string) Issue {
	if t.Chunk != nil {
		msg = inChunk(t.Chunk.Name, msg)
	}
	return Issue{Code: code, Message: msg, Hint: hint, Start: t.Span.Start, End: t.Span.End, Tag: t.Span.Index}
}

func (s *scanner) classify(surf *Surface, t Tag) {
	var neg, zero, pos int
	var negW float64
	for _, r := range t.Runs {
		if strings.TrimSpace(r.Text) == "" {
			continue
		}
		switch {
		case r.Weight < 0:
			neg++
			negW = r.Weight
		case r.Weight == 0:
			zero++
		default:
			pos++
		}
	}
	if s.mode == modeRaw {
		surf.Tags = append(surf.Tags, t)
		return
	}
	if s.mode.config() {
		if neg+zero > 0 {
			s.errs = append(s.errs, s.tagIssue(t, CodeWeightNonPositive,
				fmt.Sprintf("`%s` has a weight ≤ 0; config prompts take weights in (0, %v]", t.Text, MaxConfigWeight), "Use a weight above 0"))
			return
		}
		surf.Tags = append(surf.Tags, t)
		return
	}
	switch {
	case (neg > 0 && pos+zero > 0) || (zero > 0 && pos > 0):
		s.errs = append(s.errs, s.tagIssue(t, CodeMixedSign,
			fmt.Sprintf("`%s` is only partly negative or zero-weighted", t.Text), "Put the negative part in its own tag"))
	case neg > 0 && s.mode == Negative:
		s.errs = append(s.errs, s.tagIssue(t, CodeNegativeInUC,
			fmt.Sprintf("`%s` has a negative weight in Undesired Content — that steers toward it", t.Text),
			"Remove the minus sign, or move the tag to the prompt"))
	case neg > 0:
		for k := range t.Runs {
			t.Runs[k].Weight = math.Abs(t.Runs[k].Weight)
		}
		t.Relocated = true
		surf.Relocated = append(surf.Relocated, t)
		s.notes = append(s.notes, s.tagIssue(t, NoticeRelocated,
			fmt.Sprintf("`%s` (weight %s) moved to Undesired Content at weight %s", t.Text, naiNum(negW), naiNum(-negW)),
			"SDXL can't invert a tag; Undesired Content is where avoidance lives"))
	case zero > 0:
		s.notes = append(s.notes, s.tagIssue(t, NoticeZeroDropped,
			fmt.Sprintf("`%s` has weight 0 and was dropped — SDXL has no meaning for it", t.Text),
			"Remove it, or put it in Undesired Content"))
	default:
		surf.Tags = append(surf.Tags, t)
	}
}

func round6(w float64) float64 { return math.Round(w*1e6) / 1e6 }

// ParseSafety parses FORBIDDEN_TAG_UC. It must contain no operator at all
// — no brackets, parens, colons, escapes — so every safety tag is plain
// text at exactly weight 1 (S3); otherwise boot is refused.
func ParseSafety(text string) ([]Tag, error) {
	if strings.Contains(text, ChunkPrefix) {
		return nil, &Error{Issue{Code: CodeChunkInConfig, Message: "FORBIDDEN_TAG_UC must not contain a chunk reference"}}
	}
	for _, r := range text {
		if strings.ContainsRune(`{}[]()\:|<`, r) {
			return nil, &Error{Issue{Code: CodeSafetyOperator,
				Message: fmt.Sprintf("FORBIDDEN_TAG_UC must be plain comma-separated tags; it contains %q", r)}}
		}
	}
	surf, err := Parse(text, ConfigNegative)
	if err != nil {
		return nil, err
	}
	if len(surf.Tags) == 0 {
		return nil, &Error{Issue{Code: CodeEmpty, Message: "FORBIDDEN_TAG_UC is empty"}}
	}
	for i := range surf.Tags {
		t := &surf.Tags[i]
		t.Source = Safety
		for k := range t.Runs {
			if t.Runs[k].Weight != 1 {
				return nil, &Error{Issue{Code: CodeSafetyOperator, Message: "FORBIDDEN_TAG_UC tags must all be at weight 1"}}
			}
			t.Runs[k].Emitted = 1
		}
	}
	return surf.Tags, nil
}
