package executor

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	"smallctl/internal/config"
	"smallctl/internal/general"
	"smallctl/internal/protocol"
)

func New(logger *slog.Logger) *Executor {
	return &Executor{
		Logger:          logger,
		Shell:           general.DefaultShell,
		DefaultTimeout:  general.DefaultTimeout * time.Second,
		failedCommands:  make(map[string]time.Time),
		failureCacheTTL: general.DefaultFailedCommandTTL * time.Second,
	}
}

func (e *Executor) SetShell(shell string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Shell = shell
}

func (e *Executor) SetDefaultTimeout(timeout time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.DefaultTimeout = timeout
}

// SetEnvironment sets a runtime environment override. It takes precedence over
// options.env_command and $SMALLCTL_ENV until the server stops.
func (e *Executor) SetEnvironment(environment string) {
	e.mu.Lock()
	e.EnvName = strings.TrimSpace(environment)
	e.environmentOverride = true
	stop, ttl, version := e.resetFailureCacheLocked()
	e.mu.Unlock()
	e.startFailureCacheWorker(stop, ttl, version)

	e.Logger.Info("environment set at runtime", "env", strings.TrimSpace(environment))
}

// ConfigureFailureCache applies the failed-command cache settings and probes
// simple command invocations immediately. It is called on startup and each
// config reload; the probe is then repeated at the configured TTL.
func (e *Executor) ConfigureFailureCache(cfg *config.Config) {
	ttl := general.DefaultFailedCommandTTL * time.Second
	if cfg.Options.FailedCommandTTL != nil {
		ttl = time.Duration(*cfg.Options.FailedCommandTTL) * time.Second
	}

	probes := configuredCommandTemplates(cfg)

	e.mu.Lock()
	e.stopFailureCacheWorkerLocked()
	e.clearFailedCommandsLocked()
	e.failureCacheTTL = ttl
	e.failureProbeCommands = probes
	e.failureCacheVersion++
	version := e.failureCacheVersion
	if ttl > 0 {
		e.failureCacheStop = make(chan struct{})
	}
	stop := e.failureCacheStop
	e.mu.Unlock()

	e.startFailureCacheWorker(stop, ttl, version)
}

// ClearFailedCommands removes all temporary failure marks.
func (e *Executor) ClearFailedCommands() {
	e.mu.Lock()
	stop, ttl, version := e.resetFailureCacheLocked()
	e.mu.Unlock()
	e.startFailureCacheWorker(stop, ttl, version)
	e.Logger.Info("cleared failed command cache")
}

// Close stops the background command availability probe.
func (e *Executor) Close() {
	e.mu.Lock()
	e.stopFailureCacheWorkerLocked()
	e.mu.Unlock()
}

// Environment returns the active environment name.
func (e *Executor) Environment() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.EnvName
}

func (e *Executor) ResolveEnv(cfg *config.Config) error {
	e.mu.RLock()
	overridden := e.environmentOverride
	activeEnvironment := e.EnvName
	e.mu.RUnlock()
	if overridden {
		e.Logger.Debug("using runtime environment override", "env", activeEnvironment)
		return nil
	}

	if cfg.Options.EnvCommand != "" {
		ctx, cancel := context.WithTimeout(context.Background(), general.EnvResolveTimeout)
		defer cancel()

		e.mu.RLock()
		shell := e.Shell
		e.mu.RUnlock()
		cmd := exec.CommandContext(ctx, shell, "-c", cfg.Options.EnvCommand)
		cmd.Stderr = nil
		out, err := cmd.Output()
		if err == nil {
			e.setResolvedEnvironment(strings.TrimSpace(string(out)))
			e.Logger.Info("environment resolved via env_command",
				"command", cfg.Options.EnvCommand, "env", e.Environment())
			return nil
		}
		e.Logger.Warn("env_command failed, falling back to $SMALLCTL_ENV",
			"command", cfg.Options.EnvCommand, "error", err)
	}

	if env := os.Getenv("SMALLCTL_ENV"); env != "" {
		e.setResolvedEnvironment(strings.TrimSpace(env))
		e.Logger.Info("environment resolved via $SMALLCTL_ENV", "env", e.Environment())
		return nil
	}

	e.setResolvedEnvironment("")
	e.Logger.Debug("no environment configured, fallback chain will always be used")
	return nil
}

func (e *Executor) setResolvedEnvironment(environment string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.environmentOverride {
		e.EnvName = environment
	}
}

