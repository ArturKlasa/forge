package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	goyaml "github.com/goccy/go-yaml"
)

// SetKey writes key=value to the target YAML file (per-repo unless global=true).
// Creates the file and parent directories if they don't exist.
func (m *Manager) SetKey(key, value string, global bool) error {
	path := m.targetPath(global)
	return writeKey(path, key, value)
}

// UnsetKey removes key from the target YAML file.
func (m *Manager) UnsetKey(key string, global bool) error {
	path := m.targetPath(global)
	return deleteKey(path, key)
}

// targetPath returns the config file path to write to.
func (m *Manager) targetPath(global bool) string {
	if global {
		return m.GlobalPath
	}
	return m.RepoPath
}

// writeKey sets a nested dot-separated key to value in a YAML file.
func writeKey(path, key, value string) error {
	data, err := readYAMLMap(path)
	if err != nil {
		return err
	}
	setNestedKey(data, strings.Split(key, "."), value)
	return writeYAMLMap(path, data)
}

// deleteKey removes a nested dot-separated key from a YAML file.
func deleteKey(path, key string) error {
	data, err := readYAMLMap(path)
	if err != nil {
		return err
	}
	deleteNestedKey(data, strings.Split(key, "."))
	return writeYAMLMap(path, data)
}

// readYAMLMap reads a YAML file into a map. Returns an empty map if the file doesn't exist.
func readYAMLMap(path string) (map[string]interface{}, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]interface{}{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var out map[string]interface{}
	if err := goyaml.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if out == nil {
		out = map[string]interface{}{}
	}
	return out, nil
}

// writeYAMLMap writes a map to a YAML file, creating parent directories as needed.
func writeYAMLMap(path string, data map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating directory for %s: %w", path, err)
	}
	b, err := goyaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}
	if err := os.WriteFile(path, b, 0o640); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// setNestedKey sets a value at a path (e.g., ["backend","default"]) in a nested map.
func setNestedKey(m map[string]interface{}, parts []string, value interface{}) {
	if len(parts) == 0 {
		return
	}
	if len(parts) == 1 {
		m[parts[0]] = value
		return
	}
	sub, ok := m[parts[0]]
	if !ok {
		sub = map[string]interface{}{}
	}
	subMap, ok := sub.(map[string]interface{})
	if !ok {
		subMap = map[string]interface{}{}
	}
	setNestedKey(subMap, parts[1:], value)
	m[parts[0]] = subMap
}

// deleteNestedKey removes a key at a path in a nested map.
func deleteNestedKey(m map[string]interface{}, parts []string) {
	if len(parts) == 0 {
		return
	}
	if len(parts) == 1 {
		delete(m, parts[0])
		return
	}
	sub, ok := m[parts[0]]
	if !ok {
		return
	}
	subMap, ok := sub.(map[string]interface{})
	if !ok {
		return
	}
	deleteNestedKey(subMap, parts[1:])
	m[parts[0]] = subMap
}
