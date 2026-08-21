package config

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

type configFile struct {
	path string
	data []byte
}

func scanConfigDir(mainPath string) ([]configFile, []configFile, error) {
	dir := filepath.Dir(mainPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("reading config directory %s: %w", dir, err)
	}

	mainBase := filepath.Base(mainPath)
	var optionFiles, envFiles []configFile
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isConfigFileName(name) {
			continue
		}
		if name == mainBase || name == MainConfigName {
			continue
		}

		path := filepath.Join(dir, name)
		if !isRegularFile(path) {
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("reading config file %s: %w", path, err)
		}

		isEnv, err := isEnvFile(data, path)
		if err != nil {
			return nil, nil, err
		}

		file := configFile{path: path, data: data}
		if isEnv {
			envFiles = append(envFiles, file)
		} else {
			optionFiles = append(optionFiles, file)
		}
	}

	sortByPath := func(files []configFile) {
		slices.SortFunc(files, func(a, b configFile) int {
			return strings.Compare(a.path, b.path)
		})
	}
	sortByPath(optionFiles)
	sortByPath(envFiles)
	return optionFiles, envFiles, nil
}

// isConfigFileName reports whether name is a YAML file that participates in
// config loading: non-hidden with a .yaml extension.
func isConfigFileName(name string) bool {
	return !strings.HasPrefix(name, ".") && strings.HasSuffix(name, yamlExt)
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// isEnvFile reports whether the YAML document defines a commands section and
// is therefore an env file.
func isEnvFile(data []byte, path string) (bool, error) {
	top, err := parseTopLevel(data, path)
	if err != nil {
		return false, err
	}

	node, ok := top["commands"]
	if !ok || node.Tag == "!!null" {
		return false, nil
	}
	if node.Kind != yaml.MappingNode {
		return false, fmt.Errorf("parsing config file %s: commands must be a mapping", path)
	}
	return true, nil
}

func parseTopLevel(data []byte, path string) (map[string]yaml.Node, error) {
	var top map[string]yaml.Node
	if err := yaml.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w", path, err)
	}
	return top, nil
}

func mergeOptionsFile(merger *optionsMerger, file configFile) error {
	top, err := parseTopLevel(file.data, file.path)
	if err != nil {
		return err
	}

	patch := optionsPatch{}
	if node, ok := top["options"]; ok && node.Tag != "!!null" {
		if err := node.Decode(&patch); err != nil {
			return fmt.Errorf("parsing config file %s: %w", file.path, err)
		}
	}
	return merger.merge(patch, filepath.Base(file.path))
}

func mergeEnvFile(cfg *Config, file configFile, envName string, envOrigins map[string]map[string]string) error {
	top, err := parseTopLevel(file.data, file.path)
	if err != nil {
		return err
	}

	if node, ok := top["options"]; ok && node.Tag != "!!null" {
		return fmt.Errorf(
			"config file %s: options are not allowed in env files; put options into %s or into a file without a commands section",
			file.path, MainConfigName,
		)
	}

	commandsNode, ok := top["commands"]
	if !ok || commandsNode.Tag == "!!null" {
		return nil
	}

	var commands map[string]yaml.Node
	if err := commandsNode.Decode(&commands); err != nil {
		return fmt.Errorf("parsing config file %s: %w", file.path, err)
	}

	for _, name := range slices.Sorted(maps.Keys(commands)) {
		command, err := commandFromNode(commands[name], envName, file.path, name)
		if err != nil {
			return err
		}
		if err := mergeCommand(cfg, name, command, filepath.Base(file.path), envOrigins); err != nil {
			return err
		}
	}
	return nil
}

// commandFromNode interprets one entry of an env file's commands section.
// A string value is shorthand for envs: {<envName>: <value>}; a mapping is
// a full command definition merged as written.
func commandFromNode(node yaml.Node, envName, path, name string) (Command, error) {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag != "!!str" {
			return Command{}, fmt.Errorf("config file %s: commands.%s must be a command string or a mapping", path, name)
		}
		if strings.TrimSpace(node.Value) == "" {
			return Command{}, fmt.Errorf("config file %s: commands.%s: command string must not be empty", path, name)
		}
		return Command{Envs: map[string]string{envName: node.Value}}, nil

	case yaml.MappingNode:
		var command Command
		if err := node.Decode(&command); err != nil {
			return Command{}, fmt.Errorf("parsing config file %s: commands.%s: %w", path, name, err)
		}
		return command, nil

	default:
		return Command{}, fmt.Errorf("config file %s: commands.%s must be a command string or a mapping", path, name)
	}
}