func (e *Executor) Execute(cfg *config.Config, req protocol.Request) protocol.Response {
	resp := protocol.Response{
		Success: false,
		Tried:   make([]string, 0),
		Skipped: make([]string, 0),
		Errors:  make([]protocol.ErrorEntry, 0),
	}

	cmd, ok := cfg.Commands[req.Command]
	if !ok {
		resp.Stderr = "command not found: " + req.Command
		resp.ExitCode = 1
		e.Logger.Warn("command not found", "command", req.Command)
		return resp
	}

	resolvedArgs := mergeArgs(cmd.Args, req.Args)

	tryList := make([]commandAttempt, 0)

	if environment := e.Environment(); environment != "" {
		if envCmd, ok := cmd.Envs[environment]; ok && strings.TrimSpace(envCmd) != "" {
			substituted := config.Substitute(envCmd, cmd.Args, resolvedArgs)
			tryList = append(tryList, commandAttempt{template: envCmd, command: substituted})
		}
	}

	for _, fb := range cmd.Fallback {
		substituted := config.Substitute(fb, cmd.Args, resolvedArgs)
		tryList = append(tryList, commandAttempt{template: fb, command: substituted})
	}

	if len(tryList) == 0 {
		resp.Stderr = "no commands defined for: " + req.Command
		resp.ExitCode = 1
		e.Logger.Warn("no commands to execute", "command", req.Command)
		return resp
	}

	var timeout time.Duration
	if cfg.Options.Timeout != nil && *cfg.Options.Timeout > 0 {
		timeout = time.Duration(*cfg.Options.Timeout) * time.Second
	} else if cfg.Options.Timeout != nil && *cfg.Options.Timeout == 0 {
		timeout = 0
	} else {
		e.mu.RLock()
		timeout = e.DefaultTimeout
		e.mu.RUnlock()
	}

	cacheVersion := e.failureCacheVersionValue()
	lastExitCode := 1
	for _, attempt := range tryList {
		if e.isFailedCommand(attempt.template, cacheVersion) {
			resp.Skipped = append(resp.Skipped, attempt.command)
			e.Logger.Debug("skipping failed command", "command", req.Command, "cmd", attempt.command)
			continue
		}

		cmdStr := attempt.command
		resp.Tried = append(resp.Tried, cmdStr)

		e.Logger.Debug("executing command", "command", req.Command, "cmd", cmdStr)

		stdout, stderr, exitCode, execErr := e.runCmd(cmdStr, timeout)

		if execErr != nil {
			entry := protocol.ErrorEntry{
				Command:  cmdStr,
				ExitCode: exitCode,
				Stderr:   execErr.Error(),
			}
			resp.Errors = append(resp.Errors, entry)
			lastExitCode = exitCode
			e.markFailedCommand(attempt.template, cacheVersion)
			e.Logger.Warn("command execution error",
				"command", req.Command, "cmd", cmdStr, "error", execErr)
			continue
		}

		if exitCode == 0 {
			e.clearFailedCommand(attempt.template, cacheVersion)
			resp.Success = true
			resp.ExitCode = 0
			resp.Stdout = stdout
			resp.Stderr = stderr
			e.Logger.Debug("command succeeded",
				"command", req.Command, "cmd", cmdStr)
			return resp
		}

		entry := protocol.ErrorEntry{
			Command:  cmdStr,
			ExitCode: exitCode,
			Stderr:   stderr,
		}
		resp.Errors = append(resp.Errors, entry)
		lastExitCode = exitCode
		e.markFailedCommand(attempt.template, cacheVersion)
	}

	resp.Success = false
	resp.ExitCode = lastExitCode
	if len(resp.Errors) > 0 {
		resp.Stderr = resp.Errors[len(resp.Errors)-1].Stderr
	} else if len(resp.Skipped) > 0 {
		resp.Stderr = "all configured commands are temporarily skipped after recent failures"
	}
	e.Logger.Warn("all attempts failed",
		"command", req.Command, "tried", len(resp.Tried), "skipped", len(resp.Skipped), "errors", len(resp.Errors))

	return resp
}

type commandAttempt struct {
	template string
	command  string
}

func configuredCommandTemplates(cfg *config.Config) []string {
	seen := make(map[string]struct{})
	for _, command := range cfg.Commands {
		for _, envCommand := range command.Envs {
			seen[envCommand] = struct{}{}
		}
		for _, fallback := range command.Fallback {
			seen[fallback] = struct{}{}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

func (e *Executor) failureCacheWorker(stop <-chan struct{}, interval time.Duration, version uint64) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			e.refreshCommandAvailability(version)
		case <-stop:
			return
		}
	}
}

