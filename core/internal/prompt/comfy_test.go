package prompt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strconv"
	"testing"
)

type comfyFixture struct {
	ComfyUI struct {
		Tag    string `json:"tag"`
		Commit string `json:"commit"`
		SHA256 string `json:"sd1_clip_sha256"`
	} `json:"comfyui"`
	Cases []struct {
		Input    string `json:"input"`
		Segments []struct {
			Text string `json:"text"`
			W    string `json:"w"`
		} `json:"segments"`
	} `json:"cases"`
}

func loadFixture(t *testing.T) comfyFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/comfy_parse.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx comfyFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	return fx
}

// TestComfyParseParity: the Go port reproduces ComfyUI v0.28.0's own
// functions on every fixture case (generated from the pinned Python by
// eval/comfy_parse_fixtures.py), bit-for-bit on weights.
func TestComfyParseParity(t *testing.T) {
	fx := loadFixture(t)
	if fx.ComfyUI.Tag != "v0.28.0" || len(fx.Cases) < 40 {
		t.Fatalf("fixture header %+v, %d cases", fx.ComfyUI, len(fx.Cases))
	}
	for _, c := range fx.Cases {
		got, err := ComfyParse(c.Input)
		if err != nil {
			t.Errorf("%q: %v", c.Input, err)
			continue
		}
		if len(got) != len(c.Segments) {
			t.Errorf("%q: %d segments %+v, want %d %+v", c.Input, len(got), got, len(c.Segments), c.Segments)
			continue
		}
		for i, s := range c.Segments {
			want, err := strconv.ParseFloat(s.W, 64)
			if err != nil {
				t.Fatalf("fixture weight %q: %v", s.W, err)
			}
			g := got[i]
			same := g.Weight == want || (math.IsNaN(g.Weight) && math.IsNaN(want))
			if g.Text != s.Text || !same {
				t.Errorf("%q segment %d = (%q, %v), want (%q, %v)", c.Input, i, g.Text, g.Weight, s.Text, want)
			}
		}
	}
}

// TestComfyFixtureMatchesCheckout: when the native ComfyUI checkout is
// present, the fixture must have been generated from exactly it — a moved
// checkout without regenerated fixtures fails here.
func TestComfyFixtureMatchesCheckout(t *testing.T) {
	raw, err := os.ReadFile("../../../comfy/ComfyUI/comfy/sd1_clip.py")
	if err != nil {
		t.Skip("comfy/ComfyUI checkout not present")
	}
	sum := sha256.Sum256(raw)
	if got, want := hex.EncodeToString(sum[:]), loadFixture(t).ComfyUI.SHA256; got != want {
		t.Fatalf("comfy/ComfyUI/comfy/sd1_clip.py moved (sha256 %s, fixture %s): run python3 -I eval/comfy_parse_fixtures.py and review the diff", got, want)
	}
}

func TestComfyParseAmbiguous(t *testing.T) {
	// Python's float() accepts non-ASCII digits; the port refuses to guess
	for _, in := range []string{"(x:١.٥)", "(x:１)", "(x:\u20031.2)"} {
		if _, err := ComfyParse(in); !errors.Is(err, ErrComfyAmbiguous) {
			t.Errorf("%q: err = %v, want ErrComfyAmbiguous", in, err)
		}
	}
	deep := ""
	for range 1000 {
		deep += "("
	}
	deep += "x"
	for range 1000 {
		deep += ")"
	}
	if _, err := ComfyParse(deep); !errors.Is(err, ErrComfyAmbiguous) {
		t.Errorf("recursion past Python's limit must fail closed, got %v", err)
	}
}

func TestPyFloat(t *testing.T) {
	for in, want := range map[string]float64{
		"1": 1, " 1.3 ": 1.3, "+1.5": 1.5, "-1": -1, ".5": 0.5, "5.": 5, "1e1": 10, "1E-3": 0.001,
		"1_0": 10, "1_000.5": 1000.5, "\t2\n": 2,
	} {
		if v, ok, amb := pyFloat(in); !ok || amb || v != want {
			t.Errorf("pyFloat(%q) = %v %v %v, want %v", in, v, ok, amb, want)
		}
	}
	for _, in := range []string{"", " ", ".", "1__0", "_1", "1_", "1.5e", "e5", "1.2.3", "1,5", "--1", "0x10", " star rail", "1.2\x00"} {
		if _, ok, _ := pyFloat(in); ok {
			t.Errorf("pyFloat(%q) accepted", in)
		}
	}
	if v, ok, _ := pyFloat("nan"); !ok || !math.IsNaN(v) {
		t.Error("nan")
	}
	if v, ok, _ := pyFloat("-Infinity"); !ok || !math.IsInf(v, -1) {
		t.Error("-Infinity")
	}
}
