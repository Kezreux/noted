// Package store is noted's data layer: it owns the SQLite database, the schema
// migrations, and every query. It deliberately imports nothing from Wails, so
// it can be exercised with plain `go test` and no GUI.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" driver
)

// ErrNotFound is returned when a note id does not exist, or names a note that
// has been deleted. Callers compare with errors.Is rather than by string.
var ErrNotFound = errors.New("note not found")

//go:embed migrations/*.sql
var migrationFS embed.FS

// Store is a handle on the database. It is safe for concurrent use.
type Store struct {
	db *sql.DB
	// now is time.Now in production and a fixed clock in tests, so that
	// timestamp assertions do not have to guess.
	now func() time.Time
}

// Note is a single logbook entry.
type Note struct {
	ID         int64     `json:"id"`
	Title      string    `json:"title"`
	Body       string    `json:"body"` // markdown
	Instrument string    `json:"instrument"`
	DUT        string    `json:"dut"`
	Author     string    `json:"author"`
	Tags       []string  `json:"tags"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// NoteInput carries the writable fields of a note. It is separate from Note so
// that callers cannot pretend to set an id or a timestamp.
type NoteInput struct {
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	Instrument string   `json:"instrument"`
	DUT        string   `json:"dut"`
	Author     string   `json:"author"`
	Tags       []string `json:"tags"`
}

// Summary is the reduced form used for the note list, where the full body would
// be wasted bandwidth.
type Summary struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Excerpt   string    `json:"excerpt"`
	Tags      []string  `json:"tags"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Revision is a superseded version of a note's text.
type Revision struct {
	ID         int64     `json:"id"`
	NoteID     int64     `json:"noteId"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	ReplacedAt time.Time `json:"replacedAt"`
}

// Open opens (creating if absent) the database at dbPath and brings its schema
// up to date.
func Open(dbPath string) (*Store, error) {
	// The _pragma query parameters are applied to every connection in the pool,
	// which matters because foreign_keys is a per-connection setting in SQLite
	// and silently defaults to off.
	//
	//   busy_timeout  wait rather than failing instantly on a locked database
	//   foreign_keys  enforce ON DELETE CASCADE
	//   journal_mode  WAL survives a crash mid-write better than the default
	dsn := "file:" + dbPath +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dbPath, err)
	}

	// One connection. A desktop logbook has a single user, so serialising access
	// costs nothing measurable and removes "database is locked" as a category of
	// bug entirely.
	db.SetMaxOpenConns(1)

	s := &Store{db: db, now: time.Now}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close checkpoints the write-ahead log and closes the database.
func (s *Store) Close() error {
	// Folding the WAL back into the main file keeps "back up = copy the folder"
	// honest: after a clean shutdown the -wal file is empty, so noted.db alone
	// is a complete copy.
	if _, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		s.db.Close()
		return fmt.Errorf("checkpoint: %w", err)
	}
	return s.db.Close()
}

// BackupTo writes a consistent snapshot of the database to path, safely even
// while the app is running. It is the mechanism behind an "Export backup" button.
func (s *Store) BackupTo(ctx context.Context, path string) error {
	// VACUUM INTO takes its own read transaction, so the result is a coherent
	// snapshot rather than a half-written file.
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, path); err != nil {
		return fmt.Errorf("backup to %s: %w", path, err)
	}
	return nil
}

// migrate applies every embedded migration that has not run yet.
//
// Progress is recorded in SQLite's own user_version field, an integer stored in
// the database header, rather than in a table we would have to create first.
func migrate(db *sql.DB) error {
	names, err := migrationNames()
	if err != nil {
		return err
	}

	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	if version > len(names) {
		return fmt.Errorf("database schema is version %d but this build only knows %d: "+
			"it was written by a newer version of noted", version, len(names))
	}

	for i, name := range names[version:] {
		target := version + i + 1
		sqlText, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if err := applyMigration(db, name, string(sqlText), target); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(db *sql.DB, name, sqlText string, target int) error {
	// SQLite has transactional DDL, so a migration that fails half way leaves
	// the schema untouched instead of mangled.
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once Commit succeeds

	if _, err := tx.Exec(sqlText); err != nil {
		return fmt.Errorf("migration %s: %w", name, err)
	}
	// PRAGMA does not accept bound parameters, so this is formatted in. target
	// is derived from the number of embedded files, never from user input.
	if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, target)); err != nil {
		return fmt.Errorf("migration %s: set user_version: %w", name, err)
	}
	return tx.Commit()
}

// migrationNames returns the migration filenames in lexical order, which is
// also their apply order -- hence the zero-padded 001_ prefixes.
func migrationNames() ([]string, error) {
	entries, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, filepath.Base(e))
	}
	slices.Sort(names)
	return names, nil
}

// SchemaVersion reports how many migrations have been applied. Useful in tests
// and when diagnosing a database by hand.
func (s *Store) SchemaVersion() (int, error) {
	var v int
	err := s.db.QueryRow(`PRAGMA user_version`).Scan(&v)
	return v, err
}

// ---------------------------------------------------------------------------
// writes
// ---------------------------------------------------------------------------

// Create inserts a new note and returns its id.
func (s *Store) Create(ctx context.Context, in NoteInput) (int64, error) {
	var id int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		ts := s.timestamp()
		res, err := tx.ExecContext(ctx,
			`INSERT INTO notes (title, body, instrument, dut, author, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			in.Title, in.Body, in.Instrument, in.DUT, in.Author, ts, ts)
		if err != nil {
			return fmt.Errorf("insert note: %w", err)
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		return setTags(ctx, tx, id, in.Tags)
	})
	return id, err
}

