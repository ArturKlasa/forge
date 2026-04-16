package escalate

import (
	"fmt"
	"strings"
	"time"
)

// ParsedAnswer is the result of parsing answer.md.
type ParsedAnswer struct {
	IDField   string
	OptionKey string
	Note      string
}

// ParseAnswer parses the content of answer.md.
// Returns an error when the format is invalid (no terminator, etc.).
// Returns empty OptionKey when the file is clearly incomplete/empty.
func ParseAnswer(data []byte) (ParsedAnswer, error) {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.TrimSpace(s)
	if s == "" {
		return ParsedAnswer{}, nil
	}

	lines := strings.Split(s, "\n")

	var id, answer, note string
	var terminated bool
	bodyLines := []string{}
	inBody := false

	for _, line := range lines {
		if line == "---" {
			terminated = true
			break
		}
		if !inBody {
			if strings.HasPrefix(line, "id:") {
				id = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
				continue
			}
			if strings.HasPrefix(line, "answer:") {
				answer = strings.TrimSpace(strings.TrimPrefix(line, "answer:"))
				inBody = true
				continue
			}
		} else {
			bodyLines = append(bodyLines, line)
		}
	}

	if !terminated {
		return ParsedAnswer{}, fmt.Errorf("answer.md: missing '---' terminator (partial write?)")
	}
	if id == "" {
		return ParsedAnswer{}, fmt.Errorf("answer.md: missing 'id:' field")
	}
	if answer == "" {
		return ParsedAnswer{}, fmt.Errorf("answer.md: missing 'answer:' field")
	}
	// answer must be a single letter
	if len(answer) != 1 {
		return ParsedAnswer{}, fmt.Errorf("answer.md: 'answer:' must be a single letter, got %q", answer)
	}

	note = strings.TrimSpace(strings.Join(bodyLines, "\n"))

	return ParsedAnswer{IDField: id, OptionKey: answer, Note: note}, nil
}

// renderAwaitingHuman composes the awaiting-human.md content for the given escalation.
func renderAwaitingHuman(esc *Escalation) string {
	var sb strings.Builder

	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("id: %s\n", esc.ID))
	sb.WriteString(fmt.Sprintf("raised_at: %s\n", esc.RaisedAt.UTC().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("tier: %d\n", esc.Tier))
	sb.WriteString(fmt.Sprintf("path: %s\n", esc.Path))
	sb.WriteString(fmt.Sprintf("iteration: %d\n", esc.Iteration))
	sb.WriteString("---\n\n")

	if esc.WhatTried != "" {
		sb.WriteString("## What Forge tried\n\n")
		sb.WriteString(esc.WhatTried)
		sb.WriteString("\n\n")
	}

	if esc.Decision != "" {
		sb.WriteString("## Decision\n\n")
		sb.WriteString(esc.Decision)
		sb.WriteString("\n\n")
	}

	if len(esc.Options) > 0 {
		sb.WriteString("## Options\n\n")
		for _, o := range esc.Options {
			label := o.Label
			if o.Description != "" {
				label = o.Description
			}
			rec := ""
			if o.Key == esc.Recommended {
				rec = " ← Recommended"
			}
			sb.WriteString(fmt.Sprintf("- **[%s] %s** — %s%s\n", o.Key, o.Label, label, rec))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## How to answer\n\n")
	sb.WriteString("Create `answer.md` in this directory with:\n\n")
	sb.WriteString("```\n")
	sb.WriteString(fmt.Sprintf("id: %s\n", esc.ID))
	if esc.Recommended != "" {
		sb.WriteString(fmt.Sprintf("answer: %s\n", esc.Recommended))
	} else if len(esc.Options) > 0 {
		sb.WriteString(fmt.Sprintf("answer: %s\n", esc.Options[0].Key))
	}
	sb.WriteString("---\n")
	sb.WriteString("```\n")

	return sb.String()
}
