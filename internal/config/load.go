package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/weissmall/smallctl/internal/general"
)

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
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

func applyDefaults(cfg *Config) {
	if cfg.Options.Shell == "" {
		cfg.Options.Shell = general.DefaultShell
	}
	if cfg.Options.Notify == "" {
		cfg.Options.Notify = general.DefaultNotify
	}
	if cfg.Options.Timeout == nil {
		d := general.DefaultTimeout
		cfg.Options.Timeout = &d
	}
	if cfg.Commands == nil {
		cfg.Commands = make(map[string]Command)
	}
}
