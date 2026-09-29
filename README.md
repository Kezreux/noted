# noted

An offline logbook for a technical electronics test environment.

Record what you changed, why you changed it, and which parameters were involved.
Months later, search finds it again by keyword, parameter name, register value or
filename. A fault trace written up once takes minutes to look up instead of days
to rediscover.

Everything stays on your machine: no cloud, no sync, no accounts, no network code.

---

## Run it

A build already exists in `bin/`:

```sh
./bin/noted
```

It creates `~/Documents/noted-data/` on first run and starts with an empty note
ready to type into.

To try it without touching your real notes, point it at a scratch folder:

```sh
NOTED_DATA=$(mktemp -d) ./bin/noted
```

---

## Using it

| Key | Does |
|---|---|
| `Ctrl+N` | New note |
| `Ctrl+S` | Save now (it also autosaves) |
| `Ctrl+K` or `Ctrl+F` | Jump to search |
| `Ctrl+P` | Toggle the Markdown preview |
| `Esc` | Clear the search box, then move to the body |

Notes are Markdown. Tables are worth knowing about, since a set of parameter
values is naturally tabular:

```markdown
| param           | before | after |
|-----------------|--------|-------|
| VDD_TRIM_OFFSET | 0x1A   | 0x12  |
```

Saving is automatic: on a short pause in typing, when you leave a field, and when
the window closes. The indicator top-right shows `saved` when it has landed.

### Search

Search is substring-based, which is what technical identifiers need. It is not
word-based, so you do not have to remember the whole name:

| You type | You find |
|---|---|
| `trim` | a note containing `VDD_TRIM_OFFSET` |
| `R4` | a note mentioning the designator `R4` |
| `0x1A` | that register value |
| `cal_v2.cfg` | that filename, dots and all |
| `divider R4` | only notes containing **both** |

Several words narrow the result rather than widening it. Punctuation is handled
for you: you never need to quote or escape anything.

### Your notes are not fragile

- **Editing keeps the old text.** Every edit that changes the title or body
  stores the previous version first.
- **Deleting is a soft delete.** The note stops appearing, but the row and its
  history stay in the database.

---

## Where your data lives

```
~/Documents/noted-data/          # Windows: %USERPROFILE%\Documents\noted-data
├── noted.db                     # everything you have written
└── attachments/
```

The app shows this path in the bottom-right corner.

**Backing up means copying that folder.** There is nothing else to export. It is
under `Documents` rather than a hidden config directory precisely so you can find
it and drag it to a USB stick.

Set `NOTED_DATA` to override the location. Do **not** put it on a network share:
SQLite over SMB can corrupt the database.

Close the app before copying. On a clean shutdown the folder contains only
`noted.db`; if you copy while it is running you must take the `noted.db-wal` and
`noted.db-shm` files too, or the copy will be missing recent notes.

---

## Building it yourself

### Requirements

| Tool | Version | Install |
|---|---|---|
| Go | 1.27+ | `mise use -g go@1.27.1` |
| Node | LTS | `mise use -g node@lts` |
| Wails CLI | must match `go.mod` | see below |
| C compiler | any | `sudo pacman -S base-devel` |

The Wails v3 CLI **must be the same version as the `github.com/wailsapp/wails/v3`
line in `go.mod`**. v3 is in beta and releases near-nightly; a mismatch makes
binding generation fail in confusing ways.

```sh
grep 'wailsapp/wails/v3' go.mod        # check what go.mod wants

go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.26
```

On a Linux machine without `webkitgtk-6.0` — including this one — that install
needs the legacy tag, because the CLI itself links against the web engine:

```sh
go install -tags gtk3 github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.26
```

Check the toolchain with `wails3 doctor`.

### Build

```sh
wails3 build                 # Linux   -> bin/noted
wails3 build GOOS=windows    # Windows -> bin/noted.exe
```

No flags needed for either. The Linux build detects whether `webkitgtk-6.0` is
installed and falls back to the older GTK3 web engine automatically.

### Making the Windows .exe

```sh
wails3 build GOOS=windows
```

That produces **`bin/noted.exe`**, about 15 MB. It is a single self-contained
file — the interface is compiled into it — so copying that one file to a Windows
machine is the whole install. No Go, no Node, no runtime, no Docker and no C
cross-compiler is needed to produce it: the Windows build is pure Go
(`CGO_ENABLED=0`).

Verify what you built:

```sh
file bin/noted.exe                      # PE32+ executable ... x86-64
go version -m bin/noted.exe | grep CGO  # CGO_ENABLED=0
```

### First run on Windows

Two things will happen the first time, neither of them a fault:

1. **SmartScreen** shows *"Windows protected your PC"*, because the executable is
   unsigned. Click **More info → Run anyway**.
2. **WebView2** must be present. It is built into Windows 11. On an older or
   offline Windows 10 machine the window may open blank — install Microsoft's
   *Evergreen Standalone Installer*, which works without a network connection.

`WINDOWS-CHECKLIST.md` lists what to confirm the first time you run it on real
hardware. The Windows build is currently **compile-verified only**: it has never
been run on Windows.

---

## Development

```sh
go test ./internal/...              # all the tests; no build tags needed
go test ./internal/... -cover
wails3 dev                          # live reload: edit JS/CSS and see it
```

`wails3 dev` serves the interface from Vite, so frontend edits appear without a
rebuild. Go changes trigger a rebuild and restart.

### Layout

```
main.go              window and service registration
noteservice.go       the methods the interface calls; no SQL here
internal/store/      database, migrations, search  (never imports Wails)
internal/markdown/   Markdown to HTML for the preview
frontend/src/        api.js, notelist.js, editor.js, main.js
frontend/bindings/   GENERATED — never edit by hand
```

`internal/store` deliberately does not import Wails, so the whole data layer is
testable with plain `go test` and no window ever opens.

After changing any method signature on `NoteService`, regenerate the bindings:

```sh
wails3 generate bindings -f '-tags gtk3' -clean=true -time-type=Date
```

Drop `-f '-tags gtk3'` once `webkitgtk-6.0` is installed.

### Note on build tags

`go build ./...` and `go vet ./...` touch the root package, which links the web
engine, so on this machine they need `-tags gtk3`:

```sh
go vet -tags gtk3 ./...
```

Tests never need it, because everything tested lives under `internal/`. All of
this disappears after:

```sh
sudo pacman -S --needed webkitgtk-6.0
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.26   # no tag
```

---

## Troubleshooting

**`Package 'webkitgtk-6.0' not found`** — a `go` command hit the root package
without the tag. Use `-tags gtk3`, or install `webkitgtk-6.0`.

**`pattern all:frontend/dist: no matching files found`** — the interface has not
been built yet. Run `wails3 build`, which builds it first, or
`cd frontend && npm install && npm run build`.

**Bindings errors after changing Go code** — regenerate them (above). If they
still look wrong, check that the `wails3` CLI version matches `go.mod`.

**Search finds nothing you expect** — one- and two-character searches use a
different code path from longer ones. If `R4` behaves oddly but `divider` is
fine, that boundary is worth mentioning.
