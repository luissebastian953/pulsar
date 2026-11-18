package prompt

import (
	"math"
	"strings"
	"testing"
)

// tw is one expected tag: its text and the NAI weight of each non-space
// rune run, in order.
type tw struct {
	text string
	w    []float64
}

func one(text string, w float64) tw { return tw{text, []float64{w}} }

func checkTags(t *testing.T, in string, got []Tag, want []tw) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%q: %d tags %v, want %d %v", in, len(got), texts(got), len(want), want)
	}
	for i, w := range want {
		g := got[i]
		if g.Text != w.text {
			t.Errorf("%q tag %d text = %q, want %q", in, i, g.Text, w.text)
		}
		var ws []float64
		for _, r := range g.Runs {
			if strings.TrimSpace(r.Text) != "" {
				ws = append(ws, r.Weight)
			}
		}
		if len(ws) != len(w.w) {
			t.Errorf("%q tag %q runs = %+v, want weights %v", in, g.Text, g.Runs, w.w)
			continue
		}
		for k := range ws {
			if math.Abs(ws[k]-w.w[k]) > 1e-6 {
				t.Errorf("%q tag %q run %d weight = %v, want %v", in, g.Text, k, ws[k], w.w[k])
			}
		}
	}
}

func texts(ts []Tag) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Text
	}
	return out
}

func hasNotice(s Surface, code string) bool {
	for _, n := range s.Notices {
		if n.Code == code {
			return true
		}
	}
	return false
}

// TestParseGolden is the S9 table, adjusted by the product decisions
// (unbalanced brackets are errors, scoped closers restore, negative
// weights relocate, SD syntax converts).
func TestParseGolden(t *testing.T) {
	cases := []struct {
		in      string
		want    []tw
		notices []string
	}{
		{"{{{x}}}", []tw{one("x", 1.157625)}, nil},
		{"{[x]}", []tw{one("x", 1)}, nil},
		{"1girl, 1.5::rain, night ::, 0.5::coat ::, black shoes",
			[]tw{one("1girl", 1), one("rain", 1.5), one("night", 1.5), one("coat", 0.5), one("black shoes", 1)}, nil},
		// pulsar divergence: the closer restores the weight from before its
		// opener; NovelAI would leak 1.8/1.05 = 1.714 onto smile
		{"{1.8::holding hands}, smile", []tw{one("holding hands", 1.8), one("smile", 1)}, nil},
		{"[0.71::long legs], smile", []tw{one("long legs", 0.71), one("smile", 1)}, nil},
		{"1.5::{red hair}::, smile", []tw{one("red hair", 1.575), one("smile", 1)}, nil},
		{"{{rain::}}, smile", []tw{one("rain", 1.1025), one("smile", 1)}, nil},
		{"2::a, {b}, c::, d", []tw{one("a", 2), one("b", 2.1), one("c", 2), one("d", 1)}, nil},
		{"1.5::year 2000 ::, smile", []tw{one("year 2000", 1.5), one("smile", 1)}, nil},
		{".5::coat::", []tw{one("coat", 0.5)}, nil},
		{"10::x::", []tw{one("x", 10)}, nil},
		{"blue {hair}, x", []tw{{"blue hair", []float64{1, 1.05}}, one("x", 1)}, nil},
		// weight carries across newlines inside one scope
		{"1girl,\n{red eyes\nsmile}", []tw{one("1girl", 1), one("red eyes", 1.05), one("smile", 1.05)}, nil},
		{"thin legs ::, x", []tw{one("thin legs", 1), one("x", 1)}, nil},
		{"1.5 ::hat::", []tw{one("1.5 hat", 1)}, []string{NoticeSpacedNumber}},
		{"castorice (honkai: star rail), 1girl", []tw{one("castorice (honkai: star rail)", 1), one("1girl", 1)}, nil},
		{"makima \\(chainsaw man\\)", []tw{one("makima (chainsaw man)", 1)}, nil},
		{"  spaced   out  ,  tag ", []tw{one("spaced out", 1), one("tag", 1)}, nil},
		{"a,,b, ,c", []tw{one("a", 1), one("b", 1), one("c", 1)}, nil},
		{"artist:foo, rating:general", []tw{one("artist:foo", 1), one("rating:general", 1)}, nil},
		// the user's example: no error, unclosed 0.9:: runs to the end
		{"0.9::sleeveless, [0.71::long legs], {1.8::holding hands}",
			[]tw{one("sleeveless", 0.9), one("long legs", 0.71), one("holding hands", 1.8)},
			[]string{NoticeOpenSection}},
		{"1.5::rain, night", []tw{one("rain", 1.5), one("night", 1.5)}, []string{NoticeOpenSection}},
		{"1.5::year 5::, x", []tw{one("year", 1.5), one("x", 5)}, []string{NoticeNumberInTag}},
		{"hat::1.5 x", []tw{one("hat1.5 x", 1)}, []string{NoticeNumberAfter}},
		// SD/ComfyUI syntax auto-converts (decision 2)
		{"(blue hair:1.2), x", []tw{one("blue hair", 1.2), one("x", 1)}, []string{NoticeConvertedSD}},
		{"(castorice (honkai: star rail):1.2)", []tw{one("castorice (honkai: star rail)", 1.2)}, []string{NoticeConvertedSD}},
		{"(castorice \\(honkai: star rail\\):1.2)", []tw{one("castorice (honkai: star rail)", 1.2)}, []string{NoticeConvertedSD}},
		{"(artist:foo:0.8)", []tw{one("artist:foo", 0.8)}, []string{NoticeConvertedSD}},
		{"(red, blue hair:1.2), x", []tw{one("red", 1.2), one("blue hair", 1.2), one("x", 1)}, []string{NoticeConvertedSD}},
		{"{(a:1.2)}, b", []tw{one("a", 1.2), one("b", 1)}, []string{NoticeConvertedSD}},
		{"(blue {hair}:1.2)", []tw{{"blue hair", []float64{1.2, 1.26}}}, []string{NoticeConvertedSD}},
		{"( x : 1.5 )", []tw{one("x", 1.5)}, []string{NoticeConvertedSD}},
		// bare parens stay literal (NovelAI semantics)
		{"(blue hair), x", []tw{one("(blue hair)", 1), one("x", 1)}, []string{NoticeLiteralParens}},
		{"(:3), x", []tw{one("(:3)", 1), one("x", 1)}, []string{NoticeLiteralParens}},
		{"(unclosed, red", []tw{one("(unclosed", 1), one("red", 1)}, nil},
	}
	for _, tc := range cases {
		s, err := Parse(tc.in, Positive)
		if err != nil {
			t.Errorf("%q: unexpected error %v", tc.in, err)
			continue
		}
		checkTags(t, tc.in, s.Tags, tc.want)
		for _, code := range tc.notices {
			if !hasNotice(s, code) {
				t.Errorf("%q: missing notice %s (got %+v)", tc.in, code, s.Notices)
			}
		}
		if len(tc.notices) == 0 {
			for _, n := range s.Notices {
				t.Errorf("%q: unexpected notice %s: %s", tc.in, n.Code, n.Message)
			}
		}
	}
}

