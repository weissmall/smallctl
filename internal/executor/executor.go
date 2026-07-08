// Package executor resolves the current environment, substitutes command
// template variables, and executes shell commands via the configured shell.
// It implements the core logic of the smallctl server: env matching,
// ordered fallback chain, timeout enforcement, and error collection.
package executor

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/weissmall/smallctl/internal/config"
	"github.com/weissmall/smallctl/internal/protocol"
)

// envResolveTimeout is the maximum time allowed for env_command to run.
const envResolveTimeout = 5 * time.Second

// Executor holds the runtime state needed to execute commands.
type Executor struct {
	// Logger for executor-specific messages.
	Logger *slog.Logger

	// EnvName is the resolved current environment name.
	// Set by ResolveEnv() based on env_command or $SMALLCTL_ENV.
	EnvName string

	// Shell is the shell binary path (e.g., "bash").
	// Defaults to config.DefaultShell if not configured.
	Shell string

	// DefaultTimeout is the per-command timeout when not specified per-command.
	DefaultTimeout time.Duration
}

// New creates a new Executor with the given logger.
// Default shell is "bash"; default timeout is 30s.
func New(logger *slog.Logger) *Executor {
	return &Executor{
		Logger:         logger,
		Shell:          config.DefaultShell,
		DefaultTimeout: config.DefaultTimeout * time.Second,
	}
}

// ResolveEnv determines the current environment name.
//
// Priority:
//  1. Run cfg.Options.EnvCommand (if set), capture stdout (trimmed) as env name.
//     If the command fails, log a warning and fall through to #2.
//  2. Read $SMALLCTL_ENV from environment.
//  3. If neither is set, return "" (empty string) — no env, use fallback always.
//
// The resolved env name is stored in e.EnvName for subsequent Execute calls.
func (e *Executor) ResolveEnv(cfg *config.Config) error {
	// 1. Try env_command from config.
	if cfg.Options.EnvCommand != "" {
		ctx, cancel := context.WithTimeout(context.Background(), envResolveTimeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, e.Shell, "-c", cfg.Options.EnvCommand)
		cmd.Stderr = nil // discard stderr
		out, err := cmd.Output()
		if err == nil {
			e.EnvName = strings.TrimSpace(string(out))
			e.Logger.Info("environment resolved via env_command",
				"command", cfg.Options.EnvCommand, "env", e.EnvName)
			return nil
		}
		e.Logger.Warn("env_command failed, falling back to $SMALLCTL_ENV",
			"command", cfg.Options.EnvCommand, "error", err)
	}

	// 2. Try $SMALLCTL_ENV.
	if env := os.Getenv("SMALLCTL_ENV"); env != "" {
		e.EnvName = strings.TrimSpace(env)
		e.Logger.Info("environment resolved via $SMALLCTL_ENV", "env", e.EnvName)
		return nil
	}

	// 3. No env — use fallback always.
	e.EnvName = ""
	e.Logger.Debug("no environment configured, fallback chain will always be used")
	return nil
}

