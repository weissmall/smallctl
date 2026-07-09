package server

import (
	"log/slog"
	"net"
	"sync"

	"github.com/weissmall/smallctl/internal/config"
	"github.com/weissmall/smallctl/internal/notify"
	"github.com/weissmall/smallctl/internal/ports"
)

// Server is the Unix socket IPC server.
type Server struct {
	SocketPath  string
	Logger      *slog.Logger
	ConfigMu    sync.RWMutex
	Cfg         *config.Config
	Executor    ports.ExecutionEngine
	Notifier    ports.NotificationDispatcher
	NotifyLevel notify.Level
	listener    net.Listener
	wg          sync.WaitGroup
}
