// Package webconfig provides the local browser UI for editing smallctl's
// primary configuration file.
package webconfig

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"smallctl/internal/config"
	"smallctl/internal/general"
)

var validName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// Server owns a Gin handler and an in-memory configuration draft. The draft is
// deliberately never written to disk: this UI iteration is safe to explore
// against both single-file and split-file configurations.
type Server struct {
	configPath string
	router     *gin.Engine
	mu         sync.RWMutex
	draft      *config.Config
}

// New creates the web configuration application for configPath.
func New(configPath string) *Server {
	gin.SetMode(gin.ReleaseMode)
	s := &Server{configPath: configPath, router: gin.New()}
	s.router.SetHTMLTemplate(ginTemplate)
	s.router.Use(gin.Recovery())
	s.router.GET("/", s.index)
	s.router.GET("/partials/arg-row", s.argRow)
	s.router.GET("/partials/fallback-row", s.fallbackRow)
	s.router.POST("/environment-test", s.testEnvironment)
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
	ConfigPath        string
	Config            *config.Config
	ActiveEnvironment activeEnvironment
	Environments      []environmentView
	Commands          []commandView
	NewCommand        commandView
	Notice            string
}

type commandView struct {
	Name string
	config.Command
	ArgRows           []frontMatterRow
	EnvironmentRows   []environmentCommandRow
	FallbackRows      []fallbackRow
	ActiveEnvironment string
	Preview           string
	PreviewSource     string
}

type frontMatterRow struct {
	Key   string
	Value string
}

type environmentCommandRow struct {
	Name    string
	Command string
	Active  bool
}

type fallbackRow struct{ Command string }

type activeEnvironment struct {
	Name   string
	Source string
}

type environmentView struct {
	Name          string
	Configuration string
	StatusClass   string
	Active        bool
}

type environmentTestResult struct {
	Success bool
	Message string
	Output  string
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

func (s *Server) fallbackRow(c *gin.Context) {
	c.HTML(http.StatusOK, "fallback-row", fallbackRow{})
}

func (s *Server) testEnvironment(c *gin.Context) {
	shell := strings.TrimSpace(c.PostForm("shell"))
	command := strings.TrimSpace(c.PostForm("env_command"))
	if shell == "" {
		c.HTML(http.StatusBadRequest, "environment-test-result", environmentTestResult{Message: "Shell is required before the environment command can be tested."})
		return
	}
	if command == "" {
		c.HTML(http.StatusBadRequest, "environment-test-result", environmentTestResult{Message: "No environment command is configured. smallctl will use SMALLCTL_ENV when it is set."})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), general.EnvResolveTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, shell, "-c", command).CombinedOutput()
	result := environmentTestResult{Output: strings.TrimSpace(string(output))}
	if ctx.Err() == context.DeadlineExceeded {
		result.Message = "The environment command timed out."
		c.HTML(http.StatusGatewayTimeout, "environment-test-result", result)
		return
	}
	if err != nil {
		result.Message = "The environment command failed."
		c.HTML(http.StatusBadRequest, "environment-test-result", result)
		return
	}
	result.Success = true
	if result.Output == "" {
		result.Message = "The command succeeded but did not return an environment name."
	} else {
		result.Message = "The command succeeded. smallctl would use this environment name:"
	}
	c.HTML(http.StatusOK, "environment-test-result", result)
}

func (s *Server) updateOptions(c *gin.Context) {
	cfg, err := s.load()
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
	c.Header("HX-Refresh", "true")
	s.notice(c, http.StatusOK, "Global settings saved to this in-memory preview. No configuration files were changed.")
}

func (s *Server) createEnvironment(c *gin.Context) {
	cfg, err := s.load()
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
	c.HTML(http.StatusCreated, "environment-list", s.environmentViews(cfg, resolveActiveEnvironment(cfg).Name))
}

