package config

import (
	"fmt"
	"strings"

	"smallctl/internal/general"
)

func Validate(cfg *Config) error {
	if cfg.Options.Notify != "" && !general.ValidNotifyValues[cfg.Options.Notify] {
		return fmt.Errorf("options.notify must be one of: off, error, all (got: %q)", cfg.Options.Notify)
	}

	if cfg.Options.Timeout != nil && *cfg.Options.Timeout < 0 {
		return fmt.Errorf("options.timeout must be >= 0 (got: %d)", *cfg.Options.Timeout)
	}

	if cfg.Options.LogLevel != nil {
		lvl := *cfg.Options.LogLevel
		if lvl < 0 || lvl > 5 {
			return fmt.Errorf("options.log_level must be 0..5 (got: %d)", lvl)
		}
	}

	if cfg.Options.Shell == "" {
		return fmt.Errorf("options.shell must not be empty")
	}

	environments := make(map[string]bool)
	for _, environment := range cfg.Options.Environments {
		if strings.TrimSpace(environment) == "" {
			return fmt.Errorf("options.environments must not contain an empty name")
		}
		if environments[environment] {
			return fmt.Errorf("options.environments contains duplicate name %q", environment)
		}
		environments[environment] = true
	}

	for name, cmd := range cfg.Commands {
		for env, shellCmd := range cmd.Envs {
			if strings.TrimSpace(shellCmd) == "" {
				return fmt.Errorf("commands.%s.envs.%s: command string must not be empty", name, env)
			}
		}

		for i, shellCmd := range cmd.Fallback {
			if strings.TrimSpace(shellCmd) == "" {
				return fmt.Errorf("commands.%s.fallback[%d]: command string must not be empty", name, i)
			}
		}
	}

	return nil
}
