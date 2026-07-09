package config

// AppPaths bundles all derived filesystem paths for the running server.
type AppPaths struct {
	// BinaryName is the basename of the running binary (os.Args[0]).
	BinaryName string

	// ConfigFile is the resolved path to config.yaml.
	ConfigFile string

	// SocketPath is the Unix domain socket path.
	SocketPath string

	// LockPath is the PID lock file path.
	LockPath string

	// LogFile is the resolved log file path (empty if no file logging).
	LogFile string
}
