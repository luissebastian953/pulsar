package nltag

// ratio: indel similarity in [0, 1] (rapidfuzz `ratio` formulation — the
// docs' threshold is calibrated against it, not plain Levenshtein).
func ratio(a, b string) float64 {
	if a == b {
		return 1
	}
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 || len(rb) == 0 {
		return 0
	}
	return 1 - float64(indelDistance(ra, rb))/float64(len(ra)+len(rb))
}

// indelDistance is edit distance where substitution costs 2 (a delete plus
// an insert), single-row DP.
func indelDistance(a, b []rune) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i := 1; i <= len(a); i++ {
		prevDiag := row[0]
		row[0] = i
		for j := 1; j <= len(b); j++ {
			cur := row[j]
			m := prevDiag
			if a[i-1] != b[j-1] {
				m += 2
			}
			if row[j]+1 < m {
				m = row[j] + 1
			}
			if row[j-1]+1 < m {
				m = row[j-1] + 1
			}
			row[j] = m
			prevDiag = cur
		}
	}
	return row[len(b)]
}