// TestParseErrors: every rejection carries a code and UTF-16 offsets into
// the surface plus the piece index the chip UI maps through.
func TestParseErrors(t *testing.T) {
	cases := []struct {
		in         string
		code       string
		start, end int
		tag        int
	}{
		{"{a, [b], c", CodeUnclosedBracket, 0, 1, 0},
		{"{{{{{rain ::, smile", CodeUnclosedBracket, 4, 5, 0}, // `::` doesn't close brackets in pulsar
		{"a}, b", CodeStrayCloser, 1, 2, 0},
		{"1girl, red}", CodeStrayCloser, 10, 11, 1},
		{"{]", CodeMismatchedBracket, 0, 2, 0},
		{"x, [a}", CodeMismatchedBracket, 3, 6, 1},
		{"{(a}:1.2)", CodeMismatchedBracket, 3, 4, 0},
		{"1.5::year 2000::, smile", CodeGluedNumber, 10, 16, 0},
		{"year 2000::", CodeGluedNumber, 5, 11, 0},
		{"1.2::artist:coupe50::", CodeGluedNumber, 17, 21, 0},
		{"1.2.3::hat::", CodeGluedNumber, 2, 7, 0},
		{"type-2::x", CodeGluedNumber, 4, 8, 0},
		{"11::x::", CodeWeightTooHigh, 0, 4, 0},
		{"-11::x::", CodeWeightTooHigh, 0, 5, 0},
		{strings.Repeat("{", 48) + "x" + strings.Repeat("}", 48), CodeWeightTooHigh, 47, 48, 0},
		{"(x:11)", CodeWeightTooHigh, 0, 6, 0},
		{"blue -1::hair", CodeMixedSign, 0, 13, 0},
		{"a, b 0::c", CodeMixedSign, 3, 9, 1},
		{"((blue hair):1.5)", CodeSDWeightSyntax, 1, 12, 0},
		{"(red, (blue hair):1.5)", CodeSDWeightSyntax, 6, 17, 1},
		{"(a (b:1.2))", CodeSDWeightSyntax, 0, 11, 0},
		{"((b:1.2):1.3)", CodeSDWeightSyntax, 0, 13, 0},
		{"(x:nan)", CodeSDWeightSyntax, 0, 7, 0},
		{"(x:1e1)", CodeSDWeightSyntax, 0, 7, 0},
		{"(a::1.2)", CodeSDWeightSyntax, 2, 4, 0},
		{"Embedding:Foo", CodeEmbedding, 0, 10, 0},
		{"x, embedding:bar", CodeEmbedding, 3, 13, 1},
		{"a | b", CodePipe, 2, 3, 0},
		{"<lora:x:1>", CodeExtraNetwork, 0, 6, 0},
		{"a:::b", CodeTripleColon, 1, 4, 0},
		{"a\x00b", CodeControlChar, 1, 2, 0},
		{"a\x01\x02b", CodeControlChar, 1, 3, 0},
		{"a�b", CodeControlChar, 1, 2, 0},
		// UTF-16 offsets: CJK is one unit, the emoji two
		{"日本語 {タグ}, 😀 x}", CodeStrayCloser, 14, 15, 1},
		{"😀😀 {x", CodeUnclosedBracket, 5, 6, 0},
		{"タグ, 😀 1.5::year 2000::", CodeGluedNumber, 17, 23, 1},
	}
	for _, tc := range cases {
		_, errs := Analyze(tc.in, Positive)
		if len(errs) == 0 {
			t.Errorf("%q: accepted, want %s", tc.in, tc.code)
			continue
		}
		e := errs[0]
		if e.Code != tc.code || e.Start != tc.start || e.End != tc.end || e.Tag != tc.tag {
			t.Errorf("%q: got %s [%d,%d) tag %d (%s), want %s [%d,%d) tag %d",
				tc.in, e.Code, e.Start, e.End, e.Tag, e.Message, tc.code, tc.start, tc.end, tc.tag)
		}
		if e.Message == "" {
			t.Errorf("%q: empty message", tc.in)
		}
	}
	// the glued-number hint tells the user the fix
	_, errs := Analyze("1.5::year 2000::, smile", Positive)
	if !strings.Contains(errs[0].Hint, "space before `::`") || !strings.Contains(errs[0].Hint, "year 2000 ::") {
		t.Errorf("hint = %q", errs[0].Hint)
	}
	if _, err := Parse("{a", Positive); err == nil || !strings.Contains(err.Error(), "never closed") {
		t.Errorf("Parse error = %v", err)
	}
}

