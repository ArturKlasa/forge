package planphase

import (
	"context"
	"fmt"

	forgegit "github.com/arturklasa/forge/internal/git"
	"github.com/arturklasa/forge/internal/router"
)

// runPreGates checks dirty tree + protected branch.
// Returns the branch name to use for this run (may differ from HEAD if we switched).
func runPreGates(ctx context.Context, opts Options, path router.Path) (string, error) {
	g := opts.GitHelper
	if g == nil {
		g = forgegit.New(opts.WorkDir)
	}

	// Check dirty working tree.
	dirty, err := g.IsDirty(ctx)
	if err != nil {
		return "", fmt.Errorf("check dirty tree: %w", err)
	}
	if dirty {
		return "", fmt.Errorf("working tree has uncommitted changes — please commit or stash before running forge")
	}

	// Get current branch.
	_, branch, err := g.HEAD(ctx)
	if err != nil {
		return "", fmt.Errorf("get HEAD: %w", err)
	}

	// Check if current branch is protected.
	protected, _, err := g.IsProtected(ctx, branch, opts.ProtectedBranches)
	if err != nil {
		// Non-fatal: log and continue assuming protected.
		protected = true
	}

	if protected {
		// Auto-switch to a forge branch.
		forgeBranch := branchName(opts.Clock(), path, opts.Task)
		fmt.Fprintf(opts.Output, "Branch %q is protected — creating %q\n", branch, forgeBranch)

		if err := g.CreateBranch(ctx, forgeBranch); err != nil {
			return "", fmt.Errorf("create forge branch: %w", err)
		}
		if err := g.Checkout(ctx, forgeBranch); err != nil {
			return "", fmt.Errorf("checkout forge branch: %w", err)
		}
		return forgeBranch, nil
	}

	return branch, nil
}
