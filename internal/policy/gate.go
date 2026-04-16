package policy

import (
	"path/filepath"
	"strings"
)

// GateClass categorises a policy-gate file hit.
type GateClass string

const (
	GateClassDependency   GateClass = "dependency"
	GateClassCI           GateClass = "ci"
	GateClassSecretEnv    GateClass = "secret-env"
	GateClassLockfile     GateClass = "lockfile"
	GateClassLockfileOnly GateClass = "lockfile-only-ok"
)

// GateHit records a single gate-scanner hit.
type GateHit struct {
	Class  GateClass
	File   string
	Reason string
}

// IsHardStop returns true when the hit requires a mandatory stop (not auto-OK).
func (g GateHit) IsHardStop() bool {
	return g.Class != GateClassLockfileOnly
}

// GateScanner checks diff file paths against policy gate tables.
type GateScanner struct {
	// AdditionalManifests, AdditionalCI, AdditionalSecrets allow per-repo extensibility.
	AdditionalManifests []string
	AdditionalCI        []string
	AdditionalSecrets   []string
}

// Scan returns gate hits for the diff. testsPassed affects lockfile classification.
func (g *GateScanner) Scan(diff []byte, testsPassed bool) []GateHit {
	files := diffChangedFiles(diff)
	var hits []GateHit

	hasManifest := false
	hasLockfile := false
	lockfileFiles := []string{}

	for _, f := range files {
		switch {
		case isManifest(f, g.AdditionalManifests):
			hits = append(hits, GateHit{
				Class:  GateClassDependency,
				File:   f,
				Reason: "dependency manifest modified: " + f,
			})
			hasManifest = true
		case isCI(f, g.AdditionalCI):
			hits = append(hits, GateHit{
				Class:  GateClassCI,
				File:   f,
				Reason: "CI/CD pipeline file modified: " + f,
			})
		case isSecretEnv(f, g.AdditionalSecrets):
			hits = append(hits, GateHit{
				Class:  GateClassSecretEnv,
				File:   f,
				Reason: "secrets/env file modified: " + f,
			})
		case isLockfile(f):
			lockfileFiles = append(lockfileFiles, f)
			hasLockfile = true
		}
	}

	// Lockfile logic: pure lockfile-only with tests passing → auto-OK;
	// manifest + lockfile combo → treat as hard-stop dependency hit (already recorded above).
	if hasLockfile && !hasManifest {
		cls := GateClassLockfile
		if testsPassed {
			cls = GateClassLockfileOnly
		}
		for _, lf := range lockfileFiles {
			hits = append(hits, GateHit{
				Class:  cls,
				File:   lf,
				Reason: "lockfile modified: " + lf,
			})
		}
	}

	return hits
}

// diffChangedFiles extracts file paths changed in the diff ("+++ b/" lines).
func diffChangedFiles(diff []byte) []string {
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(string(diff), "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			f := strings.TrimPrefix(line, "+++ b/")
			if !seen[f] {
				seen[f] = true
				out = append(out, f)
			}
		}
	}
	return out
}

var manifestNames = map[string]bool{
	"package.json":      true,
	"cargo.toml":        true,
	"go.mod":            true,
	"gemfile":           true,
	"pyproject.toml":    true,
	"requirements.txt":  true,
	"composer.json":     true,
	"setup.py":          true,
	"setup.cfg":         true,
	"pipfile":           true,
	"environment.yml":   true,
	"podfile":           true,
	"build.gradle":      true,
	"build.gradle.kts":  true,
	"pom.xml":           true,
	"deno.json":         true,
}

func isManifest(path string, additional []string) bool {
	base := strings.ToLower(filepath.Base(path))
	if manifestNames[base] {
		return true
	}
	// requirements-*.txt
	if strings.HasPrefix(base, "requirements-") && strings.HasSuffix(base, ".txt") {
		return true
	}
	for _, a := range additional {
		if matchGlob(a, path) {
			return true
		}
	}
	return false
}

var ciPatterns = []string{
	".github/workflows/**",
	".gitlab-ci.yml",
	"azure-pipelines.yml",
	".circleci/config.yml",
	".travis.yml",
	"jenkinsfile",
	".drone.yml",
	"bitbucket-pipelines.yml",
	"buildkite.yml",
	".buildkite/**",
	".gitlab/ci/**",
	"cloudbuild.yaml",
	"codefresh.yml",
}

func isCI(path string, additional []string) bool {
	lower := strings.ToLower(path)
	for _, pat := range ciPatterns {
		if matchGlob(pat, lower) {
			return true
		}
	}
	if strings.ToLower(filepath.Base(path)) == "jenkinsfile" {
		return true
	}
	for _, a := range additional {
		if matchGlob(a, path) {
			return true
		}
	}
	return false
}

var secretEnvNames = map[string]bool{
	".env":               true,
	"secrets.yaml":       true,
	"secrets.yml":        true,
	"credentials.json":   true,
}

func isSecretEnv(path string, additional []string) bool {
	base := strings.ToLower(filepath.Base(path))
	if secretEnvNames[base] {
		return true
	}
	// .env.* variants
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	lower := strings.ToLower(path)
	// *.pem, *.key, *.secret
	if strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") || strings.HasSuffix(lower, ".secret") {
		return true
	}
	// service-account*.json
	if strings.HasPrefix(base, "service-account") && strings.HasSuffix(base, ".json") {
		return true
	}
	for _, a := range additional {
		if matchGlob(a, path) {
			return true
		}
	}
	return false
}

var lockfileNames = map[string]bool{
	"package-lock.json": true,
	"yarn.lock":         true,
	"pnpm-lock.yaml":    true,
	"cargo.lock":        true,
	"go.sum":            true,
	"gemfile.lock":      true,
	"poetry.lock":       true,
	"uv.lock":           true,
	"composer.lock":     true,
	"podfile.lock":      true,
}

func isLockfile(path string) bool {
	return lockfileNames[strings.ToLower(filepath.Base(path))]
}

// matchGlob supports simple ** and * glob patterns against a slash-separated path.
func matchGlob(pattern, path string) bool {
	pattern = strings.ToLower(pattern)
	path = strings.ToLower(path)
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return strings.HasPrefix(path, prefix+"/") || path == prefix
	}
	ok, _ := filepath.Match(pattern, path)
	if ok {
		return true
	}
	// also match against basename
	ok, _ = filepath.Match(pattern, filepath.Base(path))
	return ok
}