// Update replaces a note's fields, first preserving the outgoing title and body
// in note_revisions.
func (s *Store) Update(ctx context.Context, id int64, in NoteInput) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		var oldTitle, oldBody string
		err := tx.QueryRowContext(ctx,
			`SELECT title, body FROM notes WHERE id = ? AND deleted_at IS NULL`, id,
		).Scan(&oldTitle, &oldBody)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("update %d: %w", id, ErrNotFound)
		}
		if err != nil {
			return err
		}

		ts := s.timestamp()

		// Only record a revision when the text actually changed. Autosave fires
		// on a timer, and an unchanged save should not bury the real history
		// under identical rows.
		if oldTitle != in.Title || oldBody != in.Body {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO note_revisions (note_id, title, body, replaced_at) VALUES (?, ?, ?, ?)`,
				id, oldTitle, oldBody, ts,
			); err != nil {
				return fmt.Errorf("record revision: %w", err)
			}
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE notes SET title = ?, body = ?, instrument = ?, dut = ?, author = ?, updated_at = ?
			 WHERE id = ?`,
			in.Title, in.Body, in.Instrument, in.DUT, in.Author, ts, id,
		); err != nil {
			return fmt.Errorf("update note: %w", err)
		}
		return setTags(ctx, tx, id, in.Tags)
	})
}

// Delete soft-deletes a note: it stops appearing in lists and searches, but the
// row and its revisions survive. Nothing in a logbook should be destroyed by a
// single keystroke.
func (s *Store) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE notes SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		s.timestamp(), s.timestamp(), id)
	if err != nil {
		return fmt.Errorf("delete %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("delete %d: %w", id, ErrNotFound)
	}
	return nil
}

