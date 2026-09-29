package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// newStore returns a Store backed by a real database file in a temp directory.
//
// These tests use real SQLite rather than a mock on purpose: almost everything
// worth testing here -- the FTS triggers, the trigram tokenizer, LIKE escaping,
// cascading deletes -- is SQLite's behaviour, and a mock would only assert that
// the queries are the ones we wrote.
func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	// A fixed, advancing clock: every call is 1 second later than the last, so
	// "most recently updated first" is deterministic instead of racing the
	// system clock's resolution.
	tick := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time {
		tick = tick.Add(time.Second)
		return tick
	}
	return s
}

func mustCreate(t *testing.T, s *Store, in NoteInput) int64 {
	t.Helper()
	id, err := s.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("Create(%q): %v", in.Title, err)
	}
	return id
}

func TestMigrateFromEmpty(t *testing.T) {
	s := newStore(t)

	got, err := s.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	names, err := migrationNames()
	if err != nil {
		t.Fatalf("migrationNames: %v", err)
	}
	if got != len(names) {
		t.Errorf("schema version = %d, want %d (one per migration file)", got, len(names))
	}
}

// Reopening an existing database must be a no-op, not a second attempt to
// CREATE TABLE. This is the regression test for the migration bookkeeping.
func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.db")

	first, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	id := mustCreate(t, first, NoteInput{Title: "survives"})
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer second.Close()

	if _, err := second.Get(context.Background(), id); err != nil {
		t.Errorf("note did not survive reopen: %v", err)
	}
}

// A database written by a future version of noted must be refused rather than
// used with a schema this build does not understand.
func TestMigrateRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.db.Exec(`PRAGMA user_version = 999`); err != nil {
		t.Fatalf("bump user_version: %v", err)
	}
	s.db.Close()

	if _, err := Open(path); err == nil {
		t.Fatal("Open accepted a database from a newer version; want an error")
	}
}

func TestCreateAndGet(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	in := NoteInput{
		Title:      "VDD trim for the 1.8 V rail",
		Body:       "Reduced VDD_TRIM_OFFSET from 0x1A to 0x12.",
		Instrument: "Keysight 34465A",
		DUT:        "rev C3",
		Author:     "kez",
		Tags:       []string{"power", "trim"},
	}
	id := mustCreate(t, s, in)

	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != in.Title || got.Body != in.Body {
		t.Errorf("text round-trip failed:\n got title=%q body=%q\nwant title=%q body=%q",
			got.Title, got.Body, in.Title, in.Body)
	}
	if got.Instrument != in.Instrument || got.DUT != in.DUT || got.Author != in.Author {
		t.Errorf("metadata round-trip failed: %+v", got)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "power" || got.Tags[1] != "trim" {
		t.Errorf("tags = %v, want [power trim] in alphabetical order", got.Tags)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Errorf("timestamps not set: created=%v updated=%v", got.CreatedAt, got.UpdatedAt)
	}
}

func TestGetMissingReturnsErrNotFound(t *testing.T) {
	if _, err := newStore(t).Get(context.Background(), 4242); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(missing) error = %v, want ErrNotFound", err)
	}
}

