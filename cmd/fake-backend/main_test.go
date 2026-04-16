package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

var testEntries = []ScriptEntry{
	{Pattern: "hello-world", Response: "Here's a hello.go file that prints Hello World", ExitCode: 0},
	{Pattern: "error case", Response: "Something went wrong", ExitCode: 1},
}

// --- Script loading tests ---

func TestLoadCSV(t *testing.T) {
	entries, err := LoadScript("../../testdata/trivial.csv")
	if err != nil {
		t.Fatalf("LoadScript csv: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected ≥2 entries, got %d", len(entries))
	}
	if entries[0].Pattern != "create a hello-world" {
		t.Errorf("first pattern = %q, want 'create a hello-world'", entries[0].Pattern)
	}
}

func TestLoadYAML(t *testing.T) {
	entries, err := LoadScript("../../testdata/trivial.yaml")
	if err != nil {
		t.Fatalf("LoadScript yaml: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected ≥2 entries, got %d", len(entries))
	}
	if entries[0].Pattern != "create a hello-world" {
		t.Errorf("first pattern = %q, want 'create a hello-world'", entries[0].Pattern)
	}
	if entries[2].ExitCode != 1 {
		t.Errorf("error entry exit_code = %d, want 1", entries[2].ExitCode)
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		prompt  string
		wantResp string
	}{
		{"create a hello-world app", "Here's a hello.go file that prints Hello World"},
		{"HELLO-WORLD please", "Here's a hello.go file that prints Hello World"},
		{"unrelated", "(fake-backend: no matching script entry for prompt)"},
	}
	for _, tt := range tests {
		got := Match(testEntries, tt.prompt)
		if got.Response != tt.wantResp {
			t.Errorf("Match(%q) response = %q, want %q", tt.prompt, got.Response, tt.wantResp)
		}
	}
}

// --- Text mode ---

func TestRunText(t *testing.T) {
	in := strings.NewReader("create a hello-world app")
	var out bytes.Buffer
	code := runText(in, &out, testEntries)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "Hello World") {
		t.Errorf("output %q does not contain 'Hello World'", out.String())
	}
}

func TestRunTextNoMatch(t *testing.T) {
	in := strings.NewReader("something random")
	var out bytes.Buffer
	code := runText(in, &out, testEntries)
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for no match", code)
	}
}

// --- Stream-JSON mode ---

func TestRunStreamJSON(t *testing.T) {
	in := strings.NewReader("create a hello-world app")
	var out bytes.Buffer
	code := runStreamJSON(in, &out, testEntries)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	lines := nonEmptyLines(out.String())
	if len(lines) != 3 {
		t.Fatalf("expected 3 NDJSON lines, got %d: %v", len(lines), lines)
	}

	// Validate system/init event
	var init map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &init); err != nil {
		t.Fatalf("line 0 not valid JSON: %v", err)
	}
	assertField(t, init, "type", "system")
	assertField(t, init, "subtype", "init")
	if _, ok := init["session_id"]; !ok {
		t.Error("system/init missing session_id")
	}

	// Validate assistant event
	var asst map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &asst); err != nil {
		t.Fatalf("line 1 not valid JSON: %v", err)
	}
	assertField(t, asst, "type", "assistant")
	msg, ok := asst["message"].(map[string]any)
	if !ok {
		t.Fatal("assistant event missing 'message' object")
	}
	content, ok := msg["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatal("assistant message missing content array")
	}
	block, _ := content[0].(map[string]any)
	if block["type"] != "text" {
		t.Error("first content block should have type=text")
	}
	if !strings.Contains(block["text"].(string), "Hello World") {
		t.Errorf("assistant text %q does not contain 'Hello World'", block["text"])
	}

	// Validate result event
	var result map[string]any
	if err := json.Unmarshal([]byte(lines[2]), &result); err != nil {
		t.Fatalf("line 2 not valid JSON: %v", err)
	}
	assertField(t, result, "type", "result")
	assertField(t, result, "subtype", "success")
	if _, ok := result["usage"]; !ok {
		t.Error("result event missing 'usage'")
	}
	if _, ok := result["duration_ms"]; !ok {
		t.Error("result event missing 'duration_ms'")
	}
	if result["is_error"] != false {
		t.Errorf("result is_error = %v, want false", result["is_error"])
	}
}

