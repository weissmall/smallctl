package ports

import (
	"time"

	"github.com/weissmall/smallctl/internal/config"
	"github.com/weissmall/smallctl/internal/notify"
	"github.com/weissmall/smallctl/internal/protocol"
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
