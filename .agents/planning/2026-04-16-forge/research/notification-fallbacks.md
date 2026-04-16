# Research: Desktop notification detection + in-band fallbacks

**Date:** 2026-04-16
**Scope:** Robust escalation-signaling for Forge when desktop OS notifications silently fail.
**Purpose:** Guarantee that a walked-away user learns about escalations promptly.

## Summary of decisions

| Concern | Decision |
|---|---|
| Primary channel | OS-native desktop notification via `gen2brain/beeep` |
| Guarantee floor | Always emit a loud `===== ESCALATION =====` banner to stdout AND `/dev/tty` (bypasses `--quiet`) |
| Persistent signal | Write `.forge/current/ESCALATION` sentinel file (user can `tail -f` or watch from another terminal/phone) |
| Detection | Probe `$DBUS_SESSION_BUS_ADDRESS` / `$DISPLAY` / `$WAYLAND_DISPLAY` / `$SSH_TTY` / `$TMUX`; run live notify test in `forge doctor` with user consent |
| Terminal multiplexers | Optional secondary via `tmux display-message` if `$TMUX` set; OSC 9 for iTerm2/WezTerm/Kitty |
| WSL | Detect via `/proc/version` containing "microsoft"; use `wsl-notify-send` if present; fall back gracefully |
| Fail-loud policy | On notification failure, auto-fall-back WITHOUT silencing — write both to stdout and sentinel file |
| Auto-resolve nudge | If `forge doctor` detects notification unreliability, surface recommendation to set `--auto-resolve accept-recommended` |

---

# Desktop Notification Detection + In-Band Fallbacks

## 1. Environments where OS notifications silently fail

**Linux without a running notification daemon.** `gen2brain/beeep` uses D-Bus `org.freedesktop.Notifications`. If no daemon is listening, the D-Bus call succeeds (the bus accepts the message) but nothing renders it. **The failure is invisible to the caller.** Common cases:
- Minimal desktop environments (i3, dwm, bspwm without `dunst`/`mako`).
- `tmux`/`screen` sessions started before the desktop session's D-Bus bus was initialized (rare but real).
- Wayland sessions where the user killed their notifier.
- Ubuntu server / headless boxes.
- Docker containers.

**tmux/screen over SSH.** The D-Bus and display environment variables are set on the *remote* machine, not the user's machine. Notifications (if they appear) show up where the user isn't looking. This is the most common walk-away failure mode for developers.

**WSL1** — no X server, no D-Bus by default, no bridge to Windows notifications. WSL2 is better but still requires `wsl-notify-send` (a community binary that forwards to Windows toast) or manual scripting.

**macOS without `terminal-notifier`** — beeep falls back to `osascript display notification`, which works but shows a generic alert without an app icon or action button. Still visible; still audible.

**macOS without Terminal.app notification permission** — silent drop. User's first indication is missed escalations.

**Windows non-packaged context** — without a registered AppUserModelID, WinRT toast notifications may attribute to "PowerShell" generic identity or fail to appear depending on group policy.

## 2. Detection — predicting whether notifications will reach the user

**Environment-variable probe ladder:**

```
Linux desktop likely present:  $DBUS_SESSION_BUS_ADDRESS or $DISPLAY or $WAYLAND_DISPLAY set
Linux headless/bare:            none of the above set
SSH remote session:             $SSH_TTY or $SSH_CONNECTION set
tmux:                           $TMUX set
screen:                         $STY set
WSL:                            /proc/version contains "microsoft" or "WSL"
CI / automation:                $CI set (GitHub Actions, GitLab, CircleCI, Jenkins all set this)
```

**Canary probe on Linux:** `exec.LookPath("notify-send")` + a dbus ping:

```go
dbus-send --session --dest=org.freedesktop.DBus \
  --type=method_call --print-reply /org/freedesktop/DBus \
  org.freedesktop.DBus.ListNames
```

If this returns and `org.freedesktop.Notifications` is in the list, a daemon is registered. This is the most reliable signal. (Some environments register the name lazily, so false negatives are possible but false positives are rare.)

