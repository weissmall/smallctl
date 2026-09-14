package config

// Options holds global server configuration.
type Options struct {
	// EnvCommand is a shell command whose stdout (trimmed) determines the
	// current environment name. Overrides $SMALLCTL_ENV.
	// Example: "hostname"
	EnvCommand string `yaml:"env_command"`

	// LogLevel is the logging verbosity (0=quiet .. 5=verbose).
	// Overrides $SMALLCTL_LOG_LEVEL.
	LogLevel *int `yaml:"log_level"`

	// LogFile is the path to the log file. Overrides $SMALLCTL_LOG_FILE.
	// An empty string means no file logging.
	LogFile string `yaml:"log_file"`

	// Notify controls desktop notification behavior:
	//   "off"   — never notify (default)
	//   "error" — notify on command failure
	//   "all"   — notify on every invocation result
	// Overrides $SMALLCTL_NOTIFY.
	Notify string `yaml:"notify"`

	// Timeout is the per-command execution timeout in seconds.
	// nil → use DefaultTimeout (30). 0 → no timeout.
	Timeout *int `yaml:"timeout"`

	// FailedCommandTTL is the number of seconds a failed command is skipped.
	// nil → use the default (60). 0 → disable failure caching and probing.
	FailedCommandTTL *int `yaml:"failed_command_ttl"`

	// Shell is the shell binary used for -c execution. Default: "bash".
	Shell string `yaml:"shell"`
}
