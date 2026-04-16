# Research: Cross-platform process + file lifecycle

**Date:** 2026-04-16
**Scope:** Single-instance locking, subprocess-tree kill, graceful termination, dead-PID detection, network-filesystem gotchas for Forge.
**Target:** Go 1.22+ on Linux, macOS, Windows 11.

## Summary of decisions

| Concern | Decision |
|---|---|
| Single-instance lock | `github.com/gofrs/flock` + PID-file sidecar with `{pid, run_id, start_time_ns, hostname}` tuple |
| Stale-lock recovery | PID liveness + start-time comparison (defeats PID reuse); `run_id` as final guard |
| Windows subprocess-tree kill | Job Object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` via `golang.org/x/sys/windows` directly |
| Unix subprocess-tree kill | `SysProcAttr{Setpgid: true, Setsid: true}` + `syscall.Kill(-pgid, SIGTERM)` |
| Graceful escalation | `Cmd.Cancel` + `Cmd.WaitDelay` (Go 1.20+) for direct child; custom `terminateTree` helper for the group |
| Dead-PID detection | `syscall.Kill(pid, 0)` on Unix; `OpenProcess` + `GetExitCodeProcess` on Windows; compare `start_time_ns` |
| Network FS detection | `unix.Statfs` magic numbers (Linux); `Fstypename` match (macOS); `GetDriveType(root)` (Windows) |
| Network FS fallback | Polling instead of fsnotify; PID-file-only locking; in-process mutex around `ledger.jsonl` appends |

---

# Forge Cross-Platform Process & File Lifecycle Research Note

## 1. Single-instance lock file

**Library evaluation.** Four candidates:

- **`gofrs/flock`** — de facto community standard. `syscall.Flock` on Linux/Darwin, `windows.LockFileEx` on Windows. Thread-safe. Actively maintained.
- **`rogpeppe/go-internal/lockedfile`** — extracted from Go command. Higher-level `Read`/`Write`/`Transform` + `Mutex`. More ergonomic for lock+read/write flows but closer to an internal utility.
- **`juju/fslock`** — older, less active. Not recommended.
- **Stdlib-only** — you reinvent the Windows path + retry wrapper. Not worth it.

**Behavior per platform.**
- Linux: `flock(2)` advisory, per-open-file-description. NFS emulates since 2.6.12.
- macOS: BSD-style `flock(2)`; unreliable on FUSE mounts.
- Windows: `LockFileEx` mandatory; exclusive lock on a sentinel range `[0, 1)` is the canonical single-instance idiom.

**NFS / sshfs / FUSE reality.** Poettering's "[On the Brokenness of File Locking](http://0pointer.de/blog/projects/locking.html)" still applies: "there is no way to properly detect whether file locking works on a specific NFS mount." Treat network/FUSE mounts as best-effort with a warning.

**Canonical fallback strategy.** When filesystem detection (§6) flags a network/FUSE mount, fall back to **PID-file + liveness check** scheme. Log warning in `forge doctor`.

**Stale lock handling pattern:**
1. `TryLock` `.forge/lock`.
2. On success: write `{pid, run_id, start_time_ns, hostname}` JSON, keep fd open for process lifetime.
3. On failure: read existing contents, check `hostname` matches; if not, refuse (another host on shared FS).
4. Liveness check: `syscall.Kill(pid, 0)` on Unix; `OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION)` on Windows. Compare `start_time_ns` from `/proc/<pid>/stat` (Linux), `sysctl KERN_PROC_PID` (macOS), `GetProcessTimes` (Windows) vs. recorded value to defeat PID reuse.
5. If pid dead or start-time mismatches: remove stale file, retry `TryLock` once.

**Per-fd gotcha.** `flock()` locks are per-open-file-description, not per-process. Children inherit the lockfile fd only if not `O_CLOEXEC`. Go's `os.OpenFile` sets `O_CLOEXEC` by default on Unix — inherited-fd leakage not a practical risk. **Do not** put the lockfile fd in `cmd.ExtraFiles`.

**Recommendation for Forge.** `github.com/gofrs/flock` + PID/run-id sidecar JSON. Implement stale-lock recovery above. On NFS/FUSE, fall back to PID-file-only with warning.

## 2. Windows Job Objects for subprocess-tree kill

**Why `CREATE_NEW_PROCESS_GROUP` is insufficient.** Only lets you send `CTRL_BREAK_EVENT` via `GenerateConsoleCtrlEvent`. Fails on: (a) non-console subprocesses; (b) GUI subprocesses; (c) anything calling `FreeConsole()`; (d) grandchildren with their own groups; (e) no forced-kill equivalent. It's a "please clean up" hint, not a guaranteed terminator.

**Canonical solution — Job Objects.** Per [Microsoft docs](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects): *"If the job has the `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` flag specified, closing the last job object handle terminates all associated processes and then destroys the job object itself."* Combined with implicit tree-tracking (children inherit membership unless they set `CREATE_BREAKAWAY_FROM_JOB`), this is the robust answer.

**Canonical Go wrapping.** Use `golang.org/x/sys/windows` directly:
- `windows.CreateJobObject`
- Build `JOBOBJECT_EXTENDED_LIMIT_INFORMATION` with `LimitFlags: JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`
- `windows.SetInformationJobObject`
- Start child with `syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}`
- `windows.AssignProcessToJobObject(job, process_handle)`
- `windows.ResumeThread`

`kolesnikovae/go-winjob` wraps this but has limited maintenance (single v1.0.0 release, 9 commits). Use stdlib wrapping directly (~60 LOC).

**Interaction with `taskkill /T`.** Walks the parent-child tree via snapshots — racy (grandchildren may escape, PIDs may be reused between enumeration and kill). Job Objects solve at kernel level. Use `taskkill` only as last-resort fallback.

**Elevated-privilege.** A process running with higher integrity can't be in the parent's job without `SeTcbPrivilege`. If Forge launches an elevated helper (ShellExecute "runas"), that helper is in a fresh job.

**Recommendation for Forge.** One Job Object per Forge run with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE + JOB_OBJECT_LIMIT_BREAKAWAY_OK = 0`. Keep handle open for Forge process lifetime; on crash, kernel closes handle and kills tree. Direct `x/sys/windows`; no third-party wrapper.

