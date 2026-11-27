package prompt

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

func ck(pairs ...string) []Chunk {
	var out []Chunk
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, Chunk{Name: pairs[i], Content: pairs[i+1]})
	}
	return out
}

// TestExpandOrigin: every expanded rune maps back to the reference's
// UTF-16 span and piece index, also after an astral character and with
// commas inside the chunk.
func TestExpandOrigin(t *testing.T) {
	// "😀" is two UTF-16 units; the reference is piece 1
	src := "😀, !macro:Hair!, smile"
	exp, err := Expand(src, ck("Hair", "silver hair, long hair"), Positive)
	if err != nil {
		t.Fatal(err)
	}
	if exp.Text != "😀, silver hair, long hair, smile" {
		t.Fatalf("text = %q", exp.Text)
	}
	if len(exp.Origin) != utf8.RuneCountInString(exp.Text) {
		t.Fatalf("origin has %d entries for %d runes", len(exp.Origin), utf8.RuneCountInString(exp.Text))
	}
	if len(exp.Refs) != 1 {
		t.Fatalf("refs = %+v", exp.Refs)
	}
	ref := exp.Refs[0]
	// 😀(0..2) ,(2) space(3) !macro:Hair!(4..16)
	if ref.Name != "Hair" || ref.Start != 4 || ref.End != 16 || ref.Index != 1 || ref.Text != "silver hair, long hair" ||
		ref.Hash != HashChunk("silver hair, long hair") || len(ref.Hash) != 16 {
		t.Errorf("ref = %+v", ref)
	}
	for i, o := range exp.Origin {
		r := []rune(exp.Text)[i]
		switch {
		case i < 3: // typed prefix
			if o.Ref != -1 {
				t.Errorf("rune %d %q: typed text has ref %d", i, r, o.Ref)
			}
		case i < 3+len("silver hair, long hair"):
			if o.Ref != 0 || o.Start != 4 || o.End != 16 || o.Piece != 1 {
				t.Errorf("rune %d %q: origin %+v, want the reference [4,16) piece 1", i, r, o)
			}
		default:
			if o.Ref != -1 {
				t.Errorf("rune %d %q: typed suffix has ref %d", i, r, o.Ref)
			}
		}
	}
	// the typed text after the chunk keeps its own offsets and piece
	last := exp.Origin[len(exp.Origin)-1]
	if last.End != exp.SrcLen || last.Piece != 2 || exp.SrcPieces != 2 {
		t.Errorf("last origin %+v, src len %d pieces %d", last, exp.SrcLen, exp.SrcPieces)
	}
}

// TestChunkSpansLandOnReference: tags and issues inside a chunk are
// located on the token, with the piece index of the token's own piece.
func TestChunkSpansLandOnReference(t *testing.T) {
	chunks := ck("Hair", "silver hair, {long hair}", "Bad", "smile, red}")
	s, err := ParseWith("😀, !macro:Hair!, smile", Positive, chunks)
	if err != nil {
		t.Fatal(err)
	}
	checkTags(t, "ref", s.Tags, []tw{one("😀", 1), one("silver hair", 1), one("long hair", 1.05), one("smile", 1)})
	for _, i := range []int{1, 2} {
		g := s.Tags[i]
		if g.Span != (Span{Start: 4, End: 16, Index: 1}) {
			t.Errorf("tag %q span = %+v, want the reference", g.Text, g.Span)
		}
		if g.Chunk == nil || g.Chunk.Name != "Hair" || g.Chunk.Hash != HashChunk("silver hair, {long hair}") {
			t.Errorf("tag %q chunk = %+v", g.Text, g.Chunk)
		}
	}
	if s.Tags[0].Chunk != nil || s.Tags[3].Chunk != nil {
		t.Errorf("typed tags carry provenance: %+v", s.Tags)
	}
	// the typed tag after the chunk is piece 2, not shifted by the chunk's comma
	if s.Tags[3].Span != (Span{Start: 18, End: 23, Index: 2}) {
		t.Errorf("typed tag after the chunk: %+v", s.Tags[3].Span)
	}

	// an error inside a chunk: located on the reference, names the chunk
	_, _, errs := AnalyzeWith("1girl, !macro:Bad!", Positive, chunks)
	if len(errs) != 1 || errs[0].Code != CodeStrayCloser || errs[0].Start != 7 || errs[0].End != 18 || errs[0].Tag != 1 ||
		!strings.HasPrefix(errs[0].Message, "in chunk `Bad`: ") {
		t.Errorf("error inside a chunk = %+v", errs)
	}
	// an unclosed bracket inside a chunk never leaks into the typed text
	_, err = ParseWith("!macro:Open!, tail", Positive, ck("Open", "{a"))
	if e, ok := err.(*Error); !ok || e.Code != CodeUnclosedBracket || e.Start != 0 || e.End != 12 {
		t.Errorf("unclosed bracket in a chunk = %v", err)
	}
	// a tag only partly from a chunk carries no provenance
	s, err = ParseWith("blue !macro:H!", Positive, ck("H", "hair, eyes"))
	if err != nil {
		t.Fatal(err)
	}
	checkTags(t, "glued", s.Tags, []tw{one("blue hair", 1), one("eyes", 1)})
	if s.Tags[0].Chunk != nil || s.Tags[1].Chunk == nil {
		t.Errorf("provenance of a glued tag: %+v", s.Tags)
	}
	// an outer weight applies to the whole expansion and is restored after
	s, err = ParseWith("{!macro:H!}, x, 1.5::!macro:H! ::, y", Positive, ck("H", "hair, eyes"))
	if err != nil {
		t.Fatal(err)
	}
	checkTags(t, "weighted", s.Tags, []tw{one("hair", 1.05), one("eyes", 1.05), one("x", 1), one("hair", 1.5), one("eyes", 1.5), one("y", 1)})
}

