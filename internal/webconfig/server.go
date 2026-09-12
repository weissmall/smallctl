// Package webconfig provides the local browser UI for editing smallctl's
// primary configuration file.
package webconfig

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

	"smallctl/internal/config"
)

var validName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// Server owns a Gin handler for a single main configuration file.
//
// The PoC purposely only writes a standalone main config. A directory with
// additional participating YAML files is displayed read-only, because writing
// the merged configuration back to config.yaml would create duplicate env
// definitions on the next reload.
type Server struct {
	configPath string
	router     *gin.Engine
}

// New creates the web configuration application for configPath.
func New(configPath string) *Server {
	gin.SetMode(gin.ReleaseMode)
	s := &Server{configPath: configPath, router: gin.New()}
	s.router.SetHTMLTemplate(ginTemplate)
	s.router.Use(gin.Recovery())
	s.router.GET("/", s.index)
	s.router.GET("/partials/arg-row", s.argRow)
	s.router.POST("/options", s.updateOptions)
	s.router.POST("/environments", s.createEnvironment)
	s.router.DELETE("/environments/:name", s.deleteEnvironment)
	s.router.POST("/commands", s.createCommand)
	s.router.POST("/commands/:name", s.updateCommand)
	s.router.DELETE("/commands/:name", s.deleteCommand)
	return s
}

// Handler exposes the application to an HTTP server or tests.
func (s *Server) Handler() http.Handler { return s.router }

type pageData struct {
	ConfigPath   string
	Config       *config.Config
	Environments []environmentView
	Commands     []commandView
	NewCommand   commandView
	ReadOnly     bool
	Notice       string
}

type commandView struct {
	Name string
	config.Command
	ArgRows         []frontMatterRow
	EnvironmentRows []environmentCommandRow
	FallbackText    string
}

type frontMatterRow struct {
	Key   string
	Value string
}

type environmentCommandRow struct {
	Name    string
	Command string
}

type environmentView struct {
	Name   string
	Preset bool
	InUse  bool
}

func (s *Server) index(c *gin.Context) {
	cfg, err := s.load()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "page", pageData{ConfigPath: s.configPath, Notice: err.Error()})
		return
	}
	c.HTML(http.StatusOK, "page", s.view(cfg, ""))
}

func (s *Server) argRow(c *gin.Context) {
	c.HTML(http.StatusOK, "arg-row", frontMatterRow{})
}

func (s *Server) updateOptions(c *gin.Context) {
	cfg, err := s.editableConfig()
	if err != nil {
		s.notice(c, http.StatusConflict, err.Error())
		return
	}

	if timeout := strings.TrimSpace(c.PostForm("timeout")); timeout == "" {
		cfg.Options.Timeout = nil
	} else {
		value, parseErr := strconv.Atoi(timeout)
		if parseErr != nil || value < 0 {
			s.notice(c, http.StatusBadRequest, "Timeout must be a whole number greater than or equal to 0.")
			return
		}
		cfg.Options.Timeout = &value
	}
	if logLevel := strings.TrimSpace(c.PostForm("log_level")); logLevel == "" {
		cfg.Options.LogLevel = nil
	} else {
		value, parseErr := strconv.Atoi(logLevel)
		if parseErr != nil || value < 0 || value > 5 {
			s.notice(c, http.StatusBadRequest, "Log level must be a whole number from 0 to 5.")
			return
		}
		cfg.Options.LogLevel = &value
	}
	cfg.Options.EnvCommand = strings.TrimSpace(c.PostForm("env_command"))
	cfg.Options.LogFile = strings.TrimSpace(c.PostForm("log_file"))
	cfg.Options.Notify = strings.TrimSpace(c.PostForm("notify"))
	cfg.Options.Shell = strings.TrimSpace(c.PostForm("shell"))

	if err := s.save(cfg); err != nil {
		s.notice(c, http.StatusBadRequest, err.Error())
		return
	}
	s.notice(c, http.StatusOK, "Global settings saved. The running server will pick up supported changes automatically.")
}