// Execute runs the requested command through the full env-match + fallback chain.
//
// Steps:
//  1. Look up req.Command in cfg.Commands.
//  2. Merge req.Args (invocation overrides) over command.Args (defaults).
//  3. If e.EnvName matches a key in command.Envs, run that shell command.
//     On success (exit 0), return the response. On failure, proceed to fallback.
//  4. Walk command.Fallback in order. First success (exit 0) returns.
//  5. If all env + fallback attempts fail, return a failure Response with
//     all errors collected in Tried and Errors.
func (e *Executor) Execute(cfg *config.Config, req protocol.Request) protocol.Response {
	resp := protocol.Response{
		Success: false,
		Tried:   make([]string, 0),
		Errors:  make([]protocol.ErrorEntry, 0),
	}

	// 1. Look up the command.
	cmd, ok := cfg.Commands[req.Command]
	if !ok {
		resp.Stderr = "command not found: " + req.Command
		resp.ExitCode = 1
		e.Logger.Warn("command not found", "command", req.Command)
		return resp
	}

	// 2. Merge invocation args over config defaults.
	resolvedArgs := mergeArgs(cmd.Args, req.Args)

	// 3. Build the ordered list of commands to try.
	tryList := make([]string, 0)

	// If env matches, prepend the env-specific command.
	if e.EnvName != "" {
		if envCmd, ok := cmd.Envs[e.EnvName]; ok && strings.TrimSpace(envCmd) != "" {
			substituted := config.Substitute(envCmd, cmd.Args, resolvedArgs)
			tryList = append(tryList, substituted)
		}
	}

	// Append all fallback commands.
	for _, fb := range cmd.Fallback {
		substituted := config.Substitute(fb, cmd.Args, resolvedArgs)
		tryList = append(tryList, substituted)
	}

	// If nothing to try, return an error.
	if len(tryList) == 0 {
		resp.Stderr = "no commands defined for: " + req.Command
		resp.ExitCode = 1
		e.Logger.Warn("no commands to execute", "command", req.Command)
		return resp
	}

	// Determine timeout for this execution.
	var timeout time.Duration
	if cfg.Options.Timeout != nil && *cfg.Options.Timeout > 0 {
		timeout = time.Duration(*cfg.Options.Timeout) * time.Second
	} else if cfg.Options.Timeout != nil && *cfg.Options.Timeout == 0 {
		timeout = 0 // no timeout
	} else {
		timeout = e.DefaultTimeout
	}

	// 4. Walk the try list.
	var lastExitCode int
	for _, cmdStr := range tryList {
		// Record that we tried this command.
		resp.Tried = append(resp.Tried, cmdStr)

		e.Logger.Debug("executing command", "command", req.Command, "cmd", cmdStr)

		stdout, stderr, exitCode, execErr := e.runCmd(cmdStr, timeout)

		if execErr != nil {
			// Execution error (shell not found, timeout, etc.).
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
			// Success!
			resp.Success = true
			resp.ExitCode = 0
			resp.Stdout = stdout
			resp.Stderr = stderr
			e.Logger.Debug("command succeeded",
				"command", req.Command, "cmd", cmdStr)
			return resp
		}

		// Non-zero exit — record the failure and continue to next.
		entry := protocol.ErrorEntry{
			Command:  cmdStr,
			ExitCode: exitCode,
			Stderr:   stderr,
		}
		resp.Errors = append(resp.Errors, entry)
		lastExitCode = exitCode
	}

	// 5. All attempts failed.
	resp.Success = false
	resp.ExitCode = lastExitCode
	if len(resp.Errors) > 0 {
		resp.Stderr = resp.Errors[len(resp.Errors)-1].Stderr
	}
	e.Logger.Warn("all attempts failed",
		"command", req.Command, "tried", len(resp.Tried), "errors", len(resp.Errors))

	return resp
}

// runCmd executes a single shell command via bash -c (or the configured shell).
//
// Parameters:
//   - cmdStr: the substituted shell command string.
//   - timeout: maximum execution duration (0 = no timeout).
//
// Returns stdout, stderr, exit code, and any execution error.
// A non-zero exit code is NOT an execution error — it's reflected in exitCode.
// An execution error means the shell couldn't start, or the command timed out.
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

	// Check the type of error.
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		// Non-zero exit — not an execution error.
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return stdout, stderr, status.ExitStatus(), nil
		}
		return stdout, stderr, exitErr.ExitCode(), nil
	}

	if ctx.Err() == context.DeadlineExceeded {
		return stdout, stderr, -1, context.DeadlineExceeded
	}

	// Shell not found, permission denied, etc.
	return "", runErr.Error(), -1, runErr
}

// mergeArgs merges invocation overrides over config defaults.
// Override values take priority; missing overrides fall back to defaults.
// Both maps may be nil — returns a new map in all cases.
func mergeArgs(defaults, overrides map[string]string) map[string]string {
	merged := make(map[string]string)

	// Start with defaults.
	for k, v := range defaults {
		merged[k] = v
	}

	// Override with invocation args.
	for k, v := range overrides {
		merged[k] = v
	}

	return merged
}

// Ensure types compile.
var (
	_ context.Context
	_ time.Duration
	_ = protocol.Response{}
	_ = config.Command{}
	_ = config.DefaultShell
	_ = config.DefaultTimeout
	_ = bytes.Buffer{}
	_ = exec.Cmd{}
	_ = syscall.WaitStatus(0)
)
