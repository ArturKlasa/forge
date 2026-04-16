# Research: fsnotify patterns for bidirectional user-editable files

**Date:** 2026-04-16
**Scope:** Reliable fsnotify for files concurrently written by Forge and edited by user editors (vim/VSCode/Emacs/JetBrains/etc.)
**Purpose:** Support the `awaiting-human.md` + `answer.md` mailbox protocol.

## Summary of decisions

| Concern | Decision |
|---|---|
| Watch target | **Directory, not file** (single-file watches die on atomic-rename) |
| Event filtering | Match by `filepath.Base(Event.Name)`; ignore dotfiles, sidecars (`*.swp`, `*~`, `___jb_*___`, `#*#`, `4913`) |
| Atomic writes by Forge | `google/renameio/v2` on Unix, `natefinch/atomic` on Windows, behind a unified `AtomicWrite(path, data)` shim |
| Debounce interval | **250 ms** (absorbs 3–9 event bursts from editors) |
| Size-stability check | `os.Stat` twice 20 ms apart; require identical `Size`+`ModTime` before parsing |
| Mailbox protocol | Forge writes `awaiting-human.md`; user writes `answer.md`; validate `id:` matches; delete on consume |
| Parser tolerance | Normalize CRLF → LF; require explicit `id:` + body terminator |
| Network FS fallback | Polling (2s interval) instead of fsnotify |

---

# Forge fsnotify Research Note: Reliable Bidirectional File Communication

## 1. How popular editors save files — the actual syscalls

Different editors use fundamentally different save strategies.

**vim / neovim (`:w`, defaults).** `writebackup=on` + `backupcopy=auto` default: typically renames original to backup, writes fresh file, deletes backup on success. `backupcopy=no` changes inode; `backupcopy=yes` preserves it. Many distros/LSP configs ship `nowritebackup`. Net: atomic-rename save by default; target inode can change on every save.

