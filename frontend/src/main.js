// main.js -- wiring: builds the sidebar and editor, and owns the keyboard map.

import * as api from "./api.js";
import { NoteList } from "./notelist.js";
import { Editor } from "./editor.js";

const $ = (id) => document.getElementById(id);

const els = {
    list: $("note-list"),
    search: $("search"),
    searchClear: $("search-clear"),
    searchStatus: $("search-status"),
    newNote: $("new-note"),
    emptyNew: $("empty-new"),
    editor: $("editor"),
    empty: $("empty"),
    title: $("title"),
    body: $("body"),
    instrument: $("instrument"),
    dut: $("dut"),
    tags: $("tags"),
    preview: $("preview"),
    previewToggle: $("toggle-preview"),
    saveState: $("save-state"),
    deleteNote: $("delete-note"),
    dataDir: $("data-dir"),
    toast: $("toast"),
};

let toastTimer;

/**
 * Surfaces a failure to the user instead of leaving it in the console.
 *
 * Everything here is local, so an error means something is genuinely wrong --
 * a missing data folder, a locked database -- and the user needs to know rather
 * than wonder why their note did not save.
 */
function showError(err, what) {
    console.error(what, err);
    els.toast.textContent = `Could not ${what}: ${err?.message ?? err}`;
    els.toast.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { els.toast.hidden = true; }, 6000);
}

const list = new NoteList({
    list: els.list,
    search: els.search,
    clear: els.searchClear,
    status: els.searchStatus,
    onSelect: (id) => void openNote(id),
    onError: showError,
});

const editor = new Editor({
    fields: {
        title: els.title,
        body: els.body,
        instrument: els.instrument,
        dut: els.dut,
        tags: els.tags,
    },
    preview: els.preview,
    previewToggle: els.previewToggle,
    saveState: els.saveState,
    onSaved: (id) => {
        // A save changes the title and the updated-at order, so the sidebar has
        // to catch up.
        list.select(id);
        void list.refresh();
    },
    onError: showError,
});

/** Loads a note into the editor, saving whatever is currently open first. */
async function openNote(id) {
    if (editor.id === id) return;
    await editor.save();
    try {
        const note = await api.getNote(id);
        editor.load(note);
        list.select(id);
        showEditor(true);
    } catch (err) {
        showError(err, "open the note");
        // The note is gone -- most likely deleted in another window. Re-syncing
        // the list is more useful than leaving a stale row on screen.
        void list.refresh();
    }
}

/** Starts a new, empty note. */
async function newNote() {
    await editor.save();
    editor.blank();
    list.select(-1); // nothing in the list is current any more
    showEditor(true);
}

function showEditor(visible) {
    els.editor.hidden = !visible;
    els.empty.hidden = visible;
}

async function deleteCurrent() {
    if (editor.id === null) {
        // Never saved, so there is nothing to delete; just clear the form.
        editor.blank();
        return;
    }
    const id = editor.id;
    try {
        await api.deleteNote(id);
        // Deletion is a soft delete, so this is recoverable -- which is why it
        // does not demand a confirmation dialog first.
        editor.blank();
        showEditor(false);
        await list.refresh();
    } catch (err) {
        showError(err, "delete the note");
    }
}

// ---------------------------------------------------------------- keyboard

document.addEventListener("keydown", (ev) => {
    const ctrl = ev.ctrlKey || ev.metaKey;

    if (ctrl && ev.key === "n") {
        ev.preventDefault();
        void newNote();
        return;
    }
    if (ctrl && ev.key === "s") {
        ev.preventDefault();
        void editor.save();
        return;
    }
    // Both, because Ctrl+F is the reflex for "find" and Ctrl+K for "go to".
    if (ctrl && (ev.key === "k" || ev.key === "f")) {
        ev.preventDefault();
        list.focusSearch();
        return;
    }
    if (ctrl && ev.key === "p") {
        ev.preventDefault();
        editor.togglePreview();
        return;
    }
    if (ev.key === "Escape") {
        if (document.activeElement === els.search) {
            // First Escape clears the search, rather than jumping away from it.
            if (list.query !== "") {
                list.clearSearch();
            } else {
                els.body.focus();
            }
            return;
        }
        list.focusSearch();
    }
});

els.newNote.addEventListener("click", () => void newNote());
els.emptyNew.addEventListener("click", () => void newNote());
els.deleteNote.addEventListener("click", () => void deleteCurrent());

// A note in progress must survive the window closing.
window.addEventListener("beforeunload", () => { void editor.save(); });

// ------------------------------------------------------------------ startup

async function start() {
    try {
        // Showing the data folder makes backing up concrete: this is the one
        // folder to copy, and the user would otherwise have to be told where it is.
        els.dataDir.textContent = await api.dataDir();
    } catch (err) {
        showError(err, "find the data folder");
    }

    await list.refresh();

    // Open on an empty note ready to type into, because the common case is
    // arriving with something to write down.
    editor.blank();
    showEditor(true);
}

void start();
