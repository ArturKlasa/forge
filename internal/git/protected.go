package git

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// IsProtected checks whether branch is protected using the tiered strategy from §B.4:
//  1. .forge/config.yml git.protected_branches
//  2. GitHub Rulesets API (via gh CLI)
//  3. GitHub legacy protection API
//  4. GitLab/Bitbucket API (GitLab only; Bitbucket skipped in v1)
//  5. Default branch from origin/HEAD
//  6. .github/rulesets/*.json scan
//  7. .pre-commit-config.yaml / .git/hooks/pre-commit grep
//  8. Offline convention fallback
//
// Returns (protected, source, err). source describes which tier matched.
// Degraded-mode: API errors are logged and next tier is tried.
func (g *Git) IsProtected(ctx context.Context, branch string, configBranches []string) (bool, string, error) {
	// Tier 1: explicit config list
	for _, b := range configBranches {
		if matchBranchPattern(b, branch) {
			return true, "forge config", nil
		}
	}

	// Parse remote URL for API tiers (best-effort; errors fall through)
	remoteURL, _ := g.getRemoteURL(ctx)
	owner, repo := parseOwnerRepo(remoteURL, "github.com")

	// Tier 2: GitHub Rulesets API
	if owner != "" {
		ok, err := g.checkGitHubRulesetsAPI(ctx, owner, repo, branch)
		if err == nil {
			if ok {
				return true, "github rulesets api", nil
			}
			// API reachable but branch not protected — still fall through
		}
	}

	// Tier 3: GitHub legacy branch-protection API
	if owner != "" {
		ok, err := g.checkGitHubLegacyAPI(ctx, owner, repo, branch)
		if err == nil && ok {
			return true, "github legacy protection api", nil
		}
	}

	// Tier 4: GitLab protected-branches API (HTTP, no CLI)
	if owner != "" && strings.Contains(remoteURL, "gitlab.com") {
		ok, err := g.checkGitLabAPI(ctx, remoteURL, branch)
		if err == nil && ok {
			return true, "gitlab api", nil
		}
	}

	// Tier 5: default branch from origin/HEAD symref
	defaultBranch, err := g.getDefaultBranch(ctx)
	if err == nil && defaultBranch == branch {
		return true, "default branch", nil
	}

	// Tier 6: .github/rulesets/*.json offline scan
	ok, err := g.checkGitHubRulesetsJSON(branch)
	if err == nil && ok {
		return true, ".github/rulesets/*.json", nil
	}

	// Tier 7: pre-commit hook grep
	ok, err = g.checkPreCommitHooks(branch)
	if err == nil && ok {
		return true, "pre-commit hook", nil
	}

	// Tier 8: offline convention fallback
	if isConventionallyProtected(branch) {
		return true, "offline convention", nil
	}

	return false, "", nil
}

// DetectProtectedBranches discovers protected branches by iterating tiers until one yields results.
// Primarily used by forge doctor.
func (g *Git) DetectProtectedBranches(ctx context.Context, configBranches []string) (branches []string, source string) {
	if len(configBranches) > 0 {
		return configBranches, "forge config"
	}

	remoteURL, _ := g.getRemoteURL(ctx)
	owner, repo := parseOwnerRepo(remoteURL, "github.com")

	if owner != "" {
		// Tier 2: enumerate from rulesets API
		out, err := g.runGH(ctx, "api", fmt.Sprintf("repos/%s/%s/rulesets", owner, repo))
		if err == nil {
			var rulesets []map[string]interface{}
			if json.Unmarshal(out, &rulesets) == nil {
				var found []string
				for _, rs := range rulesets {
					found = append(found, extractRulesetBranches(rs)...)
				}
				if len(found) > 0 {
					return deduplicate(found), "github rulesets api"
				}
			}
		}
	}

	// Tier 5: default branch
	if def, err := g.getDefaultBranch(ctx); err == nil && def != "" {
		return []string{def}, "default branch"
	}

	// Tier 8: offline — scan all local branches for conventional names
	out, err := g.run(ctx, "branch", "-a", "--format=%(refname:short)")
	if err == nil {
		var found []string
		for _, b := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			b = strings.TrimPrefix(b, "origin/")
			b = strings.TrimSpace(b)
			if b != "" && isConventionallyProtected(b) {
				found = append(found, b)
			}
		}
		if len(found) > 0 {
			return deduplicate(found), "offline convention"
		}
	}

	return nil, ""
}

// ─── tier helpers ────────────────────────────────────────────────────────────

func (g *Git) checkGitHubRulesetsAPI(ctx context.Context, owner, repo, branch string) (bool, error) {
	out, err := g.runGH(ctx, "api",
		fmt.Sprintf("repos/%s/%s/rules/branches/%s", owner, repo, branch))
	if err != nil {
		return false, err
	}
	var rules []json.RawMessage
	if err := json.Unmarshal(out, &rules); err != nil {
		return false, err
	}
	return len(rules) > 0, nil
}

