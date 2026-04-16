package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// ScriptEntry maps a pattern to a canned response.
type ScriptEntry struct {
	Pattern  string `yaml:"pattern"`
	Response string `yaml:"response"`
	ExitCode int    `yaml:"exit_code"`
}

// LoadScript loads a script file (.csv or .yaml/.yml).
func LoadScript(path string) ([]ScriptEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".csv":
		return loadCSV(f)
	case ".yaml", ".yml":
		return loadYAML(f)
	default:
		return nil, fmt.Errorf("unsupported script format %q (want .csv or .yaml)", ext)
	}
}

func loadCSV(r io.Reader) ([]ScriptEntry, error) {
	cr := csv.NewReader(r)
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	var entries []ScriptEntry
	for i, rec := range records {
		if i == 0 && len(rec) >= 2 && strings.ToLower(rec[0]) == "pattern" {
			continue // skip header
		}
		if len(rec) < 2 {
			continue
		}
		entries = append(entries, ScriptEntry{
			Pattern:  rec[0],
			Response: rec[1],
		})
	}
	return entries, nil
}

func loadYAML(r io.Reader) ([]ScriptEntry, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var entries []ScriptEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// Match finds the first entry whose pattern is contained in the prompt (case-insensitive).
// Falls back to a default "no match" entry if nothing matches.
func Match(entries []ScriptEntry, prompt string) ScriptEntry {
	lower := strings.ToLower(prompt)
	for _, e := range entries {
		if strings.Contains(lower, strings.ToLower(e.Pattern)) {
			return e
		}
	}
	return ScriptEntry{
		Pattern:  "*",
		Response: "(fake-backend: no matching script entry for prompt)",
		ExitCode: 1,
	}
}
