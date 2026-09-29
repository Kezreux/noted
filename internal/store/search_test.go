package store

import (
	"context"
	"strings"
	"testing"
)

// seedRealistic inserts notes that look like real bench entries. Search is only
// worth having if it handles parameter names, register values and filenames, so
// those are the fixture rather than "note 1" / "note 2".
func seedRealistic(t *testing.T, s *Store) map[string]int64 {
	t.Helper()
	notes := map[string]NoteInput{
		"param": {
			Title: "VDD trim for the 1.8 V rail",
			Body:  "Reduced VDD_TRIM_OFFSET from 0x1A to 0x12 to bring the rail to 1.802 V.",
			Tags:  []string{"power"},
		},
		"ref": {
			Title: "Divider leg fault trace",
			Body:  "Replaced R4 (the 4k7 divider leg); U1 was fine after all.",
		},
		"file": {
			Title: "Bench calibration profile",
			Body:  "Loaded cal_v2.cfg on the bench unit; the older cal-v1.cfg overshoots.",
		},
		"log": {
			Title: "PLL lock failure at 24.576 MHz",
			Body:  `PLL failed to lock: "ERR_PLL_UNLOCK" at 24.576 MHz, see adc-sweep.log line 881.`,
		},
		"decoy": {
			Title: "ADC swept on board rev A1",
			Body:  "Routine sweep, gain 12, offset -3 counts. Nothing remarkable.",
		},
	}
	ids := make(map[string]int64, len(notes))
	for key, in := range notes {
		ids[key] = mustCreate(t, s, in)
	}
	return ids
}

// TestSearchQueryShapes covers each kind of thing someone actually types at a
// bench. The two strategies behind Search have different blind spots, so every
// case names which one it exercises.
func TestSearchQueryShapes(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	ids := seedRealistic(t, s)

	cases := []struct {
		query string
		want  string // key into ids
		why   string
	}{
		{"VDD_TRIM_OFFSET", "param", "exact parameter name"},
		{"trim", "param", "substring of a parameter name -- the main case for search"},
		{"TRIM_OFF", "param", "substring spanning an underscore"},
		{"trim_off", "param", "the same, lowercased: trigram search is case-insensitive"},
		{"0x1A", "param", "hex register value"},
		{"0X1a", "param", "hex value with the case flipped"},
		{"R4", "ref", "2-char designator -- below the trigram floor, LIKE fallback"},
		{"U1", "ref", "2-char designator -- below the trigram floor, LIKE fallback"},
		{"4k7", "ref", "3-char value with a letter infix"},
		{"cal_v2.cfg", "file", "filename with a dot -- would be an FTS5 syntax error unquoted"},
		{"cal-v1.cfg", "file", "filename with a dash -- ditto"},
		{"adc-sweep.log", "log", "log filename"},
		{"ERR_PLL_UNLOCK", "log", "error identifier"},
		{"24.576", "log", "decimal frequency"},
	}

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			hits, err := s.Search(ctx, tc.query, 20)
			if err != nil {
				t.Fatalf("Search(%q): %v  [%s]", tc.query, err, tc.why)
			}
			if !hasID(hits, ids[tc.want]) {
				t.Errorf("Search(%q) missed the %s note (%s); got %d hits: %v",
					tc.query, tc.want, tc.why, len(hits), titles(hits))
			}
		})
	}
}

// Mixed queries are the reason the planner exists: one token above the trigram
// floor, one below it, combined so that adding a word narrows the result.
func TestSearchCombinesTokensWithAND(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	ids := seedRealistic(t, s)

	cases := []struct {
		query string
		want  int
		why   string
	}{
		{"VDD_TRIM_OFFSET", 1, "single long token"},
		{"trim rail", 1, "two long tokens, both in the VDD note"},
		{"divider R4", 1, "long + short: trigram AND like"},
		{"R4 U1", 1, "two short tokens, pure LIKE path"},
		{"trim R4", 0, "long + short sharing no note -- must narrow to nothing"},
		{"cal_v2.cfg bench", 1, "dotted filename + word"},
		{"trim nonexistentword", 0, "one token matching nothing removes every hit"},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			hits, err := s.Search(ctx, tc.query, 20)
			if err != nil {
				t.Fatalf("Search(%q): %v", tc.query, err)
			}
			if len(hits) != tc.want {
				t.Errorf("Search(%q) = %d hits, want %d (%s); got %v",
					tc.query, len(hits), tc.want, tc.why, titles(hits))
			}
		})
	}
	_ = ids
}