func (g *Git) checkGitHubLegacyAPI(ctx context.Context, owner, repo, branch string) (bool, error) {
	out, err := g.runGH(ctx, "api",
		fmt.Sprintf("repos/%s/%s/branches/%s/protection", owner, repo, branch))
	if err != nil {
		return false, err
	}
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(out, &resp); err != nil {
		return false, err
	}
	_, ok := resp["url"]
	return ok, nil
}

func (g *Git) checkGitLabAPI(_ context.Context, _ string, _ string) (bool, error) {
	// GitLab HTTP API requires a token; skip in degraded mode.
	return false, fmt.Errorf("gitlab api: not implemented in v1")
}

func (g *Git) getDefaultBranch(ctx context.Context) (string, error) {
	out, err := g.run(ctx, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", err
	}
	ref := strings.TrimSpace(string(out))
	parts := strings.Split(ref, "/")
	if len(parts) == 0 {
		return "", fmt.Errorf("unexpected ref: %s", ref)
	}
	return parts[len(parts)-1], nil
}

func (g *Git) checkGitHubRulesetsJSON(branch string) (bool, error) {
	pattern := filepath.Join(g.dir, ".github", "rulesets", "*.json")
	matches, _ := filepath.Glob(pattern)
	for _, f := range matches {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var rs map[string]interface{}
		if json.Unmarshal(data, &rs) != nil {
			continue
		}
		for _, b := range extractRulesetBranches(rs) {
			if matchBranchPattern(b, branch) {
				return true, nil
			}
		}
	}
	return false, nil
}

var noCommitRe = regexp.MustCompile(`no-commit-to-branch`)

func (g *Git) checkPreCommitHooks(branch string) (bool, error) {
	for _, relPath := range []string{
		".pre-commit-config.yaml",
		filepath.Join(".git", "hooks", "pre-commit"),
	} {
		data, err := os.ReadFile(filepath.Join(g.dir, relPath))
		if err != nil {
			continue
		}
		s := string(data)
		if noCommitRe.MatchString(s) && strings.Contains(s, branch) {
			return true, nil
		}
	}
	return false, nil
}

// ─── URL / remote helpers ─────────────────────────────────────────────────────

func (g *Git) getRemoteURL(ctx context.Context) (string, error) {
	out, err := g.run(ctx, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// parseOwnerRepo extracts owner and repo from a remote URL for the given host.
// Handles HTTPS and SSH formats.
func parseOwnerRepo(remoteURL, host string) (owner, repo string) {
	if !strings.Contains(remoteURL, host) {
		return "", ""
	}
	var ownerRepo string
	switch {
	case strings.HasPrefix(remoteURL, "git@"):
		// git@github.com:owner/repo.git
		idx := strings.Index(remoteURL, ":")
		if idx < 0 {
			return "", ""
		}
		ownerRepo = remoteURL[idx+1:]
	default:
		// https://github.com/owner/repo.git
		for _, prefix := range []string{"https://", "http://"} {
			remoteURL = strings.TrimPrefix(remoteURL, prefix)
		}
		parts := strings.SplitN(remoteURL, "/", 3) // host / owner / repo
		if len(parts) < 3 {
			return "", ""
		}
		ownerRepo = parts[1] + "/" + parts[2]
	}
	ownerRepo = strings.TrimSuffix(ownerRepo, ".git")
	parts := strings.SplitN(ownerRepo, "/", 2)
	if len(parts) < 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

// runGH executes the gh CLI with args.
func (g *Git) runGH(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, g.ghCommand, args...)
	cmd.Dir = g.dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// ─── pattern helpers ──────────────────────────────────────────────────────────

// matchBranchPattern returns true if branch matches pattern.
// Supports simple prefix/* globs.
func matchBranchPattern(pattern, branch string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == branch
	}
	parts := strings.SplitN(pattern, "*", 2)
	return strings.HasPrefix(branch, parts[0]) && strings.HasSuffix(branch, parts[1])
}

// isConventionallyProtected reports whether branch matches offline convention names.
func isConventionallyProtected(branch string) bool {
	switch branch {
	case "main", "master", "trunk", "develop", "development",
		"staging", "production", "prod", "release":
		return true
	}
	for _, prefix := range []string{"release/", "hotfix/", "env/"} {
		if strings.HasPrefix(branch, prefix) {
			return true
		}
	}
	return false
}

func extractRulesetBranches(rs map[string]interface{}) []string {
	conds, ok := rs["conditions"].(map[string]interface{})
	if !ok {
		return nil
	}
	refName, ok := conds["ref_name"].(map[string]interface{})
	if !ok {
		return nil
	}
	includes, ok := refName["include"].([]interface{})
	if !ok {
		return nil
	}
	var out []string
	for _, inc := range includes {
		s, ok := inc.(string)
		if !ok {
			continue
		}
		s = strings.TrimPrefix(s, "refs/heads/")
		if s != "" && !strings.HasPrefix(s, "~") {
			out = append(out, s)
		}
	}
	return out
}

func deduplicate(s []string) []string {
	seen := make(map[string]bool, len(s))
	out := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
