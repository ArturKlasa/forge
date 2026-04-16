# Research: Proven patterns for secret scanning, placeholder detection, protected-branch detection

**Date:** 2026-04-16
**Scope:** Three safety mechanisms Forge's design requires (Q9 stuck detection, Q10 mandatory gates, Q14 placeholder scan). Each with a recommendation grounded in open-source prior art.

## Summary of decisions

| Mechanism | Decision | Rationale |
|---|---|---|
| Secret scanning | **Embed gitleaks as a Go library** (`github.com/zricethezav/gitleaks/v8/detect`) | MIT-licensed, 222 battle-tested rules, `DetectBytes([]byte)` native API |
| Placeholder detection | **Embedded regex/heuristic table in Go** (per-language patterns) | AST not needed for v1; patterns are highly syntactic |
| Protected-branch detection | **Tiered strategy:** Forge config file → GitHub rulesets API → default-branch — always → offline convention fallback | Multiple sources of truth; graceful degradation |

---

# 1. Secret Scanning in Git Diffs

## Landscape (2026)

| Tool | Language | Stars | Last push | License | Approach |
|---|---|---|---|---|---|
| **gitleaks** | Go | ~26k | 2026-03-25 | MIT | Regex + entropy + keyword prefilter |
| **trufflehog** | Go | ~25.7k | 2026-04-15 | AGPL-3.0 | Per-provider detectors with optional live verification |
| **detect-secrets** | Python | ~4.5k | 2026-04-02 | Apache-2.0 | Plugin-based regex + baseline/allowlist |

## gitleaks — embeddable Go library

`detect` package exports:
- `detect.NewDetector(cfg config.Config) (*Detector, error)`
- `(*Detector).DetectBytes([]byte) []report.Finding`
- `(*Detector).DetectReader(io.Reader, int) []report.Finding`
- `(*Detector).DetectGit(source sources.GitSource) ([]report.Finding, error)`

Import path: `github.com/zricethezav/gitleaks/v8`. TOML rules format. Default config has **222 rules**. Keywords feed Aho-Corasick prefilter (`BobuSumisu/aho-corasick` import) — fast even with hundreds of rules.

Allowlisting: `.gitleaksignore` (fingerprint-per-line), inline `gitleaks:allow` comments, global path/regex/stopword allowlists. Default excludes `go.sum`, `package-lock.json`, `vendor/`, `node_modules/`, fonts, images.

## trufflehog — rejected

- **AGPL-3.0** — would require Forge to ship under AGPL. Non-starter for a CLI intended for broad distribution.
- **Verification calls egress.** Network-noisy in a Ralph loop; can trigger provider-side alerts.
- Heavy dependency (~100 internal packages).

## detect-secrets — rejected

Python runtime dep defeats Forge's single-binary distribution. **Baseline** concept worth borrowing: scan once, accept existing findings into `.secrets.baseline`, only new findings fail. Gitleaks already has equivalent via `.gitleaksignore`.

## Canonical regex patterns (from gitleaks default config)

| Secret | Regex |
|---|---|
| AWS access key ID | `\b((?:A3T[A-Z0-9]\|AKIA\|ASIA\|ABIA\|ACCA)[A-Z2-7]{16})\b` |
| Stripe | `\b((?:sk\|rk)_(?:test\|live\|prod)_[a-zA-Z0-9]{10,99})\b` |
| GitHub PAT (classic) | `ghp_[0-9a-zA-Z]{36}` |
| GitHub fine-grained PAT | `github_pat_\w{82}` |
| GitHub OAuth | `gho_[0-9a-zA-Z]{36}` |
| GitHub App (user/server) | `(?:ghu\|ghs)_[0-9a-zA-Z]{36}` |
| GitLab PAT | `glpat-[\w-]{20}` |
| Slack bot | `xoxb-[0-9]{10,13}-[0-9]{10,13}[a-zA-Z0-9-]*` |
| Slack webhook | `(?:https?://)?hooks\.slack\.com/(?:services\|workflows\|triggers)/[A-Za-z0-9+/]{43,56}` |
| JWT | `\b(ey[a-zA-Z0-9]{17,}\.ey[a-zA-Z0-9/\\_-]{17,}\.(?:[a-zA-Z0-9/\\_-]{10,}={0,2})?)\b` |
| Private key PEM | `(?i)-----BEGIN[ A-Z0-9_-]{0,100}PRIVATE KEY(?: BLOCK)?-----[\s\S-]{64,}?KEY(?: BLOCK)?-----` |
| Anthropic API key | `\b(sk-ant-api03-[a-zA-Z0-9_\-]{93}AA)\b` |
| 1Password service-account | `ops_eyJ[a-zA-Z0-9+/]{250,}={0,3}` |
| GCP service-account JSON | `"type":\s*"service_account"` paired with `"private_key":` |
| Generic `API_KEY=...` | `(?i)[\w.-]{0,50}?(?:access\|auth\|api\|credential\|creds\|key\|passw(?:or)?d\|secret\|token)[ \t\w.-]{0,20}['"\s]{0,3}(?:=\|:\|=>)[`'"\s=]{0,5}([\w.=-]{10,150})\b` + entropy ≥ 3.5 |

