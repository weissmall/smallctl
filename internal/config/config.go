// Package config handles loading, parsing, validating, and hot-reloading
// the smallctl configuration file (YAML format).
//
// The Config struct is designed to be held behind a sync.RWMutex so that
// the watcher can atomically swap it without blocking active requests.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ── Top-level types ──────────────────────────────────────────────

// Config is the root of the configuration tree.
// It is always kept in RAM; the watcher re-parses and swaps it atomically.
type Config struct {
	// Options holds global server settings.
	Options Options `yaml:"options"`

	// Commands maps command names to their definitions.
	Commands map[string]Command `yaml:"commands"`
}

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

	// Shell is the shell binary used for -c execution. Default: "bash".
	Shell string `yaml:"shell"`
}

// Command defines a single named command that can be invoked.
type Command struct {
	// Description is a human-readable explanation shown in help output.
	Description string `yaml:"description"`

	// Args declares default values for template variables.
	// Invocation overrides (--args) take priority over these defaults.
	Args map[string]string `yaml:"args"`

	// Envs maps environment names to shell command strings.
	// The current environment (from env_command or $SMALLCTL_ENV) selects
	// which command to run. If no env matches, fallback is tried.
	Envs map[string]string `yaml:"envs"`

	// Fallback is an ordered list of shell commands tried when:
	//   - No env matched the current environment, or
	//   - The matched env command failed (non-zero exit).
	// Execution stops at the first success (exit 0).
	Fallback []string `yaml:"fallback"`
}

// ── Defaults ─────────────────────────────────────────────────────

// DefaultTimeout is the fallback execution timeout in seconds.
const DefaultTimeout = 30

// DefaultShell is the fallback shell binary.
const DefaultShell = "bash"

// DefaultNotify is the fallback notification mode.
const DefaultNotify = "off"

// ValidNotifyValues lists accepted values for options.notify.
var ValidNotifyValues = map[string]bool{"off": true, "error": true, "all": true}

// ── Loading ──────────────────────────────────────────────────────

// Load reads and parses a YAML configuration file.
//
// If the file does not exist, it returns an empty Config with defaults
// (no error) — the server starts with no commands and logs a warning.
//
// Returns an error if the file exists but cannot be read, or contains
// invalid YAML syntax or fails validation.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Config file doesn't exist — return empty config with defaults.
			cfg := &Config{
				Commands: make(map[string]Command),
			}
			applyDefaults(cfg)
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w", path, err)
	}

	applyDefaults(&cfg)
	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("validating config file %s: %w", path, err)
	}

	return &cfg, nil
}

// ── Validation ───────────────────────────────────────────────────

// Validate checks the config for semantic correctness after loading.
func Validate(cfg *Config) error {
	// Check notify value.
	if cfg.Options.Notify != "" && !ValidNotifyValues[cfg.Options.Notify] {
		return fmt.Errorf("options.notify must be one of: off, error, all (got: %q)", cfg.Options.Notify)
	}

	// Check timeout is non-negative.
	if cfg.Options.Timeout != nil && *cfg.Options.Timeout < 0 {
		return fmt.Errorf("options.timeout must be >= 0 (got: %d)", *cfg.Options.Timeout)
	}

	// Check log level.
	if cfg.Options.LogLevel != nil {
		lvl := *cfg.Options.LogLevel
		if lvl < 0 || lvl > 5 {
			return fmt.Errorf("options.log_level must be 0..5 (got: %d)", lvl)
		}
	}

	// Check shell is non-empty (applyDefaults should have set it, but be defensive).
	if cfg.Options.Shell == "" {
		return fmt.Errorf("options.shell must not be empty")
	}

	// Validate each command.
	for name, cmd := range cfg.Commands {
		if cmd.Description == "" {
			// Description is optional.
		}

		// Check env commands are non-empty.
		for env, shellCmd := range cmd.Envs {
			if strings.TrimSpace(shellCmd) == "" {
				return fmt.Errorf("commands.%s.envs.%s: command string must not be empty", name, env)
			}
		}

		// Check fallback commands are non-empty.
		for i, shellCmd := range cmd.Fallback {
			if strings.TrimSpace(shellCmd) == "" {
				return fmt.Errorf("commands.%s.fallback[%d]: command string must not be empty", name, i)
			}
		}
	}

	return nil
}