func (s *Server) deleteEnvironment(c *gin.Context) {
	cfg, err := s.load()
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
	for commandName, command := range cfg.Commands {
		if _, exists := command.Envs[name]; exists {
			delete(command.Envs, name)
			cfg.Commands[commandName] = command
			found = true
		}
	}
	if !found {
		s.notice(c, http.StatusNotFound, fmt.Sprintf("Environment %q does not exist.", name))
		return
	}
	cfg.Options.Environments = filtered
	if err := s.save(cfg); err != nil {
		s.notice(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Header("HX-Refresh", "true")
	c.HTML(http.StatusOK, "environment-list", s.environmentViews(cfg, resolveActiveEnvironment(cfg).Name))
}

func (s *Server) createCommand(c *gin.Context) {
	cfg, err := s.load()
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
	active := resolveActiveEnvironment(cfg)
	c.Header("HX-Refresh", "true")
	c.HTML(http.StatusCreated, "command", s.commandView(name, command, environmentNames(cfg, active.Name), active.Name))
}

func (s *Server) updateCommand(c *gin.Context) {
	cfg, err := s.load()
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
	c.Header("HX-Refresh", "true")
	s.notice(c, http.StatusOK, fmt.Sprintf("%s saved to this in-memory preview. No configuration files were changed.", name))
}

func (s *Server) deleteCommand(c *gin.Context) {
	cfg, err := s.load()
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
	c.Header("HX-Refresh", "true")
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
	fallback := nonEmptyValues(c.PostFormArray("fallback_command"))
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

func nonEmptyValues(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func (s *Server) load() (*config.Config, error) {
	s.mu.RLock()
	if s.draft != nil {
		draft := cloneConfig(s.draft)
		s.mu.RUnlock()
		return draft, nil
	}
	s.mu.RUnlock()

	cfg, err := config.Load(s.configPath)
	if err != nil {
		return nil, fmt.Errorf("could not load configuration: %w", err)
	}
	s.mu.Lock()
	if s.draft == nil {
		s.draft = cloneConfig(cfg)
	}
	draft := cloneConfig(s.draft)
	s.mu.Unlock()
	return draft, nil
}

func (s *Server) save(cfg *config.Config) error {
	if err := config.Validate(cfg); err != nil {
		return err
	}
	s.mu.Lock()
	s.draft = cloneConfig(cfg)
	s.mu.Unlock()
	return nil
}

func (s *Server) view(cfg *config.Config, notice string) pageData {
	active := resolveActiveEnvironment(cfg)
	environments := environmentNames(cfg, active.Name)
	commands := make([]commandView, 0, len(cfg.Commands))
	for name, command := range cfg.Commands {
		commands = append(commands, s.commandView(name, command, environments, active.Name))
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })
	return pageData{
		ConfigPath:        s.configPath,
		Config:            cfg,
		ActiveEnvironment: active,
		Environments:      s.environmentViews(cfg, active.Name),
		Commands:          commands,
		NewCommand:        s.commandView("", config.Command{}, environments, active.Name),
		Notice:            notice,
	}
}

func (s *Server) commandView(name string, command config.Command, environments []string, activeEnvironment string) commandView {
	args := mapRows(command.Args, 2)
	environmentRows := make([]environmentCommandRow, 0, len(environments))
	for _, environment := range environments {
		environmentRows = append(environmentRows, environmentCommandRow{Name: environment, Command: command.Envs[environment], Active: environment == activeEnvironment})
	}
	return commandView{
		Name:              name,
		Command:           command,
		ArgRows:           args,
		EnvironmentRows:   environmentRows,
		FallbackRows:      fallbackRows(command.Fallback),
		ActiveEnvironment: activeEnvironment,
		Preview:           commandPreview(command, activeEnvironment),
		PreviewSource:     commandPreviewSource(command, activeEnvironment),
	}
}

func commandPreview(command config.Command, activeEnvironment string) string {
	if activeEnvironment != "" {
		if environmentCommand := strings.TrimSpace(command.Envs[activeEnvironment]); environmentCommand != "" {
			return config.Substitute(environmentCommand, command.Args, nil)
		}
	}
	if len(command.Fallback) > 0 {
		return config.Substitute(command.Fallback[0], command.Args, nil)
	}
	return "No command is configured for this environment."
}

func commandPreviewSource(command config.Command, activeEnvironment string) string {
	if activeEnvironment != "" && strings.TrimSpace(command.Envs[activeEnvironment]) != "" {
		return "active environment"
	}
	if len(command.Fallback) > 0 {
		return "first fallback"
	}
	return "command preview"
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

func fallbackRows(fallback []string) []fallbackRow {
	rows := make([]fallbackRow, 0, max(1, len(fallback)))
	for _, command := range fallback {
		rows = append(rows, fallbackRow{Command: command})
	}
	if len(rows) == 0 {
		rows = append(rows, fallbackRow{})
	}
	return rows
}

func environmentNames(cfg *config.Config, activeEnvironment string) []string {
	names := make(map[string]bool)
	for _, environment := range cfg.Options.Environments {
		names[environment] = true
	}
	for _, command := range cfg.Commands {
		for environment := range command.Envs {
			names[environment] = true
		}
	}
	if activeEnvironment != "" {
		names[activeEnvironment] = true
	}
	result := make([]string, 0, len(names))
	for environment := range names {
		result = append(result, environment)
	}
	sort.Strings(result)
	return result
}

func (s *Server) environmentViews(cfg *config.Config, activeEnvironment string) []environmentView {
	names := environmentNames(cfg, "")
	views := make([]environmentView, 0, len(names))
	for _, name := range names {
		configured := 0
		for _, command := range cfg.Commands {
			if strings.TrimSpace(command.Envs[name]) != "" {
				configured++
			}
		}
		status := "empty"
		statusClass := "empty"
		if configured == len(cfg.Commands) && configured > 0 {
			status = "complete"
			statusClass = "complete"
		} else if configured > 0 {
			status = fmt.Sprintf("partial (%d/%d commands)", configured, len(cfg.Commands))
			statusClass = "partial"
		}
		views = append(views, environmentView{Name: name, Configuration: status, StatusClass: statusClass, Active: name == activeEnvironment})
	}
	return views
}

func resolveActiveEnvironment(cfg *config.Config) activeEnvironment {
	if cfg.Options.EnvCommand != "" {
		ctx, cancel := context.WithTimeout(context.Background(), general.EnvResolveTimeout)
		defer cancel()
		output, err := exec.CommandContext(ctx, cfg.Options.Shell, "-c", cfg.Options.EnvCommand).Output()
		if err == nil {
			if name := strings.TrimSpace(string(output)); name != "" {
				return activeEnvironment{Name: name, Source: "resolved by environment command"}
			}
		}
	}
	if name := strings.TrimSpace(os.Getenv("SMALLCTL_ENV")); name != "" {
		return activeEnvironment{Name: name, Source: "read from SMALLCTL_ENV"}
	}
	return activeEnvironment{Source: "not set — fallback commands will be used"}
}

func cloneConfig(cfg *config.Config) *config.Config {
	clone := &config.Config{Options: cfg.Options, Commands: make(map[string]config.Command, len(cfg.Commands))}
	clone.Options.Environments = slices.Clone(cfg.Options.Environments)
	if cfg.Options.LogLevel != nil {
		value := *cfg.Options.LogLevel
		clone.Options.LogLevel = &value
	}
	if cfg.Options.Timeout != nil {
		value := *cfg.Options.Timeout
		clone.Options.Timeout = &value
	}
	for name, command := range cfg.Commands {
		clone.Commands[name] = config.Command{
			Description: command.Description,
			Args:        maps.Clone(command.Args),
			Envs:        maps.Clone(command.Envs),
			Fallback:    slices.Clone(command.Fallback),
		}
	}
	return clone
}

func (s *Server) notice(c *gin.Context, status int, message string) {
	c.HTML(status, "notice", gin.H{"Message": message})
}
