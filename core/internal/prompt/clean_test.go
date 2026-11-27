package prompt

import (
	"strings"
	"testing"
	"unicode/utf16"
)

func TestClean(t *testing.T) {
	cases := []struct {
		name, in, want string
		codes          []string
	}{
		{"untouched", "1girl, {red dress}, 1.3::smile ::, (blue hair:1.2)", "1girl, {red dress}, 1.3::smile ::, (blue hair:1.2)", nil},
		{"lora", "1girl, <lora:Acheron_v2:0.8>, smile", "1girl, , smile", []string{NoticeCleanedNetwork}},
		{"lora glued", "1girl <lora:x:1> smile", "1girl  smile", []string{NoticeCleanedNetwork}},
		{"hypernet + lyco", "<hypernet:a:1>, <lyco:b:0.5>cat", ", cat", []string{NoticeCleanedNetwork, NoticeCleanedNetwork}},
		{"unclosed network stops at the tag", "<lora:broken:0.8, cat", ", cat", []string{NoticeCleanedNetwork}},
		{"not a network", "a < b, <3, <:x>", "a < b, <3, <:x>", nil},
		{"embedding", "embedding:EasyNegative, lowres", ", lowres", []string{NoticeCleanedEmbedding}},
		{"embedding case + parens", "(Embedding:bad-hands-5:1.1), x", ", x", []string{NoticeCleanedEmbedding}},
		{"pipe", "red hair | blue eyes", "red hair ,  blue eyes", []string{NoticeCleanedPipe}},
		{"random", "1girl, ||red|blue|green|| hair, smile", "1girl,  hair, smile", []string{NoticeCleanedRandom}},
		{"unclosed random", "a || b", "a ,  b", []string{NoticeCleanedPipe}},
		{"random stays on its line", "||a\nb||c", ", a\nb, c", []string{NoticeCleanedPipe, NoticeCleanedPipe}},
		{"control", "a\x00\x01b,\tc\r\nd\x7f", "ab,\tc\r\nd", []string{NoticeCleanedControl, NoticeCleanedControl}},
		{"empty", "", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, notes := Clean(c.in)
			if got != c.want {
				t.Errorf("Clean(%q) = %q, want %q", c.in, got, c.want)
			}
			if len(notes) != len(c.codes) {
				t.Fatalf("notices = %+v, want codes %v", notes, c.codes)
			}
			for i, n := range notes {
				if n.Code != c.codes[i] {
					t.Errorf("notice %d = %s, want %s", i, n.Code, c.codes[i])
				}
			}
		})
	}
}

// Notices point into the ORIGINAL text, in UTF-16 units.
func TestCleanNoticeOffsets(t *testing.T) {
	in := "猫😀, <lora:x:1>, a|b"
	_, notes := Clean(in)
	if len(notes) != 2 {
		t.Fatalf("notices = %+v", notes)
	}
	u := utf16.Encode([]rune(in))
	slice := func(n Issue) string { return string(utf16.Decode(u[n.Start:n.End])) }
	if got := slice(notes[0]); got != "<lora:x:1>" || notes[0].Tag != 1 {
		t.Errorf("network notice covers %q (tag %d)", got, notes[0].Tag)
	}
	if got := slice(notes[1]); got != "|" || notes[1].Tag != 2 {
		t.Errorf("pipe notice covers %q (tag %d)", got, notes[1].Tag)
	}
}

// Clean only removes what the lexer rejects outright: after it, none of
// those errors remain — and nothing else is forgiven.
func TestCleanedTextParsesOrFailsForOtherReasons(t *testing.T) {
	in := "masterpiece, <lora:a:1>, embedding:neg, x | y, ||p|q||, \x02z, (blue hair:1.2)"
	if _, errs := Analyze(in, Positive); len(errs) == 0 {
		t.Fatal("fixture should not parse uncleaned")
	}
	cleaned, _ := Clean(in)
	surf, errs := Analyze(cleaned, Positive)
	if len(errs) != 0 {
		t.Fatalf("cleaned text still has errors: %+v", errs)
	}
	var names []string
	for _, tg := range surf.Tags {
		names = append(names, tg.Name)
	}
	if got := strings.Join(names, "|"); got != "masterpiece|x|y|z|blue_hair" {
		t.Errorf("tags = %s", got)
	}

	// grammar errors Clean does not own stay errors
	for _, bad := range []string{"{unclosed, <lora:a:1>", "a:::b | c", "9999::x ::"} {
		cleaned, _ := Clean(bad)
		if _, errs := Analyze(cleaned, Positive); len(errs) == 0 {
			t.Errorf("Clean made %q parse", bad)
		}
	}
}

func FuzzClean(f *testing.F) {
	for _, s := range []string{"", "a|b", "||a|b||", "<lora:x:1>", "embedding:x", "<", "||", "e", "\x00"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		out, notes := Clean(s)
		if len(out) > len(s)+2*len(s) {
			t.Fatal("output grew unreasonably")
		}
		u := len(utf16.Encode([]rune(s)))
		for _, n := range notes {
			if n.Start < 0 || n.End > u || n.Start >= n.End {
				t.Fatalf("notice span [%d,%d) outside [0,%d)", n.Start, n.End, u)
			}
		}
		for _, r := range out {
			if r == '|' {
				t.Fatal("a pipe survived")
			}
		}
		// only ever removes: whatever survives parses or fails like any text
		Analyze(out, Positive)
	})
}
