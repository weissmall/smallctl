package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/weissmall/smallctl/internal/config"
	"github.com/weissmall/smallctl/internal/executor"
	"github.com/weissmall/smallctl/internal/logging"
	"github.com/weissmall/smallctl/internal/notify"
	"github.com/weissmall/smallctl/internal/protocol"
	"github.com/weissmall/smallctl/internal/server"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		runServe(os.Args[2:])
	case "invoke":
		runInvoke(os.Args[2:])
	default:
		printUsage()
		os.Exit(1)
	}
}

// printUsage prints the help text to stderr.
func printUsage() {
	binary := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "Usage: %s <command> [flags]\n", binary)
	fmt.Fprintf(os.Stderr, "\nCommands:\n")
	fmt.Fprintf(os.Stderr, "  serve    Start the IPC server\n")
	fmt.Fprintf(os.Stderr, "  invoke   Invoke a named command\n")
	fmt.Fprintf(os.Stderr, "\nEnvironment variables:\n")
	fmt.Fprintf(os.Stderr, "  SMALLCTL_ENV           Static environment name override\n")
	fmt.Fprintf(os.Stderr, "  SMALLCTL_LOG_LEVEL     Log verbosity (0-5)\n")
	fmt.Fprintf(os.Stderr, "  SMALLCTL_LOG_FILE      Log file path (empty disables file logging)\n")
	fmt.Fprintf(os.Stderr, "  SMALLCTL_NOTIFY        Notification level (off|error|all)\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  %s serve\n", binary)
	fmt.Fprintf(os.Stderr, "  %s invoke brightnessIncrease --args step=20\n", binary)
	fmt.Fprintf(os.Stderr, "  %s invoke screenshot --args mode=full --no-wait\n", binary)
}

// ── serve subcommand ─────────────────────────────────────────────