## 3. Unix process groups for subprocess-tree kill

**`Setpgid: true` guarantees.** After `fork`, before `exec`, Go calls `setpgid(0, 0)` — child gets new PGID = its own PID. Descendants inherit unless they call `setsid()`/`setpgid()`.

**Escape cases.** Any descendant that calls `setsid()`/`setpgid()` — MCP servers that daemonize, `nohup`'d helpers, `systemd-run --user --scope`, shells with job control — escapes.

**Killing the group.** `syscall.Kill(-pgid, sig)` — `Kill` with negative pid signals the whole group. **Do not** use `cmd.Process.Kill()` (only reaps direct child).

**macOS idiosyncrasies.** `setpgid` semantics match POSIX. No `prctl(PR_SET_PDEATHSIG)` equivalent. App Sandbox / TCC can block cross-responsibility-domain signals — not an issue for dev tools launched from terminal.

**Grandchildren in their own groups (MCP daemons).** Mitigations in order:
1. Launch backend with `Setsid: true` — becomes session leader, guaranteed distinct group.
2. Before killing, traverse `/proc/<pid>/task/<tid>/children` (Linux) or `ps -o pid,ppid,pgid` (macOS) from backend PID; kill descendants whose PGID differs.
3. Linux only: open **pidfd** for race-free kill (Go 1.23+ uses internally).

**Recommendation for Forge.** Backend CLI with `SysProcAttr{Setpgid: true, Setsid: true}`. On shutdown, `syscall.Kill(-cmd.Process.Pid, SIGTERM)`, escalate to `SIGKILL` after grace. For MCP daemons that escape, track PIDs explicitly.

## 4. Graceful SIGTERM → SIGKILL escalation

