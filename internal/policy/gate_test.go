package policy

import (
	"testing"
)

func makeDiff(files ...string) []byte {
	var out []byte
	for _, f := range files {
		out = append(out, []byte("diff --git a/"+f+" b/"+f+"\n--- a/"+f+"\n+++ b/"+f+"\n@@ -1 +1,2 @@\n+change\n")...)
	}
	return out
}

func TestGateScanner_Dependency(t *testing.T) {
	g := &GateScanner{}
	diff := makeDiff("package.json")
	hits := g.Scan(diff, false)
	if len(hits) == 0 {
		t.Fatal("expected gate hit for package.json")
	}
	if hits[0].Class != GateClassDependency {
		t.Errorf("expected dependency class, got %s", hits[0].Class)
	}
	if !hits[0].IsHardStop() {
		t.Error("dependency gate should be hard stop")
	}
}

func TestGateScanner_CI(t *testing.T) {
	g := &GateScanner{}
	diff := makeDiff(".github/workflows/ci.yml")
	hits := g.Scan(diff, false)
	if len(hits) == 0 {
		t.Fatal("expected gate hit for CI file")
	}
	if hits[0].Class != GateClassCI {
		t.Errorf("expected ci class, got %s", hits[0].Class)
	}
}

func TestGateScanner_SecretEnv(t *testing.T) {
	g := &GateScanner{}
	diff := makeDiff(".env.production")
	hits := g.Scan(diff, false)
	if len(hits) == 0 {
		t.Fatal("expected gate hit for .env.production")
	}
	if hits[0].Class != GateClassSecretEnv {
		t.Errorf("expected secret-env class, got %s", hits[0].Class)
	}
}

func TestGateScanner_LockfileOnlyTestsPassing(t *testing.T) {
	g := &GateScanner{}
	diff := makeDiff("yarn.lock")
	hits := g.Scan(diff, true)
	if len(hits) == 0 {
		t.Fatal("expected lockfile-only-ok hit")
	}
	if hits[0].Class != GateClassLockfileOnly {
		t.Errorf("expected lockfile-only-ok, got %s", hits[0].Class)
	}
	if hits[0].IsHardStop() {
		t.Error("lockfile-only-ok should not be hard stop")
	}
}

func TestGateScanner_LockfileOnlyTestsFailing(t *testing.T) {
	g := &GateScanner{}
	diff := makeDiff("yarn.lock")
	hits := g.Scan(diff, false)
	if len(hits) == 0 {
		t.Fatal("expected lockfile hit")
	}
	if hits[0].Class != GateClassLockfile {
		t.Errorf("expected lockfile (hard-stop) when tests failing, got %s", hits[0].Class)
	}
	if !hits[0].IsHardStop() {
		t.Error("lockfile with tests-failing should be hard stop")
	}
}

func TestGateScanner_ManifestPlusLockfileHardStop(t *testing.T) {
	g := &GateScanner{}
	diff := makeDiff("package.json", "yarn.lock")
	hits := g.Scan(diff, true)
	// manifest hit makes it a hard stop; lockfile not separately added when manifest present
	hasManifest := false
	for _, h := range hits {
		if h.Class == GateClassDependency {
			hasManifest = true
		}
	}
	if !hasManifest {
		t.Error("manifest+lockfile combo should produce dependency hit")
	}
}

func TestGateScanner_Clean(t *testing.T) {
	g := &GateScanner{}
	diff := makeDiff("internal/handler.go")
	hits := g.Scan(diff, false)
	if len(hits) != 0 {
		t.Fatalf("expected no hits for clean Go file, got %d", len(hits))
	}
}