## False-positive mitigations

- **Entropy threshold** (Shannon entropy on captured group) — gitleaks `entropy = 3.0–4.5` per rule.
- **Keyword prefilter** (Aho-Corasick over rule keywords).
- **Global path allowlist** — lockfiles, vendor dirs, fonts, minified JS.
- **Global regex allowlist** — template expressions (`${VAR}`, `{{ var }}`, `%env%`), `$0..$9`, `true/false/null`.
- **Stopword list** — `abcdefghijklmnopqrstuvwxyz`, `014df517-39d1-4453-b7b3-9930c563627c` (Microsoft sample GUID).
- **Inline `gitleaks:allow` comment** — per-line opt-out.
- **`.gitleaksignore`** — fingerprints: `<commit>:<path>:<rule>:<line>`.
- **Test-file exclusion** — `_test.go`, `*.test.ts`, `tests/`, `__tests__/`, `fixtures/`.

## Recommendation for Forge

**Embed gitleaks as a Go library.**
1. Same language, clean public API, MIT license.
2. 222 battle-tested rules with entropy, keyword prefiltering, allowlisting solved.
3. Forge scans per-iteration diffs — `DetectBytes([]byte)` handles this natively. Pipe `git diff --cached` stdin.
4. Ship a curated subset as default profile; expose "full gitleaks ruleset" as flag. Users can point Forge at their existing `.gitleaks.toml`.

Fallback if Go-lib integration proves too heavy: shell out to `gitleaks detect --no-git --pipe --report-format json`. ~30ms fork overhead per iteration.

Reject trufflehog (AGPL). Reject detect-secrets (Python runtime).

---

# 2. Placeholder / Unfinished-Code Detection

## The Ralph-Huntley approach

