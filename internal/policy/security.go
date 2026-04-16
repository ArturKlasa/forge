package policy

import (
	"os"
	"strings"
	"sync"

	"github.com/spf13/viper"
	glconfig "github.com/zricethezav/gitleaks/v8/config"
	"github.com/zricethezav/gitleaks/v8/detect"
	"github.com/zricethezav/gitleaks/v8/report"
)

// SecretFinding is a detected secret in a diff.
type SecretFinding struct {
	RuleID      string
	Description string
	File        string
	Line        int
	Secret      string // partially redacted
}

// SecurityScanner scans diffs for secrets using gitleaks.
type SecurityScanner struct {
	mu       sync.Mutex
	detector *detect.Detector
}

// NewSecurityScanner creates a scanner. If gitleaksToml is non-empty and
// the file exists, it overrides the default ruleset.
func NewSecurityScanner(gitleaksToml string) (*SecurityScanner, error) {
	d, err := newDetector(gitleaksToml)
	if err != nil {
		return nil, err
	}
	return &SecurityScanner{detector: d}, nil
}

func newDetector(customToml string) (*detect.Detector, error) {
	v := viper.New()
	if customToml != "" {
		if _, err := os.Stat(customToml); err == nil {
			v.SetConfigFile(customToml)
			if err := v.ReadInConfig(); err != nil {
				return nil, err
			}
			var vc glconfig.ViperConfig
			if err := v.Unmarshal(&vc); err != nil {
				return nil, err
			}
			cfg, err := vc.Translate()
			if err != nil {
				return nil, err
			}
			return detect.NewDetector(cfg), nil
		}
	}
	// Default 222-rule config.
	v.SetConfigType("toml")
	if err := v.ReadConfig(strings.NewReader(glconfig.DefaultConfig)); err != nil {
		return nil, err
	}
	var vc glconfig.ViperConfig
	if err := v.Unmarshal(&vc); err != nil {
		return nil, err
	}
	cfg, err := vc.Translate()
	if err != nil {
		return nil, err
	}
	return detect.NewDetector(cfg), nil
}

// Scan checks diff bytes for secrets. Returns findings; any non-empty result
// means hard-stop (mandatory gate).
func (s *SecurityScanner) Scan(diff []byte) []SecretFinding {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw := s.detector.DetectBytes(diff)
	return convertFindings(raw)
}

func convertFindings(raw []report.Finding) []SecretFinding {
	out := make([]SecretFinding, 0, len(raw))
	for _, f := range raw {
		out = append(out, SecretFinding{
			RuleID:      f.RuleID,
			Description: f.Description,
			File:        f.File,
			Line:        f.StartLine,
			Secret:      redact(f.Secret),
		})
	}
	return out
}

func redact(s string) string {
	if len(s) <= 6 {
		return "***"
	}
	return s[:3] + "***" + s[len(s)-3:]
}
