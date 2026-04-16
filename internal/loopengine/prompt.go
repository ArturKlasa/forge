package loopengine

import (
	"os"
	"path/filepath"
	"strings"
)

// assemblePrompt naively concatenates task.md + plan.md + state.md from the run dir.
// Real context management with distillation comes in step 17.
func assemblePrompt(runDir string) (string, error) {
	files := []string{"task.md", "plan.md", "state.md"}
	var parts []string
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(runDir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		if len(data) > 0 {
			parts = append(parts, strings.TrimSpace(string(data)))
		}
	}
	return strings.Join(parts, "\n\n---\n\n"), nil
}
