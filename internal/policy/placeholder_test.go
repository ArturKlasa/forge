package policy

import (
	"testing"
)

func TestPlaceholderScanner_GoPanic(t *testing.T) {
	ps := NewPlaceholderScanner()
	diff := []byte(`diff --git a/handler.go b/handler.go
--- a/handler.go
+++ b/handler.go
@@ -1,3 +1,5 @@
 func Handle() error {
+	panic("not implemented")
 	return nil
 }
`)
	hits := ps.ScanDiff(diff)
	if len(hits) == 0 {
		t.Fatal("expected placeholder hit for panic('not implemented'), got none")
	}
	if hits[0].Severity != SeverityHigh {
		t.Errorf("expected SeverityHigh, got %v", hits[0].Severity)
	}
}

func TestPlaceholderScanner_TODO_Tracked_Allowed(t *testing.T) {
	ps := NewPlaceholderScanner()
	// TODO(#123) is a tracked TODO — should NOT block
	diff := []byte(`diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,2 +1,3 @@
+// TODO(#123): fix this later
 func main() {}
`)
	hits := ps.ScanDiff(diff)
	for _, h := range hits {
		if h.Severity == SeverityHigh {
			t.Errorf("tracked TODO(#N) should not generate high-severity hit")
		}
	}
}

func TestPlaceholderScanner_TODO_Untracked_Low(t *testing.T) {
	ps := NewPlaceholderScanner()
	diff := []byte(`diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,2 +1,3 @@
+// TODO: fix this
 func main() {}
`)
	hits := ps.ScanDiff(diff)
	if len(hits) == 0 {
		t.Fatal("expected low-severity hit for bare TODO")
	}
	if hits[0].Severity != SeverityLow {
		t.Errorf("expected SeverityLow, got %v", hits[0].Severity)
	}
}

func TestPlaceholderScanner_TestFile_Skipped(t *testing.T) {
	ps := NewPlaceholderScanner()
	diff := []byte(`diff --git a/handler_test.go b/handler_test.go
--- a/handler_test.go
+++ b/handler_test.go
@@ -1,3 +1,5 @@
 func TestHandle(t *testing.T) {
+	panic("not implemented")
 }
`)
	hits := ps.ScanDiff(diff)
	if len(hits) != 0 {
		t.Fatalf("test files should be skipped, got %d hits", len(hits))
	}
}

func TestPlaceholderScanner_InlineAllow(t *testing.T) {
	ps := NewPlaceholderScanner()
	diff := []byte(`diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,2 +1,3 @@
+	panic("not implemented") // forge:allow-todo
`)
	hits := ps.ScanDiff(diff)
	if len(hits) != 0 {
		t.Fatalf("forge:allow-todo should suppress hit, got %d", len(hits))
	}
}

func TestPlaceholderScanner_RustTodo(t *testing.T) {
	ps := NewPlaceholderScanner()
	diff := []byte(`diff --git a/lib.rs b/lib.rs
--- a/lib.rs
+++ b/lib.rs
@@ -1,2 +1,3 @@
+    todo!()
`)
	hits := ps.ScanDiff(diff)
	if len(hits) == 0 {
		t.Fatal("expected high-severity hit for todo!()")
	}
	if hits[0].Severity != SeverityHigh {
		t.Errorf("expected SeverityHigh for todo!()")
	}
}
