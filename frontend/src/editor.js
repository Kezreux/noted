// editor.js -- the note editor: fields, autosave, and the Markdown preview.

import * as api from "./api.js";

/** Autosave fires this long after the last keystroke. */
const IDLE_SAVE_MS = 1200;
/** The preview re-renders this long after the last keystroke. */
const PREVIEW_MS = 250;

export class Editor {
    /**
     * @param {{fields: {title: HTMLInputElement, body: HTMLTextAreaElement,
     *                   instrument: HTMLInputElement, dut: HTMLInputElement,
     *                   tags: HTMLInputElement},
     *          preview: HTMLElement, previewToggle: HTMLButtonElement,
     *          saveState: HTMLElement,
     *          onSaved: (id: number) => void,
     *          onError: (err: unknown, what: string) => void}} opts
     */
    constructor(opts) {
        this.f = opts.fields;
        this.previewEl = opts.preview;
        this.previewToggle = opts.previewToggle;
        this.saveStateEl = opts.saveState;
        this.onSaved = opts.onSaved;
        this.onError = opts.onError;

        /** Note id, or null for a note that has never been saved. */
        this.id = null;
        /** Serialised form of the last saved state, to detect real changes. */
        this.clean = "";
        /** True while a save is in flight, so two saves cannot overlap. */
        this.saving = false;
        /** Set when an edit arrives during a save, so it is not lost. */
        this.dirtyDuringSave = false;
        this.idleTimer = undefined;
        this.previewTimer = undefined;
        this.previewOpen = false;

        for (const el of Object.values(this.f)) {
            el.addEventListener("input", () => this.onInput());
            // Leaving a field is a natural save point, and covers the case where
            // the user clicks straight to another note.
            el.addEventListener("blur", () => this.save());
        }

        this.previewToggle.addEventListener("click", () => this.togglePreview());
    }

    /** Fields as a plain object. */
    values() {
        return {
            title: this.f.title.value,
            body: this.f.body.value,
            instrument: this.f.instrument.value,
            dut: this.f.dut.value,
            tags: this.f.tags.value,
        };
    }

    /** A stable string for change detection. */
    snapshot() {
        return JSON.stringify(this.values());
    }

    get dirty() {
        return this.snapshot() !== this.clean;
    }

    /** Loads a saved note into the fields. */
    load(note) {
        this.id = note.id;
        this.f.title.value = note.title ?? "";
        this.f.body.value = note.body ?? "";
        this.f.instrument.value = note.instrument ?? "";
        this.f.dut.value = note.dut ?? "";
        this.f.tags.value = (note.tags ?? []).join(", ");
        this.clean = this.snapshot();
        this.setSaveState("");
        this.refreshPreview();
    }

    /** Resets to a blank, unsaved note and focuses the title. */
    blank() {
        this.id = null;
        for (const el of Object.values(this.f)) el.value = "";
        this.clean = this.snapshot();
        this.setSaveState("");
        this.refreshPreview();
        this.f.title.focus();
    }

    onInput() {
        clearTimeout(this.idleTimer);
        // Saving on an idle timer means the user never has to think about it, and
        // never loses a note to a closed window. Friction is what stops people
        // writing things down, and a note nobody wrote is the failure case here.
        this.idleTimer = setTimeout(() => this.save(), IDLE_SAVE_MS);
        this.setSaveState("unsaved");

        clearTimeout(this.previewTimer);
        this.previewTimer = setTimeout(() => this.refreshPreview(), PREVIEW_MS);
    }

    /**
     * Saves if there is anything to save.
     * @returns {Promise<void>}
     */
    async save() {
        clearTimeout(this.idleTimer);

        if (this.saving) {
            // Remember that more edits arrived, and save again once this one lands.
            this.dirtyDuringSave = true;
            return;
        }
        if (!this.dirty) return;
        // Never create a row for a note the user has not actually typed anything
        // into: opening the app and clicking away should leave no trace.
        if (this.id === null && this.isEmpty()) return;

        const pending = this.snapshot();
        const input = api.toNoteInput(this.values());
        this.saving = true;
        this.setSaveState("saving…");

        try {
            if (this.id === null) {
                this.id = await api.createNote(input);
            } else {
                await api.updateNote(this.id, input);
            }
            this.clean = pending;
            this.setSaveState("saved");
            this.onSaved(this.id);
        } catch (err) {
            this.setSaveState("save failed");
            this.onError(err, "save the note");
        } finally {
            this.saving = false;
            if (this.dirtyDuringSave) {
                this.dirtyDuringSave = false;
                // Edits landed mid-save; persist them too.
                void this.save();
            }
        }
    }

    isEmpty() {
        const v = this.values();
        return Object.values(v).every((s) => s.trim() === "");
    }

    setSaveState(text) {
        this.saveStateEl.textContent = text;
    }

    togglePreview() {
        this.previewOpen = !this.previewOpen;
        this.previewEl.hidden = !this.previewOpen;
        this.previewToggle.setAttribute("aria-pressed", String(this.previewOpen));
        if (this.previewOpen) this.refreshPreview();
    }

    async refreshPreview() {
        if (!this.previewOpen) return;
        const source = this.f.body.value;
        try {
            const html = await api.renderMarkdown(source);
            // The body is still the same text we rendered: a slow render must not
            // overwrite a newer one.
            if (this.f.body.value !== source) return;
            // Safe: RenderMarkdown escapes raw HTML in the note and filters link
            // schemes, so this string is markup we generated, not note content.
            this.previewEl.innerHTML = html;
        } catch (err) {
            this.onError(err, "render the preview");
        }
    }
}
