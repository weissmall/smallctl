package protocol

// ErrorEntry describes a single failed command execution in the fallback chain.
type ErrorEntry struct {
	// Command is the shell command string that was executed.
	Command string `json:"command"`

	// ExitCode is the non-zero exit code returned by the command.
	ExitCode int `json:"exit_code"`

	// Stderr is the captured standard error from the failed command.
	Stderr string `json:"stderr"`
}
