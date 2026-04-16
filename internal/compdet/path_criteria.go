package compdet

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// PathCriteriaCheck evaluates path-specific programmatic completion criteria.
// It returns true when the path's primary programmatic signal is satisfied.
//
// Per §2.1.9:
//   - Create: target-shape.md present (plan-phase artifact confirms scope).
//   - Add: specs.md + codebase-map.md present (artifacts confirm scope).
//   - Fix: bug.md present AND diff contains a new test function/file.
//   - Refactor: invariants.md present (invariant gate confirmed).
//   - Default (unknown path): false (signal not available).
func PathCriteriaCheck(path, runDir, diff string) bool {
	switch path {
	case "create":
		return fileExists(runDir, "target-shape.md")
	case "add":
		return fileExists(runDir, "specs.md") && fileExists(runDir, "codebase-map.md")
	case "fix":
		return fileExists(runDir, "bug.md") && diffAddsTest(diff)
	case "refactor":
		return fileExists(runDir, "invariants.md")
	case "upgrade":
		return fileExists(runDir, "upgrade-scope.md")
	default:
		return false
	}
}

// fileExists reports whether name exists within dir.
func fileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// diffAddsTest reports whether the diff adds a new test function or test file.
// A new test file is a "+++ b/*_test.go" added file; a new test function is
// a "+" line matching "func Test".
func diffAddsTest(diff string) bool {
	newTestFile := regexp.MustCompile(`^\+\+\+ b/.*_test\.go`)
	newTestFunc := regexp.MustCompile(`^\+func Test`)

	for _, line := range strings.Split(diff, "\n") {
		if newTestFile.MatchString(line) || newTestFunc.MatchString(line) {
			return true
		}
	}
	return false
}
