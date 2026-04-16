package policy

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"
)

// PlaceholderSeverity classifies detected placeholder patterns.
type PlaceholderSeverity int

const (
	// SeverityLow covers TODO/FIXME etc. — logged but don't block completion.
	SeverityLow PlaceholderSeverity = iota
	// SeverityHigh covers panic("not implemented") etc. — blocks completion.
	SeverityHigh
)

// PlaceholderHit records a single placeholder detection in an added diff line.
type PlaceholderHit struct {
	File     string
	Line     int // 1-indexed line number in the original file (best-effort from diff hunk headers)
	Pattern  string
	Text     string // trimmed added line
	Severity PlaceholderSeverity
}

// compiled pattern entry.
type phPattern struct {
	re       *regexp.Regexp
	severity PlaceholderSeverity
	name     string
}

var phPatterns []phPattern

func init() {
	defs := []struct {
		name     string
		pattern  string
		severity PlaceholderSeverity
	}{
		// High-confidence stubs — block completion.
		{"go-panic-not-impl", `panic\("(?i)(not\s*(yet\s*)?implemented|unimplemented|todo|tbd)"`, SeverityHigh},
		{"go-error-not-impl", `return\s+(?:nil,\s*)?(?:fmt|errors)\.(?:Errorf|New)\("(?i)not\s*implemented"`, SeverityHigh},
		{"py-raise-notimpl", `raise\s+NotImplementedError`, SeverityHigh},
		{"rust-todo-macro", `\b(?:todo!|unimplemented!)\s*\(`, SeverityHigh},
		{"ts-throw-notimpl", `throw\s+new\s+Error\(\s*['"\x60](?i)(not\s*implemented|todo|unimplemented)`, SeverityHigh},
		{"java-unsupported", `throw\s+new\s+UnsupportedOperationException\(`, SeverityHigh},
		{"java-notimpl", `throw\s+new\s+NotImplementedException\(`, SeverityHigh},
		{"cs-notimpl", `throw\s+new\s+NotImplementedException\(`, SeverityHigh},
		{"ruby-raise-notimpl", `raise\s+NotImplementedError`, SeverityHigh},

		// Low-confidence markers — log, count, don't block.
		{"todo-fixme", `\b(TODO|FIXME|XXX|HACK|BUG)\b[:( ]`, SeverityLow},
		{"note-stub", `\bNOTE:\s*(stub|placeholder|temporary|temp|wip|implement|unimplemented)`, SeverityLow},
	}
	for _, d := range defs {
		phPatterns = append(phPatterns, phPattern{
			re:       regexp.MustCompile(d.pattern),
			severity: d.severity,
			name:     d.name,
		})
	}
}

// isTestFile returns true for files that are legitimately allowed to contain
// placeholder-like patterns (test scaffolding, stubs, fixtures).
func isTestFile(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, "_test.go") ||
		strings.HasSuffix(lower, ".test.ts") ||
		strings.HasSuffix(lower, ".test.js") ||
		strings.HasSuffix(lower, ".spec.js") ||
		strings.HasSuffix(lower, ".spec.ts") ||
		strings.HasSuffix(lower, "test_.py") ||
		strings.Contains(lower, "/tests/") ||
		strings.Contains(lower, "/__tests__/") ||
		strings.Contains(lower, "/testdata/")
}

// isAllowedTODO returns true for tracked TODO forms like TODO(#123) or TODO(@user).
func isAllowedTODO(line string) bool {
	return regexp.MustCompile(`\bTODO\s*\([#@]`).MatchString(line)
}

// hasInlineAllow returns true when the line carries a forge:allow-todo annotation.
func hasInlineAllow(line string) bool {
	return strings.Contains(line, "forge:allow-todo")
}

// PlaceholderScanner scans added diff lines for placeholder patterns.
type PlaceholderScanner struct{}

// NewPlaceholderScanner constructs a PlaceholderScanner.
func NewPlaceholderScanner() *PlaceholderScanner { return &PlaceholderScanner{} }

// ScanDiff scans the added lines of a unified diff for placeholder patterns.
func (ps *PlaceholderScanner) ScanDiff(diff []byte) []PlaceholderHit {
	var hits []PlaceholderHit
	currentFile := ""
	lineNum := 0

	scanner := bufio.NewScanner(bytes.NewReader(diff))
	for scanner.Scan() {
		raw := scanner.Text()

		// Track current file from diff header.
		if strings.HasPrefix(raw, "+++ b/") {
			currentFile = strings.TrimPrefix(raw, "+++ b/")
			lineNum = 0
			continue
		}
		if strings.HasPrefix(raw, "--- ") || strings.HasPrefix(raw, "diff ") ||
			strings.HasPrefix(raw, "index ") || strings.HasPrefix(raw, "new file") ||
			strings.HasPrefix(raw, "deleted file") {
			continue
		}
		// Hunk header: @@ -a,b +c,d @@
		if strings.HasPrefix(raw, "@@") {
			lineNum = parseHunkStart(raw)
			continue
		}

		switch {
		case strings.HasPrefix(raw, "+"):
			lineNum++
			added := raw[1:]
			if currentFile == "" || isTestFile(currentFile) {
				continue
			}
			if hasInlineAllow(added) {
				continue
			}
			if isAllowedTODO(added) {
				continue
			}
			for _, pat := range phPatterns {
				if pat.re.MatchString(added) {
					hits = append(hits, PlaceholderHit{
						File:     currentFile,
						Line:     lineNum,
						Pattern:  pat.name,
						Text:     strings.TrimSpace(added),
						Severity: pat.severity,
					})
					break // one hit per line is enough
				}
			}
		case strings.HasPrefix(raw, "-"):
			// deleted lines: don't advance lineNum
		default:
			// context lines
			lineNum++
		}
	}
	return hits
}

// parseHunkStart extracts the destination start line from a hunk header.
// Format: @@ -a[,b] +c[,d] @@ ...
func parseHunkStart(hunk string) int {
	// find "+N" after the first "@@ "
	idx := strings.Index(hunk, " +")
	if idx < 0 {
		return 0
	}
	rest := hunk[idx+2:]
	end := strings.IndexAny(rest, ", @")
	if end < 0 {
		end = len(rest)
	}
	n := 0
	for _, c := range rest[:end] {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}