func (e *Executor) refreshCommandAvailability(version uint64) {
	e.mu.RLock()
	if version != e.failureCacheVersion || e.failureCacheTTL <= 0 {
		e.mu.RUnlock()
		return
	}
	shell := e.Shell
	templates := slices.Clone(e.failureProbeCommands)
	e.mu.RUnlock()

	for _, template := range templates {
		program, ok := probeableProgram(template)
		if !ok {
			continue
		}
		available, checked := commandAvailable(shell, program)
		if !checked {
			continue
		}
		if available {
			e.clearFailedCommand(template, version)
			continue
		}
		if e.markFailedCommand(template, version) {
			e.Logger.Warn("command unavailable; temporarily skipping", "command", template, "program", program)
		}
	}
}

// probeableProgram extracts a command name only when doing so is unambiguous.
// Shell grammar is intentionally not interpreted here: complex commands are
// allowed to run and are cached only after an actual failure.
func probeableProgram(template string) (string, bool) {
	fields := strings.Fields(template)
	if len(fields) == 0 {
		return "", false
	}
	program := fields[0]
	if strings.HasPrefix(program, "-") || strings.Contains(program, "=") || strings.ContainsAny(program, "|;&()<>`$'\\\"") {
		return "", false
	}
	return program, true
}

func commandAvailable(shell, program string) (available, checked bool) {
	ctx, cancel := context.WithTimeout(context.Background(), general.CommandProbeTimeout)
	defer cancel()
	err := exec.CommandContext(ctx, shell, "-c", "command -v \"$1\" >/dev/null", shell, program).Run()
	if err == nil {
		return true, true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false, true
	}
	return false, false
}

func (e *Executor) isFailedCommand(template string, version uint64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if version != e.failureCacheVersion {
		return false
	}
	expiresAt, ok := e.failedCommands[template]
	if !ok {
		return false
	}
	if !time.Now().Before(expiresAt) {
		delete(e.failedCommands, template)
		return false
	}
	return true
}

func (e *Executor) markFailedCommand(template string, version uint64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if version != e.failureCacheVersion || e.failureCacheTTL <= 0 {
		return false
	}
	_, alreadyFailed := e.failedCommands[template]
	e.failedCommands[template] = time.Now().Add(e.failureCacheTTL)
	return !alreadyFailed
}

func (e *Executor) clearFailedCommand(template string, version uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if version == e.failureCacheVersion {
		delete(e.failedCommands, template)
	}
}

func (e *Executor) failureCacheVersionValue() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.failureCacheVersion
}

func (e *Executor) clearFailedCommandsLocked() {
	clear(e.failedCommands)
}

func (e *Executor) stopFailureCacheWorkerLocked() {
	if e.failureCacheStop != nil {
		close(e.failureCacheStop)
		e.failureCacheStop = nil
	}
}

func (e *Executor) resetFailureCacheLocked() (stop <-chan struct{}, ttl time.Duration, version uint64) {
	e.stopFailureCacheWorkerLocked()
	e.clearFailedCommandsLocked()
	e.failureCacheVersion++
	if e.failureCacheTTL <= 0 {
		return nil, 0, e.failureCacheVersion
	}
	e.failureCacheStop = make(chan struct{})
	return e.failureCacheStop, e.failureCacheTTL, e.failureCacheVersion
}

func (e *Executor) startFailureCacheWorker(stop <-chan struct{}, ttl time.Duration, version uint64) {
	if stop == nil || ttl <= 0 {
		return
	}
	e.refreshCommandAvailability(version)
	go e.failureCacheWorker(stop, ttl, version)
}

func (e *Executor) runCmd(cmdStr string, timeout time.Duration) (stdout, stderr string, exitCode int, err error) {
	ctx := context.Background()

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	e.mu.RLock()
	shell := e.Shell
	e.mu.RUnlock()
	cmd := exec.CommandContext(ctx, shell, "-c", cmdStr)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	runErr := cmd.Run()

	stdout = outBuf.String()
	stderr = errBuf.String()

	if runErr == nil {
		return stdout, stderr, 0, nil
	}

	if exitErr, ok := runErr.(*exec.ExitError); ok {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return stdout, stderr, status.ExitStatus(), nil
		}
		return stdout, stderr, exitErr.ExitCode(), nil
	}

	if ctx.Err() == context.DeadlineExceeded {
		return stdout, stderr, -1, context.DeadlineExceeded
	}

	return "", runErr.Error(), -1, runErr
}

func mergeArgs(defaults, overrides map[string]string) map[string]string {
	merged := make(map[string]string)

	maps.Copy(merged, defaults)
	maps.Copy(merged, overrides)

	return merged
}