func (s *Server) createEnvironment(c *gin.Context) {
	cfg, err := s.editableConfig()
	if err != nil {
		s.notice(c, http.StatusConflict, err.Error())
		return
	}
	name := strings.TrimSpace(c.PostForm("environment"))
	if !validName.MatchString(name) {
		s.notice(c, http.StatusBadRequest, "Environment names may contain letters, numbers, dots, dashes, and underscores.")
		return
	}
	for _, environment := range cfg.Options.Environments {
		if environment == name {
			s.notice(c, http.StatusConflict, fmt.Sprintf("Environment %q is already prepared.", name))
			return
		}
	}
	cfg.Options.Environments = append(cfg.Options.Environments, name)
	sort.Strings(cfg.Options.Environments)
	if err := s.save(cfg); err != nil {
		s.notice(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Header("HX-Refresh", "true")
	c.HTML(http.StatusCreated, "environment-list", s.environmentViews(cfg))
}

func (s *Server) deleteEnvironment(c *gin.Context) {
	cfg, err := s.editableConfig()
	if err != nil {
		s.notice(c, http.StatusConflict, err.Error())
		return
	}
	name := c.Param("name")
	filtered := make([]string, 0, len(cfg.Options.Environments))
	found := false
	for _, environment := range cfg.Options.Environments {
		if environment == name {
			found = true
			continue
		}
		filtered = append(filtered, environment)
	}
	if !found {
		s.notice(c, http.StatusNotFound, fmt.Sprintf("Environment %q is not a prepared environment.", name))
		return
	}
	cfg.Options.Environments = filtered
	if err := s.save(cfg); err != nil {
		s.notice(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Header("HX-Refresh", "true")
	c.HTML(http.StatusOK, "environment-list", s.environmentViews(cfg))
}

func (s *Server) createCommand(c *gin.Context) {
	cfg, err := s.editableConfig()
	if err != nil {
		s.notice(c, http.StatusConflict, err.Error())
		return
	}

	name := strings.TrimSpace(c.PostForm("name"))
	if !validName.MatchString(name) {
		s.notice(c, http.StatusBadRequest, "Command names may contain letters, numbers, dots, dashes, and underscores.")
		return
	}
	if _, exists := cfg.Commands[name]; exists {
		s.notice(c, http.StatusConflict, fmt.Sprintf("A command named %q already exists.", name))
		return
	}
	command, err := commandFromForm(c)
	if err != nil {
		s.notice(c, http.StatusBadRequest, err.Error())
		return
	}
	cfg.Commands[name] = command
	if err := s.save(cfg); err != nil {
		s.notice(c, http.StatusBadRequest, err.Error())
		return
	}
	c.HTML(http.StatusCreated, "command", s.commandView(name, command, environmentNames(cfg)))
}

func (s *Server) updateCommand(c *gin.Context) {
	cfg, err := s.editableConfig()
	if err != nil {
		s.notice(c, http.StatusConflict, err.Error())
		return
	}
	name := c.Param("name")
	if _, exists := cfg.Commands[name]; !exists {
		s.notice(c, http.StatusNotFound, fmt.Sprintf("Command %q no longer exists.", name))
		return
	}
	command, err := commandFromForm(c)
	if err != nil {
		s.notice(c, http.StatusBadRequest, err.Error())
		return
	}
	cfg.Commands[name] = command
	if err := s.save(cfg); err != nil {
		s.notice(c, http.StatusBadRequest, err.Error())
		return
	}
	s.notice(c, http.StatusOK, fmt.Sprintf("%s saved.", name))
}

func (s *Server) deleteCommand(c *gin.Context) {
	cfg, err := s.editableConfig()
	if err != nil {
		s.notice(c, http.StatusConflict, err.Error())
		return
	}
	name := c.Param("name")
	if _, exists := cfg.Commands[name]; !exists {
		s.notice(c, http.StatusNotFound, fmt.Sprintf("Command %q no longer exists.", name))
		return
	}
	delete(cfg.Commands, name)
	if err := s.save(cfg); err != nil {
		s.notice(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Status(http.StatusOK)
}

func commandFromForm(c *gin.Context) (config.Command, error) {
	args, err := parseFormPairs(c.PostFormArray("args_key"), c.PostFormArray("args_value"), "argument", false)
	if err != nil {
		return config.Command{}, err
	}
	envs, err := parseFormPairs(c.PostFormArray("environment_name"), c.PostFormArray("environment_command"), "environment", true)
	if err != nil {
		return config.Command{}, err
	}
	fallback := nonEmptyLines(c.PostForm("fallback"))
	return config.Command{
		Description: strings.TrimSpace(c.PostForm("description")),
		Args:        args,
		Envs:        envs,
		Fallback:    fallback,
	}, nil
}

func parseFormPairs(keys, values []string, kind string, omitEmptyValue bool) (map[string]string, error) {
	if len(keys) != len(values) {
		return nil, fmt.Errorf("Invalid %s form data.", kind)
	}
	pairs := make(map[string]string)
	for index, key := range keys {
		key = strings.TrimSpace(key)
		value := strings.TrimSpace(values[index])
		if key == "" {
			if value != "" {
				return nil, fmt.Errorf("Each %s must have a name.", kind)
			}
			continue
		}
		if _, exists := pairs[key]; exists {
			return nil, fmt.Errorf("The %s %q is listed more than once.", kind, key)
		}
		if omitEmptyValue && value == "" {
			continue
		}
		pairs[key] = value
	}
	if len(pairs) == 0 {
		return nil, nil
	}
	return pairs, nil
}

func nonEmptyLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func (s *Server) load() (*config.Config, error) {
	cfg, err := config.Load(s.configPath)
	if err != nil {
		return nil, fmt.Errorf("could not load configuration: %w", err)
	}
	return cfg, nil
}

func (s *Server) editableConfig() (*config.Config, error) {
	if s.hasAdditionalFiles() {
		return nil, fmt.Errorf("This configuration uses additional YAML files. The PoC leaves it read-only to prevent duplicate environment definitions. Consolidate it into %s before editing here.", filepath.Base(s.configPath))
	}
	return s.load()
}

func (s *Server) hasAdditionalFiles() bool {
	entries, err := os.ReadDir(filepath.Dir(s.configPath))
	if err != nil {
		return false
	}
	mainName := filepath.Base(s.configPath)
	for _, entry := range entries {
		if entry.Type().IsRegular() && entry.Name() != mainName && entry.Name() != config.MainConfigName && strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasPrefix(entry.Name(), ".") {
			return true
		}
	}
	return false
}

func (s *Server) save(cfg *config.Config) error {
	if err := config.Validate(cfg); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encoding configuration: %w", err)
	}
	dir := filepath.Dir(s.configPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating configuration directory: %w", err)
	}

	mode := fs.FileMode(0o600)
	if info, err := os.Stat(s.configPath); err == nil {
		mode = info.Mode().Perm()
	}
	temp, err := os.CreateTemp(dir, ".smallctl-config-*.yaml")
	if err != nil {
		return fmt.Errorf("creating temporary configuration: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("setting temporary configuration permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("writing configuration: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("closing configuration: %w", err)
	}
	if err := os.Rename(tempName, s.configPath); err != nil {
		return fmt.Errorf("replacing configuration: %w", err)
	}
	return nil
}

func (s *Server) view(cfg *config.Config, notice string) pageData {
	environments := environmentNames(cfg)
	commands := make([]commandView, 0, len(cfg.Commands))
	for name, command := range cfg.Commands {
		commands = append(commands, s.commandView(name, command, environments))
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })
	return pageData{
		ConfigPath:   s.configPath,
		Config:       cfg,
		Environments: s.environmentViews(cfg),
		Commands:     commands,
		NewCommand:   s.commandView("", config.Command{}, environments),
		ReadOnly:     s.hasAdditionalFiles(),
		Notice:       notice,
	}
}

func (s *Server) commandView(name string, command config.Command, environments []string) commandView {
	args := mapRows(command.Args, 2)
	environmentRows := make([]environmentCommandRow, 0, len(environments))
	for _, environment := range environments {
		environmentRows = append(environmentRows, environmentCommandRow{Name: environment, Command: command.Envs[environment]})
	}
	return commandView{
		Name:            name,
		Command:         command,
		ArgRows:         args,
		EnvironmentRows: environmentRows,
		FallbackText:    strings.Join(command.Fallback, "\n"),
	}
}

func mapRows(values map[string]string, minimum int) []frontMatterRow {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]frontMatterRow, 0, max(len(keys), minimum))
	for _, key := range keys {
		rows = append(rows, frontMatterRow{Key: key, Value: values[key]})
	}
	for len(rows) < minimum {
		rows = append(rows, frontMatterRow{})
	}
	return rows
}

func environmentNames(cfg *config.Config) []string {
	names := make(map[string]bool)
	for _, environment := range cfg.Options.Environments {
		names[environment] = true
	}
	for _, command := range cfg.Commands {
		for environment := range command.Envs {
			names[environment] = true
		}
	}
	result := make([]string, 0, len(names))
	for environment := range names {
		result = append(result, environment)
	}
	sort.Strings(result)
	return result
}

func (s *Server) environmentViews(cfg *config.Config) []environmentView {
	presets := make(map[string]bool)
	for _, environment := range cfg.Options.Environments {
		presets[environment] = true
	}
	inUse := make(map[string]bool)
	for _, command := range cfg.Commands {
		for environment := range command.Envs {
			inUse[environment] = true
		}
	}
	names := environmentNames(cfg)
	views := make([]environmentView, 0, len(names))
	for _, name := range names {
		views = append(views, environmentView{Name: name, Preset: presets[name], InUse: inUse[name]})
	}
	return views
}

func (s *Server) notice(c *gin.Context, status int, message string) {
	c.HTML(status, "notice", gin.H{"Message": message})
}
