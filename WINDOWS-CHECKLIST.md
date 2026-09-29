# Windows verification checklist

`noted` is developed on Linux and used on Windows 10/11. The Windows build is
produced by cross-compiling and is **verified only as far as "it compiles and is
a valid PE binary"** — nothing below has been run on real Windows hardware.

Work through this the first time you copy `bin/noted.exe` to a lab PC, and tick
items off in this file as you confirm them.

## How the build is produced

```
wails3 build GOOS=windows      # -> bin/noted.exe
```

Confirmed automatically on Linux:

- [x] Produces a `PE32+ executable for MS Windows (GUI), x86-64`
- [x] Built with **`CGO_ENABLED=0`** — the pure-Go constraint holds, so no MinGW,
      no Docker, no C toolchain is involved
- [x] Frontend assets embedded (`//go:embed all:frontend/dist`), so the `.exe`
      is the only file to copy

## To check on a real Windows machine

### First launch

- [ ] **SmartScreen.** The unsigned `.exe` shows "Windows protected your PC" on
      first run. *More info → Run anyway.* Expected, not a fault.
- [ ] **WebView2 present.** Built into Windows 11. On an older or offline
      Windows 10 PC it may be missing, and the window will open blank or fail.
      Fix with Microsoft's *Evergreen Standalone Installer* (works offline).
- [ ] The window opens at a sensible size and is resizable.

### Data folder

- [ ] The path shown bottom-right reads `C:\Users\<you>\Documents\noted-data`.
- [ ] `noted.db` and `attachments\` are created there on first run.
- [ ] Notes survive closing and reopening the app.
- [ ] Closing the app cleanly leaves **only** `noted.db` — no `-wal` or `-shm`
      file. That is what makes "back up = copy the folder" true. If a `-wal`
      file lingers, the shutdown checkpoint did not run.
- [ ] Copy `noted-data` to a USB stick, open the copy elsewhere, confirm the
      notes and search are intact.

### The UI engine differs

Linux dev uses WebKitGTK; Windows uses WebView2, which is Chromium. Layout and
font rendering can differ.

- [ ] Sidebar and editor are side by side, filling the window height.
- [ ] The three metadata fields sit on one row and do not overflow.
- [ ] The monospace body font resolves (Cascadia Code or Consolas should be
      present on Windows; otherwise it falls back).
- [ ] Preview pane: headings, tables, code blocks and blockquotes all render.
- [ ] No horizontal scrollbar on the whole page.

### Search — the part that matters

Type these into the search box and confirm each behaves as on Linux:

- [ ] `trim` finds a note containing `VDD_TRIM_OFFSET` (substring, trigram index)
- [ ] `R4` finds a note mentioning `R4` (below the trigram floor, LIKE fallback)
- [ ] `cal_v2.cfg` finds it and does **not** error (FTS5 quoting)
- [ ] `divider R4` narrows to notes containing both
- [ ] Matches are highlighted in the result list
- [ ] Typing feels instant

### Keyboard

- [ ] `Ctrl+N` new note, `Ctrl+S` save, `Ctrl+K` / `Ctrl+F` search,
      `Ctrl+P` preview, `Esc` clears search
- [ ] These do not collide with anything WebView2 handles itself

### Paths and files

- [ ] A note containing Windows paths (`C:\data\cal_v2.cfg`) saves and searches
      correctly — backslashes must not be mangled
- [ ] Deleting a note works (soft delete; the row survives in the database)

## Known differences by design

- Linux dev builds use **CGO** (GTK/WebKit); Windows builds do not. This is
  expected — see design.md §2.
- Linux currently builds against the legacy **GTK3/WebKit2GTK 4.1** path via
  `EXTRA_TAGS=gtk3`, because `webkitgtk-6.0` is not installed on the dev machine.
  This affects the Linux build only; Windows always uses WebView2.
