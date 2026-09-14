package config

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"smallctl/internal/general"
)

// MainConfigName is the file name of the main config file. It is always
// parsed in full format and is never treated as an env file.
const MainConfigName = "config.yaml"

const yamlExt = ".yaml"

// Load reads the main config file at path and every additional YAML file
// placed next to it, and combines them into a single Config:
//
//   - The file at path is the main config (options plus full-format commands).
//     It may be missing, in which case loading continues with the other files.
//   - Any sibling *.yaml file with a commands section is an env file: its file
//     name (without extension) becomes the key under envs for its commands.
//   - Any sibling *.yaml file without a commands section is merged as
//     main-format (its options section).
//
// Hidden files, non-.yaml files, and directories are ignored. A sibling named
// config.yaml is only loaded when it is the main config itself.
func Load(path string) (*Config, error) {
	cfg := &Config{Commands: make(map[string]Command)}
	merger := newOptionsMerger()
	envOrigins := make(map[string]map[string]string)

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := parseMainConfig(cfg, data, path, merger, envOrigins); err != nil {
			return nil, err
		}
	case os.IsNotExist(err):
	default:
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}

	optionFiles, envFiles, err := scanConfigDir(path)
	if err != nil {
		return nil, err
	}

	for _, file := range optionFiles {
		if err := mergeOptionsFile(merger, file); err != nil {
			return nil, err
		}
	}

	for _, file := range envFiles {
		envName := strings.TrimSuffix(filepath.Base(file.path), yamlExt)
		if err := mergeEnvFile(cfg, file, envName, envOrigins); err != nil {
			return nil, err
		}
	}

	merger.applyTo(&cfg.Options)
	applyDefaults(cfg)
	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}
	return cfg, nil
}

func parseMainConfig(cfg *Config, data []byte, path string, merger *optionsMerger, envOrigins map[string]map[string]string) error {
	var parsed Config
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("parsing config file %s: %w", path, err)
	}

	if err := merger.merge(optionsPatchFrom(parsed.Options), filepath.Base(path)); err != nil {
		return err
	}

	if parsed.Commands == nil {
		parsed.Commands = make(map[string]Command)
	}
	cfg.Commands = parsed.Commands
	for _, name := range slices.Sorted(maps.Keys(parsed.Commands)) {
		for env := range parsed.Commands[name].Envs {
			recordEnvOrigin(envOrigins, name, env, filepath.Base(path))
		}
	}
	return nil
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
	if cfg.Options.FailedCommandTTL == nil {
		d := general.DefaultFailedCommandTTL
		cfg.Options.FailedCommandTTL = &d
	}
	if cfg.Commands == nil {
		cfg.Commands = make(map[string]Command)
	}
}