**macOS probe:** Try `osascript -e 'display notification "test"'` in `forge doctor` (with user consent).

**Windows probe:** beeep's call surface returns an error more reliably on Windows than on Linux — just try and check the error.

**No canonical Go library wraps all these heuristics.** The probe logic is small enough (~50 LOC) that rolling our own is right.

## 3. In-band terminal fallbacks that survive walk-aways

**Terminal bell (`\a`).** Disabled by default in many terminals (macOS Terminal.app has "Silence bell" as a common preference; tmux absorbs it unless `set-option -g bell-action any`). Audible only in some; visible bell (flash) more common. **Not reliable alone** but cheap to include.

**OSC escape sequences for terminal-level attention:**
- **OSC 9** (iTerm2-style): `\033]9;<message>\007` — iTerm2, WezTerm, Kitty, ConEmu all support. Shows a notification badge on the tab/terminal icon.
- **OSC 777** (urxvt-style): `\033]777;notify;<title>;<body>\007` — urxvt, some recent terminals.
- **OSC 99** (newer): growing support, richer notification fields.

These escape codes are harmless on terminals that don't understand them. **Forge should emit OSC 9 at minimum** — it works in the most popular modern terminals and costs nothing on others.

**Loud ASCII banners to stdout.** Guaranteed to land in scrollback, `tail -f` of log, and whatever reconnecting tool the user has. Example:

```
========================================================================
ESCALATION — Forge needs your decision
Run: forge/2026-04-16-143022-fix-login · Iteration 14 · Tier 3
Time: 2026-04-16T14:52:18Z

What Forge tried: ...
Decision: ...
Options: [p] pivot  [r] reset+retry  [s] split task  [a] abort  [d] defer
Recommendation: [r] Reset + retry

Answer:
  - keystroke in this terminal, OR
  - edit .forge/current/answer.md with `answer: <letter>`
========================================================================
```

**Direct-to-`/dev/tty`.** When stdout is redirected (`> log.txt`) or suppressed (`--quiet`), writing to `/dev/tty` bypasses and still reaches the attached terminal. Forge should write the escalation banner to `/dev/tty` *in addition to* stdout — ensures visibility even under redirection.

**Sentinel file for async observers.** Forge writes `.forge/current/ESCALATION` containing a 1-line summary + path to `awaiting-human.md`. The user can:
- `watch ls .forge/current/` — poll from another terminal.
- `tail -f .forge/current/forge.log` — same.
- Configure their shell's prompt to show "FORGE-ESCALATED" if the file exists.
- Run a phone-side cron that rsyncs the file and pushes to Pushover/ntfy.sh themselves.

This is the opt-in async channel equivalent of Telegram without shipping Telegram support.

**tmux display-message.** If `$TMUX` is set, Forge can invoke `tmux display-message "ESCALATION"` — shows in all tmux panes briefly. Not guaranteed (depends on status line config), but zero-cost addition.

## 4. How comparable long-running tools handle this

- **`terraform apply`, `cargo build`, `make`** — just print and wait. No notification. User is expected to be watching.
- **`gh run watch`** — polls and prints status; no notification. Tab-icon OSC not used.
- **`docker run -it`** — interactive; no notification (bell on error possible).
- **`cargo watch`** — uses `notify-rust` if desktop available; silent fallback.
- **`just` / `mask` / `task`** — no notifications.
- **`npm install`** — prints summary; sometimes rings terminal bell on warnings.
- **`tilt`** — displays within its own TUI dashboard; optional webhook.
- **`k9s`** — TUI-only.
- **VSCode compile tasks** — show notification via editor, not OS directly.

**Summary:** long-running CLI tools don't reliably try to get the user's attention. Forge's use case — multi-hour runs with mandatory human approval — is unusual enough that the mainstream pattern ("just print and wait") is insufficient.

**Precedent worth adopting:** Kubernetes operators / ArgoCD use file-flag sentinels + webhooks. GitHub Actions uses step summary in an env-pointed file. Neither maps directly but the "write-a-file-the-user-can-tail" pattern is proven.

## 5. Concrete recommendation for Forge