**VSCode / Cursor.** Truncate-and-overwrite on original path — no rename. Microsoft has open issue ([#98063](https://github.com/microsoft/vscode/issues/98063)) for optional atomic saves; recent versions expose `files.experimentalAtomicWrites`. Watcher uses ParcelWatcher (recursive) and Node `fs.watch`.

**Emacs.** Default: rename-based backup. Original → `foo~`, buffer written to new file at `foo`, so target inode changes. With `(setq backup-by-copying t)`, copies instead and preserves inode.

**JetBrains IDEs.** "Safe write" default ON: writes to `file___jb_tmp___`, renames original to `file___jb_old___`, renames tmp to real name, deletes old. Inode always changes. Canonical atomic-rename save.

**Sublime Text.** `atomic_save` defaults `false` (truncate-and-overwrite). When enabled, writes to sibling temp + renames.

**nano / micro.** Direct truncate-overwrite. With `-B`, preserves `~` backup by renaming first, but the save itself is in-place.

**Canonical syscall sequences Forge must handle:**
1. **Overwrite** (VSCode, nano, Sublime default): `open(O_WRONLY|O_TRUNC) → write* → close`. Inode preserved.
2. **Atomic-rename** (JetBrains, vim default, emacs default, Sublime with atomic_save): `open(tmp, O_CREAT) → write* → fsync → close(tmp) → rename(tmp, target)`. Inode changes. **Watch on file-by-inode is lost.**
3. **Backup-rename-plus-write** (vim with backup, some emacs configs): three-way shuffle plus sidecar files (`foo~`, `.foo.un~`, `.foo.swp`).

## 2. fsnotify event sequences Forge will see

**Linux (inotify).** The fsnotify README is explicit:

> "Watching individual files (rather than directories) is generally not recommended as many programs (especially editors) update files atomically: it will write to a temporary file which is then moved to destination, overwriting the original… The watcher on the original file is now lost, as that no longer exists."

Directory watch for atomic-rename editors delivers `CREATE` (tmp), `MODIFY`s, `MOVED_FROM` (tmp), `MOVED_TO` (target). Mapped to `Create` and `Rename` on old name + `Create` on new name.

Per-editor on Linux:
- **vim (default):** `ATTRIB`, `MOVED_FROM`, `MOVED_TO`; file-direct watch gets `MOVE_SELF` + `IGNORED` + **dead**.
- **JetBrains:** `CREATE(___jb_tmp___)`, `MODIFY`, `CLOSE_WRITE`, `MOVED_FROM(file)`, `MOVED_TO(___jb_old___)`, `MOVED_FROM(___jb_tmp___)`, `MOVED_TO(file)`, `DELETE(___jb_old___)`.
- **VSCode / nano (truncate):** `MODIFY`s, `CLOSE_WRITE` — watch survives.
- **Sublime atomic:** `CREATE(tmp)`, `MODIFY`, `MOVED_TO(file)`.

**The critical bug.** Per `inotify(7)`: watched-file move/delete emits `IN_MOVE_SELF`/`IN_DELETE_SELF` + `IN_IGNORED`; watch descriptor is gone. Re-adding works only if something recreated the file; events between rename and re-add are missed.

**Fix.** `fsnotify.Watcher.Add(filepath.Dir(target))` and filter on `Event.Name == target`.

**macOS (kqueue).** `EVFILT_VNODE` requires open fd per watched file/dir. Atomic-rename → `NOTE_DELETE`/`NOTE_RENAME` on original fd, no further events (fd points at old unlinked inode). Same fix: watch parent directory.

**Windows (`ReadDirectoryChangesW`).** Directory-based by design. Atomic-rename produces `FILE_ACTION_ADDED` (tmp), `FILE_ACTION_MODIFIED`s, `FILE_ACTION_RENAMED_OLD_NAME`, `FILE_ACTION_RENAMED_NEW_NAME`. fsnotify on Windows does NOT remove a watch on rename (asymmetric with Linux/macOS).

**Recommendation.** Watch the directory containing `awaiting-human.md`/`answer.md`, filter by `filepath.Base(Event.Name)`, treat `Create|Write|Rename` uniformly as "re-read and re-validate."

## 3. Atomic write pattern for Forge's own writes

**Canonical Go pattern:**
```go
dir := filepath.Dir(dst)
tmp, err := os.CreateTemp(dir, ".awaiting-human.*.tmp")  // same dir ⇒ same filesystem
_, err = tmp.Write(data)
err = tmp.Sync()                                         // fsync(2) — required
err = tmp.Close()
err = os.Chmod(tmp.Name(), 0644)
err = os.Rename(tmp.Name(), dst)                         // atomic on POSIX
```
fsync is not optional; without it, `rename(2)` can expose a 0-byte file after a crash.

**Library choice.** Use [`github.com/google/renameio/v2`](https://pkg.go.dev/github.com/google/renameio/v2) on Unix. Caveat: panics/no-ops on Windows. For Windows parity use [`github.com/natefinch/atomic`](https://github.com/natefinch/atomic), which wraps `MoveFileEx(MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH)`.

**Recommendation for Forge:** small shim delegating to `renameio` on Unix, `natefinch/atomic` on Windows, behind unified `AtomicWrite(path, data)` call.

**Rename atomicity.** POSIX `rename(2)` is atomic w.r.t. path resolution — concurrent `open(target)` sees either old or new, never torn/empty. Readers holding an fd keep reading old (now unlinked) inode until they close/reopen — Forge must reopen on each wake.

**Windows.** Go's `os.Rename` calls `MoveFileEx` with `MOVEFILE_REPLACE_EXISTING` since Go 1.5. Atomicity weaker than POSIX — readers with file open can block rename with sharing violation. Retry on `ERROR_SHARING_VIOLATION`/`ERROR_ACCESS_DENIED` with backoff (`natefinch/atomic` does this).

## 4. Mailbox-pair protocol analysis

**Proposal recap.** Forge writes only `awaiting-human.md` (out-mailbox); human writes only `answer.md` (in-mailbox). Forge watches directory, filters `answer.md`. On event: read, parse, validate `id:` matches current escalation, process, then `os.Remove("answer.md")`.

**Failure modes + defenses:**

1. **Partial/torn reads.** Truncate-overwrite saves fire multiple `Write` events during write. Reading on first `Write` sees half-written file. *Defense:* debounce (§5) + strict parse + mandatory terminator (`---` or explicit `id:` field). On parse failure, don't consume; wait for next event.
2. **Editor sidecars.** vim: `.answer.md.swp`, `.answer.md.swo`, `4913`, `answer.md~`. JetBrains: `answer.md___jb_tmp___`, `___jb_old___`. emacs: `#answer.md#`, `.#answer.md`. Each triggers events. *Defense:* exact filename match.
3. **User deletes `answer.md` manually.** Treat `Remove` as "clear pending answer, keep escalation open."
4. **Stale answers.** `id:` validation: mismatch → log + ignore (or rename to `answer.stale.md` for auditability).
5. **Empty writes.** After consumption Forge deletes `answer.md`. If user saves empty, treat as no-op.
6. **Line-endings.** Windows `\r\n`. Normalize: `strings.ReplaceAll(s, "\r\n", "\n")` before parsing.
7. **Race: Forge reads while editor still writing.** Even with debounce, possible. `os.Stat` → check `Size`/`ModTime` → if next event within 500ms changes either, re-read. Or require last-line sentinel (`# end`).
8. **Directory does not exist on startup.** Create eagerly; fsnotify can't watch non-existent dir.

**Robustness recommendations:**
- Watch parent directory, not files.
- Filter exactly `{awaiting-human.md, answer.md}` by basename.
- Ignore basenames matching `^\..*`, `.*~$`, `.*\.sw[a-p]$`, `.*___jb_(tmp|old)___$`, `^#.*#$`, `^4913$`.
- Debounce per-file 250 ms.
- On wake: `os.ReadFile`, tolerate `ENOENT`, tolerate empty, strict parse, validate id, atomic delete on success.
- Validation shape: first line `id: <uuid>`, body until `---` terminator; reject anything else.

## 5. Debounce / deduplication

One save = 3–9 events depending on editor. Forge must coalesce.

**Canonical interval.** Community consensus (Watchman, webpack, nodemon, air): **100–300 ms**. Recommend **250 ms** for Forge:
- Responsive to human answering.
- Absorbs 3-event atomic-rename burst and 4–6 event JetBrains shuffle (~150 ms on slow disks).

**Implementation.** Roll-your-own is idiomatic:
```go
var timer *time.Timer
reset := func() {
    if timer != nil { timer.Stop() }
    timer = time.AfterFunc(250*time.Millisecond, handleStable)
}
```

**Caveat:** debounce alone insufficient for large files on slow disks. Combine with **size-stability check**: after timer fires, `os.Stat` twice 20 ms apart; require identical `Size`+`ModTime` before parsing.

## 6. Testing strategy

**Unit tests** — emulate save strategies against temp dir:
- Truncate-overwrite: `os.WriteFile(path, data, 0644)`.
- Atomic-rename: `renameio.WriteFile(path, data, 0644)` or manual `CreateTemp+Rename`.
- Vim-style: rename original → `.bak`, write new, remove `.bak`.
- JetBrains-style: write `___jb_tmp___`, rename original → `___jb_old___`, rename tmp → target, remove old.

For each: assert watcher observes change, parses answer, deletes file, doesn't fire on siblings.

**Integration tests:** drive real editors headless on CI. vim: `vim -Es -c ':wq' answer.md` with content piped.

**Flakiness controls:** N=100 iterations; `GOMAXPROCS=1` and `=8`; `t.TempDir()`; seeded rand.

**Cross-platform CI:** Linux (inotify), macOS (kqueue), Windows (ReadDirectoryChangesW) — event shapes differ meaningfully; bugs hide per-backend.

---

## Uncertainties flagged

- **Neovim `nowritebackup` defaults** in LSP setups — don't assume either way.
- **VSCode atomic-write flag naming** — has moved between `files.useExperimentalFileWatcher`, `files.experimentalAtomicWrites`. Directory-watch strategy is resilient either way.
- **Windows rename under AV software** — scanners can briefly lock newly-renamed files. Retry 10ms backoff, up to ~500ms.
- **NFS/SSHFS/SMB** — fsnotify unreliable or absent. Polling fallback (2s) when directory detected as remote via `statfs` (see `process-lifecycle.md` §6).
- **emacs `backup-by-copying-when-linked`** and similar edge cases — directory watch catches them regardless.

## Primary sources

- [fsnotify README](https://github.com/fsnotify/fsnotify) + [pkg.go.dev](https://pkg.go.dev/github.com/fsnotify/fsnotify)
- fsnotify issues: [#17](https://github.com/fsnotify/fsnotify/issues/17), [#214](https://github.com/fsnotify/fsnotify/issues/214), [#372](https://github.com/fsnotify/fsnotify/issues/372), [#553](https://github.com/fsnotify/fsnotify/issues/553)
- [inotify(7) man page](https://man7.org/linux/man-pages/man7/inotify.7.html)
- [Guard editor event analysis](https://github.com/guard/guard/wiki/Analysis-of-inotify-events-for-different-editors)
- [Apple Kernel Queues guide](https://developer.apple.com/library/archive/documentation/Darwin/Conceptual/FSEvents_ProgGuide/KernelQueues/KernelQueues.html)
- [Microsoft ReadDirectoryChangesW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-readdirectorychangesw)
- [google/renameio](https://github.com/google/renameio), [natefinch/atomic](https://github.com/natefinch/atomic)
- [Stapelberg: Atomically writing files in Go](https://michael.stapelberg.ch/posts/2017-01-28-golang_atomically_writing/)
- [GNU Emacs Lisp: Rename or Copy](https://www.gnu.org/software/emacs/manual/html_node/elisp/Rename-or-Copy.html)
- [JetBrains safe-write](https://intellij-support.jetbrains.com/hc/en-us/community/posts/206864695)
- [VSCode File-Watcher-Internals](https://github.com/microsoft/vscode/wiki/File-Watcher-Internals) + [atomic-save request #98063](https://github.com/microsoft/vscode/issues/98063)
- [Sublime atomic_save #379](https://github.com/sublimehq/sublime_text/issues/379)
- [Go os.Rename Windows #8914](https://github.com/golang/go/issues/8914) + [commit 92c5736](https://github.com/golang/go/commit/92c57363e0b4d193c4324e2af6902fe56b7524a0)