// runServe starts the smallctl IPC server.
func runServe(args []string) {
	flagSet := flag.NewFlagSet("serve", flag.ExitOnError)
	flagSet.Usage = func() { printUsage() }
	configOverride := flagSet.String("config", "", "path to config file")
	if err := flagSet.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	if envConfig := strings.TrimSpace(os.Getenv("SMALLCTL_CONFIG")); envConfig != "" && *configOverride == "" {
		*configOverride = envConfig
	}

	binaryName := filepath.Base(os.Args[0])
	paths := config.ResolveAppPaths(binaryName, *configOverride)

	if pid, exists := lockFileExists(paths.LockPath); exists {
		if pingServer(paths.SocketPath) {
			fmt.Fprintf(os.Stderr, "smallctl server already running (PID %s)\n", pid)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "warning: removing stale lock/socket\n")
		_ = os.Remove(paths.LockPath)
		_ = config.CleanupPath(paths.SocketPath)
	}

	if err := os.MkdirAll(filepath.Dir(paths.LockPath), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating lock directory: %v\n", err)
		os.Exit(1)
	}
	lockContents := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(paths.LockPath, []byte(lockContents+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing lock file: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(paths.LockPath)

	initialLevel := parseLogLevel(os.Getenv("SMALLCTL_LOG_LEVEL"))
	logFileEnv := resolveLogFile(os.Getenv("SMALLCTL_LOG_FILE"), binaryName)
	logger, cleanup := logging.Setup(initialLevel, logFileEnv)
	defer func() {
		if cleanup != nil {
			_ = cleanup()
		}
	}()

	logger.Info("starting smallctl server", "version", "dev", "socket", paths.SocketPath)

	if *configOverride == "" && !config.FileExists(paths.ConfigFile) && binaryName != "smallctl" {
		fallback := config.ConfigPath("", "smallctl")
		if fallback != paths.ConfigFile && config.FileExists(fallback) {
			logger.Info("config not found for binary name; using fallback", "path", fallback)
			paths.ConfigFile = fallback
		}
	}

	cfg, err := config.Load(paths.ConfigFile)
	if err != nil {
		logger.Error("failed to load config", "error", err, "config", paths.ConfigFile)
		os.Exit(1)
	}
	logger.Info("config loaded", "commands", len(cfg.Commands), "config", paths.ConfigFile)

	finalLevel := initialLevel
	if cfg.Options.LogLevel != nil {
		finalLevel = clampLevel(*cfg.Options.LogLevel)
	}
	finalLogFile := logFileEnv
	if cfg.Options.LogFile != "" {
		finalLogFile = cfg.Options.LogFile
	}
	if finalLogFile == "" {
		finalLogFile = resolveLogFile("", binaryName)
	}

	if finalLevel != initialLevel || finalLogFile != logFileEnv {
		if cleanup != nil {
			_ = cleanup()
		}
		logger, cleanup = logging.Setup(finalLevel, finalLogFile)
		logger.Info("logging reconfigured", "level", finalLevel, "file", finalLogFile)
	}

	exec := executor.New(logger)
	exec.SetShell(cfg.Options.Shell)
	if cfg.Options.Timeout != nil {
		exec.SetDefaultTimeout(time.Duration(*cfg.Options.Timeout) * time.Second)
	}
	if err := exec.ResolveEnv(cfg); err != nil {
		logger.Warn("failed to resolve environment", "error", err)
	} else {
		logger.Info("environment resolved", "env", exec.EnvName)
	}

	notifyLevelStr := strings.TrimSpace(os.Getenv("SMALLCTL_NOTIFY"))
	if notifyLevelStr == "" {
		notifyLevelStr = cfg.Options.Notify
	}
	notifyLevel := notify.ParseLevel(strings.ToLower(notifyLevelStr))
	notifier := notify.Probe(logger)
	if notifier != nil {
		logger.Info("notifications enabled", "transport", notifier.TransportName, "level", notifyLevel)
	} else {
		logger.Info("notifications disabled")
	}

	srv := server.New(paths.SocketPath, logger, exec, notifier, notifyLevel)
	srv.UpdateConfig(cfg)

	var watchDone chan struct{}
	watchDone, err = config.Watch(paths.ConfigFile, logger, func(newCfg *config.Config) {
		logger.Info("config hot-reloaded", "commands", len(newCfg.Commands))
		srv.UpdateConfig(newCfg)
	})
	if err != nil {
		logger.Warn("config watcher disabled", "error", err)
	} else {
		defer close(watchDone)
	}

	if err := srv.Listen(); err != nil {
		logger.Error("failed to listen", "error", err)
		os.Exit(1)
	}

	serverErrCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(); err != nil {
			serverErrCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Info("signal received", "signal", sig.String())
	case err := <-serverErrCh:
		if err != nil {
			logger.Error("server error", "error", err)
		}
	}

	signal.Stop(sigCh)

	if err := srv.Shutdown(); err != nil {
		logger.Error("shutdown error", "error", err)
	} else {
		logger.Info("shutdown complete")
	}
}

// ── invoke subcommand ────────────────────────────────────────────

// runInvoke sends a command invocation to the running smallctl server.
func runInvoke(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "error: command name required")
		os.Exit(2)
	}

	commandName := args[0]
	flagSet := flag.NewFlagSet("invoke", flag.ExitOnError)
	flagSet.Usage = func() { printUsage() }
	argsFlag := flagSet.String("args", "", "comma-separated key=value overrides")
	noWaitFlag := flagSet.Bool("no-wait", false, "do not wait for command to finish")
	if err := flagSet.Parse(args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	if extras := flagSet.Args(); len(extras) > 0 {
		fmt.Fprintf(os.Stderr, "error: unexpected arguments: %v\n", extras)
		os.Exit(2)
	}

	parsedArgs, err := parseArgs(*argsFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing --args: %v\n", err)
		os.Exit(2)
	}

	binaryName := filepath.Base(os.Args[0])
	paths := config.ResolveAppPaths(binaryName, "")
	conn, err := net.Dial("unix", paths.SocketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: server not running (socket %s)\n", paths.SocketPath)
		os.Exit(1)
	}
	defer conn.Close()

	req := protocol.Request{
		Type:    protocol.TypeCommand,
		Command: commandName,
		Args:    parsedArgs,
		Wait:    !*noWaitFlag,
	}

	payload, err := json.Marshal(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error encoding request: %v\n", err)
		os.Exit(1)
	}
	payload = append(payload, '\n')

	if _, err := conn.Write(payload); err != nil {
		fmt.Fprintf(os.Stderr, "error sending request: %v\n", err)
		os.Exit(1)
	}

	if *noWaitFlag {
		os.Exit(0)
	}

	if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		fmt.Fprintf(os.Stderr, "error setting read deadline: %v\n", err)
		os.Exit(1)
	}

	respData, err := io.ReadAll(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading response: %v\n", err)
		os.Exit(1)
	}

	reason := strings.TrimSpace(string(respData))
	if reason == "" {
		fmt.Fprintln(os.Stderr, "error: empty response from server")
		os.Exit(1)
	}

	var resp protocol.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		fmt.Fprintf(os.Stderr, "invalid response: %v\nbody: %s\n", err, reason)
		os.Exit(1)
	}

	if resp.Success {
		if resp.Stdout != "" {
			fmt.Fprint(os.Stdout, resp.Stdout)
		}
		if resp.Stderr != "" {
			fmt.Fprint(os.Stderr, resp.Stderr)
		}
		exit(resp.ExitCode)
	}

	if resp.Stderr != "" {
		fmt.Fprint(os.Stderr, resp.Stderr)
	}
	if len(resp.Errors) > 0 {
		fmt.Fprintln(os.Stderr, "Attempts:")
		for _, entry := range resp.Errors {
			fmt.Fprintf(os.Stderr, "  cmd=%q exit=%d stderr=%s\n", entry.Command, entry.ExitCode, strings.TrimSpace(entry.Stderr))
		}
	}
	if len(resp.Tried) > 0 {
		fmt.Fprintf(os.Stderr, "Tried commands: %s\n", strings.Join(resp.Tried, "; "))
	}
	exit(resp.ExitCode)
}

