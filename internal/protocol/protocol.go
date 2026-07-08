// Package protocol defines the JSON request/response types used over the
// Unix domain socket between the smallctl server and the invoke client.
package protocol

// RequestType distinguishes command invocations from health checks.
type RequestType string

const (
	// TypeCommand is the default — invoke a named command from the config.
	TypeCommand RequestType = "command"
	// TypePing is a health check; the server responds with Pong=true immediately.
	TypePing RequestType = "ping"
)

// Request is the JSON payload sent by smallctl invoke to the server.
type Request struct {
	// Type of request: "command" (default) or "ping".
	Type RequestType `json:"type,omitempty"`

	// Command is the name of the command to invoke (looked up in config.commands).
	// Required when Type is "command" (or empty, which defaults to "command").
	Command string `json:"command,omitempty"`

	// Args are invocation-time overrides for the command's declared args.defaults.
	// Merged with defaults: overrides win, then defaults, then empty string.
	Args map[string]string `json:"args,omitempty"`

	// Wait controls whether the server sends a response after execution.
	// true (default) → block until command finishes, return response.
	// false → fire-and-forget: server executes but client disconnects immediately.
	Wait bool `json:"wait"`
}

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

// ErrorEntry describes a single failed command execution in the fallback chain.
type ErrorEntry struct {
	// Command is the shell command string that was executed.
	Command string `json:"command"`

	// ExitCode is the non-zero exit code returned by the command.
	ExitCode int `json:"exit_code"`

	// Stderr is the captured standard error from the failed command.
	Stderr string `json:"stderr"`
}

// MaxRequestSize is the maximum JSON request size in bytes (16 KB).
const MaxRequestSize = 16 * 1024