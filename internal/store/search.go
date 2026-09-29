package store

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// trigramFloor is the shortest token the trigram tokenizer can index: it works
// by splitting text into overlapping three-character windows, so a one- or
// two-character token produces no window and can never be found through MATCH.
// Those tokens go to the LIKE fallback instead. Component designators like R4
// and U1 land here, so the fallback is not an edge case.
const trigramFloor = 3

// Hit is one search result.
//
// Title and Excerpt are carried as segments rather than HTML; see the Segment
// doc comment for why this package never emits markup.
type Hit struct {
	ID            int64     `json:"id"`
	Title         string    `json:"title"`
	TitleSegments []Segment `json:"titleSegments"`
	Excerpt       []Segment `json:"excerpt"`
	Tags          []string  `json:"tags"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// Search finds live notes matching query.
//
// Tokens of three or more characters go to the trigram index; shorter ones go to
// a LIKE scan, which is unindexed but costs only a few milliseconds at the size a
// hand-written logbook reaches. The two are combined with AND, so adding a word
// always narrows the result rather than widening it.
//
// A query with no usable tokens is not an error; it returns no hits, and the UI
// shows the unfiltered list instead.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	if limit <= 0 {
		limit = 50
	}

	long, short := splitTokens(query)
	if len(long) == 0 && len(short) == 0 {
		return nil, nil
	}

	where := []string{`n.deleted_at IS NULL`}
	var args []any

	if len(long) > 0 {
		// One MATCH per statement. FTS5 AND-joins the quoted phrases itself, so
		// all long tokens travel as a single argument.
		phrases := make([]string, len(long))
		for i, t := range long {
			phrases[i] = ftsPhrase(t)
		}
		where = append(where, `n.id IN (SELECT rowid FROM notes_fts WHERE notes_fts MATCH ?)`)
		args = append(args, strings.Join(phrases, " AND "))
	}

	for _, t := range short {
		// The short-token path has no index to lean on, so it checks the columns
		// a 1-2 character designator plausibly appears in.
		where = append(where, `(n.title LIKE ? ESCAPE '\' OR n.body LIKE ? ESCAPE '\'`+
			` OR n.instrument LIKE ? ESCAPE '\' OR n.dut LIKE ? ESCAPE '\')`)
		p := likePattern(t)
		args = append(args, p, p, p, p)
	}

	q := `SELECT n.id, n.title, n.body, n.updated_at
	      FROM notes n
	      WHERE ` + strings.Join(where, " AND ") + `
	      ORDER BY n.updated_at DESC, n.id DESC
	      LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("search %q: %w", query, err)
	}
	defer rows.Close()

	// All tokens are highlighted, not just the indexed ones, so a mixed query
	// like "divider R4" marks both words.
	tokens := append(append([]string{}, long...), short...)

	var hits []Hit
	for rows.Next() {
		var h Hit
		var body, updated string
		if err := rows.Scan(&h.ID, &h.Title, &body, &updated); err != nil {
			return nil, err
		}
		if h.UpdatedAt, err = parseTime(updated); err != nil {
			return nil, err
		}
		h.TitleSegments = makeExcerpt(h.Title, tokens, excerptWidth)
		h.Excerpt = makeExcerpt(body, tokens, excerptWidth)
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.attachHitTags(ctx, hits)
}

// splitTokens divides a query on whitespace into tokens the trigram index can
// serve and tokens it cannot.
func splitTokens(query string) (long, short []string) {
	for _, tok := range strings.Fields(query) {
		if utf8.RuneCountInString(tok) >= trigramFloor {
			long = append(long, tok)
		} else {
			short = append(short, tok)
		}
	}
	return long, short
}

// ftsPhrase wraps a token as an FTS5 string literal.
//
// FTS5 has a query language of its own in which '.', '-' and ':' are operators,
// so an unquoted cal_v2.cfg is a syntax error rather than a search. Wrapping the
// token in double quotes makes FTS5 treat it as literal text; an embedded double
// quote is escaped by doubling it, as in SQL. The user never types a quote.
func ftsPhrase(token string) string {
	return `"` + strings.ReplaceAll(token, `"`, `""`) + `"`
}

// likePattern escapes the LIKE wildcards, so a query containing '_' searches for
// an underscore instead of matching any character. Pairs with ESCAPE '\' in the
// SQL -- without it, searching for "R_4" would quietly match "R14".
func likePattern(token string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(token) + "%"
}

// attachHitTags is the Summary tag-loading trick applied to search results:
// one query for all rows rather than one per row.
func (s *Store) attachHitTags(ctx context.Context, hits []Hit) ([]Hit, error) {
	if len(hits) == 0 {
		return hits, nil
	}
	ids := make([]any, len(hits))
	index := make(map[int64]int, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
		index[h.ID] = i
	}
	q := `SELECT nt.note_id, t.name FROM note_tags nt
	      JOIN tags t ON t.id = nt.tag_id
	      WHERE nt.note_id IN (` + placeholders(len(ids)) + `)
	      ORDER BY t.name COLLATE NOCASE`
	rows, err := s.db.QueryContext(ctx, q, ids...)
	if err != nil {
		return nil, fmt.Errorf("load hit tags: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var noteID int64
		var name string
		if err := rows.Scan(&noteID, &name); err != nil {
			return nil, err
		}
		if i, ok := index[noteID]; ok {
			hits[i].Tags = append(hits[i].Tags, name)
		}
	}
	return hits, rows.Err()
}
