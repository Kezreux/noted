// api.js -- the only module that touches the generated Wails bindings.
//
// Everything else in the frontend calls these functions. Keeping the boundary in
// one file means the bindings can be regenerated, or the UI ported to a
// framework, without the change spreading through every module.
//
// Never edit ../bindings/: Wails regenerates that directory.

import * as NoteService from "../bindings/github.com/Kezreux/noted/noteservice.js";

/** How many notes the sidebar loads at a time. */
export const PAGE_SIZE = 200;

/**
 * A note as the Go side defines it.
 * @typedef {{id: number, title: string, body: string, instrument: string,
 *            dut: string, author: string, tags: string[]|null,
 *            createdAt: string, updatedAt: string}} Note
 */

/**
 * One run of excerpt text, flagged if it matched the query. The Go side returns
 * segments rather than HTML so that note content can never become markup.
 * @typedef {{text: string, match: boolean}} Segment
 */

export const listNotes = (limit = PAGE_SIZE, offset = 0) => NoteService.List(limit, offset);
export const searchNotes = (query, limit = 50) => NoteService.Search(query, limit);
export const getNote = (id) => NoteService.Get(id);
export const createNote = (input) => NoteService.Create(input);
export const updateNote = (id, input) => NoteService.Update(id, input);
export const deleteNote = (id) => NoteService.Delete(id);
export const restoreNote = (id) => NoteService.Restore(id);
export const revisions = (id) => NoteService.Revisions(id);
export const allTags = () => NoteService.AllTags();
export const renderMarkdown = (source) => NoteService.RenderMarkdown(source);
export const dataDir = () => NoteService.DataDir();
export const exportBackup = (path) => NoteService.ExportBackup(path);

/**
 * Builds the NoteInput shape the Go side expects.
 *
 * Tags arrive from a single comma-separated field; blank entries are dropped
 * here so the store is not asked to store them. It trims too, but the UI should
 * not send obvious rubbish.
 *
 * @param {{title: string, body: string, instrument: string, dut: string,
 *          author?: string, tags: string}} fields
 */
export function toNoteInput(fields) {
    return {
        title: fields.title,
        body: fields.body,
        instrument: fields.instrument,
        dut: fields.dut,
        author: fields.author ?? "",
        tags: fields.tags
            .split(",")
            .map((t) => t.trim())
            .filter((t) => t !== ""),
    };
}