// TestNegativeWeights pins decision 1: relocation out of positive
// surfaces, rejection inside negative ones, zero dropped everywhere.
func TestNegativeWeights(t *testing.T) {
	s, err := Parse("1girl, -1::hat ::, -2.5::flat color ::, smile", Positive)
	if err != nil {
		t.Fatal(err)
	}
	checkTags(t, "positive", s.Tags, []tw{one("1girl", 1), one("smile", 1)})
	checkTags(t, "relocated", s.Relocated, []tw{one("hat", 1), one("flat color", 2.5)})
	for _, r := range s.Relocated {
		if !r.Relocated {
			t.Errorf("%q not marked relocated", r.Text)
		}
	}
	n := 0
	for _, no := range s.Notices {
		if no.Code == NoticeRelocated {
			n++
		}
	}
	if n != 2 {
		t.Errorf("want a notice per relocated tag, got %+v", s.Notices)
	}
	if _, err := Parse("-11::hat ::", Positive); err == nil {
		t.Error("|w| > 10 must reject for negative weights too")
	}
	s, err = Parse("(hat:-1), x", Positive)
	if err != nil || len(s.Relocated) != 1 || s.Relocated[0].Text != "hat" {
		t.Errorf("SD negative weight must relocate: %+v %v", s, err)
	}

	for _, in := range []string{"0::hat::, x", "-::hat::, x", ".::hat::, x", "-.::hat::, x", "(hat:0), x"} {
		for _, m := range []Mode{Positive, Negative} {
			s, err := Parse(in, m)
			if err != nil {
				t.Errorf("%q mode %d: %v", in, m, err)
				continue
			}
			checkTags(t, in, s.Tags, []tw{one("x", 1)})
			if !hasNotice(s, NoticeZeroDropped) {
				t.Errorf("%q: zero weight must be dropped with a notice", in)
			}
		}
	}

	for _, in := range []string{"lowres, -1::bad hands ::", "(x:-0.5)", "[-2::x]"} {
		_, errs := Analyze(in, Negative)
		if len(errs) == 0 || errs[0].Code != CodeNegativeInUC {
			t.Errorf("%q in UC: %+v, want %s", in, errs, CodeNegativeInUC)
		}
	}
	if s, err := Parse("{lowres}, [blurry], 1.2::bad hands ::", Negative); err != nil || len(s.Tags) != 3 {
		t.Errorf("weights in UC are legal: %+v %v", s, err)
	}
}

