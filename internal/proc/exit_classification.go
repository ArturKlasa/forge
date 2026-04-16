package proc

import (
	"os"
)

// ExitClassification describes why a subprocess exited.
type ExitClassification int

const (
	// ExitNormal means the process exited with status 0 on its own.
	ExitNormal ExitClassification = iota
	// ExitIterationFail means the process exited with a non-zero status on its own.
	ExitIterationFail
	// ExitForgeTerminated means Forge sent SIGTERM or SIGKILL to the process.
	ExitForgeTerminated
	// ExitExternalSignal means the OS sent a signal that Forge did not initiate.
	ExitExternalSignal
)

// Result holds the outcome of a subprocess after it has exited.
type Result struct {
	// ExitCode is the numeric exit status. -1 if the process was killed by a signal.
	ExitCode int
	// Classification describes the reason for the exit.
	Classification ExitClassification
	// Signal is non-nil when the process was terminated by a signal.
	Signal os.Signal
	// Err holds any error returned by cmd.Wait() that is not a normal exit error.
	Err error
}
