package router

import (
	"os"
)

// writeTempPrompt writes content to a temp file and returns its path.
// The caller is responsible for removing the file.
func writeTempPrompt(content string) (string, error) {
	f, err := os.CreateTemp("", "forge-router-*.md")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}
