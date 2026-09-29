// notelist.js -- the sidebar: the note list, and search-as-you-type over it.

import * as api from "./api.js";

/** Search is re-run this long after the last keystroke. */
const DEBOUNCE_MS = 150;

export class NoteList {
    /**
     * @param {{list: HTMLElement, search: HTMLInputElement,
     *          clear: HTMLElement, status: HTMLElement,
     *          onSelect: (id: number) => void,
     *          onError: (err: unknown, what: string) => void}} opts
     */
    constructor(opts) {
        this.el = opts.list;
        this.searchEl = opts.search;
        this.clearEl = opts.clear;
        this.statusEl = opts.status;
        this.onSelect = opts.onSelect;
        this.onError = opts.onError;

        /** @type {number|null} */
        this.selectedID = null;
        /** @type {number|undefined} */
        this.debounceTimer = undefined;
        /**
         * Incremented on every refresh. A response whose token is stale is
         * discarded, so a slow search cannot overwrite the results of a newer
         * one -- the classic search-as-you-type race.
         */
        this.requestToken = 0;

        this.searchEl.addEventListener("input", () => this.onQueryChanged());
        this.clearEl.addEventListener("click", () => this.clearSearch());

        // One listener for the whole list rather than one per row, so rebuilding
        // the list does not leak handlers.
        this.el.addEventListener("click", (ev) => {
            const row = /** @type {HTMLElement} */ (ev.target).closest(".note-item");
            if (!row) return;
            const id = Number(row.dataset.id);
            if (Number.isFinite(id)) this.onSelect(id);
        });
    }

    get query() {
        return this.searchEl.value.trim();
    }

    focusSearch() {
        this.searchEl.focus();
        this.searchEl.select();
    }

    clearSearch() {
        this.searchEl.value = "";
        this.onQueryChanged();
        this.searchEl.focus();
    }

    onQueryChanged() {
        this.clearEl.hidden = this.searchEl.value === "";
        clearTimeout(this.debounceTimer);
        // Debounced so that typing "VDD_TRIM_OFFSET" runs one query, not fifteen.
        this.debounceTimer = setTimeout(() => this.refresh(), DEBOUNCE_MS);
    }

    /** Marks a note as selected without reloading the list. */
    select(id) {
        this.selectedID = id;
        for (const row of this.el.querySelectorAll(".note-item")) {
            row.classList.toggle("selected", Number(row.dataset.id) === id);
        }
    }

    /** Reloads the list, running a search when the box is non-empty. */
    async refresh() {
        const token = ++this.requestToken;
        const q = this.query;
        try {
            const rows = q === "" ? await api.listNotes() : await api.searchNotes(q);
            if (token !== this.requestToken) return; // a newer request has started
            this.render(rows, q);
        } catch (err) {
            if (token !== this.requestToken) return;
            this.onError(err, q === "" ? "load notes" : `search for "${q}"`);
        }
    }

    /**
     * @param {Array<any>} rows summaries (browsing) or hits (searching)
     * @param {string} query
     */
    render(rows, query) {
        this.statusEl.textContent = statusLine(rows.length, query);

        this.el.replaceChildren();
        if (rows.length === 0) {
            const li = document.createElement("li");
            li.className = "list-empty";
            li.textContent = query === ""
                ? "No notes yet. Ctrl+N starts one."
                : `Nothing matches “${query}”.`;
            this.el.append(li);
            return;
        }

        const frag = document.createDocumentFragment();
        for (const row of rows) frag.append(this.row(row));
        this.el.append(frag);
    }

    /** @param {any} row */
    row(row) {
        const li = document.createElement("li");
        li.className = "note-item";
        li.dataset.id = String(row.id);
        if (row.id === this.selectedID) li.classList.add("selected");

        const title = document.createElement("div");
        title.className = "note-title";
        // A search hit carries highlighted title segments; a plain listing does not.
        if (row.titleSegments?.length) {
            title.append(segmentsToNodes(row.titleSegments));
        } else if (row.title) {
            title.textContent = row.title;
        } else {
            title.textContent = "Untitled";
            title.classList.add("untitled");
        }
        li.append(title);

        const excerpt = document.createElement("div");
        excerpt.className = "excerpt";
        if (Array.isArray(row.excerpt)) {
            // Search result: segments, some highlighted.
            excerpt.append(segmentsToNodes(row.excerpt));
        } else if (row.excerpt) {
            // Plain listing: the start of the body, as text.
            excerpt.textContent = row.excerpt;
        }
        if (excerpt.textContent !== "") li.append(excerpt);

        const foot = document.createElement("div");
        foot.className = "note-foot";
        const when = document.createElement("span");
        when.textContent = formatWhen(row.updatedAt);
        foot.append(when);
        for (const tag of row.tags ?? []) {
            const el = document.createElement("span");
            el.className = "tag";
            el.textContent = tag;
            foot.append(el);
        }
        li.append(foot);

        return li;
    }
}

/**
 * Turns excerpt segments into DOM nodes, wrapping matches in <mark>.
 *
 * Text is set through textContent, never innerHTML, so a note containing
 * something like "<script>" is displayed as those characters rather than being
 * parsed as markup.
 *
 * @param {Array<{text: string, match: boolean}>} segments
 */
function segmentsToNodes(segments) {
    const frag = document.createDocumentFragment();
    for (const seg of segments) {
        if (seg.match) {
            const mark = document.createElement("mark");
            mark.textContent = seg.text;
            frag.append(mark);
        } else {
            frag.append(document.createTextNode(seg.text));
        }
    }
    return frag;
}

function statusLine(count, query) {
    if (query === "") return count === 0 ? "" : `${count} note${count === 1 ? "" : "s"}`;
    if (count === 0) return "no matches";
    return `${count} match${count === 1 ? "" : "es"} for “${query}”`;
}

/**
 * Formats a timestamp the way a logbook wants it: a time for today, a date for
 * anything older, because "which day was that?" is the usual question.
 *
 * The bindings are generated with -time-type=Date, so this normally receives a
 * Date already; the string branch keeps it working if that ever changes.
 *
 * @param {Date|string} value
 */
function formatWhen(value) {
    const d = value instanceof Date ? value : new Date(String(value));
    if (Number.isNaN(d.getTime())) return "";
    const now = new Date();
    const sameDay = d.toDateString() === now.toDateString();
    return sameDay
        ? d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })
        : d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}