**Why `exec.CommandContext` alone is insufficient.** Before Go 1.20, `CommandContext` on context-cancel called `Process.Kill()` — SIGKILL only. Issue [golang/go #21135](https://github.com/golang/go/issues/21135) was never directly implemented but solved in Go 1.20 by `Cmd.Cancel` + `Cmd.WaitDelay` fields (issue [#50436](https://github.com/golang/go/issues/50436)).

**Modern idiomatic pattern (Go 1.20+):**

```go
cmd := exec.CommandContext(ctx, bin, args...)
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
cmd.Cancel = func() error {
    return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)  // group TERM
}
cmd.WaitDelay = 10 * time.Second  // Go then SIGKILLs + closes pipes
```

For process groups, still wrap with a custom escalator because `Cmd.Cancel` fires once and `WaitDelay` only terminates direct child:

```go
func terminateTree(cmd *exec.Cmd, grace time.Duration) error {
    pgid := cmd.Process.Pid
    _ = syscall.Kill(-pgid, syscall.SIGTERM)
    done := make(chan error, 1)
    go func() { done <- cmd.Wait() }()
    select {
    case err := <-done:
        return err
    case <-time.After(grace):
        _ = syscall.Kill(-pgid, syscall.SIGKILL)
        return <-done
    }
}
```

Windows: `Cmd.Cancel` closes Job Object handle; `WaitDelay` provides hard-kill backstop for pipe drainage.

**Recommendation for Forge.** Use `Cmd.Cancel` + `Cmd.WaitDelay = 10s`. For Unix group case, also run `terminateTree` since `WaitDelay` kills only direct child.

## 5. Dead-PID detection

**Linux / macOS.** `syscall.Kill(pid, 0)` — portable POSIX idiom; existence + permission check without signaling. `ESRCH` = gone; `EPERM` = exists but can't signal (treat as alive). Prefer over `/proc` parsing or `ps`.

Linux pidfd: `pidfd_open(pid, 0)` returns handle tied to specific process. Go 1.23+ uses internally.

**Windows.** `os.FindProcess` succeeds for the lower-2-bit rounded nearby PIDs (misnomer). Use `windows.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, pid)` + `GetExitCodeProcess`; `STILL_ACTIVE` (259) means alive.

**PID reuse paranoia.** PID recycling is real: Linux default `pid_max=4194304` (rare); macOS caps ~99998; Windows PIDs multiples of 4, wrap fast.

The `Run-Id` in PID file is necessary but not sufficient alone. Guard tuple:

```
{pid, run_id, start_time_ns, hostname}
```

Compare recorded `start_time_ns` against live start time (`/proc/<pid>/stat` Linux, `sysctl kern.proc.pid.<pid>` macOS, `GetProcessTimes` Windows). If differ, PID reused — stale. `run_id` protects remaining cosmic-case collisions.

**Recommendation for Forge.** `syscall.Kill(pid, 0)` + start-time comparison on Unix; `OpenProcess + GetExitCodeProcess + GetProcessTimes` on Windows. Full tuple in PID file.

## 6. Filesystem consistency on network drives

**What breaks where.**

| Operation | NFSv3 | NFSv4 | sshfs | SMB/CIFS | Local FUSE | ext4/APFS/NTFS |
|---|---|---|---|---|---|---|
| `flock()` cross-client | emulated ≥2.6.12 | delegated | no-op | server-side, quirky | depends | solid |
| `fsnotify` | **broken** | **broken** | **broken** | **broken** | usually works | solid |
| atomic rename | mostly OK | OK | quirky | **unreliable** | depends | solid |
| `O_APPEND` atomicity | **broken** | OK | broken | depends | depends | solid |

Sources: fsnotify FAQ ("current NFS and SMB protocols do not provide network level support for file notifications"), Chris Siebenmann on NFS append complications, Microsoft renaming-network-folders troubleshooting.

Forge-specific concerns:
- **fsnotify on NFS/sshfs/SMB** — never fires. Polling fallback required.
- **flock on FUSE** — most pass as no-op. Fall back to PID + liveness.
- **atomic rename on SMB** — Windows clients can hold deferred-close handles causing `ERROR_SHARING_VIOLATION` on rename. For `ledger.jsonl`, prefer append-in-place on network shares.
- **`O_APPEND` on NFSv3** — not atomic. Interleaved bytes possible. Mitigation: flock around each append batch on NFS.

**Detection in `forge doctor` (Linux).** `unix.Statfs` + magic numbers:

```go
const (
    NFS_SUPER_MAGIC  = 0x6969
    SMB_SUPER_MAGIC  = 0x517B
    CIFS_MAGIC       = 0xFF534D42
    FUSE_SUPER_MAGIC = 0x65735546
    SMB2_MAGIC       = 0xFE534D42
)
```

**macOS detection.** `unix.Statfs` returns `Statfs_t` with `Fstypename` ("nfs", "smbfs", "macfuse", "afpfs"). Match string.

**Windows detection.** `windows.GetDriveType(root)` — `DRIVE_REMOTE` (4) is network. UNC paths (`\\server\share`) always remote.

**Recommendation for Forge.** In `forge doctor`, detect network/FUSE for repo root, `.forge/`, `$HOME`. If remote:
- Warn once.
- Disable fsnotify watchers; fall back to polling (default 2s).
- Switch single-instance locking to PID-file-only mode.
- Serialize `ledger.jsonl` appends behind in-process mutex + whole-file flock.
- Refuse Windows UNC paths by default; `--allow-network-fs` flag to override.

---

## Unknowns flagged

- **Windows 11 sandbox / MDAC behavior with Job Objects in 2026** — no 2026-specific regression found; recommend dedicated Windows 11 integration test.
- **macOS 15+ (Sequoia and later) under endpoint-security frameworks** — some enterprise EDRs block cross-group signals. Not verified.
- **pidfd on macOS** — doesn't exist. Race-free kill path is Linux-only.
- **Go 1.20's `Cmd.WaitDelay` on Windows** — interaction with a Job-Objected hung child not clearly tested in official suite. Add Windows-specific hang test.

## Primary sources

- [gofrs/flock](https://github.com/gofrs/flock)
- [rogpeppe/go-internal/lockedfile](https://pkg.go.dev/github.com/rogpeppe/go-internal/lockedfile)
- [flock(2) man page](https://www.man7.org/linux/man-pages/man2/flock.2.html)
- [kill(2) man page](https://www.man7.org/linux/man-pages/man2/kill.2.html)
- [statfs(2) man page](https://man7.org/linux/man-pages/man2/statfs.2.html)
- [Microsoft Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)
- [taskkill docs](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/taskkill)
- [golang/go #21135](https://github.com/golang/go/issues/21135), [#50436](https://github.com/golang/go/issues/50436), [#62654](https://github.com/golang/go/issues/62654), [#33814](https://github.com/golang/go/issues/33814), [#53199](https://github.com/golang/go/issues/53199)
- [os/exec Cmd.Cancel / Cmd.WaitDelay](https://pkg.go.dev/os/exec)
- [Job Object Go example](https://gist.github.com/hallazzang/76f3970bfc949831808bbebc8ca15209)
- [kolesnikovae/go-winjob](https://github.com/kolesnikovae/go-winjob)
- [Nikhil: Windows Job Objects](https://nikhilism.com/post/2017/windows-job-objects-process-tree-management/)
- [Poettering: On the Brokenness of File Locking](http://0pointer.de/blog/projects/locking.html)
- [Chris Siebenmann: NFS append complications](https://utcc.utoronto.ca/~cks/space/blog/unix/NFSServerAppendComplications)
- [fsnotify](https://github.com/fsnotify/fsnotify)
- [LWN: change notifications for network filesystems](https://lwn.net/Articles/896055/)
- [Felix Geisendörfer: killing Go process trees](https://medium.com/@felixge/killing-a-child-process-and-all-of-its-children-in-go-54079af94773)
- [Mezhenskyi: Managing Linux Processes in Go](https://mezhenskyi.dev/posts/go-linux-processes/)
- [VictoriaMetrics: graceful shutdown](https://victoriametrics.com/blog/go-graceful-shutdown/)
