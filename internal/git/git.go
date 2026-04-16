package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// HumanConfirmation is a type-level safety gate for destructive git operations.
type HumanConfirmation struct {
	IHaveHumanConfirmation bool
}

// Commit represents a single git commit entry.
type Commit struct {
	SHA     string
	Message string
	Author  string
	Date    time.Time
}

// LogOptions controls git log output.
type LogOptions struct {
	MaxCount int
	Grep     string // filter by commit message substring
}

// Git wraps git operations for a repository directory.
type Git struct {
	dir       string
	timeout   time.Duration
	ghCommand string // path to gh CLI binary; default "gh"
}

// New creates a Git helper rooted at dir.
func New(dir string) *Git {
	return &Git{
		dir:       dir,
		timeout:   30 * time.Second,
		ghCommand: "gh",
	}
}

// SetGHCommand overrides the gh binary path (for tests).
func (g *Git) SetGHCommand(cmd string) { g.ghCommand = cmd }

// run executes a git subcommand in g.dir and returns stdout.
func (g *Git) run(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.dir
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("git %s: %w: %s",
				strings.Join(args, " "), err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// IsRepo returns true if dir is inside a git repository.
func (g *Git) IsRepo(ctx context.Context) bool {
	_, err := g.run(ctx, "rev-parse", "--git-dir")
	return err == nil
}

// HEAD returns the current full commit SHA and branch name.
// On a detached HEAD, branch is the 7-character short SHA.
func (g *Git) HEAD(ctx context.Context) (sha, branch string, err error) {
	out, err := g.run(ctx, "rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	sha = strings.TrimSpace(string(out))

	out, err = g.run(ctx, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		n := len(sha)
		if n > 7 {
			n = 7
		}
		return sha, sha[:n], nil
	}
	return sha, strings.TrimSpace(string(out)), nil
}

// IsDirty returns true if the working tree has uncommitted changes.
// Uses git status --porcelain=v2 (stable machine-readable format).
func (g *Git) IsDirty(ctx context.Context) (bool, error) {
	out, err := g.run(ctx, "status", "--porcelain=v2")
	if err != nil {
		return false, err
	}
	return len(bytes.TrimSpace(out)) > 0, nil
}

// CreateBranch creates and checks out a new branch from current HEAD.
func (g *Git) CreateBranch(ctx context.Context, name string) error {
	_, err := g.run(ctx, "checkout", "-b", name)
	return err
}

// Checkout switches to the named branch.
func (g *Git) Checkout(ctx context.Context, branch string) error {
	_, err := g.run(ctx, "checkout", branch)
	return err
}

// Commit stages files and creates a commit with the given message.
// message is a fully pre-rendered string (including any Run-Id / Iteration / Path trailers).
// If files is empty, only already-staged changes are committed.
func (g *Git) Commit(ctx context.Context, message string, files []string) error {
	if len(files) > 0 {
		args := append([]string{"add", "--"}, files...)
		if _, err := g.run(ctx, args...); err != nil {
			return fmt.Errorf("git add: %w", err)
		}
	}
	_, err := g.run(ctx, "commit", "-m", message)
	return err
}

// CommitAll stages all changes (including untracked files) and creates a commit.
func (g *Git) CommitAll(ctx context.Context, message string) error {
	if _, err := g.run(ctx, "add", "-A"); err != nil {
		return fmt.Errorf("git add -A: %w", err)
	}
	_, err := g.run(ctx, "commit", "-m", message)
	return err
}

// ResetHard resets the working tree and index to sha.
// Requires confirm.IHaveHumanConfirmation == true as a type-level safety gate.
func (g *Git) ResetHard(ctx context.Context, sha string, confirm HumanConfirmation) error {
	if !confirm.IHaveHumanConfirmation {
		return fmt.Errorf("git.ResetHard: caller must set IHaveHumanConfirmation: true")
	}
	_, err := g.run(ctx, "reset", "--hard", sha)
	return err
}

// DiffSinceLastCommit returns the diff of all changes (staged + unstaged) since HEAD.
func (g *Git) DiffSinceLastCommit(ctx context.Context) ([]byte, error) {
	return g.run(ctx, "diff", "HEAD")
}

// Log returns commits matching opts.
func (g *Git) Log(ctx context.Context, opts LogOptions) ([]Commit, error) {
	args := []string{"log", "--format=%H\x1f%s\x1f%an\x1f%aI"}
	if opts.MaxCount > 0 {
		args = append(args, fmt.Sprintf("-n%d", opts.MaxCount))
	}
	if opts.Grep != "" {
		args = append(args, "--grep="+opts.Grep)
	}
	out, err := g.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	var commits []Commit
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "\x1f", 4)
		if len(parts) < 4 {
			continue
		}
		t, _ := time.Parse(time.RFC3339, parts[3])
		commits = append(commits, Commit{
			SHA:     parts[0],
			Message: parts[1],
			Author:  parts[2],
			Date:    t,
		})
	}
	return commits, nil
}

// Tag creates an annotated tag at HEAD.
func (g *Git) Tag(ctx context.Context, name, message string) error {
	_, err := g.run(ctx, "tag", "-a", name, "-m", message)
	return err
}

// Version returns the installed git version string (e.g. "2.43.0").
func Version(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "--version").Output()
	if err != nil {
		return "", fmt.Errorf("git --version: %w", err)
	}
	v := strings.TrimSpace(string(out))
	return strings.TrimPrefix(v, "git version "), nil
}
