package config

// Options holds global server configuration.
type Options struct {
	// Environments is the optional set of environment names prepared in the
	// web configuration UI. It is metadata only; command execution still uses
	// the environment selected by EnvCommand or SMALLCTL_ENV.
	Environments []string `yaml:"environments"`

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

	// Shell is the shell binary used for -c execution. Default: "bash".
	Shell string `yaml:"shell"`
}
