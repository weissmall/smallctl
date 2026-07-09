package protocol

// Response is the JSON payload returned by the server after executing a command.
type Response struct {
	// Success is true when the command (or a fallback) exited with code 0.
	Success bool `json:"success"`

	// Pong is true when this response is answering a ping request.
	Pong bool `json:"pong,omitempty"`

	// ExitCode is the exit code of the successful command, or the last failed one.
	ExitCode int `json:"exit_code"`

	// Stdout is the captured standard output of the successful command.
	Stdout string `json:"stdout"`

	// Stderr is the captured standard error of the successful command.
	Stderr string `json:"stderr"`

	// Tried lists every command that was attempted, in order.
	Tried []string `json:"tried"`

	// Errors lists every failed attempt with details. Empty on success.
	Errors []ErrorEntry `json:"errors"`
}
