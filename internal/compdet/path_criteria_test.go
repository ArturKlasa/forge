package compdet

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathCriteriaCheck_Create(t *testing.T) {
	dir := t.TempDir()
	// Without artifact: false.
	if PathCriteriaCheck("create", dir, "") {
		t.Error("create: expected false without target-shape.md")
	}
	// With artifact: true.
	os.WriteFile(filepath.Join(dir, "target-shape.md"), []byte("# Target Shape\n"), 0o644)
	if !PathCriteriaCheck("create", dir, "") {
		t.Error("create: expected true with target-shape.md")
	}
}

func TestPathCriteriaCheck_Add(t *testing.T) {
	dir := t.TempDir()
	if PathCriteriaCheck("add", dir, "") {
		t.Error("add: expected false without artifacts")
	}
	os.WriteFile(filepath.Join(dir, "codebase-map.md"), []byte("# Codebase Map\n"), 0o644)
	if PathCriteriaCheck("add", dir, "") {
		t.Error("add: expected false without specs.md")
	}
	os.WriteFile(filepath.Join(dir, "specs.md"), []byte("# Specs\n"), 0o644)
	if !PathCriteriaCheck("add", dir, "") {
		t.Error("add: expected true with both artifacts")
	}
}

func TestPathCriteriaCheck_Fix(t *testing.T) {
	dir := t.TempDir()
	// Without bug.md: false.
	if PathCriteriaCheck("fix", dir, "") {
		t.Error("fix: expected false without bug.md")
	}
	os.WriteFile(filepath.Join(dir, "bug.md"), []byte("# Bug\n"), 0o644)
	// With bug.md but no test in diff: false.
	if PathCriteriaCheck("fix", dir, "- some deletion") {
		t.Error("fix: expected false without test in diff")
	}
	// With bug.md and new test function: true.
	diff := "+func TestRegressionOffByOne(t *testing.T) {}"
	if !PathCriteriaCheck("fix", dir, diff) {
		t.Error("fix: expected true with test function in diff")
	}
	// With bug.md and new test file: true.
	diff2 := "+++ b/parser_test.go\n+func TestParser(t *testing.T) {}"
	if !PathCriteriaCheck("fix", dir, diff2) {
		t.Error("fix: expected true with new test file in diff")
	}
}

func TestPathCriteriaCheck_Refactor(t *testing.T) {
	dir := t.TempDir()
	if PathCriteriaCheck("refactor", dir, "") {
		t.Error("refactor: expected false without invariants.md")
	}
	os.WriteFile(filepath.Join(dir, "invariants.md"), []byte("# Invariants\n"), 0o644)
	if !PathCriteriaCheck("refactor", dir, "") {
		t.Error("refactor: expected true with invariants.md")
	}
}

func TestPathCriteriaCheck_Unknown(t *testing.T) {
	if PathCriteriaCheck("unknown", t.TempDir(), "") {
		t.Error("unknown: expected false")
	}
}

func TestDiffAddsTest(t *testing.T) {
	tests := []struct {
		name string
		diff string
		want bool
	}{
		{"empty diff", "", false},
		{"removal only", "-func TestFoo(t *testing.T) {}", false},
		{"new test func", "+func TestFoo(t *testing.T) {}", true},
		{"new test file header", "+++ b/foo_test.go", true},
		{"non-test func", "+func helperFoo() {}", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := diffAddsTest(tt.diff)
			if got != tt.want {
				t.Errorf("diffAddsTest(%q) = %v, want %v", tt.diff, got, tt.want)
			}
		})
	}
}
