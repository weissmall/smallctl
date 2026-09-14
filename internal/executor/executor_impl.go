package executor

import (
	"bytes"
	"context"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"smallctl/internal/config"
	"smallctl/internal/general"
	"smallctl/internal/protocol"
)

func New(logger *slog.Logger) *Executor {
	return &Executor{
		Logger:         logger,
		Shell:          general.DefaultShell,
		DefaultTimeout: general.DefaultTimeout * time.Second,
	}
}

func (e *Executor) SetShell(shell string) {
	e.Shell = shell
}

func (e *Executor) SetDefaultTimeout(timeout time.Duration) {
	e.DefaultTimeout = timeout
}

// SetEnvironment sets a runtime environment override. It takes precedence over
// options.env_command and $SMALLCTL_ENV until the server stops.
func (e *Executor) SetEnvironment(environment string) {
	e.mu.Lock()
	e.EnvName = strings.TrimSpace(environment)
	e.environmentOverride = true
	e.mu.Unlock()

	e.Logger.Info("environment set at runtime", "env", strings.TrimSpace(environment))
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

		cmd := exec.CommandContext(ctx, e.Shell, "-c", cfg.Options.EnvCommand)
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

	tryList := make([]string, 0)

	if environment := e.Environment(); environment != "" {
		if envCmd, ok := cmd.Envs[environment]; ok && strings.TrimSpace(envCmd) != "" {
			substituted := config.Substitute(envCmd, cmd.Args, resolvedArgs)
			tryList = append(tryList, substituted)
		}
	}

	for _, fb := range cmd.Fallback {
		substituted := config.Substitute(fb, cmd.Args, resolvedArgs)
		tryList = append(tryList, substituted)
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
		timeout = e.DefaultTimeout
	}

	var lastExitCode int
	for _, cmdStr := range tryList {
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
			e.Logger.Warn("command execution error",
				"command", req.Command, "cmd", cmdStr, "error", execErr)
			continue
		}

		if exitCode == 0 {
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
	}

	resp.Success = false
	resp.ExitCode = lastExitCode
	if len(resp.Errors) > 0 {
		resp.Stderr = resp.Errors[len(resp.Errors)-1].Stderr
	}
	e.Logger.Warn("all attempts failed",
		"command", req.Command, "tried", len(resp.Tried), "errors", len(resp.Errors))

	return resp
}

func (e *Executor) runCmd(cmdStr string, timeout time.Duration) (stdout, stderr string, exitCode int, err error) {
	ctx := context.Background()

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, e.Shell, "-c", cmdStr)

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
