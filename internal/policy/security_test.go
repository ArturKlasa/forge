package policy

import (
	"testing"
)

func TestSecurityScanner_AWS(t *testing.T) {
	s, err := NewSecurityScanner("")
	if err != nil {
		t.Fatalf("NewSecurityScanner: %v", err)
	}
	// Inject a fake AWS access key in a diff.
	// Note: AKIAIOSFODNN7EXAMPLE is allowlisted by gitleaks (ends with EXAMPLE).
	// Use a high-entropy key that passes the stopword filter.
	diff := []byte(`diff --git a/config.go b/config.go
--- a/config.go
+++ b/config.go
@@ -1,3 +1,4 @@
+const awsKey = "AKIAY3T6Z7WQXV5MNPKR"
 func main() {}
`)
	findings := s.Scan(diff)
	if len(findings) == 0 {
		t.Fatal("expected secret finding for AWS key, got none")
	}
}

func TestSecurityScanner_InlineAllow(t *testing.T) {
	s, err := NewSecurityScanner("")
	if err != nil {
		t.Fatalf("NewSecurityScanner: %v", err)
	}
	// gitleaks:allow suppresses the finding.
	diff := []byte(`diff --git a/config.go b/config.go
--- a/config.go
+++ b/config.go
@@ -1,3 +1,4 @@
+const awsKey = "AKIAY3T6Z7WQXV5MNPKR" // gitleaks:allow
 func main() {}
`)
	findings := s.Scan(diff)
	if len(findings) != 0 {
		t.Fatalf("expected no finding with gitleaks:allow, got %d", len(findings))
	}
}

func TestSecurityScanner_Clean(t *testing.T) {
	s, err := NewSecurityScanner("")
	if err != nil {
		t.Fatalf("NewSecurityScanner: %v", err)
	}
	diff := []byte(`diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
+fmt.Println("hello, world")
`)
	findings := s.Scan(diff)
	if len(findings) != 0 {
		t.Fatalf("expected no findings in clean diff, got %d", len(findings))
	}
}