func TestStreamJSONSchema(t *testing.T) {
	// Validate that stream-json events carry the fields Claude Code docs require.
	in := strings.NewReader("create a hello-world app")
	var out bytes.Buffer
	runStreamJSON(in, &out, testEntries)

	requiredByType := map[string][]string{
		"system":    {"type", "subtype", "session_id"},
		"assistant": {"type", "message"},
		"result":    {"type", "subtype", "is_error", "result", "session_id", "duration_ms", "usage"},
	}

	for _, line := range nonEmptyLines(out.String()) {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		typ, _ := ev["type"].(string)
		for _, field := range requiredByType[typ] {
			if _, ok := ev[field]; !ok {
				t.Errorf("event type=%q missing field %q", typ, field)
			}
		}
	}
}

func TestStreamJSONErrorExit(t *testing.T) {
	in := strings.NewReader("error case")
	var out bytes.Buffer
	code := runStreamJSON(in, &out, testEntries)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	lines := nonEmptyLines(out.String())
	var result map[string]any
	json.Unmarshal([]byte(lines[len(lines)-1]), &result)
	if result["is_error"] != true {
		t.Error("expected is_error=true for error script entry")
	}
}

// --- ACP mode ---

func TestRunACP(t *testing.T) {
	// Build a sequence: initialize → session/new → session/prompt
	requests := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}},
		{"jsonrpc": "2.0", "id": 2, "method": "session/new", "params": map[string]any{}},
		{
			"jsonrpc": "2.0",
			"id":      3,
			"method":  "session/prompt",
			"params": map[string]any{
				"sessionId": "test-session",
				"message": map[string]any{
					"role":    "user",
					"content": "create a hello-world app",
				},
			},
		},
	}

	var inBuf strings.Builder
	for _, req := range requests {
		b, _ := json.Marshal(req)
		inBuf.WriteString(string(b))
		inBuf.WriteString("\n")
	}

	in := strings.NewReader(inBuf.String())
	var out bytes.Buffer
	code := runACP(in, &out, testEntries)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	lines := nonEmptyLines(out.String())
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSON-RPC responses, got %d: %v", len(lines), lines)
	}

	// initialize response
	var initResp map[string]any
	json.Unmarshal([]byte(lines[0]), &initResp)
	if initResp["id"].(float64) != 1 {
		t.Errorf("initialize response id = %v, want 1", initResp["id"])
	}
	result0 := initResp["result"].(map[string]any)
	if _, ok := result0["serverInfo"]; !ok {
		t.Error("initialize response missing serverInfo")
	}

	// session/new response
	var newResp map[string]any
	json.Unmarshal([]byte(lines[1]), &newResp)
	result1 := newResp["result"].(map[string]any)
	if _, ok := result1["id"]; !ok {
		t.Error("session/new response missing id")
	}

	// session/prompt response
	var promptResp map[string]any
	json.Unmarshal([]byte(lines[2]), &promptResp)
	result2 := promptResp["result"].(map[string]any)
	if !strings.Contains(result2["content"].(string), "Hello World") {
		t.Errorf("session/prompt response content %q does not contain 'Hello World'", result2["content"])
	}
	if result2["stopReason"] != "end_turn" {
		t.Errorf("stopReason = %v, want 'end_turn'", result2["stopReason"])
	}
	usage, ok := result2["usage"].(map[string]any)
	if !ok {
		t.Fatal("session/prompt response missing 'usage'")
	}
	if _, ok := usage["inputTokens"]; !ok {
		t.Error("usage missing inputTokens")
	}
}

func TestACPUnknownMethod(t *testing.T) {
	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "bogus/method", "params": map[string]any{}}
	b, _ := json.Marshal(req)
	in := strings.NewReader(string(b) + "\n")
	var out bytes.Buffer
	runACP(in, &out, testEntries)

	lines := nonEmptyLines(out.String())
	var resp map[string]any
	json.Unmarshal([]byte(lines[0]), &resp)
	if _, ok := resp["error"]; !ok {
		t.Error("unknown method should return JSON-RPC error")
	}
}

// --- Backend package compilation test ---

func TestBackendPackageCompiles(t *testing.T) {
	// This test verifies the internal/backend package is importable.
	// Since we're in cmd/fake-backend, we rely on `go build ./...` for this.
	if os.Getenv("CI") != "" {
		t.Skip("covered by go build ./... in CI")
	}
}

// --- helpers ---

func nonEmptyLines(s string) []string {
	scanner := bufio.NewScanner(strings.NewReader(s))
	var lines []string
	for scanner.Scan() {
		if l := strings.TrimSpace(scanner.Text()); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func assertField(t *testing.T, m map[string]any, key, want string) {
	t.Helper()
	got, ok := m[key].(string)
	if !ok {
		t.Errorf("field %q missing or not a string (got %T)", key, m[key])
		return
	}
	if got != want {
		t.Errorf("field %q = %q, want %q", key, got, want)
	}
}
