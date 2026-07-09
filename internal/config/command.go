package config

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