// mergeCommand merges a command definition into cfg. Fields set by the
// incoming definition override the existing one; envs maps are union-merged
// and a duplicate command+env pair is an error.
func mergeCommand(cfg *Config, name string, command Command, file string, envOrigins map[string]map[string]string) error {
	existing, ok := cfg.Commands[name]
	if !ok {
		cfg.Commands[name] = command
		for env := range command.Envs {
			recordEnvOrigin(envOrigins, name, env, file)
		}
		return nil
	}

	if command.Description != "" {
		existing.Description = command.Description
	}

	if len(command.Args) > 0 {
		if existing.Args == nil {
			existing.Args = make(map[string]string)
		}
		maps.Copy(existing.Args, command.Args)
	}

	if len(command.Envs) > 0 {
		if existing.Envs == nil {
			existing.Envs = make(map[string]string)
		}
		for env, value := range command.Envs {
			if prev := lookupEnvOrigin(envOrigins, name, env); prev != "" {
				return fmt.Errorf("conflict in commands.%s: env %q is defined in both %s and %s", name, env, prev, file)
			}
			existing.Envs[env] = value
			recordEnvOrigin(envOrigins, name, env, file)
		}
	}

	if len(command.Fallback) > 0 {
		existing.Fallback = command.Fallback
	}

	cfg.Commands[name] = existing
	return nil
}

func recordEnvOrigin(envOrigins map[string]map[string]string, command, env, file string) {
	envs, ok := envOrigins[command]
	if !ok {
		envs = make(map[string]string)
		envOrigins[command] = envs
	}
	envs[env] = file
}

func lookupEnvOrigin(envOrigins map[string]map[string]string, command, env string) string {
	return envOrigins[command][env]
}

// optionsPatch mirrors Options with pointer fields so that explicitly set
// values can be distinguished from unset ones while merging files.
type optionsPatch struct {
	EnvCommand *string `yaml:"env_command"`
	LogLevel   *int    `yaml:"log_level"`
	LogFile    *string `yaml:"log_file"`
	Notify     *string `yaml:"notify"`
	Timeout    *int    `yaml:"timeout"`
	Shell      *string `yaml:"shell"`
}

func optionsPatchFrom(options Options) optionsPatch {
	patch := optionsPatch{}
	if options.EnvCommand != "" {
		patch.EnvCommand = &options.EnvCommand
	}
	if options.LogLevel != nil {
		patch.LogLevel = options.LogLevel
	}
	if options.LogFile != "" {
		patch.LogFile = &options.LogFile
	}
	if options.Notify != "" {
		patch.Notify = &options.Notify
	}
	if options.Timeout != nil {
		patch.Timeout = options.Timeout
	}
	if options.Shell != "" {
		patch.Shell = &options.Shell
	}
	return patch
}

// optionsMerger accumulates options from multiple files. The same field may
// be set by several files as long as the values are identical; conflicting
// values are an error.
type optionsMerger struct {
	values  optionsPatch
	origins map[string]string
}

func newOptionsMerger() *optionsMerger {
	return &optionsMerger{origins: make(map[string]string)}
}

func (m *optionsMerger) merge(patch optionsPatch, file string) error {
	fields := []struct {
		name    string
		current **string
		next    *string
	}{
		{"env_command", &m.values.EnvCommand, patch.EnvCommand},
		{"log_file", &m.values.LogFile, patch.LogFile},
		{"notify", &m.values.Notify, patch.Notify},
		{"shell", &m.values.Shell, patch.Shell},
	}
	for _, field := range fields {
		if err := mergeOptionField(field.name, field.current, field.next, file, m.origins); err != nil {
			return err
		}
	}

	numbers := []struct {
		name    string
		current **int
		next    *int
	}{
		{"log_level", &m.values.LogLevel, patch.LogLevel},
		{"timeout", &m.values.Timeout, patch.Timeout},
	}
	for _, field := range numbers {
		if err := mergeOptionField(field.name, field.current, field.next, file, m.origins); err != nil {
			return err
		}
	}
	return nil
}

func mergeOptionField[T comparable](field string, current **T, next *T, file string, origins map[string]string) error {
	if next == nil {
		return nil
	}
	if *current != nil {
		if prev, exists := origins[field]; exists {
			if **current != *next {
				return fmt.Errorf("conflict in options.%s: %v (set in %s) and %v (set in %s)", field, **current, prev, *next, file)
			}
			return nil
		}
	}
	*current = next
	origins[field] = file
	return nil
}

func (m *optionsMerger) applyTo(options *Options) {
	if m.values.EnvCommand != nil {
		options.EnvCommand = *m.values.EnvCommand
	}
	if m.values.LogLevel != nil {
		options.LogLevel = m.values.LogLevel
	}
	if m.values.LogFile != nil {
		options.LogFile = *m.values.LogFile
	}
	if m.values.Notify != nil {
		options.Notify = *m.values.Notify
	}
	if m.values.Timeout != nil {
		options.Timeout = m.values.Timeout
	}
	if m.values.Shell != nil {
		options.Shell = *m.values.Shell
	}
}
