package protocol

// RequestType distinguishes command invocations from health checks.
type RequestType string

const (
	TypeCommand RequestType = "command"
	TypePing    RequestType = "ping"
)
