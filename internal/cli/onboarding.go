package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/arturklasa/forge/internal/config"
)

// backendSpec describes a supported backend CLI.
type backendSpec struct {
	Name string
	Bin  string
}

// supportedBackends lists known backends in preference order.
var supportedBackends = []backendSpec{
	{"claude", "claude"},
	{"gemini", "gemini"},
	{"kiro", "kiro-cli"},
}

// runFirstRunOnboarding checks whether a backend is configured and prompts the
// user to pick one if not. It writes to out and reads from stdin.
// Returns an error if no backend is available or selection fails.
func runFirstRunOnboarding(out io.Writer, cfgMgr *config.Manager) error {
	cur := cfgMgr.GetString("backend.default")
	if cur != "" {
		return nil // already configured
	}

	fmt.Fprintln(out, "Welcome to Forge. Which backend should I use?")

	type found struct {
		name    string
		version string
	}
	var available []found
	for _, kb := range supportedBackends {
		path, err := exec.LookPath(kb.Bin)
		if err != nil {
			continue
		}
		ver := probeBackendVersion(path, kb.Name)
		available = append(available, found{kb.Name, ver})
	}

	if len(available) == 0 {
		fmt.Fprintln(out, "No supported backend found in $PATH.")
		fmt.Fprintln(out, "Install one of:")
		fmt.Fprintln(out, "  • claude   → https://claude.ai/download")
		fmt.Fprintln(out, "  • gemini   → https://github.com/google-gemini/gemini-cli")
		fmt.Fprintln(out, "  • kiro-cli → https://kiro.dev")
		return fmt.Errorf("no backend installed")
	}

	for i, a := range available {
		fmt.Fprintf(out, "  %d. %-10s [%s]\n", i+1, a.name, a.version)
	}
	fmt.Fprint(out, "> ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return fmt.Errorf("no input")
	}
	line := strings.TrimSpace(scanner.Text())
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(available) {
		return fmt.Errorf("invalid selection %q", line)
	}

	selected := available[n-1].name
	if err := cfgMgr.SetKey("backend.default", selected, true); err != nil {
		return fmt.Errorf("save backend selection: %w", err)
	}
	fmt.Fprintf(out, "Saved backend.default = %s.\n\n", selected)
	return nil
}

// probeBackendVersion attempts to get the version string from a backend CLI.
func probeBackendVersion(path, name string) string {
	out, err := exec.Command(path, "--version").Output() //nolint:gosec
	if err != nil {
		return "installed"
	}
	v := strings.TrimSpace(string(out))
	lines := strings.SplitN(v, "\n", 2)
	v = lines[0]
	if len(v) > 40 {
		v = v[:40]
	}
	return v
}