func TestChunkNesting(t *testing.T) {
	chunks := ck("A", "a1, !macro:B!", "B", "b1, !macro:C!", "C", "c1")
	s, err := ParseWith("!macro:A!, z", Positive, chunks)
	if err != nil {
		t.Fatal(err)
	}
	checkTags(t, "nested", s.Tags, []tw{one("a1", 1), one("b1", 1), one("c1", 1), one("z", 1)})
	// provenance is the reference the user typed, hashed over the resolved text
	for _, g := range s.Tags[:3] {
		if g.Chunk == nil || g.Chunk.Name != "A" || g.Chunk.Hash != HashChunk("a1, b1, c1") {
			t.Errorf("nested tag %q chunk = %+v", g.Text, g.Chunk)
		}
	}
	// editing a nested chunk changes the outer hash
	s2, err := ParseWith("!macro:A!", Positive, ck("A", "a1, !macro:B!", "B", "b1, !macro:C!", "C", "c2"))
	if err != nil || s2.Tags[0].Chunk.Hash == s.Tags[0].Chunk.Hash {
		t.Errorf("hash ignores a nested edit: %v %+v", err, s2.Tags)
	}
	// NovelAI trims the name inside the token
	if s, err := ParseWith("!macro: C !", Positive, chunks); err != nil || s.Tags[0].Text != "c1" {
		t.Errorf("trimmed name: %v %+v", err, s)
	}
	// names are case-sensitive
	if _, err := ParseWith("!macro:c!", Positive, chunks); err == nil {
		t.Error("names must be case-sensitive")
	}
}

func chain(n int) []Chunk {
	var out []Chunk
	for i := 1; i <= n; i++ {
		content := fmt.Sprintf("t%d", i)
		if i < n {
			content += fmt.Sprintf(", !macro:L%d!", i+1)
		}
		out = append(out, Chunk{Name: fmt.Sprintf("L%d", i), Content: content})
	}
	return out
}