// Restore undoes a soft delete.
func (s *Store) Restore(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE notes SET deleted_at = NULL, updated_at = ? WHERE id = ? AND deleted_at IS NOT NULL`,
		s.timestamp(), id)
	if err != nil {
		return fmt.Errorf("restore %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("restore %d: %w", id, ErrNotFound)
	}
	return nil
}

// setTags makes note id's tags exactly want, creating tag rows as needed.
func setTags(ctx context.Context, tx *sql.Tx, id int64, want []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM note_tags WHERE note_id = ?`, id); err != nil {
		return fmt.Errorf("clear tags: %w", err)
	}
	for _, raw := range want {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		// tags.name is UNIQUE COLLATE NOCASE, so the upsert collapses "SPI"
		// and "spi" onto whichever spelling was stored first.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO tags (name) VALUES (?) ON CONFLICT (name) DO NOTHING`, name,
		); err != nil {
			return fmt.Errorf("upsert tag %q: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO note_tags (note_id, tag_id)
			 VALUES (?, (SELECT id FROM tags WHERE name = ?))
			 ON CONFLICT DO NOTHING`, id, name,
		); err != nil {
			return fmt.Errorf("link tag %q: %w", name, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// reads
// ---------------------------------------------------------------------------

// Get returns one note, tags included.
func (s *Store) Get(ctx context.Context, id int64) (Note, error) {
	var n Note
	var created, updated string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, title, body, instrument, dut, author, created_at, updated_at
		 FROM notes WHERE id = ? AND deleted_at IS NULL`, id,
	).Scan(&n.ID, &n.Title, &n.Body, &n.Instrument, &n.DUT, &n.Author, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Note{}, fmt.Errorf("get %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return Note{}, err
	}
	if n.CreatedAt, err = parseTime(created); err != nil {
		return Note{}, err
	}
	if n.UpdatedAt, err = parseTime(updated); err != nil {
		return Note{}, err
	}
	if n.Tags, err = s.tagsFor(ctx, id); err != nil {
		return Note{}, err
	}
	return n, nil
}

// List returns live notes, most recently updated first.
func (s *Store) List(ctx context.Context, limit, offset int) ([]Summary, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, body, updated_at
		 FROM notes WHERE deleted_at IS NULL
		 ORDER BY updated_at DESC, id DESC
		 LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	defer rows.Close()

	var out []Summary
	for rows.Next() {
		var s2 Summary
		var body, updated string
		if err := rows.Scan(&s2.ID, &s2.Title, &body, &updated); err != nil {
			return nil, err
		}
		if s2.UpdatedAt, err = parseTime(updated); err != nil {
			return nil, err
		}
		s2.Excerpt = leadingExcerpt(body, excerptWidth)
		out = append(out, s2)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.attachTags(ctx, out)
}

// Revisions returns a note's superseded versions, newest first.
func (s *Store) Revisions(ctx context.Context, noteID int64) ([]Revision, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, note_id, title, body, replaced_at
		 FROM note_revisions WHERE note_id = ? ORDER BY replaced_at DESC, id DESC`, noteID)
	if err != nil {
		return nil, fmt.Errorf("revisions for %d: %w", noteID, err)
	}
	defer rows.Close()

	var out []Revision
	for rows.Next() {
		var r Revision
		var replaced string
		if err := rows.Scan(&r.ID, &r.NoteID, &r.Title, &r.Body, &replaced); err != nil {
			return nil, err
		}
		if r.ReplacedAt, err = parseTime(replaced); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AllTags returns every tag in use, alphabetically.
func (s *Store) AllTags(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT t.name FROM tags t
		 JOIN note_tags nt ON nt.tag_id = t.id
		 JOIN notes n ON n.id = nt.note_id AND n.deleted_at IS NULL
		 ORDER BY t.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (s *Store) tagsFor(ctx context.Context, id int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.name FROM tags t
		 JOIN note_tags nt ON nt.tag_id = t.id
		 WHERE nt.note_id = ? ORDER BY t.name COLLATE NOCASE`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// attachTags fills in the Tags field of many summaries with one query, rather
// than one query per row.
func (s *Store) attachTags(ctx context.Context, rows []Summary) ([]Summary, error) {
	if len(rows) == 0 {
		return rows, nil
	}
	ids := make([]any, len(rows))
	index := make(map[int64]int, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
		index[r.ID] = i
	}

	// A bound parameter cannot stand for a list, so the placeholders are built
	// to match the number of ids. The values themselves are still bound.
	q := `SELECT nt.note_id, t.name FROM note_tags nt
	      JOIN tags t ON t.id = nt.tag_id
	      WHERE nt.note_id IN (` + placeholders(len(ids)) + `)
	      ORDER BY t.name COLLATE NOCASE`
	res, err := s.db.QueryContext(ctx, q, ids...)
	if err != nil {
		return nil, fmt.Errorf("load tags: %w", err)
	}
	defer res.Close()
	for res.Next() {
		var noteID int64
		var name string
		if err := res.Scan(&noteID, &name); err != nil {
			return nil, err
		}
		if i, ok := index[noteID]; ok {
			rows[i].Tags = append(rows[i].Tags, name)
		}
	}
	return rows, res.Err()
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// inTx runs fn inside a transaction, committing on success and rolling back on
// any error. This is the standard Go shape for "several statements, all or
// nothing": the deferred Rollback is a no-op once Commit has succeeded.
func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// timestamp formats the current time the way the schema stores it: RFC3339 in
// UTC, which sorts correctly as text and is unambiguous across time zones.
func (s *Store) timestamp() string {
	return s.now().UTC().Format(time.RFC3339Nano)
}

func parseTime(v string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", v, err)
	}
	return t, nil
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}