// applyDefaults fills in missing optional fields with their default values.
func applyDefaults(cfg *Config) {
	if cfg.Options.Shell == "" {
		cfg.Options.Shell = DefaultShell
	}
	if cfg.Options.Notify == "" {
		cfg.Options.Notify = DefaultNotify
	}
	if cfg.Options.Timeout == nil {
		d := DefaultTimeout
		cfg.Options.Timeout = &d
	}
	if cfg.Commands == nil {
		cfg.Commands = make(map[string]Command)
	}
}

// ── Path Resolution ──────────────────────────────────────────────

// ConfigPath resolves the configuration file path for a given binary name.
//
// Priority:
//  1. If explicitPath is non-empty, use it as-is.
//  2. Otherwise, $XDG_CONFIG_HOME/<binary>/config.yaml
//     (XDG_CONFIG_HOME defaults to ~/.config).
func ConfigPath(explicitPath string, binaryName string) string {
	if explicitPath != "" {
		return explicitPath
	}

	xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfigHome == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			xdgConfigHome = filepath.Join(home, ".config")
		}
	}
	if xdgConfigHome == "" {
		return filepath.Join(".config", binaryName, "config.yaml")
	}

	return filepath.Join(xdgConfigHome, binaryName, "config.yaml")
}

// ── Variable Substitution ────────────────────────────────────────

// Substitute replaces ${var} placeholders in a command string with
// values from defaults merged with overrides.
//
// Resolution order:
//  1. overrides["var"] — from --args at invocation time
//  2. defaults["var"] — from the command's args section in config
//  3. "" — variable not found, substituted with empty string
//
// $$ is replaced with a literal $ (escape sequence).
func Substitute(cmd string, defaults, overrides map[string]string) string {
	// Step 1: Handle $$ → literal $. Use a placeholder to avoid interfering
	// with ${var} scanning.
	const dollarPlaceholder = "\x00DOLLAR\x00"
	result := strings.ReplaceAll(cmd, "$$", dollarPlaceholder)

	// Step 2: Replace ${var} patterns.
	result = substituteVars(result, defaults, overrides)

	// Step 3: Restore literal $ from placeholders.
	result = strings.ReplaceAll(result, dollarPlaceholder, "$")

	return result
}

// substituteVars replaces ${var} patterns in s using the resolved argument map.
func substituteVars(s string, defaults, overrides map[string]string) string {
	var buf strings.Builder
	buf.Grow(len(s))

	i := 0
	for i < len(s) {
		// Look for ${ prefix.
		if i+2 < len(s) && s[i] == '$' && s[i+1] == '{' {
			// Find closing brace.
			close := strings.IndexByte(s[i+2:], '}')
			if close == -1 {
				// No closing brace — treat as literal.
				buf.WriteByte(s[i])
				i++
				continue
			}
			close += i + 2 // absolute position

			varName := s[i+2 : close]
			val := resolveVar(varName, defaults, overrides)
			buf.WriteString(val)
			i = close + 1 // skip past }
		} else {
			buf.WriteByte(s[i])
			i++
		}
	}

	return buf.String()
}

// resolveVar looks up a variable name in overrides first, then defaults, then "".
func resolveVar(name string, defaults, overrides map[string]string) string {
	if v, ok := overrides[name]; ok {
		return v
	}
	if v, ok := defaults[name]; ok {
		return v
	}
	return ""
}

// ── Helpers ──────────────────────────────────────────────────────

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

// ResolveAppPaths computes all filesystem paths from the binary name and
// an explicit config path override (--config flag, may be empty).
func ResolveAppPaths(binaryName string, explicitConfig string) AppPaths {
	return AppPaths{
		BinaryName: binaryName,
		ConfigFile: ConfigPath(explicitConfig, binaryName),
		SocketPath: filepath.Join(runtimeDir(), binaryName+".sock"),
		LockPath:   filepath.Join(runtimeDir(), binaryName+".lock"),
	}
}

// runtimeDir returns $XDG_RUNTIME_DIR or a fallback.
func runtimeDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return dir
	}
	return os.TempDir()
}

// cleanupPath removes a file if it exists. Used for stale sockets and lock files.
func cleanupPath(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// CleanupPath is the exported alias of cleanupPath, accessible from other packages.
var CleanupPath = cleanupPath

// FileExists checks whether a path exists and is a regular file.
func FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