func TestChunkErrors(t *testing.T) {
	for _, tc := range []struct {
		name, src  string
		chunks     []Chunk
		code       string
		start, end int
	}{
		{"unknown", "1girl, !macro:Nope!", nil, CodeUnknownChunk, 7, 19},
		{"unknown nested", "!macro:A!", ck("A", "x, !macro:Gone!"), CodeUnknownChunk, 0, 9},
		{"self reference", "a, !macro:A!", ck("A", "x, !macro:A!"), CodeChunkCycle, 3, 12},
		{"A→B→A", "!macro:A!", ck("A", "!macro:B!", "B", "!macro:A!"), CodeChunkCycle, 0, 9},
		{"depth 9", "!macro:L1!", chain(9), CodeChunkDepth, 0, 10},
		{"no closing !", "1girl, !macro:Name", nil, CodeChunkSyntax, 7, 18},
		{"comma in name", "!macro:a,b!", nil, CodeChunkSyntax, 0, 8},
		{"empty name", "!macro:!", nil, CodeChunkSyntax, 0, 8},
		{"bad token in a chunk", "!macro:A!", ck("A", "x, !macro:"), CodeChunkSyntax, 0, 9},
	} {
		_, err := ParseWith(tc.src, Positive, tc.chunks)
		e, ok := err.(*Error)
		if !ok || e.Code != tc.code || e.Start != tc.start || e.End != tc.end {
			t.Errorf("%s: %q = %v (%+v), want %s [%d,%d)", tc.name, tc.src, err, e, tc.code, tc.start, tc.end)
		}
		// Expand reports the same first error
		if _, xerr := Expand(tc.src, tc.chunks, Negative); xerr == nil || xerr.(*Error).Code != tc.code {
			t.Errorf("%s: Expand = %v", tc.name, xerr)
		}
	}
	// depth 8 is allowed; a chunk that fits at depth 1 is refused where it is too deep
	if s, err := ParseWith("!macro:L1!", Positive, chain(8)); err != nil || len(s.Tags) != 8 {
		t.Errorf("depth 8: %v", err)
	}
	if _, err := ParseWith("!macro:L2!, !macro:L1!", Positive, chain(9)); err == nil || err.(*Error).Code != CodeChunkDepth || err.(*Error).Start != 12 {
		t.Errorf("memoised chunk used too deep: %v", err)
	}
	// every reference error is reported for live validation, each on its token
	_, _, errs := AnalyzeWith("!macro:X!, ok, !macro:Y!", Positive, nil)
	if len(errs) != 2 || errs[0].Start != 0 || errs[1].Start != 15 || errs[1].Tag != 2 {
		t.Errorf("all reference errors: %+v", errs)
	}
	// Parse without chunks: a reference is unknown, never silently literal
	if _, err := Parse("!macro:X!", Positive); err == nil || err.(*Error).Code != CodeUnknownChunk {
		t.Errorf("Parse without chunks: %v", err)
	}
}

// TestBangTagsStillParse: the real `!`-prefixed tags are untouched — the
// lexer needs the full `!macro:` prefix.
func TestBangTagsStillParse(t *testing.T) {
	s, err := ParseWith("!, !?, !!, !macro, macro:x, surprised!", Positive, ck("x", "y"))
	if err != nil {
		t.Fatal(err)
	}
	checkTags(t, "bang", s.Tags, []tw{one("!", 1), one("!?", 1), one("!!", 1), one("!macro", 1), one("macro:x", 1), one("surprised!", 1)})
	for _, g := range s.Tags {
		if g.Chunk != nil {
			t.Errorf("%q carries provenance", g.Text)
		}
	}
}

// TestChunkInConfig: a reference in a config or safety surface is refused
// (boot fails), whether or not a chunk of that name exists.
func TestChunkInConfig(t *testing.T) {
	chunks := ck("X", "masterpiece")
	for _, m := range []Mode{ConfigPositive, ConfigNegative} {
		_, err := ParseWith("best quality, !macro:X!", m, chunks)
		if e, ok := err.(*Error); !ok || e.Code != CodeChunkInConfig || e.Start != 14 {
			t.Errorf("mode %d: %v", m, err)
		}
		if _, err := Expand("!macro:X!", chunks, m); err == nil || err.(*Error).Code != CodeChunkInConfig {
			t.Errorf("Expand mode %d: %v", m, err)
		}
		// an unterminated token is refused as well
		if _, err := Parse("a, !macro:", m); err == nil || err.(*Error).Code != CodeChunkInConfig {
			t.Errorf("mode %d bare prefix: %v", m, err)
		}
	}
	if _, err := ParseSafety("plain tag, !macro:X!"); err == nil || err.(*Error).Code != CodeChunkInConfig {
		t.Errorf("ParseSafety: %v", err)
	}
}

