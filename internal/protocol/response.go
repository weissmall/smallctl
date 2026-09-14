package protocol

// Response is the JSON payload returned by the server after executing a command.
type Response struct {
	// Success is true when the command (or a fallback) exited with code 0.
	Success bool `json:"success"`

	// Pong is true when this response is answering a ping request.
	Pong bool `json:"pong,omitempty"`

	// Environment is the current environment for an environment request.
	Environment string `json:"environment,omitempty"`

	// ExitCode is the exit code of the successful command, or the last failed one.
	ExitCode int `json:"exit_code"`

	// Stdout is the captured standard output of the successful command.
	Stdout string `json:"stdout"`

	// Stderr is the captured standard error of the successful command.
	Stderr string `json:"stderr"`

	// Tried lists every command that was attempted, in order.
	Tried []string `json:"tried"`

	// Skipped lists commands skipped because they are in the failed-command cache.
	Skipped []string `json:"skipped"`

	// Errors lists every failed attempt with details. Empty on success.
	Errors []ErrorEntry `json:"errors"`
}
