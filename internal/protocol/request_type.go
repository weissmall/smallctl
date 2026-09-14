package protocol

// RequestType distinguishes command invocations from health checks.
type RequestType string

const (
	TypeCommand             RequestType = "command"
	TypePing                RequestType = "ping"
	TypeEnvironmentGet      RequestType = "environment_get"
	TypeEnvironmentSet      RequestType = "environment_set"
	TypeFailedCommandsClear RequestType = "failed_commands_clear"
)