### At `forge doctor` time

1. Detect execution context:
   - Linux: DBUS/display/SSH/tmux/CI env vars.
   - macOS: check `osascript` availability; check notification-permission if possible.
   - Windows: test `beeep.Notify` with a sample message (user consent required).
2. Offer to send a test notification ("I'm going to send a test notification to verify OS notifications work. Continue? [y/N]").
3. If silent on user's side (user reports didn't see it) OR if Forge's probe suggests unreliable environment: **warn clearly** and recommend:
   - Set `--auto-resolve accept-recommended` for tolerance.
   - OR set up an out-of-band watcher on `.forge/current/ESCALATION` (show exact `tail -f` or `watch ls` command).
   - OR configure their shell prompt to signal the sentinel file.

### At runtime during an escalation

**Layered cascade, executed unconditionally in order:**

1. **Write `awaiting-human.md`** atomically (per `fsnotify-patterns.md`).
2. **Write `.forge/current/ESCALATION`** sentinel containing run id + escalation id + one-line summary + path to full file.
3. **Emit OSC 9 / bell** to stdout + `/dev/tty`.
4. **Print loud ASCII banner** to stdout (always, regardless of `--quiet`) AND to `/dev/tty` if stdout is redirected.
5. **Append escalation event** to `forge.log` (slog JSON).
6. **`tmux display-message`** if `$TMUX` set (best-effort; silently skip on failure).
7. **`beeep.Notify`** (OS native) — fire last, treat failure as expected-maybe.

### Don't trust any single channel

Even if beeep succeeds, Forge should ALWAYS do steps 1–5. The banner+sentinel is the guaranteed path; notifications are the convenience layer.

### Fail-loud policy

Under no circumstances should a notification-path failure silence the escalation. The escalation is ALWAYS in:
- `awaiting-human.md` (permanent)
- `.forge/current/ESCALATION` (sentinel, deleted on resolve)
- `forge.log` (append-only, grep-able)
- stdout (loud banner)
- `/dev/tty` (bypasses redirection)

### Logging

`forge.log` entry per escalation includes:
```json
{
  "event": "escalation.raised",
  "escalation_id": "esc-2026-04-16-143022-001",
  "tier": 3,
  "notify_attempts": [
    {"channel": "banner", "ok": true},
    {"channel": "tty", "ok": true},
    {"channel": "osc9", "ok": true},
    {"channel": "sentinel_file", "ok": true},
    {"channel": "tmux", "ok": true},
    {"channel": "beeep", "ok": false, "error": "no d-bus session"}
  ]
}
```

User reading `forge.log` hours later sees exactly which paths fired.

---

## Uncertainties flagged

- **WSL notification path** not definitively tested for `wsl-notify-send` availability in default WSL install — Forge should detect-and-use-if-present, not hard-require.
- **OSC 9 support across non-mainstream terminals** (Alacritty prior to recent versions, various Linux terminals) — graceful degradation assumed since OSC codes are safe to emit unconditionally.
- **Writing to `/dev/tty` under unusual TTY configurations** (e.g., nested `sudo`, `setsid` reparenting) — may fail silently; banner to stdout covers the fallback.
- **Corporate macOS with strict notification policy** — test-notification probe in `forge doctor` will catch but user may not realize the fix path requires IT action.

## Primary sources

- [gen2brain/beeep](https://github.com/gen2brain/beeep)
- [D-Bus Notifications spec (freedesktop.org)](https://specifications.freedesktop.org/notification-spec/notification-spec-latest.html)
- [iTerm2 proprietary escape codes](https://iterm2.com/documentation-escape-codes.html) (OSC 9 notification)
- [WezTerm escape code support](https://wezterm.org/escape-sequences.html)
- [Kitty terminal escape codes](https://sw.kovidgoyal.net/kitty/protocol-extensions/)
- [tmux man page — display-message](https://man.openbsd.org/tmux.1#display-message)
- [wsl-notify-send (community)](https://github.com/stuartleeks/wsl-notify-send)
- [no-color.org](https://no-color.org) — convention for opting out of styling