// A query the user is still typing must never produce an error dialog.
func TestSearchToleratesAwkwardInput(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	seedRealistic(t, s)

	// Every one of these is a real intermediate state of typing, or a paste of
	// something that happens to collide with FTS5 or LIKE syntax.
	awkward := []string{
		"", " ", "\t\n",
		`"`, `""`, `"unclosed`,
		"*", "**", "^", "-", "--", ":", "(", ")", "()",
		"AND", "OR", "NOT", "NEAR",
		"%", "_", `\`, `100%`, `a_b`,
		"trim AND", "OR trim", "trim -",
		strings.Repeat("x", 500),
	}
	for _, q := range awkward {
		if _, err := s.Search(ctx, q, 10); err != nil {
			t.Errorf("Search(%q) returned an error: %v", q, err)
		}
	}
}

// LIKE wildcards in the query must be literal characters, or a search for "_"
// would match every note.
func TestSearchEscapesLikeWildcards(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	seedRealistic(t, s)
	plain := mustCreate(t, s, NoteInput{Title: "no wildcards here", Body: "plain ascii text"})

	// "_" is a single-character wildcard in LIKE. Unescaped it matches anything.
	hits, err := s.Search(ctx, "_", 50)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if hasID(hits, plain) {
		t.Error(`Search("_") matched a note with no underscore: LIKE wildcards are not escaped`)
	}

	// The same for "%".
	if hits, err = s.Search(ctx, "%", 50); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if hasID(hits, plain) {
		t.Error(`Search("%") matched a note with no percent sign: LIKE wildcards are not escaped`)
	}
}

// FTS5 is an external-content index, so it is only correct as long as the
// triggers keep it level with the notes table.
func TestSearchIndexTracksEdits(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	id := mustCreate(t, s, NoteInput{Title: "before", Body: "ORIGINAL_PARAM was 5"})

	if hits, _ := s.Search(ctx, "ORIGINAL_PARAM", 10); !hasID(hits, id) {
		t.Fatal("insert trigger did not index the new note")
	}

	if err := s.Update(ctx, id, NoteInput{Title: "after", Body: "REPLACEMENT_PARAM is 9"}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// The new text is findable...
	if hits, _ := s.Search(ctx, "REPLACEMENT_PARAM", 10); !hasID(hits, id) {
		t.Error("update trigger did not index the new text")
	}
	// ...and, just as importantly, the old text is not. A stale index entry would
	// return a note that no longer contains what was searched for.
	if hits, _ := s.Search(ctx, "ORIGINAL_PARAM", 10); len(hits) != 0 {
		t.Errorf("update trigger left %d stale index entries for the replaced text", len(hits))
	}
}

func TestSearchFindsMetadataFields(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	id := mustCreate(t, s, NoteInput{
		Title:      "unremarkable title",
		Body:       "unremarkable body",
		Instrument: "Keysight 34465A",
		DUT:        "widget rev C3",
	})

	for _, q := range []string{"Keysight", "34465A", "widget"} {
		hits, err := s.Search(ctx, q, 10)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		if !hasID(hits, id) {
			t.Errorf("Search(%q) did not match the instrument/DUT fields", q)
		}
	}
}

func TestSearchHighlightsMatches(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	seedRealistic(t, s)

	hits, err := s.Search(ctx, "trim", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits")
	}

	// The excerpt must contain the match, highlighted -- an excerpt without the
	// match in it defeats the whole point of showing one.
	var marked []string
	for _, seg := range hits[0].Excerpt {
		if seg.Match {
			marked = append(marked, seg.Text)
		}
	}
	if len(marked) == 0 {
		t.Fatalf("no highlighted segment in excerpt %v", hits[0].Excerpt)
	}
	for _, m := range marked {
		if !strings.EqualFold(m, "trim") {
			t.Errorf("highlighted %q, want the query token %q", m, "trim")
		}
	}
}

func TestSearchRespectsLimit(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	for range 10 {
		mustCreate(t, s, NoteInput{Title: "repeated", Body: "COMMON_TOKEN here"})
	}
	hits, err := s.Search(ctx, "COMMON_TOKEN", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 3 {
		t.Errorf("got %d hits, want the limit of 3", len(hits))
	}
}

func TestSearchReturnsTags(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	seedRealistic(t, s)

	hits, err := s.Search(ctx, "VDD_TRIM_OFFSET", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}
	if len(hits[0].Tags) != 1 || hits[0].Tags[0] != "power" {
		t.Errorf("tags = %v, want [power]", hits[0].Tags)
	}
}

// ---------------------------------------------------------------------------
// unit tests for the query-building helpers, which need no database
// ---------------------------------------------------------------------------

func TestSplitTokens(t *testing.T) {
	cases := []struct {
		in          string
		long, short []string
	}{
		{"trim", []string{"trim"}, nil},
		{"R4", nil, []string{"R4"}},
		{"divider R4", []string{"divider"}, []string{"R4"}},
		{"  spaced   out  ", []string{"spaced", "out"}, nil},
		{"", nil, nil},
		{"ab abc", []string{"abc"}, []string{"ab"}},
		// Counted in runes, not bytes: "µΩ" is 2 characters but 4 bytes, so it
		// belongs on the short path.
		{"µΩ", nil, []string{"µΩ"}},
		{"µΩs", []string{"µΩs"}, nil},
	}
	for _, tc := range cases {
		long, short := splitTokens(tc.in)
		if !equalStrings(long, tc.long) || !equalStrings(short, tc.short) {
			t.Errorf("splitTokens(%q) = %v / %v, want %v / %v", tc.in, long, short, tc.long, tc.short)
		}
	}
}

func TestFTSPhraseQuoting(t *testing.T) {
	cases := map[string]string{
		"trim":       `"trim"`,
		"cal_v2.cfg": `"cal_v2.cfg"`,
		"cal-v1.cfg": `"cal-v1.cfg"`,
		`say"quote`:  `"say""quote"`,
		"NEAR":       `"NEAR"`, // an FTS5 operator, neutralised by quoting
		"a:b":        `"a:b"`,
		"5 mV":       `"5 mV"`,
	}
	for in, want := range cases {
		if got := ftsPhrase(in); got != want {
			t.Errorf("ftsPhrase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLikePatternEscaping(t *testing.T) {
	cases := map[string]string{
		"R4":     `%R4%`,
		"a_b":    `%a\_b%`,
		"50%":    `%50\%%`,
		`c:\tmp`: `%c:\\tmp%`,
	}
	for in, want := range cases {
		if got := likePattern(in); got != want {
			t.Errorf("likePattern(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------

func hasID(hits []Hit, want int64) bool {
	for _, h := range hits {
		if h.ID == want {
			return true
		}
	}
	return false
}

func titles(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Title
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
