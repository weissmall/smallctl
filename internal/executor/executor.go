package executor

import (
	"log/slog"
	"sync"
	"time"
)

// Executor holds the runtime state needed to execute commands.
type Executor struct {
	mu sync.RWMutex

	// Logger for executor-specific messages.
	Logger *slog.Logger

	// EnvName is the active environment name. Access it with Environment() at
	// runtime; it is set by ResolveEnv() or SetEnvironment().
	EnvName string

	// environmentOverride marks EnvName as set through `smallctl env set`.
	// It is kept for the server lifetime, including config reloads.
	environmentOverride bool

	// Shell is the shell binary path (e.g., "bash").
	// Defaults to config.DefaultShell if not configured.
	Shell string

	// DefaultTimeout is the per-command timeout when not specified per-command.
	DefaultTimeout time.Duration
}
