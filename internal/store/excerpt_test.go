package store

import (
	"strings"
	"testing"
)

// plainText re-joins an excerpt into the string a reader would see, so tests can
// assert on content without caring where the segment boundaries fall.
func plainText(segs []Segment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
	}
	return b.String()
}

// markedText returns only the highlighted runs.
func markedText(segs []Segment) []string {
	var out []string
	for _, s := range segs {
		if s.Match {
			out = append(out, s.Text)
		}
	}
	return out
}

func TestMakeExcerptHighlightsEveryOccurrence(t *testing.T) {
	segs := makeExcerpt("trim the TRIM and Trim again", []string{"trim"}, 200)

	marks := markedText(segs)
	if len(marks) != 3 {
		t.Errorf("highlighted %d occurrences (%v), want 3 -- matching is case-insensitive", len(marks), marks)
	}
	// Highlighting must not alter the text, only annotate it.
	if got := plainText(segs); got != "trim the TRIM and Trim again" {
		t.Errorf("excerpt text = %q, want the input unchanged", got)
	}
}

func TestMakeExcerptHighlightsMultipleTokens(t *testing.T) {
	segs := makeExcerpt("Replaced R4 in the divider leg", []string{"divider", "R4"}, 200)
	marks := markedText(segs)
	if len(marks) != 2 {
		t.Fatalf("highlighted %v, want both tokens", marks)
	}
	// Segment order follows the text, not the order the tokens were given.
	if marks[0] != "R4" || marks[1] != "divider" {
		t.Errorf("highlighted %v, want [R4 divider] in text order", marks)
	}
}

// Overlapping matches must not produce nested or duplicated highlights.
func TestMakeExcerptMergesOverlappingMatches(t *testing.T) {
	segs := makeExcerpt("VDD_TRIM_OFFSET", []string{"trim", "trim_off", "rim"}, 200)
	marks := markedText(segs)
	if len(marks) != 1 {
		t.Fatalf("highlighted %v, want one merged run", marks)
	}
	if !strings.EqualFold(marks[0], "TRIM_OFF") {
		t.Errorf("merged highlight = %q, want the union of the overlapping matches", marks[0])
	}
	if got := plainText(segs); got != "VDD_TRIM_OFFSET" {
		t.Errorf("text = %q, want it unchanged by merging", got)
	}
}

// A long body must be trimmed to a window around the match, not returned whole,
// and the match has to be inside the window.
func TestMakeExcerptWindowsAroundTheMatch(t *testing.T) {
	body := strings.Repeat("padding words here. ", 60) + "the NEEDLE appears late. " +
		strings.Repeat("more padding. ", 60)

	segs := makeExcerpt(body, []string{"NEEDLE"}, 80)
	text := plainText(segs)

	if len([]rune(text)) > 90 { // the window plus the two ellipses
		t.Errorf("excerpt is %d runes, want roughly the 80-rune budget: %q", len([]rune(text)), text)
	}
	if !strings.Contains(text, "NEEDLE") {
		t.Errorf("excerpt does not contain the match: %q", text)
	}
	if marks := markedText(segs); len(marks) != 1 || marks[0] != "NEEDLE" {
		t.Errorf("highlighted %v, want [NEEDLE]", marks)
	}
	// Elided text is signalled rather than silently dropped.
	if !strings.HasPrefix(text, "…") {
		t.Errorf("excerpt %q should start with an ellipsis, since text was cut", text)
	}
}

// A match near the start needs no leading ellipsis.
func TestMakeExcerptNoLeadingEllipsisWhenMatchIsEarly(t *testing.T) {
	body := "NEEDLE at the very beginning. " + strings.Repeat("padding. ", 60)
	text := plainText(makeExcerpt(body, []string{"NEEDLE"}, 80))
	if strings.HasPrefix(text, "…") {
		t.Errorf("excerpt %q has a leading ellipsis but nothing was cut from the front", text)
	}
}

// Search matches on the title, so the body has no match to highlight. It must
// still produce a readable excerpt rather than nothing.
func TestMakeExcerptFallsBackWhenNothingMatches(t *testing.T) {
	segs := makeExcerpt("a body with no query token in it", []string{"absent"}, 200)
	if len(segs) != 1 || segs[0].Match {
		t.Fatalf("got %v, want a single unhighlighted segment", segs)
	}
	if segs[0].Text != "a body with no query token in it" {
		t.Errorf("fallback text = %q", segs[0].Text)
	}
}

func TestMakeExcerptEmptyBody(t *testing.T) {
	if segs := makeExcerpt("", []string{"trim"}, 80); len(segs) != 0 {
		t.Errorf("makeExcerpt(\"\") = %v, want no segments", segs)
	}
}

func TestMakeExcerptCollapsesWhitespace(t *testing.T) {
	segs := makeExcerpt("line one\n\n\tline   two", []string{"two"}, 200)
	if got := plainText(segs); got != "line one line two" {
		t.Errorf("text = %q, want newlines and runs of spaces collapsed", got)
	}
}

// Multi-byte text must not be sliced mid-character, which is why the windowing
// works in runes rather than bytes.
func TestMakeExcerptHandlesMultiByteText(t *testing.T) {
	body := strings.Repeat("μεζούρα ", 40) + "NEEDLE " + strings.Repeat("μεζούρα ", 40)
	text := plainText(makeExcerpt(body, []string{"NEEDLE"}, 60))

	if !strings.Contains(text, "NEEDLE") {
		t.Errorf("excerpt lost the match: %q", text)
	}
	if !strings.ContainsRune(text, 'μ') {
		t.Errorf("excerpt %q should still contain Greek context", text)
	}
	if strings.ContainsRune(text, '�') {
		t.Errorf("excerpt %q contains a replacement character: text was cut mid-rune", text)
	}
}

func TestLeadingExcerptTruncatesWithEllipsis(t *testing.T) {
	long := strings.Repeat("word ", 100)
	got := leadingExcerpt(long, 20)

	if len([]rune(got)) > 21 { // 20 plus the ellipsis
		t.Errorf("leadingExcerpt returned %d runes, want at most 21: %q", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("leadingExcerpt(%q...) = %q, want a trailing ellipsis", long[:20], got)
	}
}

func TestLeadingExcerptLeavesShortBodyAlone(t *testing.T) {
	if got := leadingExcerpt("short body", 80); got != "short body" {
		t.Errorf("leadingExcerpt = %q, want the body unchanged with no ellipsis", got)
	}
}
