package prompt

import (
	"math"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

var plainWeight = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,4})?$`)

// groupWeights extracts the weight text of every unescaped `(…:w)` group.
func groupWeights(s string) []string {
	var out []string
	rs := []rune(s)
	open := -1
	for i := 0; i < len(rs); i++ {
		switch {
		case rs[i] == '\\' && i+1 < len(rs) && (rs[i+1] == '(' || rs[i+1] == ')'):
			i++
		case rs[i] == '(':
			open = i
		case rs[i] == ')' && open >= 0:
			inner := string(rs[open+1 : i])
			out = append(out, inner[strings.LastIndex(inner, ":")+1:])
			open = -1
		}
	}
	return out
}

var testCal = Calibration{KUp: 1.0, Hi: 1.4, KDown: 1.0, Lo: 0.3} // the illustrious starting values

func emitted(t *testing.T, in string, m Mode, cal *Calibration) (Surface, string) {
	t.Helper()
	s, err := Parse(in, m)
	if err != nil {
		t.Fatalf("%q: %v", in, err)
	}
	if cal != nil {
		cal.Apply(s.Tags)
	} else {
		Identity(s.Tags)
	}
	out, err := EmitComfy(s.Tags)
	if err != nil {
		t.Fatalf("%q: emit: %v", in, err)
	}
	return s, out
}

// TestEmitComfy is the S5 table: flat escaped groups, bare weight-1 text.
func TestEmitComfy(t *testing.T) {
	cases := []struct{ in, want string }{
		{"castorice (honkai: star rail)", `castorice \(honkai: star rail\)`},
		{"1.2::castorice (honkai: star rail) ::", `(castorice \(honkai: star rail\):1.2)`},
		{"0.8::artist:foo ::", `(artist:foo:0.8)`},
		{"blue 1.05::hair ::", `blue (hair:1.05)`},
		{"1.1:: :3 ::", `(:3:1.1)`},
		{"1.3393::year 2000 ::", `(year 2000:1.3393)`},
		{"1girl, {{x}}, [y]", `1girl, (x:1.1025), (y:0.9524)`},
		{"makima \\(chainsaw man\\), 1girl", `makima \(chainsaw man\), 1girl`},
		{"a\\ {b}", `a\ (b:1.05)`},
		{"1.00001::x ::", `x`},
	}
	for _, tc := range cases {
		_, out := emitted(t, tc.in, ConfigPositive, nil)
		if out != tc.want {
			t.Errorf("%q emitted %q, want %q", tc.in, out, tc.want)
		}
	}
	// calibrated user weights (illustrious): brackets transfer almost
	// unchanged, V4.5-style big weights saturate below Hi
	_, out := emitted(t, "{x}, 1.5::y ::, 10::z ::, 0.1::w ::", Positive, &testCal)
	if out != "(x:1.0497), (y:1.3393), (z:1.4), (w:0.3)" {
		t.Errorf("calibrated = %q", out)
	}
	if _, err := EmitComfy([]Tag{{Text: "x", Runs: []Run{{Text: "x", Emitted: math.NaN()}}}}); err == nil {
		t.Error("NaN must not emit")
	}
	if _, err := EmitComfy([]Tag{{Text: "x", Runs: []Run{{Text: "x", Emitted: -1}}}}); err == nil {
		t.Error("negative must not emit")
	}
}

func TestFormatWeight(t *testing.T) {
	for w, want := range map[float64]string{1: "1", 1.3: "1.3", 1.04974: "1.0497", 0.3: "0.3", 2: "2", 1e-5: "0", 1.99999: "2"} {
		if got := FormatWeight(w); got != want {
			t.Errorf("FormatWeight(%v) = %q, want %q", w, got, want)
		}
	}
}

// TestEmitReparses: what the emitter writes, ComfyUI's grammar reads back
// as exactly the structured tags.
func TestEmitReparses(t *testing.T) {
	for _, in := range []string{
		"castorice (honkai: star rail), 1girl",
		"(castorice (honkai: star rail):1.2), {x}, [y]",
		"0.9::sleeveless, [0.71::long legs], {1.8::holding hands}",
		"blue {hair}, a\\ {b}, artist:foo, 1.5::year 2000 ::",
		"x \\( y, \\)z(, (unclosed",
		"日本語 {タグ}, 😀 x",
	} {
		s, out := emitted(t, in, Positive, &testCal)
		segs, err := ComfyParse(out)
		if err != nil {
			t.Fatalf("%q: %v", out, err)
		}
		if err := Equivalent(s.Tags, ComfyTags(segs), 5e-5); err != nil {
			t.Errorf("%q → %q: %v", in, out, err)
		}
	}
}

// TestFormatNAIRoundTrip: Parse(FormatNAI(t)) ≡ t, with the space guard.
func TestFormatNAIRoundTrip(t *testing.T) {
	for _, in := range []string{
		"1girl, 1.5::rain, night ::, 0.5::coat ::, black shoes",
		"1.5::year 2000 ::, smile",
		"castorice (honkai: star rail), (castorice (honkai: star rail):1.2)",
		"blue {hair}, abc2 {x}, a- {b}, {{{x}}}, [y]",
		"\\(blue hair\\), x \\( y",
		"0.952381::x ::, 1.1:: :3 ::",
	} {
		s, err := Parse(in, Positive)
		if err != nil {
			t.Fatal(err)
		}
		nai := FormatNAI(s.Tags)
		back, err := Parse(nai, modeRaw)
		if err != nil {
			t.Errorf("%q → %q: %v", in, nai, err)
			continue
		}
		if err := EquivalentNAI(s.Tags, back.Tags, NAITolerance); err != nil {
			t.Errorf("%q → %q: %v", in, nai, err)
		}
	}
	tag := Tag{Text: "year 2000", Runs: []Run{{Text: "year 2000", Weight: 1.5}}}
	if got := FormatTagNAI(tag); got != "1.5::year 2000 ::" {
		t.Errorf("space guard: %q", got)
	}
	lit := Tag{Text: "(x:1.2)", Runs: []Run{{Text: "(x:1.2)", Weight: 1}}}
	if got := FormatTagNAI(lit); got != `\(x:1.2\)` {
		t.Errorf("literal SD-looking text must escape: %q", got)
	}
	neg := Tag{Text: "hat", Runs: []Run{{Text: "hat", Weight: -1}}}
	if got := FormatTagNAI(neg); got != "-1::hat ::" {
		t.Errorf("negative: %q", got)
	}
}

// TestCalibrationInvariants: Map(1) = 1, bounded in [Lo, Hi], monotone,
// for both shipped lineages and for clamped garbage.
func TestCalibrationInvariants(t *testing.T) {
	for _, c := range []Calibration{testCal, {1, 1.3, 1, 0.3}, {9, 9, 9, -3}, {0, 1, 0, 1}, {math.NaN(), 1.2, 1, 0.5}} {
		cc := c.Clamped()
		if c.Map(1) != 1 {
			t.Errorf("%+v: Map(1) = %v", c, c.Map(1))
		}
		prev := c.Map(0)
		for w := 0.0; w <= 12; w += 0.01 {
			m := c.Map(w)
			if m < cc.Lo-1e-12 || m > cc.Hi+1e-12 || m <= 0 || m > 1.6 || math.IsNaN(m) {
				t.Fatalf("%+v: Map(%v) = %v outside [%v, %v]", c, w, m, cc.Lo, cc.Hi)
			}
			if m < prev-1e-12 {
				t.Fatalf("%+v: not monotone at %v", c, w)
			}
			prev = m
		}
	}
	// the spec's table (S4)
	for w, want := range map[float64]float64{1.05: 1.0497, 1.1025: 1.1003, 1.157625: 1.1499, 1.5: 1.3393, 3: 1.4, 0.71: 0.71, 0.1: 0.3} {
		if got := round4(testCal.Map(w)); got != want {
			t.Errorf("illustrious Map(%v) = %v, want %v", w, got, want)
		}
	}
	s, _ := Parse("10::x ::, {y}", Positive)
	notes := testCal.Apply(s.Tags)
	if len(notes) != 1 || notes[0].Code != NoticeCalibrated || notes[0].Tag != 0 {
		t.Errorf("N5 notices = %+v", notes)
	}
}

// FuzzEmitRoundTrip: anything Parse accepts emits a string that ComfyUI's
// grammar reads back as the structured tags, with plain finite weights —
// never NaN/Inf/exponent — and FormatNAI round-trips.
func FuzzEmitRoundTrip(f *testing.F) {
	for _, s := range []string{
		"castorice (honkai: star rail)", "(a:1.2), {b}", "a\\ {b}", "\\(x\\)", "1.5::year 2000 ::",
		"x:1.5, (y:0.5)", "blue {hair}", "-1::hat ::, x", "((a)", "a) (b:1.2)", "日本 {語}",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		if !utf8.ValidString(in) {
			return
		}
		s, errs := Analyze(in, Positive)
		if len(errs) > 0 {
			return
		}
		for _, surface := range [][]Tag{s.Tags, s.Relocated} {
			testCal.Apply(surface)
			out, err := EmitComfy(surface)
			if err != nil {
				t.Fatalf("%q: emit: %v", in, err)
			}
			segs, err := ComfyParse(out)
			if err != nil {
				t.Fatalf("%q → %q: %v", in, out, err)
			}
			for _, sg := range segs {
				if math.IsNaN(sg.Weight) || math.IsInf(sg.Weight, 0) || sg.Weight <= 0 || sg.Weight > 1.6 {
					t.Fatalf("%q → %q: weight %v", in, out, sg.Weight)
				}
			}
			for _, ws := range groupWeights(out) {
				if !plainWeight.MatchString(ws) {
					t.Fatalf("%q → %q: weight %q is not a plain decimal", in, out, ws)
				}
			}
			if err := Equivalent(surface, ComfyTags(segs), 5e-5); err != nil {
				t.Fatalf("%q → %q: %v", in, out, err)
			}
			back, err := Parse(FormatNAI(surface), modeRaw)
			if err != nil {
				t.Fatalf("%q → NAI %q: %v", in, FormatNAI(surface), err)
			}
			if err := EquivalentNAI(surface, back.Tags, NAITolerance); err != nil {
				t.Fatalf("%q → NAI %q: %v", in, FormatNAI(surface), err)
			}
		}
	})
}