// TestExpandEqualsInline: a reference parses to the same runes, weights and
// emitted strings as its content typed in place — only Span and Chunk differ.
func TestExpandEqualsInline(t *testing.T) {
	bodies := []string{
		"silver hair, long hair",
		"{smile}, [frown]",
		"1.5::rain ::, 0.5::fog ::",
		"(castorice (honkai: star rail):1.2)",
		"name (series), \\(x\\)",
		"-1::hat ::, scarf",
		"0::gone ::, kept",
		"a\nb, c",
		"  spaced  ,  out  ",
		"😀 emoji, tag",
		"year 2000 ::, 1.2::x ::",
		"",
	}
	shapes := []string{"%s", "1girl, %s", "%s, tail", "a, %s, b", "{%s}", "1.3::%s ::, after", "[[%s]], z", "pre %s", "%s post", "%s, %s"}
	cal := Calibration{KUp: 1.0, Hi: 1.4, KDown: 1.0, Lo: 0.3}.Clamped()
	for _, m := range []Mode{Positive, Negative} {
		for _, body := range bodies {
			for _, shape := range shapes {
				n := strings.Count(shape, "%s")
				refs, inl := make([]any, n), make([]any, n)
				for i := range refs {
					refs[i], inl[i] = "!macro:C!", body
				}
				ref, inline := fmt.Sprintf(shape, refs...), fmt.Sprintf(shape, inl...)
				got, gerr := ParseWith(ref, m, ck("C", body))
				want, werr := Parse(inline, m)
				if (gerr == nil) != (werr == nil) {
					t.Errorf("mode %d %q vs %q: err %v vs %v", m, ref, inline, gerr, werr)
					continue
				}
				if gerr != nil {
					if gerr.(*Error).Code != werr.(*Error).Code {
						t.Errorf("mode %d %q vs %q: code %s vs %s", m, ref, inline, gerr.(*Error).Code, werr.(*Error).Code)
					}
					continue
				}
				for _, pair := range [][2][]Tag{{want.Tags, got.Tags}, {want.Relocated, got.Relocated}} {
					if err := EquivalentNAI(pair[0], pair[1], 0); err != nil {
						t.Errorf("mode %d %q vs %q: %v", m, ref, inline, err)
					}
					for i := range pair[0] {
						if pair[0][i].Text != pair[1][i].Text || pair[0][i].Name != pair[1][i].Name || len(pair[0][i].Runs) != len(pair[1][i].Runs) {
							t.Errorf("mode %d %q vs %q: tag %d %+v vs %+v", m, ref, inline, i, pair[0][i], pair[1][i])
						}
					}
				}
				cal.Apply(got.Tags)
				cal.Apply(want.Tags)
				ge, err1 := EmitComfy(got.Tags)
				we, err2 := EmitComfy(want.Tags)
				if err1 != nil || err2 != nil || ge != we {
					t.Errorf("mode %d %q vs %q: emitted %q vs %q (%v %v)", m, ref, inline, ge, we, err1, err2)
				}
				if FormatNAI(got.Tags) != FormatNAI(want.Tags) {
					t.Errorf("mode %d %q vs %q: nai %q vs %q", m, ref, inline, FormatNAI(got.Tags), FormatNAI(want.Tags))
				}
			}
		}
	}
}

// TestLimitsApplyAfterExpansion: MaxBytes and MaxTags count the expanded
// text and are attributed to the whole field.
func TestLimitsApplyAfterExpansion(t *testing.T) {
	big := strings.Repeat("abcdefg, ", MaxChunkBytes/9) // just under 4 KiB, 455 tags
	src := "!macro:Big!, !macro:Big!, !macro:Big!, !macro:Big!, !macro:Big!"
	_, _, errs := AnalyzeWith(src, Positive, ck("Big", big))
	if len(errs) != 1 || errs[0].Code != CodeTooLong || errs[0].Start != 0 || errs[0].End != len(src) {
		t.Errorf("expanded past MaxBytes: %+v", errs)
	}
	// 2 × 455 tags > MaxTags, under MaxBytes
	src = "!macro:Big!, !macro:Big!"
	_, _, errs = AnalyzeWith(src, Positive, ck("Big", big))
	if len(errs) != 1 || errs[0].Code != CodeTooLong || errs[0].Start != 0 || errs[0].End != len(src) {
		t.Errorf("expanded past MaxTags: %+v", errs)
	}
	// a nest that multiplies: bounded before it is built
	bomb := ck("A", strings.Repeat("!macro:B!", 400), "B", strings.Repeat("!macro:C!", 400), "C", strings.Repeat("x", 100))
	if _, err := ParseWith("!macro:A!", Positive, bomb); err == nil || err.(*Error).Code != CodeTooLong {
		t.Errorf("expansion bomb: %v", err)
	}
	// many references to an empty chunk stay linear (memoised)
	var deep []Chunk
	for i := 1; i <= 8; i++ {
		c := ""
		if i < 8 {
			c = strings.Repeat(fmt.Sprintf("!macro:E%d!", i+1), 400)
		}
		deep = append(deep, Chunk{Name: fmt.Sprintf("E%d", i), Content: c})
	}
	if s, err := ParseWith("!macro:E1!, x", Positive, deep); err != nil || len(s.Tags) != 1 {
		t.Errorf("empty nest: %v %+v", err, s)
	}
}

