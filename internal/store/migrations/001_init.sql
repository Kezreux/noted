-- 001_init.sql -- notes, revisions, tags and the full-text index.
--
-- Applied by migrate() in store.go, which tracks progress in PRAGMA
-- user_version. Never edit a migration that has shipped; add 002_*.sql instead.

CREATE TABLE notes (
    id         INTEGER PRIMARY KEY,
    title      TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL DEFAULT '',  -- markdown
    instrument TEXT NOT NULL DEFAULT '',  -- which bench instrument was involved
    dut        TEXT NOT NULL DEFAULT '',  -- device under test
    author     TEXT NOT NULL DEFAULT '',  -- who wrote it, when a bench PC is shared
    created_at TEXT NOT NULL,             -- RFC3339, UTC
    updated_at TEXT NOT NULL,
    deleted_at TEXT                       -- soft delete; NULL means live
);

-- Listing and searching both filter out soft-deleted notes and order by recency.
CREATE INDEX notes_live_updated ON notes (deleted_at, updated_at DESC);

-- Every update copies the pre-edit title and body here, in the same transaction
-- as the update itself, so a revision cannot go missing. A lab finding recorded
-- months ago is expensive to lose to a careless edit.
CREATE TABLE note_revisions (
    id          INTEGER PRIMARY KEY,
    note_id     INTEGER NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    body        TEXT NOT NULL,
    replaced_at TEXT NOT NULL
);

CREATE INDEX note_revisions_note ON note_revisions (note_id, replaced_at DESC);

CREATE TABLE tags (
    id   INTEGER PRIMARY KEY,
    -- NOCASE so "SPI" and "spi" are the same tag rather than two.
    name TEXT NOT NULL UNIQUE COLLATE NOCASE
);

CREATE TABLE note_tags (
    note_id INTEGER NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    tag_id  INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (note_id, tag_id)
);

CREATE INDEX note_tags_tag ON note_tags (tag_id);

-- Full-text index.
--
-- content='notes' makes this an *external content* table: it stores only the
-- index, and reads the text from notes when it needs it. Note text therefore
-- lives in exactly one place. The cost is that every column here must be a real
-- column of notes, which is why tags are searched by join instead.
--
-- tokenize='trigram' gives case-insensitive *substring* matching, so "trim"
-- finds VDD_TRIM_OFFSET. Its one limit is that it cannot index anything shorter
-- than three characters, because it indexes overlapping three-character windows;
-- search.go falls back to LIKE for shorter tokens such as R4.
CREATE VIRTUAL TABLE notes_fts USING fts5 (
    title,
    body,
    instrument,
    dut,
    author,
    content = 'notes',
    content_rowid = 'id',
    tokenize = 'trigram'
);

-- An external-content table is not updated by writes to notes, so these
-- triggers do it. Deletions must hand FTS5 the *old* values, via the special
-- 'delete' command, so it can find and remove the right index entries.
CREATE TRIGGER notes_fts_ai AFTER INSERT ON notes BEGIN
    INSERT INTO notes_fts (rowid, title, body, instrument, dut, author)
    VALUES (new.id, new.title, new.body, new.instrument, new.dut, new.author);
END;

CREATE TRIGGER notes_fts_ad AFTER DELETE ON notes BEGIN
    INSERT INTO notes_fts (notes_fts, rowid, title, body, instrument, dut, author)
    VALUES ('delete', old.id, old.title, old.body, old.instrument, old.dut, old.author);
END;

CREATE TRIGGER notes_fts_au AFTER UPDATE ON notes BEGIN
    INSERT INTO notes_fts (notes_fts, rowid, title, body, instrument, dut, author)
    VALUES ('delete', old.id, old.title, old.body, old.instrument, old.dut, old.author);
    INSERT INTO notes_fts (rowid, title, body, instrument, dut, author)
    VALUES (new.id, new.title, new.body, new.instrument, new.dut, new.author);
END;
