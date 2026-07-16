package nltag

import "testing"

func TestRatio(t *testing.T) {
	cases := []struct {
		a, b     string
		min, max float64
	}{
		{"blonde_hair", "blonde_hair", 1, 1},
		{"blode_hair", "blonde_hair", 0.88, 0.99}, // typo: accepted
		{"siting", "sitting", 0.88, 0.99},         // typo in a short tag: accepted
		{"silver_hair", "grey_hair", 0, 0.75},     // different concept: embedding territory
		{"", "x", 0, 0},
		{"x", "", 0, 0},
	}
	for _, tc := range cases {
		got := ratio(tc.a, tc.b)
		if got < tc.min || got > tc.max {
			t.Errorf("ratio(%q, %q) = %.3f, want [%.2f, %.2f]", tc.a, tc.b, got, tc.min, tc.max)
		}
	}
}

func TestIndelDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"sitting", "sitting", 0},
		{"siting", "sitting", 1}, // one insertion
		{"ab", "ba", 2},          // substitutionless: delete + insert
		{"abc", "", 3},
		{"kitten", "sitting", 5}, // 2 subs (2 each = 4) + 1 insert
	}
	for _, tc := range cases {
		if got := indelDistance([]rune(tc.a), []rune(tc.b)); got != tc.want {
			t.Errorf("indelDistance(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
