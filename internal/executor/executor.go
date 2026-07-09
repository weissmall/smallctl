package executor

import (
	"log/slog"
	"time"
)

// Executor holds the runtime state needed to execute commands.
type Executor struct {
	// Logger for executor-specific messages.
	Logger *slog.Logger

	// EnvName is the resolved current environment name.
	// Set by ResolveEnv() based on env_command or $SMALLCTL_ENV.
	EnvName string

	// Shell is the shell binary path (e.g., "bash").
	// Defaults to config.DefaultShell if not configured.
	Shell string

	// DefaultTimeout is the per-command timeout when not specified per-command.
	DefaultTimeout time.Duration
}
