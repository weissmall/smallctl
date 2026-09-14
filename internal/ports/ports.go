package ports

import (
	"time"

	"smallctl/internal/config"
	"smallctl/internal/notify"
	"smallctl/internal/protocol"
)

type ExecutionEngine interface {
	Environment() string
	ResolveEnv(*config.Config) error
	Execute(*config.Config, protocol.Request) protocol.Response
	SetEnvironment(string)
	SetShell(string)
	SetDefaultTimeout(time.Duration)
	ConfigureFailureCache(*config.Config)
	ClearFailedCommands()
	Close()
}

type NotificationDispatcher interface {
	Dispatch(level notify.Level, isError bool, title, body string)
}
