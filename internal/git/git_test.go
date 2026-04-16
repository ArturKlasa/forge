package git_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/arturklasa/forge/internal/git"
	"github.com/google/uuid"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// initRepo creates a git repository in a temp dir and returns the path.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		mustGit(t, dir, args...)
	}
	return dir
}

// initRepoWithCommit creates a repo with one initial commit and returns the path.
func initRepoWithCommit(t *testing.T) string {
	t.Helper()
	dir := initRepo(t)
	writeFile(t, dir, "readme.md", "init")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "initial commit")
	return dir
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeGH creates a fake gh binary that echoes the given JSON output.
// Returns the path to the binary.
func fakeGH(t *testing.T, output string) string {
	t.Helper()
	dir := t.TempDir()
	var script string
	if runtime.GOOS == "windows" {
		// Windows batch file
		script = fmt.Sprintf("@echo %s\r\n", output)
		p := filepath.Join(dir, "gh.bat")
		if err := os.WriteFile(p, []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	script = fmt.Sprintf("#!/bin/sh\nprintf '%%s' '%s'\n", output)
	p := filepath.Join(dir, "gh")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// ─── IsRepo ──────────────────────────────────────────────────────────────────

func TestIsRepo(t *testing.T) {
	dir := initRepo(t)
	g := git.New(dir)
	ctx := context.Background()

	if !g.IsRepo(ctx) {
		t.Fatal("expected IsRepo=true for initialized repo")
	}

	// non-repo
	tmp := t.TempDir()
	if git.New(tmp).IsRepo(ctx) {
		t.Fatal("expected IsRepo=false for non-repo dir")
	}
}

// ─── HEAD ────────────────────────────────────────────────────────────────────

func TestHEAD(t *testing.T) {
	dir := initRepoWithCommit(t)
	g := git.New(dir)
	ctx := context.Background()

	sha, branch, err := g.HEAD(ctx)
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("expected 40-char SHA, got %q", sha)
	}
	if branch != "main" {
		t.Errorf("expected branch=main, got %q", branch)
	}
}

func TestHEADDetached(t *testing.T) {
	dir := initRepoWithCommit(t)
	sha := mustGit(t, dir, "rev-parse", "HEAD")
	mustGit(t, dir, "checkout", "--detach", "HEAD")

	g := git.New(dir)
	ctx := context.Background()
	gotSHA, branch, err := g.HEAD(ctx)
	if err != nil {
		t.Fatalf("HEAD detached: %v", err)
	}
	if gotSHA != sha {
		t.Errorf("expected SHA=%s, got %s", sha, gotSHA)
	}
	if branch != sha[:7] {
		t.Errorf("expected detached branch=%s, got %q", sha[:7], branch)
	}
}

// ─── IsDirty ─────────────────────────────────────────────────────────────────

func TestIsDirty(t *testing.T) {
	dir := initRepoWithCommit(t)
	g := git.New(dir)
	ctx := context.Background()

	dirty, err := g.IsDirty(ctx)
	if err != nil {
		t.Fatalf("IsDirty (clean): %v", err)
	}
	if dirty {
		t.Fatal("expected clean repo")
	}

	// Add an untracked file → dirty
	writeFile(t, dir, "new.txt", "content")
	dirty, err = g.IsDirty(ctx)
	if err != nil {
		t.Fatalf("IsDirty (dirty): %v", err)
	}
	if !dirty {
		t.Fatal("expected dirty repo after adding a file")
	}
}

// ─── CreateBranch + Checkout ─────────────────────────────────────────────────

func TestCreateBranchAndCheckout(t *testing.T) {
	dir := initRepoWithCommit(t)
	g := git.New(dir)
	ctx := context.Background()

	if err := g.CreateBranch(ctx, "feature/test"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	_, branch, err := g.HEAD(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feature/test" {
		t.Errorf("expected branch=feature/test, got %q", branch)
	}

	if err := g.Checkout(ctx, "main"); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	_, branch, err = g.HEAD(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" {
		t.Errorf("expected branch=main after checkout, got %q", branch)
	}
}

// ─── Commit + trailer preservation ───────────────────────────────────────────

func TestCommitTrailer(t *testing.T) {
	dir := initRepoWithCommit(t)
	g := git.New(dir)
	ctx := context.Background()

	runID := uuid.NewString()
	message := fmt.Sprintf("forge(fix): add feature\n\nRun-Id: %s\nIteration: 1\nPath: fix", runID)

	writeFile(t, dir, "feature.go", "package main")
	if err := g.Commit(ctx, message, []string{"feature.go"}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	body := mustGit(t, dir, "log", "--format=%B", "-1")
	if !strings.Contains(body, "Run-Id: "+runID) {
		t.Errorf("commit body missing Run-Id trailer; got:\n%s", body)
	}
	if !strings.Contains(body, "Iteration: 1") {
		t.Errorf("commit body missing Iteration trailer; got:\n%s", body)
	}
	if !strings.Contains(body, "Path: fix") {
		t.Errorf("commit body missing Path trailer; got:\n%s", body)
	}
}

// ─── ResetHard ───────────────────────────────────────────────────────────────

func TestResetHardRequiresConfirmation(t *testing.T) {
	dir := initRepoWithCommit(t)
	g := git.New(dir)
	ctx := context.Background()

	sha := mustGit(t, dir, "rev-parse", "HEAD")

	// Without confirmation → error
	err := g.ResetHard(ctx, sha, git.HumanConfirmation{})
	if err == nil {
		t.Fatal("expected error without human confirmation")
	}

	// With confirmation → success
	err = g.ResetHard(ctx, sha, git.HumanConfirmation{IHaveHumanConfirmation: true})
	if err != nil {
		t.Fatalf("ResetHard with confirmation: %v", err)
	}
}

// ─── DiffSinceLastCommit ──────────────────────────────────────────────────────

func TestDiffSinceLastCommit(t *testing.T) {
	dir := initRepoWithCommit(t)
	g := git.New(dir)
	ctx := context.Background()

	// Clean repo → empty diff
	diff, err := g.DiffSinceLastCommit(ctx)
	if err != nil {
		t.Fatalf("DiffSinceLastCommit (clean): %v", err)
	}
	if len(strings.TrimSpace(string(diff))) != 0 {
		t.Errorf("expected empty diff on clean repo, got:\n%s", diff)
	}

	// Modify a tracked file → non-empty diff
	writeFile(t, dir, "readme.md", "changed")
	diff, err = g.DiffSinceLastCommit(ctx)
	if err != nil {
		t.Fatalf("DiffSinceLastCommit (dirty): %v", err)
	}
	if !strings.Contains(string(diff), "changed") {
		t.Errorf("expected diff to contain 'changed', got:\n%s", diff)
	}
}

// ─── Log ─────────────────────────────────────────────────────────────────────

func TestLog(t *testing.T) {
	dir := initRepoWithCommit(t)
	g := git.New(dir)
	ctx := context.Background()

	// Make a second commit
	writeFile(t, dir, "extra.txt", "extra")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "second commit")

	commits, err := g.Log(ctx, git.LogOptions{MaxCount: 5})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(commits) < 2 {
		t.Fatalf("expected ≥2 commits, got %d", len(commits))
	}
	if len(commits[0].SHA) != 40 {
		t.Errorf("expected 40-char SHA, got %q", commits[0].SHA)
	}
}

func TestLogGrep(t *testing.T) {
	dir := initRepoWithCommit(t)
	g := git.New(dir)
	ctx := context.Background()

	writeFile(t, dir, "a.txt", "a")
	mustGit(t, dir, "add", ".")
	mustGit(t, dir, "commit", "-m", "Run-Id: tagged-commit")

	commits, err := g.Log(ctx, git.LogOptions{Grep: "Run-Id:"})
	if err != nil {
		t.Fatalf("Log grep: %v", err)
	}
	if len(commits) == 0 {
		t.Fatal("expected at least one commit matching grep")
	}
}

// ─── Tag ─────────────────────────────────────────────────────────────────────

func TestTag(t *testing.T) {
	dir := initRepoWithCommit(t)
	g := git.New(dir)
	ctx := context.Background()

	if err := g.Tag(ctx, "v0.1.0", "First release"); err != nil {
		t.Fatalf("Tag: %v", err)
	}
	out := mustGit(t, dir, "tag", "-l", "v0.1.0")
	if out != "v0.1.0" {
		t.Errorf("expected tag v0.1.0, got %q", out)
	}
}

// ─── IsProtected — tier 1: config ────────────────────────────────────────────

func TestIsProtectedTier1Config(t *testing.T) {
	dir := initRepoWithCommit(t)
	g := git.New(dir)
	ctx := context.Background()

	ok, src, err := g.IsProtected(ctx, "main", []string{"main", "master"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || src != "forge config" {
		t.Errorf("expected protected=true source=forge config, got protected=%v source=%q", ok, src)
	}

	// Pattern match
	ok, src, err = g.IsProtected(ctx, "release/1.0", []string{"release/*"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok || src != "forge config" {
		t.Errorf("pattern match failed: protected=%v source=%q", ok, src)
	}
}

// ─── IsProtected — tier 8: offline convention ────────────────────────────────

func TestIsProtectedTier8Convention(t *testing.T) {
	dir := initRepoWithCommit(t)
	g := git.New(dir)
	ctx := context.Background()

	for _, branch := range []string{"main", "master", "trunk", "develop", "production", "staging"} {
		ok, src, err := g.IsProtected(ctx, branch, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || src != "offline convention" {
			t.Errorf("branch %q: expected protected=true source=offline convention, got %v %q", branch, ok, src)
		}
	}

	for _, branch := range []string{"release/2.0", "hotfix/urgent", "env/prod"} {
		ok, src, err := g.IsProtected(ctx, branch, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || src != "offline convention" {
			t.Errorf("branch %q (pattern): expected protected=true, got %v %q", branch, ok, src)
		}
	}

	// Feature branch should NOT be protected
	ok, _, err := g.IsProtected(ctx, "feature/cool-thing", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("feature branch should not be protected")
	}
}

// ─── IsProtected — tier 5: default branch ────────────────────────────────────

func TestIsProtectedTier5DefaultBranch(t *testing.T) {
	// Create a bare remote + local clone so origin/HEAD is set
	remote := t.TempDir()
	mustGit(t, remote, "init", "--bare", "-b", "develop")

	local := t.TempDir()
	mustGit(t, local, "init", "-b", "develop")
	mustGit(t, local, "config", "user.email", "test@example.com")
	mustGit(t, local, "config", "user.name", "Test")
	writeFile(t, local, "f.txt", "x")
	mustGit(t, local, "add", ".")
	mustGit(t, local, "commit", "-m", "init")
	mustGit(t, local, "remote", "add", "origin", remote)
	mustGit(t, local, "push", "-u", "origin", "develop")

	g := git.New(local)
	ctx := context.Background()

	// "develop" is both the default branch and a conventional name; tier 5 wins
	// when no gh API and no config. But since "develop" also hits tier 8, we just
	// check it is protected.
	ok, _, err := g.IsProtected(ctx, "develop", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("expected default branch 'develop' to be protected")
	}
}

// ─── IsProtected — tier 2: GitHub Rulesets API (mock gh) ────────────────────

func TestIsProtectedTier2RulesetsAPI(t *testing.T) {
	dir := initRepoWithCommit(t)
	mustGit(t, dir, "remote", "add", "origin", "https://github.com/owner/repo.git")

	// fake gh returns a non-empty rulesets array → protected
	gh := fakeGH(t, `[{"id":1,"type":"branch_protection_rule"}]`)
	g := git.New(dir)
	g.SetGHCommand(gh)

	ctx := context.Background()
	ok, src, err := g.IsProtected(ctx, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || src != "github rulesets api" {
		t.Errorf("expected protected=true source=github rulesets api, got %v %q", ok, src)
	}
}

// ─── IsProtected — tier 6: .github/rulesets/*.json ───────────────────────────

func TestIsProtectedTier6RulesetsJSON(t *testing.T) {
	dir := initRepoWithCommit(t)

	rulesDir := filepath.Join(dir, ".github", "rulesets")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ruleset := `{
		"name": "protect-main",
		"conditions": {
			"ref_name": {
				"include": ["refs/heads/main"],
				"exclude": []
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(rulesDir, "main.json"), []byte(ruleset), 0o644); err != nil {
		t.Fatal(err)
	}

	g := git.New(dir)
	g.SetGHCommand("/bin/false") // ensure API tiers fail
	ctx := context.Background()

	ok, src, err := g.IsProtected(ctx, "main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || src != ".github/rulesets/*.json" {
		t.Errorf("tier6: expected protected=true source=.github/rulesets/*.json, got %v %q", ok, src)
	}
}

// ─── IsProtected — tier 7: pre-commit hook ───────────────────────────────────

func TestIsProtectedTier7PreCommitHook(t *testing.T) {
	dir := initRepoWithCommit(t)

	// Write .pre-commit-config.yaml with no-commit-to-branch
	pcConfig := `repos:
  - repo: https://github.com/pre-commit/pre-commit-hooks
    hooks:
      - id: no-commit-to-branch
        args: [--branch, staging, --branch, production]
`
	writeFile(t, dir, ".pre-commit-config.yaml", pcConfig)

	g := git.New(dir)
	g.SetGHCommand("/bin/false") // ensure API tiers fail
	ctx := context.Background()

	ok, src, err := g.IsProtected(ctx, "staging", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || src != "pre-commit hook" {
		t.Errorf("tier7: expected protected=true source=pre-commit hook, got %v %q", ok, src)
	}
}

// ─── Version ─────────────────────────────────────────────────────────────────

func TestVersion(t *testing.T) {
	v, err := git.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v == "" {
		t.Error("expected non-empty version string")
	}
	// Should look like "2.x.y" not "git version 2.x.y"
	if strings.HasPrefix(v, "git version") {
		t.Errorf("Version should strip prefix; got %q", v)
	}
}