func TestCheckChunks(t *testing.T) {
	for _, name := range []string{"Silver Wolf", "a", "Category: label", "名前", strings.Repeat("n", MaxChunkName)} {
		if err := CheckChunkName(name); err != nil {
			t.Errorf("%q: %v", name, err)
		}
	}
	for _, name := range []string{"", " x", "x ", "a!b", "a,b", "a|b", "a{b", "a}b", "a[b", "a]b", "a::b", "a\nb", "a\tb", strings.Repeat("n", MaxChunkName+1), "\xff"} {
		if err := CheckChunkName(name); err == nil {
			t.Errorf("%q must be refused", name)
		}
	}
	if err := CheckChunks(ck("a", "x", "b", "y")); err != nil {
		t.Error(err)
	}
	if err := CheckChunks(ck("a", "x", "a", "y")); err == nil {
		t.Error("duplicate names must be refused")
	}
	if err := CheckChunks(ck("a", strings.Repeat("x", MaxChunkBytes+1))); err == nil {
		t.Error("oversized content must be refused")
	}
	if err := CheckChunks(ck("a", strings.Repeat("x", MaxChunkBytes))); err != nil {
		t.Error(err)
	}
	var many []Chunk
	for i := 0; i <= MaxChunks; i++ {
		many = append(many, Chunk{Name: fmt.Sprintf("c%d", i)})
	}
	if err := CheckChunks(many); err == nil {
		t.Error("65 chunks must be refused")
	}
	if err := CheckChunks(many[:MaxChunks]); err != nil {
		t.Error(err)
	}
}

// TestMarks: the emphasis golden — operator spans and weighted text, in
// UTF-16 offsets, NovelAI units.
func TestMarks(t *testing.T) {
	s, err := Parse("{a}, [b], 1.5::c::, d", Positive)
	if err != nil {
		t.Fatal(err)
	}
	down := round6(1 / bracketStep)
	want := []Mark{
		{0, 1, MarkOp, 1.05}, {1, 2, MarkUp, 1.05}, {2, 3, MarkOp, 1},
		{5, 6, MarkOp, down}, {6, 7, MarkDown, down}, {7, 8, MarkOp, 1},
		{10, 15, MarkOp, 1.5}, {15, 16, MarkUp, 1.5}, {16, 18, MarkOp, 1},
	}
	if len(s.Marks) != len(want) {
		t.Fatalf("marks = %+v", s.Marks)
	}
	for i, w := range want {
		if s.Marks[i] != w {
			t.Errorf("mark %d = %+v, want %+v", i, s.Marks[i], w)
		}
	}
	// run spans are recorded too
	if r := s.Tags[2].Runs[0]; r.Span != (Span{Start: 15, End: 16, Index: 2}) {
		t.Errorf("run span = %+v", r.Span)
	}
	// a mid-tag weight change gives one mark per run; plain text gets none
	s, _ = Parse("blue {hair} x, -1::hat ::, 😀 {y}", Positive)
	want = []Mark{{5, 6, MarkOp, 1.05}, {6, 10, MarkUp, 1.05}, {10, 11, MarkOp, 1}, {15, 19, MarkOp, -1}, {19, 22, MarkDown, -1}, {23, 25, MarkOp, 1},
		{30, 31, MarkOp, 1.05}, {31, 32, MarkUp, 1.05}, {32, 33, MarkOp, 1}}
	if fmt.Sprint(s.Marks) != fmt.Sprint(want) {
		t.Errorf("marks = %+v\nwant   %+v", s.Marks, want)
	}
	// SD parens are operators; a weight within 0.01 of 1 is not painted
	s, _ = Parse("(x:1.2), 1.005::y ::", Positive)
	want = []Mark{{0, 1, MarkOp, 1.2}, {1, 2, MarkUp, 1.2}, {2, 7, MarkOp, 1.2}, {9, 16, MarkOp, 1.005}, {18, 20, MarkOp, 1}}
	if fmt.Sprint(s.Marks) != fmt.Sprint(want) {
		t.Errorf("marks = %+v\nwant   %+v", s.Marks, want)
	}
	// chunk text is never marked — not its operators, not its weighted text
	// — while the typed operators around the reference are
	s, err = ParseWith("{!macro:C!}, 2::z", Positive, ck("C", "[a], 1.5::b ::"))
	if err != nil {
		t.Fatal(err)
	}
	want = []Mark{{0, 1, MarkOp, 1.05}, {10, 11, MarkOp, 1}, {13, 16, MarkOp, 2}, {16, 17, MarkUp, 2}}
	if fmt.Sprint(s.Marks) != fmt.Sprint(want) {
		t.Errorf("marks around a chunk = %+v\nwant %+v", s.Marks, want)
	}
	// config surfaces carry none
	if s, _ := Parse("{a}", ConfigPositive); len(s.Marks) != 0 {
		t.Errorf("config marks = %+v", s.Marks)
	}
}