Two mechanisms (from Huntley's post at https://ghuntley.com/ralph/ and community playbooks):
1. **Prompt-level prevention:** *"DO NOT IMPLEMENT PLACEHOLDER OR SIMPLE IMPLEMENTATIONS. WE WANT FULL IMPLEMENTATIONS."* + *"Before making changes search codebase (don't assume not implemented)."*
2. **Programmatic detection:** ripgrep-based searches for `TODO`/placeholder patterns. Huntley acknowledges this is *"non-deterministic"* — sometimes he runs a second Ralph loop to surface and queue placeholders.

Ralph himself doesn't have sophisticated placeholder detection. Forge can improve because it's a deterministic wrapper.

## AST vs regex

**Regex wins for v1.** Placeholder markers (`TODO`, `NotImplementedError`, `todo!()`, bodies of only `pass`/`return null`) are highly syntactic, rarely ambiguous. AST parsing across Go/Python/TS/Rust/Java requires tree-sitter per-language — heavy, marginal gain over well-tuned regex.

Semgrep's community rules repo (`semgrep/semgrep-rules`) does **not** ship a canonical "placeholder" ruleset. Revive has `unused-parameter` and `empty-block` but not `panic("not implemented")`. State of the art: regex heuristics with thin sanity layer.

## Concrete pattern list for v1

**Universal markers (all languages):**
```regex
\b(TODO|FIXME|XXX|HACK|BUG|REFACTOR)\b[:(]
\bNOTE:\s*(stub|placeholder|temporary|temp|wip|implement|unimplemented)
```

**Language-specific:**

| Language | Pattern | Catches |
|---|---|---|
| Go | `panic\("(?i:(not\s*(yet\s*)?implemented\|unimplemented\|todo\|TBD))"\)` | `panic("not implemented")` |
| Go | `return\s+(?:nil,\s*)?(?:fmt\|errors)\.(?:Errorf\|New)\("(?i:not\s*implemented)"\)` | stub error |
| Python | `raise\s+NotImplementedError` | standard stub |
| Python | empty-body (multiline): `^\s*(?:def\|async\s+def)\s+\w+\([^)]*\)(?:\s*->\s*[^:]+)?:\s*(?:#.*)?\n\s*(?:pass\|\.\.\.\|"""[^"]*"""\|'''[^']*''')\s*$` | `pass`/`...`/docstring-only body |
| Rust | `\b(?:todo!\|unimplemented!)\s*\(` | macros |
| Rust | `\bpanic!\s*\(\s*"(?i:not\s*implemented\|todo)` | manual panic stub |
| TS/JS | `throw\s+new\s+Error\(\s*['"\`](?i:not\s*implemented\|todo\|unimplemented)` | standard stub |
| TS/JS | `return\s+(?:null\|undefined);?\s*//\s*(?i:stub\|placeholder\|todo)` | lazy stub + comment |
| TS | `@unimplemented\b` | decorator |
| Java/Kotlin | `throw\s+new\s+UnsupportedOperationException\(` | JDK canonical |
| Java/Kotlin | `throw\s+new\s+NotImplementedException\(` | Apache Commons Lang |
| C# | `throw\s+new\s+NotImplementedException\(` | .NET standard |
| Ruby | `raise\s+NotImplementedError` | standard |
| Ruby | `raise\s+["']not\s+implemented["']` | informal |
| Any | `\.\.\.\s*(?://\|#).*(?i:todo\|fill\s*in\|replace)` | ellipsis + comment |

**Diff-specific heuristics (added lines only):**

- Empty-body detection: added function declaration followed by a body that's *only* one of `{pass, ..., return null, return undefined, return nil, return default, return (), {}, throw ..., return "";}`. Per-language lookup table keyed off function-declaration regex.
- "Return constant matching documented stub antipattern": `return 0` right after newly-added function whose name is `calculate*`/`compute*`/`process*`. Heuristic — gate behind verbose flag, re-prompt rather than escalate.

**False-positive mitigations:**
- Skip test files (`_test.go`, `*.test.ts`, `*.spec.js`, `tests/`, `__tests__/`, `test_*.py`) — tests legitimately use `NotImplementedError`.
- Skip lines with `// forge:allow-todo`.
- `.forgeignore-placeholders` file with fingerprint entries.
- Allow `TODO(issue-N)` / `TODO(#N)` / `TODO(@user)` — tracked TODO is not "I gave up."
- Python `...`: only flag when it's *entire* function body, not in `.pyi` stubs, slicing, `typing.Protocol`.

## semgrep: worth shelling out?

No for v1. ~250MB install, Python launcher, no canonical placeholder ruleset. Your patterns are "line contains X" rather than "AST node matches X" — regex gives equivalent precision at zero dependency cost.

Keep semgrep as optional `forge scan --deep` for users who have it installed.

## Recommendation for Forge

**Embed the regex list above directly in Forge**, organized as Go struct per language, compiled once at startup. Run against diff output (added lines only, filtered by filename extension).

On match:
- **Low-confidence markers** (`TODO`, `FIXME`): log, don't block. Count against "unfinished debt" ledger in iteration report.
- **High-confidence stubs** (`NotImplementedError`, `todo!()`, `panic("not implemented")`, empty-body patterns): block the commit, re-prompt with specific file + line + marker. After N re-prompts (default 3), escalate to human.

Don't shell out to semgrep. Keep optional `forge scan --deep` for AST-level checks if user opts in.

---

# 3. Protected-Branch Detection

## Does git itself have local branch protection?

**No.** Verified across sources. `git config` has no `branch.<name>.protected = true` key. Nothing in `.gitattributes`. Every tool implementing "don't commit to main locally" does it via:
1. `pre-commit`/`pre-push` hook checking `git rev-parse --abbrev-ref HEAD`, or
2. A wrapper tool (lazygit, jj, Forge) that checks before invoking `git commit`.

Local truth must come from either (a) server API, (b) Forge-managed config, or (c) default-list convention.

## GitHub

Two parallel APIs:
- **Legacy branch protection** — `GET /repos/{owner}/{repo}/branches/{branch}/protection`. 200 if protected, 404 if not. Needs `repo` scope for private repos.
- **Rulesets** (newer, from 2023, preferred) — `GET /repos/{owner}/{repo}/rules/branches/{branch}`. Returns *active rules applying to that branch name*, including rules for branches that don't exist yet. Perfect for Forge's "about to commit" check.
- **Default branch** — `GET /repos/{owner}/{repo}` → `.default_branch`. Always publicly readable for public repos.

`gh` CLI exposes via `gh api`:
```
gh api "repos/$OWNER/$REPO/rules/branches/$BRANCH"          # rulesets (preferred)
gh api "repos/$OWNER/$REPO/branches/$BRANCH/protection"     # legacy fallback
gh api "repos/$OWNER/$REPO" --jq .default_branch            # default branch
```

Uses user's existing `gh auth` token — zero extra config for Forge if `gh` is installed.

## GitLab

`GET /api/v4/projects/:id/protected_branches` returns list; `GET /api/v4/projects/:id/protected_branches/:name` returns one (404 if not protected). Wildcards returned verbatim — glob-match client-side. Default branch: `GET /api/v4/projects/:id` → `.default_branch`.

## Bitbucket

Bitbucket Cloud: `GET /2.0/repositories/{workspace}/{repo_slug}/branch-restrictions`. Bitbucket Server: `/rest/branch-permissions/2.0/projects/{projectKey}/repos/{repoSlug}/restrictions`. Less standardized — best-effort tier.

## Repo-local conventions

- **`.github/rulesets/*.json`** — real, documented. GitHub supports importing rulesets from JSON files, some repos check them in. Treat branches listed as protected without hitting API.
- `.github/branch-protection.yml` — **not** a standard; used by third-party automation but not GitHub.
- `CODEOWNERS` — tangentially related; presence doesn't imply protection.
- `pre-commit`/`pre-push` hooks with branch-name checks — grep `.git/hooks/pre-commit` and `.pre-commit-config.yaml`.

## How other tools do it

- **lazygit** — doesn't detect server-side protection. Open feature request.
- **gh** — delegates to API; no special helper.
- **jj (Jujutsu)** — `immutable_heads()` revset; user configures in `~/.jjconfig.toml`. Worth borrowing: explicit local config is more reliable than heuristics.
- **pre-commit** — community `pre-commit-hooks/no-commit-to-branch` hard-codes `--branch` list. Default: `['master']`. Extended: `--branch main --branch develop --pattern release/.*`.

## Tiered detection strategy for Forge

Run in order, stop at first "protected" signal:

1. **Forge config file** — `.forge.toml` or `.forge/config.yml` `[branches]` section listing protected patterns. Deterministic, versioned, offline. **Recommended primary.**
2. **GitHub rulesets API** (`GET /repos/{owner}/{repo}/rules/branches/{branch}`) if remote is GitHub + auth. Preferred over legacy protection (works on branches that don't exist yet).
3. **GitHub legacy protection API** as fallback if rulesets endpoint empty.
4. **GitLab/Bitbucket equivalents** based on `git remote get-url origin` parse.
5. **Default branch from repo API** — always mark default branch as protected regardless of explicit protection config. Highest-value rule, always cheap to fetch.
6. **`.github/rulesets/` directory scan** — present? Parse JSON, extract branch patterns. Fully offline.
7. **`pre-commit-config.yaml` / `.git/hooks/pre-commit`** — grep for `no-commit-to-branch`, extract `--branch` args.
8. **Convention fallback (offline / no auth)**:
   - Exact: `main`, `master`, `trunk`, `develop`, `development`, `staging`, `production`, `prod`, `release`
   - Patterns: `release/*`, `hotfix/*`, `env/*`
   - Plus current default branch from `git symbolic-ref refs/remotes/origin/HEAD`

Matches `pre-commit-hooks/no-commit-to-branch` + Gitflow conventions.

## Recommendation for Forge

Implement 8-tier strategy. Tier 1 (`.forge.toml`) takes precedence — offline-safe, versionable, explicit control. Cache API results (tiers 2–5) for Forge session duration.

Offline/no-auth default-protected list:
```toml
[branches.protected]
exact = ["main", "master", "trunk", "develop", "development", "staging", "production", "prod", "release"]
patterns = ["release/*", "hotfix/*", "env/*"]
always_include_default_branch = true
```

On protected-branch commit attempt: **always escalate to human**. Don't auto-create feature branch (violates user intent). Recommend `git checkout -b forge/<slug>` and let user confirm.

---

## Sources

**Secret scanning:**
- [gitleaks](https://github.com/gitleaks/gitleaks) + [detect.go](https://raw.githubusercontent.com/gitleaks/gitleaks/master/detect/detect.go) + [default config](https://raw.githubusercontent.com/gitleaks/gitleaks/master/config/gitleaks.toml) + [pkg docs](https://pkg.go.dev/github.com/zricethezav/gitleaks/v8/detect)
- [trufflehog](https://github.com/trufflesecurity/trufflehog)
- [detect-secrets](https://github.com/Yelp/detect-secrets) + [keyword plugin](https://raw.githubusercontent.com/Yelp/detect-secrets/master/detect_secrets/plugins/keyword.py)

**Placeholder detection:**
- [Ralph post](https://ghuntley.com/ralph/)
- [Ralph playbook](https://github.com/ghuntley/how-to-ralph-wiggum)
- [semgrep](https://github.com/semgrep/semgrep) + [rules](https://github.com/semgrep/semgrep-rules)
- [HumanLayer Ralph history](https://www.humanlayer.dev/blog/brief-history-of-ralph)

**Protected branches:**
- [GitHub branch protection](https://docs.github.com/en/rest/branches/branch-protection)
- [GitHub rulesets](https://docs.github.com/en/rest/repos/rules) + [about rulesets](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/about-rulesets)
- [GitLab protected branches](https://docs.gitlab.com/api/protected_branches/)
- [lazygit protection gap](https://github.com/jesseduffield/lazygit/issues/3716)
- [pre-commit-hooks](https://github.com/pre-commit/pre-commit-hooks)
- [Tower branch-protection primer](https://www.git-tower.com/git-hooks/protect-branch)
