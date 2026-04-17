package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/arturklasa/forge/internal/config"
	forgegit "github.com/arturklasa/forge/internal/git"
	"github.com/arturklasa/forge/internal/notify"
)

// doctorCheck holds the result of a single doctor check.
type doctorCheck struct {
	label  string
	status string // "OK", "WARN", "FAIL"
	detail string
}

func (c doctorCheck) print(w io.Writer, verbose bool) {
	icon := "✓"
	if c.status == "WARN" {
		icon = "⚠"
	} else if c.status == "FAIL" {
		icon = "✗"
	}
	fmt.Fprintf(w, "%s %-18s %s\n", icon, c.label+":", c.status)
	// Always show detail on non-OK; --verbose also shows OK details.
	if c.detail != "" && (c.status != "OK" || verbose) {
		for _, line := range strings.Split(c.detail, "\n") {
			if line != "" {
				fmt.Fprintf(w, "  → %s\n", line)
			}
		}
	}
}

// runFullDoctor executes all forge doctor checks and prints results.
func runFullDoctor(ctx context.Context, out io.Writer, workDir string, verbose bool) {
	checks := []doctorCheck{
		doctorCheckConfig(workDir),
		doctorCheckBackend(workDir),
		doctorCheckGit(ctx, workDir),
		doctorCheckForgeDir(workDir),
		doctorCheckDiskSpace(workDir),
		doctorCheckNotify(),
	}
	for _, c := range checks {
		c.print(out, verbose)
	}
}

func doctorCheckConfig(workDir string) doctorCheck {
	m, err := config.Load(workDir)
	if err != nil {
		return doctorCheck{"Config", "FAIL", err.Error()}
	}
	be := m.GetString("backend.default")
	if be == "" {
		return doctorCheck{"Config", "WARN", "backend.default not set; run 'forge backend set <name>'"}
	}
	return doctorCheck{"Config", "OK", fmt.Sprintf("backend.default=%s", be)}
}

func doctorCheckBackend(workDir string) doctorCheck {
	m, err := config.Load(workDir)
	if err != nil {
		return doctorCheck{"Backend", "FAIL", err.Error()}
	}
	be := m.GetString("backend.default")
	if be == "" {
		return doctorCheck{"Backend", "WARN", "no backend configured"}
	}

	var bin string
	for _, kb := range supportedBackends {
		if kb.Name == be {
			bin = kb.Bin
			break
		}
	}
	if bin == "" {
		return doctorCheck{"Backend " + be, "FAIL", "unknown backend name"}
	}

	path, err := exec.LookPath(bin)
	if err != nil {
		return doctorCheck{"Backend " + be, "FAIL", bin + " not found in $PATH"}
	}
	ver := probeBackendVersion(path, be)
	return doctorCheck{"Backend " + be, "OK", ver}
}

func doctorCheckGit(ctx context.Context, workDir string) doctorCheck {
	v, err := forgegit.Version(ctx)
	if err != nil {
		return doctorCheck{"Git", "FAIL", "git not found: " + err.Error()}
	}
	g := forgegit.New(workDir)
	if !g.IsRepo(ctx) {
		return doctorCheck{"Git", "WARN",
			fmt.Sprintf("version %s; %s is not a git repo", v, workDir)}
	}
	sha, branch, err := g.HEAD(ctx)
	if err != nil {
		return doctorCheck{"Git", "OK", fmt.Sprintf("version %s", v)}
	}
	short := sha
	if len(short) > 7 {
		short = short[:7]
	}

	branches, source := g.DetectProtectedBranches(ctx, nil)
	detail := fmt.Sprintf("version %s; HEAD=%s on %s", v, short, branch)
	if len(branches) > 0 {
		detail += fmt.Sprintf("; protected: %s (via %s)", strings.Join(branches, ", "), source)
	}
	return doctorCheck{"Git", "OK", detail}
}

func doctorCheckForgeDir(workDir string) doctorCheck {
	forgeDir := filepath.Join(workDir, ".forge")
	if err := os.MkdirAll(forgeDir, 0o755); err != nil {
		return doctorCheck{".forge/", "FAIL", "cannot create: " + err.Error()}
	}
	tmpFile := filepath.Join(forgeDir, ".write-test")
	if err := os.WriteFile(tmpFile, []byte("test"), 0o644); err != nil {
		return doctorCheck{".forge/", "FAIL", "not writable: " + err.Error()}
	}
	_ = os.Remove(tmpFile)

	// Check for run-dir integrity: runs with no marker.
	runsDir := filepath.Join(forgeDir, "runs")
	entries, _ := os.ReadDir(runsDir)
	orphans := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		runPath := filepath.Join(runsDir, e.Name())
		hasMarker := false
		for _, mk := range []string{"RUNNING", "AWAITING_HUMAN", "PAUSED", "DONE", "ABORTED", "FAILED"} {
			if _, err := os.Stat(filepath.Join(runPath, mk)); err == nil {
				hasMarker = true
				break
			}
		}
		if !hasMarker {
			orphans++
		}
	}
	if orphans > 0 {
		return doctorCheck{".forge/", "WARN",
			fmt.Sprintf("writable; %d run dir(s) missing lifecycle marker (run 'forge clean')", orphans)}
	}
	return doctorCheck{".forge/", "OK", "writable"}
}

func doctorCheckDiskSpace(workDir string) doctorCheck {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(workDir, &stat); err != nil {
		return doctorCheck{"Disk space", "WARN", "cannot determine: " + err.Error()}
	}
	freeGB := float64(stat.Bavail) * float64(stat.Bsize) / (1024 * 1024 * 1024)
	if freeGB < 1.0 {
		return doctorCheck{"Disk space", "WARN", fmt.Sprintf("%.1f GB free (low)", freeGB)}
	}
	return doctorCheck{"Disk space", "OK", fmt.Sprintf("%.1f GB free", freeGB)}
}

func doctorCheckNotify() doctorCheck {
	p := notify.Probe()
	if p.RecommendAutoResolve() {
		return doctorCheck{"Notifications", "WARN",
			"OS notifications may not reach you.\nConsider --auto-resolve accept-recommended for unattended runs."}
	}
	detail := fmt.Sprintf("dbus=%v display=%v tmux=%v wsl=%v ci=%v",
		p.DBusSession, p.Display, p.TmuxSession, p.IsWSL, p.IsCI)
	return doctorCheck{"Notifications", "OK", detail}
}