// ── Utility helpers ─────────────────────────────────────────────

func exit(code int) {
	if code < 0 {
		code = 1
	}
	os.Exit(code)
}

// pingServer attempts to ping the smallctl server at the given socket path.
func pingServer(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))

	req := protocol.Request{Type: protocol.TypePing}
	payload, _ := json.Marshal(req)
	payload = append(payload, '\n')
	if _, err := conn.Write(payload); err != nil {
		return false
	}

	respData, err := io.ReadAll(conn)
	if err != nil {
		return false
	}

	respData = bytes.TrimSpace(respData)
	if len(respData) == 0 {
		return false
	}

	var resp protocol.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		return false
	}

	return resp.Pong
}

// lockFileExists checks if a PID lock file exists.
func lockFileExists(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false
		}
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

func parseArgs(flagValue string) (map[string]string, error) {
	result := make(map[string]string)
	flagValue = strings.TrimSpace(flagValue)
	if flagValue == "" {
		return result, nil
	}

	pairs := strings.Split(flagValue, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid arg %q", pair)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("empty key in %q", pair)
		}
		result[key] = value
	}
	return result, nil
}

func parseLogLevel(raw string) int {
	if raw == "" {
		return logging.LevelInfo
	}
	lvl, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return logging.LevelInfo
	}
	return clampLevel(lvl)
}

func clampLevel(level int) int {
	if level < logging.LevelQuiet {
		return logging.LevelQuiet
	}
	if level > logging.LevelVerbose {
		return logging.LevelVerbose
	}
	return level
}

func resolveLogFile(explicit string, binaryName string) string {
	if explicit != "" {
		return explicit
	}
	xdgDataHome := os.Getenv("XDG_DATA_HOME")
	if xdgDataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		xdgDataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(xdgDataHome, binaryName, "log")
}