func FuzzExpand(f *testing.F) {
	for _, s := range [][3]string{
		{"!macro:A!", "x, !macro:B!", "{y}"},
		{"a, !macro:A!, !macro:B!", "!macro:A!", "1.5::z"},
		{"{!macro:A!}", "a}, b", "!macro:A!"},
		{"!macro:", "!macro:B", "!"},
		{"😀 !macro:A! !macro:Nope!", "😀, \n", ""},
	} {
		f.Add(s[0], s[1], s[2])
	}
	f.Fuzz(func(t *testing.T, src, a, b string) {
		chunks := []Chunk{{Name: "A", Content: a}, {Name: "B", Content: b}}
		srcLen := 0
		for _, r := range src {
			if r >= 0x10000 {
				srcLen += 2
			} else {
				srcLen++
			}
		}
		for _, m := range []Mode{Positive, Negative, ConfigPositive, ConfigNegative} {
			exp, errs := expand(src, chunks, m)
			if len(errs) == 0 {
				if len(exp.Origin) != utf8.RuneCountInString(exp.Text) {
					t.Fatalf("origin %d ≠ runes %d for %q", len(exp.Origin), utf8.RuneCountInString(exp.Text), src)
				}
				if m.config() && strings.Contains(exp.Text, ChunkPrefix) {
					t.Fatalf("a reference survived in config mode: %q", src)
				}
				if len(exp.Text) > MaxBytes && len(exp.Refs) > 0 {
					t.Fatalf("expansion past MaxBytes for %q", src)
				}
			}
			for _, o := range exp.Origin {
				if o.Start < 0 || o.End < o.Start || o.End > srcLen || o.Ref < -1 || o.Ref >= len(exp.Refs) {
					t.Fatalf("bad origin %+v (src len %d) for %q", o, srcLen, src)
				}
			}
			surf, _, errs := AnalyzeWith(src, m, chunks)
			for _, e := range errs {
				if e.Start < 0 || e.End < e.Start || e.End > srcLen || e.Code == "" {
					t.Fatalf("bad issue %+v (src len %d) for %q", e, srcLen, src)
				}
			}
			for _, tg := range append(surf.Tags, surf.Relocated...) {
				if tg.Span.Start < 0 || tg.Span.End < tg.Span.Start || tg.Span.End > srcLen {
					t.Fatalf("bad tag span %+v for %q", tg.Span, src)
				}
				for _, r := range tg.Runs {
					if math.IsNaN(r.Weight) || math.IsInf(r.Weight, 0) {
						t.Fatalf("non-finite weight in %q", src)
					}
				}
			}
			for _, mk := range surf.Marks {
				if mk.Start < 0 || mk.End <= mk.Start || mk.End > srcLen {
					t.Fatalf("bad mark %+v for %q", mk, src)
				}
			}
			for _, n := range surf.Notices {
				if n.Start < 0 || n.End < n.Start || n.End > srcLen {
					t.Fatalf("bad notice %+v for %q", n, src)
				}
			}
		}
	})
}
