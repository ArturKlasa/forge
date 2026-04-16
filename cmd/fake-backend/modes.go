package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

// runText prints the response to out and returns exit code.
func runText(in io.Reader, out io.Writer, entries []ScriptEntry) int {
	prompt := readAll(in)
	entry := Match(entries, prompt)
	fmt.Fprintln(out, entry.Response)
	return entry.ExitCode
}

// runStreamJSON emits NDJSON events matching Claude Code's stream-json shape.
func runStreamJSON(in io.Reader, out io.Writer, entries []ScriptEntry) int {
	prompt := readAll(in)
	entry := Match(entries, prompt)
	sessionID := uuid.New().String()

	emit := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintln(out, string(b))
	}

	// system init
	emit(map[string]any{
		"type":       "system",
		"subtype":    "init",
		"session_id": sessionID,
		"cwd":        ".",
		"tools":      []string{},
		"model":      "fake-1.0",
	})

	// assistant message
	emit(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": entry.Response},
			},
		},
	})

	subtype := "success"
	isError := false
	if entry.ExitCode != 0 {
		subtype = "error_max_turns"
		isError = true
	}

	// result (completion signal)
	emit(map[string]any{
		"type":       "result",
		"subtype":    subtype,
		"is_error":   isError,
		"result":     entry.Response,
		"session_id": sessionID,
		"duration_ms": 42,
		"num_turns":  1,
		"stop_reason": "end_turn",
		"total_cost_usd": 0.0,
		"usage": map[string]any{
			"input_tokens":                5,
			"output_tokens":               10,
			"cache_creation_input_tokens": 0,
			"cache_read_input_tokens":     0,
		},
		"permission_denials": []any{},
	})

	return entry.ExitCode
}

// runACP implements a minimal JSON-RPC 2.0 server over stdio for Kiro ACP mode.
// Handles: initialize, session/new, session/prompt.
func runACP(in io.Reader, out io.Writer, entries []ScriptEntry) int {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	sessionID := uuid.New().String()
	exitCode := 0

	type rpcRequest struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}

	respond := func(id any, result any) {
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result":  result,
		}
		b, _ := json.Marshal(resp)
		fmt.Fprintln(out, string(b))
	}

	respondError := func(id any, code int, msg string) {
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"error":   map[string]any{"code": code, "message": msg},
		}
		b, _ := json.Marshal(resp)
		fmt.Fprintln(out, string(b))
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			respondError(nil, -32700, "parse error")
			continue
		}

		switch req.Method {
		case "initialize":
			respond(req.ID, map[string]any{
				"capabilities":  map[string]any{"experimental": map[string]any{}},
				"serverInfo":    map[string]any{"name": "fake-backend", "version": "0.0.1"},
				"protocolVersion": "2024-11-05",
			})

		case "session/new":
			respond(req.ID, map[string]any{"id": sessionID})

		case "session/prompt":
			var params struct {
				SessionID string `json:"sessionId"`
				Message   struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				respondError(req.ID, -32602, "invalid params")
				continue
			}
			entry := Match(entries, params.Message.Content)
			if entry.ExitCode != 0 {
				exitCode = entry.ExitCode
			}
			respond(req.ID, map[string]any{
				"content":    entry.Response,
				"stopReason": "end_turn",
				"usage": map[string]any{
					"inputTokens":  5,
					"outputTokens": 10,
				},
				"durationMs": 42,
				"timestamp":  time.Now().UTC().Format(time.RFC3339),
			})

		default:
			respondError(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
		}
	}

	return exitCode
}

func readAll(r io.Reader) string {
	b, _ := io.ReadAll(r)
	return strings.TrimSpace(string(b))
}
