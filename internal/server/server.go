package server

import (
	"log/slog"
	"net"
	"sync"

	"smallctl/internal/config"
	"smallctl/internal/notify"
	"smallctl/internal/ports"
)

// Server is the Unix socket IPC server.
type Server struct {
	SocketPath   string
	Logger       *slog.Logger
	ConfigMu     sync.RWMutex
	Cfg          *config.Config
	Executor     ports.ExecutionEngine
	Notifier     ports.NotificationDispatcher
	NotifyLevel  notify.Level
	listener     net.Listener
	wg           sync.WaitGroup
	shutdownCh   chan struct{}
	shutdownOnce sync.Once
}
