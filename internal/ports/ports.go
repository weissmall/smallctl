package ports

import (
	"time"

	"smallctl/internal/config"
	"smallctl/internal/notify"
	"smallctl/internal/protocol"
)

type ExecutionEngine interface {
	ResolveEnv(*config.Config) error
	Execute(*config.Config, protocol.Request) protocol.Response
	SetShell(string)
	SetDefaultTimeout(time.Duration)
}

type NotificationDispatcher interface {
	Dispatch(level notify.Level, isError bool, title, body string)
}
