package protocol

// Request is the JSON payload sent by a smallctl client to the server.
type Request struct {
	// Type of request: "command" (default), "ping", an environment request,
	// or "shutdown".
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

	// Environment is the environment name to set for an environment_set request.
	Environment string `json:"environment,omitempty"`
}