func TestUpdateRecordsRevision(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	id := mustCreate(t, s, NoteInput{Title: "first", Body: "original finding"})
	if err := s.Update(ctx, id, NoteInput{Title: "second", Body: "corrected finding"}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	revs, err := s.Revisions(ctx, id)
	if err != nil {
		t.Fatalf("Revisions: %v", err)
	}
	if len(revs) != 1 {
		t.Fatalf("got %d revisions, want 1", len(revs))
	}
	// The revision holds the text that was replaced, not the text that replaced it.
	if revs[0].Title != "first" || revs[0].Body != "original finding" {
		t.Errorf("revision = %q/%q, want the pre-edit text \"first\"/\"original finding\"",
			revs[0].Title, revs[0].Body)
	}

	current, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if current.Body != "corrected finding" {
		t.Errorf("current body = %q, want the new text", current.Body)
	}
}

// Autosave fires on a timer, so saving without editing must not bury the real
// history under identical revisions.
func TestUpdateWithoutTextChangeAddsNoRevision(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	in := NoteInput{Title: "steady", Body: "unchanged"}
	id := mustCreate(t, s, in)

	for range 3 {
		if err := s.Update(ctx, id, in); err != nil {
			t.Fatalf("Update: %v", err)
		}
	}
	revs, err := s.Revisions(ctx, id)
	if err != nil {
		t.Fatalf("Revisions: %v", err)
	}
	if len(revs) != 0 {
		t.Errorf("got %d revisions after no-op saves, want 0", len(revs))
	}

	// Changing only the metadata still must not create a text revision.
	in.Instrument = "scope"
	if err := s.Update(ctx, id, in); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if revs, _ = s.Revisions(ctx, id); len(revs) != 0 {
		t.Errorf("metadata-only edit created %d revisions, want 0", len(revs))
	}
}

func TestUpdateAccumulatesRevisionsNewestFirst(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	id := mustCreate(t, s, NoteInput{Title: "n", Body: "v1"})
	for _, body := range []string{"v2", "v3", "v4"} {
		if err := s.Update(ctx, id, NoteInput{Title: "n", Body: body}); err != nil {
			t.Fatalf("Update(%s): %v", body, err)
		}
	}

	revs, err := s.Revisions(ctx, id)
	if err != nil {
		t.Fatalf("Revisions: %v", err)
	}
	want := []string{"v3", "v2", "v1"} // newest replacement first
	if len(revs) != len(want) {
		t.Fatalf("got %d revisions, want %d", len(revs), len(want))
	}
	for i, w := range want {
		if revs[i].Body != w {
			t.Errorf("revision[%d] = %q, want %q", i, revs[i].Body, w)
		}
	}
}

func TestUpdateMissingReturnsErrNotFound(t *testing.T) {
	err := newStore(t).Update(context.Background(), 999, NoteInput{Title: "x"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Update(missing) error = %v, want ErrNotFound", err)
	}
}

func TestDeleteIsSoftAndReversible(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	id := mustCreate(t, s, NoteInput{Title: "temporary", Body: "VDD_TRIM_OFFSET"})
	if err := s.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := s.Get(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
	list, err := s.List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("List returned %d deleted notes, want 0", len(list))
	}
	// A deleted note must not surface in search either.
	hits, err := s.Search(ctx, "VDD_TRIM_OFFSET", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("Search returned %d deleted notes, want 0", len(hits))
	}

	// The row survived, so it can come back.
	if err := s.Restore(ctx, id); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if _, err := s.Get(ctx, id); err != nil {
		t.Errorf("Get after Restore: %v", err)
	}
}

func TestDeleteTwiceReturnsErrNotFound(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	id := mustCreate(t, s, NoteInput{Title: "once"})
	if err := s.Delete(ctx, id); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	if err := s.Delete(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
}

func TestListOrdersByMostRecentlyUpdated(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	oldest := mustCreate(t, s, NoteInput{Title: "oldest"})
	mustCreate(t, s, NoteInput{Title: "middle"})
	mustCreate(t, s, NoteInput{Title: "newest"})

	// Touching the oldest note should move it to the front.
	if err := s.Update(ctx, oldest, NoteInput{Title: "oldest, edited"}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	list, err := s.List(ctx, 100, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("got %d notes, want 3", len(list))
	}
	if list[0].Title != "oldest, edited" {
		t.Errorf("first entry = %q, want the just-edited note", list[0].Title)
	}
}

func TestListPaginates(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	for i := range 5 {
		mustCreate(t, s, NoteInput{Title: string(rune('a' + i))})
	}

	first, err := s.List(ctx, 2, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	second, err := s.List(ctx, 2, 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("page sizes = %d, %d, want 2, 2", len(first), len(second))
	}
	if first[0].ID == second[0].ID {
		t.Error("offset had no effect: both pages start with the same note")
	}
}

func TestListExcerptIsTheStartOfTheBody(t *testing.T) {
	s := newStore(t)
	mustCreate(t, s, NoteInput{
		Title: "with body",
		Body:  "  First line of the finding.\n\nSecond paragraph.  ",
	})
	list, err := s.List(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// Whitespace, including the newlines, is collapsed so the list stays tidy.
	const want = "First line of the finding. Second paragraph."
	if list[0].Excerpt != want {
		t.Errorf("excerpt = %q, want %q", list[0].Excerpt, want)
	}
}

func TestTagsAreReplacedNotAccumulated(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	id := mustCreate(t, s, NoteInput{Title: "t", Tags: []string{"power", "trim"}})
	if err := s.Update(ctx, id, NoteInput{Title: "t", Tags: []string{"trim", "pll"}}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := s.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Tags) != 2 {
		t.Fatalf("tags = %v, want exactly 2 after replacement", got.Tags)
	}
	for _, tag := range got.Tags {
		if tag == "power" {
			t.Errorf("tags = %v, still contains the removed tag %q", got.Tags, "power")
		}
	}
}

func TestTagsAreCaseInsensitiveAndTrimmed(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	// "SPI" and "spi" are one tag, not two, and blank entries are dropped.
	mustCreate(t, s, NoteInput{Title: "a", Tags: []string{"SPI"}})
	mustCreate(t, s, NoteInput{Title: "b", Tags: []string{" spi ", "", "   "}})

	tags, err := s.AllTags(ctx)
	if err != nil {
		t.Fatalf("AllTags: %v", err)
	}
	if len(tags) != 1 {
		t.Errorf("AllTags = %v, want a single tag (SPI and spi are the same)", tags)
	}
}

// Deleting a note's row must take its revisions and tag links with it, which is
// what ON DELETE CASCADE is for -- and cascades are silently inert unless the
// foreign_keys pragma is on for the connection.
func TestForeignKeysCascade(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	id := mustCreate(t, s, NoteInput{Title: "v1", Body: "v1", Tags: []string{"x"}})
	if err := s.Update(ctx, id, NoteInput{Title: "v2", Body: "v2", Tags: []string{"x"}}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// A hard delete. Nothing in the interface issues one, but the cascade is what
	// keeps revisions and tag links from outliving their note when one is issued.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM notes WHERE id = ?`, id); err != nil {
		t.Fatalf("hard delete: %v", err)
	}

	var revisions, links int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM note_revisions WHERE note_id = ?`, id).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM note_tags WHERE note_id = ?`, id).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if revisions != 0 || links != 0 {
		t.Errorf("after hard delete: %d revisions, %d tag links; want 0 and 0 "+
			"(is the foreign_keys pragma reaching the connection?)", revisions, links)
	}
}

func TestBackupToProducesAReadableCopy(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	id := mustCreate(t, s, NoteInput{Title: "backed up", Body: "VDD_TRIM_OFFSET"})

	dest := filepath.Join(t.TempDir(), "backup.db")
	if err := s.BackupTo(ctx, dest); err != nil {
		t.Fatalf("BackupTo: %v", err)
	}

	copied, err := Open(dest)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer copied.Close()

	got, err := copied.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get from backup: %v", err)
	}
	if got.Title != "backed up" {
		t.Errorf("backup title = %q, want %q", got.Title, "backed up")
	}
	// The index has to come across too, or the copy is not a usable logbook.
	hits, err := copied.Search(ctx, "TRIM", 10)
	if err != nil {
		t.Fatalf("Search in backup: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("backup search returned %d hits, want 1", len(hits))
	}
}
