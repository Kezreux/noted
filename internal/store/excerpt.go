package store

import (
	"strings"
	"unicode"
)

// excerptWidth is how many characters of context a search result shows. Wide
// enough to hold a sentence like "Reduced VDD_TRIM_OFFSET from 0x1A to 0x12",
// which is usually the whole answer the user came for.
const excerptWidth = 160

// Segment is a run of excerpt text that either matched the query or did not.
//
// Search results are returned as segments rather than as pre-built HTML for two
// reasons: this package has no business generating markup, and a note body is
// arbitrary user text, so handing the frontend a string containing <mark> tags
// would mean deciding what to escape here and hoping the UI agrees. With
// segments the UI escapes every Text it receives and wraps the matching ones,
// and note content can never become markup.
type Segment struct {
	Text  string `json:"text"`
	Match bool   `json:"match"`
}

// leadingExcerpt returns the start of a note body for the list view, where
// there is no query to highlight.
func leadingExcerpt(body string, width int) string {
	return truncateRunes(collapseSpace(body), width)
}

// makeExcerpt builds a highlighted excerpt of text, centred on the first place
// a query token appears. If nothing matches -- which happens for the field that
// did not cause the hit -- it falls back to the start of the text.
func makeExcerpt(text string, tokens []string, width int) []Segment {
	flat := []rune(collapseSpace(text))
	matches := findMatches(flat, tokens)

	if len(matches) == 0 {
		if len(flat) == 0 {
			return nil
		}
		return []Segment{{Text: truncateRunes(string(flat), width)}}
	}

	start, end, cutHead, cutTail := window(flat, matches[0], width)
	return segments(flat, matches, start, end, cutHead, cutTail)
}

// span is a half-open range of rune indices.
type span struct{ start, end int }

// window picks the slice of text to show: the first match, with the remaining
// budget spent on context before and after it.
func window(flat []rune, first span, width int) (start, end int, cutHead, cutTail bool) {
	total := len(flat)
	if total <= width {
		return 0, total, false, false
	}

	// A third of the budget ahead of the match, so the reader sees what leads
	// into it without pushing the match itself off the end.
	lead := width / 3
	start = first.start - lead
	if start < 0 {
		start = 0
	}
	end = start + width
	if end > total {
		end = total
		start = max(0, end-width)
	}

	// Avoid slicing a word in half at the leading edge: step forward to just
	// after the next space, but never past the match itself.
	if start > 0 {
		for i := start; i < first.start; i++ {
			if flat[i] == ' ' {
				start = i + 1
				break
			}
		}
	}
	// At the trailing edge, only ever extend, so a match is never cut short.
	if end < total && end < first.end {
		end = first.end
	}
	return start, end, start > 0, end < total
}

// segments turns the chosen window into alternating plain and matched runs.
func segments(flat []rune, matches []span, start, end int, cutHead, cutTail bool) []Segment {
	var out []Segment
	add := func(text string, isMatch bool) {
		if text == "" {
			return
		}
		// Merge with the previous segment when they are the same kind, so the
		// UI never receives two adjacent <mark> elements.
		if n := len(out); n > 0 && out[n-1].Match == isMatch {
			out[n-1].Text += text
			return
		}
		out = append(out, Segment{Text: text, Match: isMatch})
	}

	if cutHead {
		add("…", false)
	}

	cursor := start
	for _, m := range matches {
		if m.end <= start {
			continue
		}
		if m.start >= end {
			break
		}
		// Clip a match that straddles the window edge.
		ms, me := max(m.start, start), min(m.end, end)
		add(string(flat[cursor:ms]), false)
		add(string(flat[ms:me]), true)
		cursor = me
	}
	add(string(flat[cursor:end]), false)

	if cutTail {
		add("…", false)
	}
	return out
}

// findMatches locates every case-insensitive occurrence of every token, then
// merges overlaps so that a token inside another token's match does not produce
// nested highlights.
func findMatches(text []rune, tokens []string) []span {
	lowerText := toLowerRunes(text)

	var found []span
	for _, tok := range tokens {
		needle := toLowerRunes([]rune(tok))
		if len(needle) == 0 {
			continue
		}
		for i := 0; i+len(needle) <= len(lowerText); i++ {
			if runesEqual(lowerText[i:i+len(needle)], needle) {
				found = append(found, span{i, i + len(needle)})
			}
		}
	}
	return mergeSpans(found)
}

func mergeSpans(in []span) []span {
	if len(in) < 2 {
		return in
	}
	// Insertion sort by start: the slice is short (one entry per occurrence in a
	// single excerpt), so this is cheaper than the machinery of sort.Slice.
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j].start < in[j-1].start; j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
	out := in[:1]
	for _, s := range in[1:] {
		last := &out[len(out)-1]
		if s.start <= last.end {
			last.end = max(last.end, s.end)
			continue
		}
		out = append(out, s)
	}
	return out
}

// toLowerRunes lowercases rune by rune, which keeps the result the same length
// as the input. strings.ToLower does not guarantee that for every alphabet, and
// the highlight offsets depend on it.
func toLowerRunes(in []rune) []rune {
	out := make([]rune, len(in))
	for i, r := range in {
		out[i] = unicode.ToLower(r)
	}
	return out
}

func runesEqual(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncateRunes(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	return strings.TrimRight(string(r[:width]), " ") + "…"
}
