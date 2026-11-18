package prompt

import (
	"strings"
	"unicode"
	"unicode/utf16"
)

// Notice codes for Clean — what an import dropped.
const (
	NoticeCleanedNetwork   = "cleaned_extra_network" // `<lora:name:0.8>` removed
	NoticeCleanedEmbedding = "cleaned_embedding"     // `embedding:name` removed
	NoticeCleanedPipe      = "cleaned_pipe"          // `|` became a tag break
	NoticeCleanedRandom    = "cleaned_random"        // `||a|b||` removed
	NoticeCleanedControl   = "cleaned_control_char"  // control characters removed
)

// Clean rewrites text written for another tool so pulsar's grammar can read
// it (POST /prompt/parse with "clean": true — the import path). It only
// ever REMOVES: the constructs the lexer rejects outright are dropped, each
// with a notice located in the original text, and the result still goes
// through the normal parser — Clean relaxes nothing and bypasses nothing.
//
//   - `<lora:name:0.8>` and every `<word:…>` extra network: removed through
//     the closing `>` (or to the end of the tag when there is none).
//   - `embedding:name`: removed with its name.
//   - `||a|b||` (NovelAI's random choice): removed whole — pulsar cannot
//     know which option was drawn.
//   - a lone `|` (NovelAI character separator / V3 mixing): becomes a tag
//     break, so the text on either side stays two tags.
//   - control characters (other than tab, newline, carriage return).
//
// One pass, not a fixed point: text contrived so that a removal splices a
// new construct together (`<\x00lora:x>`) is left for the parser to reject.
//
// Pure. Notice offsets are UTF-16, into the ORIGINAL text.
func Clean(text string) (string, []Issue) {
	rs := []rune(text)
	// UTF-16 offset of each rune, for the notices
	off := make([]int, len(rs)+1)
	for i, r := range rs {
		off[i+1] = off[i] + utf16.RuneLen(r)
	}
	var b strings.Builder
	var notes []Issue
	piece := 0
	note := func(code, msg string, start, end int) {
		if len(notes) < 64 {
			notes = append(notes, Issue{Code: code, Message: msg, Start: off[start], End: off[end], Tag: piece})
		}
	}
	isCtl := func(r rune) bool {
		return r == unicode.ReplacementChar || (unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r')
	}
	snippet := func(start, end int) string {
		if end-start > 40 {
			return string(rs[start:start+37]) + "…"
		}
		return string(rs[start:end])
	}
	for i := 0; i < len(rs); {
		r := rs[i]
		switch {
		case isCtl(r):
			j := i + 1
			for j < len(rs) && isCtl(rs[j]) {
				j++
			}
			note(NoticeCleanedControl, "control characters were removed", i, j)
			i = j
		case r == '|' && i+1 < len(rs) && rs[i+1] == '|':
			// `||a|b||`: to the closing `||` on this line; an unclosed
			// opener is just two separators
			j := i + 2
			for j+1 < len(rs) && rs[j] != '\n' && !(rs[j] == '|' && rs[j+1] == '|') {
				j++
			}
			if j+1 < len(rs) && rs[j] == '|' && rs[j+1] == '|' {
				note(NoticeCleanedRandom, "random choice `"+snippet(i, j+2)+"` was removed", i, j+2)
				i = j + 2
			} else {
				note(NoticeCleanedPipe, "`|` was read as a tag break", i, i+2)
				b.WriteString(", ")
				i += 2
			}
		case r == '|':
			note(NoticeCleanedPipe, "`|` was read as a tag break", i, i+1)
			b.WriteString(", ")
			i++
		case r == '<' && networkAt(rs, i):
			j := i + 1
			for j < len(rs) && rs[j] != '>' && rs[j] != ',' && rs[j] != '\n' {
				j++
			}
			if j < len(rs) && rs[j] == '>' {
				j++
			}
			note(NoticeCleanedNetwork, "`"+snippet(i, j)+"` was removed — use the LoRA picker", i, j)
			i = j
		case (r == 'e' || r == 'E') && hasFoldPrefix(rs[i:], "embedding:"):
			j := i + len("embedding:")
			for j < len(rs) && rs[j] != ',' && rs[j] != '\n' && !unicode.IsSpace(rs[j]) &&
				!strings.ContainsRune("(){}[]<>|", rs[j]) {
				j++
			}
			note(NoticeCleanedEmbedding, "`"+snippet(i, j)+"` was removed — embeddings aren't supported", i, j)
			// `(embedding:name:1.1)`: the weight group held nothing else
			if cur := b.String(); strings.HasSuffix(cur, "(") && j < len(rs) && rs[j] == ')' {
				b.Reset()
				b.WriteString(cur[:len(cur)-1])
				j++
			}
			i = j
		default:
			if r == ',' || r == '\n' {
				piece++
			}
			b.WriteRune(r)
			i++
		}
	}
	return b.String(), notes
}

// networkAt reports whether rs[i:] opens an extra network the way the
// lexer sees one: `<` + ASCII letters + `:`.
func networkAt(rs []rune, i int) bool {
	j := i + 1
	for j < len(rs) && (rs[j] >= 'a' && rs[j] <= 'z' || rs[j] >= 'A' && rs[j] <= 'Z') {
		j++
	}
	return j > i+1 && j < len(rs) && rs[j] == ':'
}
