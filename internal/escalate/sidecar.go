package escalate

import (
	"path/filepath"
	"regexp"
)

// sidecarPatterns lists basenames that editors create as sidecars and must be
// ignored when watching for answer.md.
var sidecarPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\.`),                 // dotfiles (.answer.md.swp, .#answer.md)
	regexp.MustCompile(`~$`),                  // backup (answer.md~)
	regexp.MustCompile(`\.sw[a-p]$`),          // vim swap
	regexp.MustCompile(`___jb_(tmp|old)___$`), // JetBrains safe-write
	regexp.MustCompile(`^#.*#$`),              // emacs auto-save (#answer.md#)
	regexp.MustCompile(`^4913$`),              // vim pre-check file
}

// isSidecar reports whether name (basename) is an editor sidecar to ignore.
func isSidecar(name string) bool {
	base := filepath.Base(name)
	for _, re := range sidecarPatterns {
		if re.MatchString(base) {
			return true
		}
	}
	return false
}