func TestConfigModes(t *testing.T) {
	s, err := Parse("(logo:1.3), film grain", ConfigNegative)
	if err != nil {
		t.Fatal(err)
	}
	checkTags(t, "style negative", s.Tags, []tw{one("logo", 1.3), one("film grain", 1)})
	for _, in := range []string{"2.1::x::", "(x:2.5)", strings.Repeat("{", 15) + "x" + strings.Repeat("}", 15)} {
		if _, errs := Analyze(in, ConfigPositive); len(errs) == 0 || errs[0].Code != CodeWeightTooHigh {
			t.Errorf("%q in config: %+v, want %s", in, errs, CodeWeightTooHigh)
		}
	}
	for _, in := range []string{"-1::x::", "0::x::", "(x:0)"} {
		if _, errs := Analyze(in, ConfigPositive); len(errs) == 0 || errs[0].Code != CodeWeightNonPositive {
			t.Errorf("%q in config: %+v, want %s", in, errs, CodeWeightNonPositive)
		}
	}
	if _, err := Parse("embedding:x", ConfigPositive); err == nil {
		t.Error("embedding: must reject on config surfaces too")
	}
}

func TestSizeBounds(t *testing.T) {
	_, errs := Analyze(strings.Repeat("a", MaxBytes+1), Positive)
	if len(errs) != 1 || errs[0].Code != CodeTooLong {
		t.Errorf("16 KiB bound: %+v", errs)
	}
	_, errs = Analyze(strings.Repeat("a, ", MaxTags)+"b", Positive)
	if len(errs) != 1 || errs[0].Code != CodeTooLong {
		t.Errorf("512-tag bound: %+v", errs)
	}
	if _, err := Parse(strings.Repeat("a, ", MaxTags-1)+"b", Positive); err != nil {
		t.Errorf("512 tags must pass: %v", err)
	}
}

func TestParseSafety(t *testing.T) {
	tags, err := ParseSafety("loli, shota, child, aged down")
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 4 || tags[3].Name != "aged_down" || tags[0].Source != Safety {
		t.Fatalf("safety tags = %+v", tags)
	}
	for _, tg := range tags {
		if len(tg.Runs) != 1 || tg.Runs[0].Weight != 1 || tg.Runs[0].Emitted != 1 {
			t.Errorf("safety tag %q not at exactly 1: %+v", tg.Text, tg.Runs)
		}
	}
	for _, bad := range []string{"{loli}, x", "x, [y]", "x, 1.2::y ::", "x, y::", "(x:1.2)", "x (y)", `x\y`, "", "  ,  ", "a | b", "embedding:x"} {
		if _, err := ParseSafety(bad); err == nil {
			t.Errorf("ParseSafety(%q) must refuse", bad)
		}
	}
}

// TestPerSurfaceReset: an unclosed scope is an error on its own surface
// and nothing carries into the next Parse.
func TestPerSurfaceReset(t *testing.T) {
	if _, err := Parse("1.5::a", Positive); err != nil {
		t.Fatal(err)
	}
	s, err := Parse("b", Positive)
	if err != nil || s.Tags[0].Runs[0].Weight != 1 {
		t.Errorf("weight leaked between surfaces: %+v %v", s, err)
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"{{{x}}}", "1.5::a ::, b", "(a:1.2)", "castorice (honkai: star rail)", "a}, b", "-1::x ::", "\\(x\\)"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		for _, m := range []Mode{Positive, Negative, ConfigPositive, ConfigNegative} {
			s, errs := Analyze(in, m)
			for _, e := range errs {
				if e.Start < 0 || e.End < e.Start || e.Code == "" {
					t.Fatalf("bad issue %+v for %q", e, in)
				}
			}
			for _, tg := range append(s.Tags, s.Relocated...) {
				for _, r := range tg.Runs {
					if math.IsNaN(r.Weight) || math.IsInf(r.Weight, 0) {
						t.Fatalf("non-finite weight in %q", in)
					}
				}
			}
		}
	})
}

// TestParseLinear: 1 MiB of `{` must not blow up (16 KiB bound first, and
// the scanner itself is linear).
func TestParseLinear(t *testing.T) {
	_, errs := Analyze(strings.Repeat("{", 1<<20), Positive)
	if len(errs) != 1 || errs[0].Code != CodeTooLong {
		t.Errorf("1 MiB: %+v", errs)
	}
	_, errs = Analyze(strings.Repeat("{", MaxBytes), Positive)
	if len(errs) == 0 {
		t.Error("16 KiB of `{` must be rejected (unclosed)")
	}
}
