package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/Kezreux/noted/internal/markdown"
	"github.com/Kezreux/noted/internal/store"
)

// NoteService is the bridge between the frontend and the data layer. Wails
// generates JavaScript bindings for every exported method here, which is why the
// signatures use plain types that survive a trip through JSON.
//
// It holds no SQL of its own; every query lives in internal/store.
type NoteService struct {
	store    *store.Store
	markdown *markdown.Renderer
	dataDir  string
}

func NewNoteService() *NoteService {
	return &NoteService{markdown: markdown.New()}
}

// ServiceName is used by Wails in logs and error messages.
func (n *NoteService) ServiceName() string {
	return "noted.NoteService"
}

// ServiceStartup opens the database. Wails calls it once, during application
// startup; returning an error here aborts the launch, which is what we want if
// the data folder is unusable -- better than a running app that cannot save.
func (n *NoteService) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	dir, err := store.DefaultDataDir()
	if err != nil {
		return err
	}
	s, err := store.OpenDataDir(dir)
	if err != nil {
		return err
	}
	n.store, n.dataDir = s, dir
	slog.Info("noted data folder", "path", dir)
	return nil
}

// ServiceShutdown closes the database, which also checkpoints the write-ahead
// log so that copying the folder is a complete backup.
func (n *NoteService) ServiceShutdown() error {
	if n.store == nil {
		return nil
	}
	return n.store.Close()
}

// ---------------------------------------------------------------------------
// methods the frontend calls
// ---------------------------------------------------------------------------

// DataDir returns the folder holding the database and attachments, so the UI can
// show the user where their data is and therefore what to back up.
func (n *NoteService) DataDir() string {
	return n.dataDir
}

// Create saves a new note and returns its id.
func (n *NoteService) Create(ctx context.Context, in store.NoteInput) (int64, error) {
	return n.store.Create(ctx, in)
}

// Update saves changes to an existing note, preserving the previous text as a
// revision.
func (n *NoteService) Update(ctx context.Context, id int64, in store.NoteInput) error {
	return n.store.Update(ctx, id, in)
}

// Get returns a single note.
func (n *NoteService) Get(ctx context.Context, id int64) (store.Note, error) {
	return n.store.Get(ctx, id)
}

// List returns notes for the sidebar, most recently updated first.
func (n *NoteService) List(ctx context.Context, limit, offset int) ([]store.Summary, error) {
	notes, err := n.store.List(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	// A nil slice marshals to JSON null, which the frontend would have to guard
	// against on every call. An empty slice marshals to [], so return that.
	if notes == nil {
		notes = []store.Summary{}
	}
	return notes, nil
}

// Search finds notes matching query. Tokens of three or more characters are
// matched as substrings against the full-text index; shorter ones, which that
// index cannot hold, fall back to a scan. See store.Search.
func (n *NoteService) Search(ctx context.Context, query string, limit int) ([]store.Hit, error) {
	hits, err := n.store.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	if hits == nil {
		hits = []store.Hit{}
	}
	return hits, nil
}

// Delete soft-deletes a note; Restore brings it back.
func (n *NoteService) Delete(ctx context.Context, id int64) error {
	return n.store.Delete(ctx, id)
}

// Restore undoes a Delete.
func (n *NoteService) Restore(ctx context.Context, id int64) error {
	return n.store.Restore(ctx, id)
}

// Revisions returns a note's superseded versions, newest first. Nothing in the
// interface calls this; it exists so that text replaced by a careless edit is
// still reachable, which for a months-old finding is the difference between a
// mistake and a loss.
func (n *NoteService) Revisions(ctx context.Context, id int64) ([]store.Revision, error) {
	revs, err := n.store.Revisions(ctx, id)
	if err != nil {
		return nil, err
	}
	if revs == nil {
		revs = []store.Revision{}
	}
	return revs, nil
}

// AllTags returns every tag in use, for autocomplete.
func (n *NoteService) AllTags(ctx context.Context) ([]string, error) {
	tags, err := n.store.AllTags(ctx)
	if err != nil {
		return nil, err
	}
	if tags == nil {
		tags = []string{}
	}
	return tags, nil
}

// RenderMarkdown converts a note body to HTML for the preview pane.
//
// Rendering happens in Go rather than in the webview so that the escaping rules
// are decided in one place: raw HTML in a note is shown as text, and link
// schemes are checked. See internal/markdown.
func (n *NoteService) RenderMarkdown(source string) (string, error) {
	return n.markdown.Render(source)
}

// ExportBackup writes a consistent copy of the database to path, safe to run
// while the app is in use.
func (n *NoteService) ExportBackup(ctx context.Context, path string) error {
	if path == "" {
		return fmt.Errorf("export backup: no destination given")
	}
	return n.store.BackupTo(ctx, path)
}
