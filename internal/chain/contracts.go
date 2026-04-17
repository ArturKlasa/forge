package chain

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ContractFn derives the next stage's task from the previous stage's artifacts.
type ContractFn func(prevStageDir, originalTask string) string

// contracts maps "chainKey:stageIndex" → contract function.
// stageIndex is the destination stage (0-based index into chain), e.g. "review:fix:1"
// means stage 2 (index 1) of review:fix chain.
var contracts = map[string]ContractFn{
	"review:fix:1":      reviewFixContract,
	"review:refactor:1": reviewRefactorContract,
	"upgrade:fix:1":     upgradeFixContract,
	"upgrade:test:1":    upgradeTestContract,
	"fix:test:1":        fixTestContract,
	"create:test:1":     createTestContract,
	"add:test:1":        addTestContract,
	"refactor:test:1":   refactorTestContract,
	// 3-stage: upgrade:fix:test
	"upgrade:fix:test:1": upgradeFixContract, // stage 2 of 3
	"upgrade:fix:test:2": fixTestContract,    // stage 3 of 3
}

// loadContract returns the ContractFn for the given chainKey and destination stage index.
func loadContract(chainKey string, toStageIdx int) ContractFn {
	key := fmt.Sprintf("%s:%d", chainKey, toStageIdx)
	if fn, ok := contracts[key]; ok {
		return fn
	}
	return genericPassthroughContract
}

// reviewFixContract reads report.md from the review stage and extracts findings.
func reviewFixContract(prevStageDir, originalTask string) string {
	data, err := os.ReadFile(filepath.Join(prevStageDir, "report.md"))
	if err != nil {
		return originalTask
	}
	findings := extractNumberedFindings(string(data))
	if len(findings) == 0 {
		return genericPassthroughContract(prevStageDir, originalTask)
	}
	var sb strings.Builder
	sb.WriteString("Fix the following findings from the code review:\n")
	for i, f := range findings {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, f))
	}
	return sb.String()
}

// reviewRefactorContract reads report.md and asks for refactoring based on findings.
func reviewRefactorContract(prevStageDir, originalTask string) string {
	data, err := os.ReadFile(filepath.Join(prevStageDir, "report.md"))
	if err != nil {
		return originalTask
	}
	findings := extractNumberedFindings(string(data))
	if len(findings) == 0 {
		return genericPassthroughContract(prevStageDir, originalTask)
	}
	var sb strings.Builder
	sb.WriteString("Refactor to address the following findings from the code review:\n")
	for i, f := range findings {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, f))
	}
	return sb.String()
}

// upgradeFixContract reads upgrade-scope.md from the upgrade stage and derives a fix task.
func upgradeFixContract(prevStageDir, originalTask string) string {
	data, err := os.ReadFile(filepath.Join(prevStageDir, "upgrade-scope.md"))
	if err != nil {
		// Fallback to report.md.
		return genericPassthroughContract(prevStageDir, originalTask)
	}
	return fmt.Sprintf("Fix any remaining issues from the upgrade:\n\n%s", string(data))
}

// upgradeTestContract derives a test task based on files touched during upgrade.
func upgradeTestContract(prevStageDir, originalTask string) string {
	data, err := os.ReadFile(filepath.Join(prevStageDir, "upgrade-scope.md"))
	if err != nil {
		return genericPassthroughContract(prevStageDir, originalTask)
	}
	return fmt.Sprintf("Write tests to cover the upgraded functionality:\n\n%s", string(data))
}

// fixTestContract derives a test task from a fix stage.
func fixTestContract(prevStageDir, originalTask string) string {
	// Look for any artifact that indicates what was fixed.
	for _, name := range []string{"report.md", "docs.md", "state.md"} {
		data, err := os.ReadFile(filepath.Join(prevStageDir, name))
		if err == nil && len(data) > 0 {
			return fmt.Sprintf("Write tests to cover the fixes made in the previous stage.\n\nContext from fix stage:\n\n%s\n\nOriginal task: %s", string(data), originalTask)
		}
	}
	return fmt.Sprintf("Write tests for the fixes applied to: %s", originalTask)
}

// createTestContract derives a test task after a create stage.
func createTestContract(prevStageDir, originalTask string) string {
	return fmt.Sprintf("Write tests for the code created in the previous stage.\n\nOriginal task: %s", originalTask)
}

// addTestContract derives a test task after an add stage.
func addTestContract(prevStageDir, originalTask string) string {
	return fmt.Sprintf("Write tests for the functionality added in the previous stage.\n\nOriginal task: %s", originalTask)
}

// refactorTestContract derives a test task after a refactor stage.
func refactorTestContract(prevStageDir, originalTask string) string {
	return fmt.Sprintf("Write tests to verify the refactored code behaves correctly.\n\nOriginal task: %s", originalTask)
}

// genericPassthroughContract reads the best artifact from prevStageDir and prepends it as context.
func genericPassthroughContract(prevStageDir, originalTask string) string {
	for _, name := range []string{"report.md", "docs.md", "research-report.md", "explanation.md"} {
		data, err := os.ReadFile(filepath.Join(prevStageDir, name))
		if err == nil && len(data) > 0 {
			return fmt.Sprintf("Based on the previous stage analysis:\n\n%s\n\nTask: %s", string(data), originalTask)
		}
	}
	return originalTask
}

// extractNumberedFindings extracts bullet points or numbered list items from text.
func extractNumberedFindings(text string) []string {
	var findings []string
	bulletRe := regexp.MustCompile(`^[\-\*]\s+(.+)$`)
	numberedRe := regexp.MustCompile(`^\d+\.\s+(.+)$`)
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if m := numberedRe.FindStringSubmatch(trimmed); m != nil {
			findings = append(findings, m[1])
		} else if m := bulletRe.FindStringSubmatch(trimmed); m != nil {
			findings = append(findings, m[1])
		}
	}
	return findings
}
