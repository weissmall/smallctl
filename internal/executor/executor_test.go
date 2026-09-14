package executor

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"smallctl/internal/config"
	"smallctl/internal/general"
	"smallctl/internal/protocol"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.Level(99)}))
}

func TestNew(t *testing.T) {
	e := New(testLogger())
	if e.Shell != general.DefaultShell {
		t.Errorf("expected Shell=%q, got %q", general.DefaultShell, e.Shell)
	}
	if e.DefaultTimeout != general.DefaultTimeout*time.Second {
		t.Errorf("expected DefaultTimeout=%v, got %v", general.DefaultTimeout*time.Second, e.DefaultTimeout)
	}
}

func TestResolveEnvStatic(t *testing.T) {
	e := New(testLogger())

	os.Setenv("SMALLCTL_ENV", "testenv")
	defer os.Unsetenv("SMALLCTL_ENV")

	cfg := &config.Config{
		Commands: make(map[string]config.Command),
	}

	if err := e.ResolveEnv(cfg); err != nil {
		t.Fatalf("ResolveEnv failed: %v", err)
	}
	if e.EnvName != "testenv" {
		t.Errorf("expected EnvName='testenv', got %q", e.EnvName)
	}
}

func TestResolveEnvNoEnv(t *testing.T) {
	e := New(testLogger())
	os.Unsetenv("SMALLCTL_ENV")

	cfg := &config.Config{
		Commands: make(map[string]config.Command),
	}

	if err := e.ResolveEnv(cfg); err != nil {
		t.Fatalf("ResolveEnv failed: %v", err)
	}
	if e.EnvName != "" {
		t.Errorf("expected empty EnvName, got %q", e.EnvName)
	}
}

func TestResolveEnvCommand(t *testing.T) {
	e := New(testLogger())
	os.Unsetenv("SMALLCTL_ENV")

	cfg := &config.Config{
		Options: config.Options{
			EnvCommand: "echo mymachine",
			Shell:      "bash",
		},
		Commands: make(map[string]config.Command),
	}

	if err := e.ResolveEnv(cfg); err != nil {
		t.Fatalf("ResolveEnv failed: %v", err)
	}
	if e.EnvName != "mymachine" {
		t.Errorf("expected EnvName='mymachine', got %q", e.EnvName)
	}
}

func TestResolveEnvCommandFails(t *testing.T) {
	e := New(testLogger())
	os.Setenv("SMALLCTL_ENV", "fallback")
	defer os.Unsetenv("SMALLCTL_ENV")

	cfg := &config.Config{
		Options: config.Options{
			EnvCommand: "exit 1",
			Shell:      "bash",
		},
		Commands: make(map[string]config.Command),
	}

	if err := e.ResolveEnv(cfg); err != nil {
		t.Fatalf("ResolveEnv failed: %v", err)
	}
	if e.EnvName != "fallback" {
		t.Errorf("expected EnvName='fallback', got %q", e.EnvName)
	}
}

func TestSetEnvironmentOverridesResolvedEnvironment(t *testing.T) {
	e := New(testLogger())
	cfg := &config.Config{
		Options: config.Options{
			EnvCommand: "echo detected",
			Shell:      "bash",
		},
		Commands: make(map[string]config.Command),
	}

	if err := e.ResolveEnv(cfg); err != nil {
		t.Fatalf("initial ResolveEnv failed: %v", err)
	}
	e.SetEnvironment("noctalia")

	if err := e.ResolveEnv(cfg); err != nil {
		t.Fatalf("ResolveEnv after runtime override failed: %v", err)
	}
	if got := e.Environment(); got != "noctalia" {
		t.Errorf("Environment() = %q, want %q", got, "noctalia")
	}
}

func TestExecuteSuccessFallback(t *testing.T) {
	e := New(testLogger())
	e.EnvName = "" // no env

	cfg := &config.Config{
		Commands: map[string]config.Command{
			"testHello": {
				Fallback: []string{"echo hello"},
			},
		},
	}

	resp := e.Execute(cfg, protocol.Request{Command: "testHello", Wait: true})
	if !resp.Success {
		t.Errorf("expected success, got: stderr=%q errors=%v", resp.Stderr, resp.Errors)
	}
	if resp.Stdout != "hello\n" {
		t.Errorf("expected stdout='hello\\n', got %q", resp.Stdout)
	}
	if resp.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", resp.ExitCode)
	}
	if len(resp.Tried) != 1 {
		t.Errorf("expected 1 tried, got %d", len(resp.Tried))
	}
}

func TestExecuteEnvMatch(t *testing.T) {
	e := New(testLogger())
	e.EnvName = "production"

	cfg := &config.Config{
		Commands: map[string]config.Command{
			"greet": {
				Args: map[string]string{"name": "world"},
				Envs: map[string]string{
					"production": "echo prod-${name}",
					"staging":    "echo stag-${name}",
				},
			},
		},
	}

	resp := e.Execute(cfg, protocol.Request{Command: "greet", Wait: true})
	if !resp.Success {
		t.Errorf("expected success, got: %v", resp.Errors)
	}
	if resp.Stdout != "prod-world\n" {
		t.Errorf("expected 'prod-world\\n', got %q", resp.Stdout)
	}
}

func TestExecuteFallbackChain(t *testing.T) {
	e := New(testLogger())
	e.EnvName = "" // no env match

	cfg := &config.Config{
		Commands: map[string]config.Command{
			"chain": {
				Fallback: []string{
					"exit 1",  // fails
					"exit 2",  // fails
					"echo ok", // succeeds
				},
			},
		},
	}

	resp := e.Execute(cfg, protocol.Request{Command: "chain", Wait: true})
	if !resp.Success {
		t.Errorf("expected success, got errors: %v", resp.Errors)
	}
	if resp.Stdout != "ok\n" {
		t.Errorf("expected 'ok\\n', got %q", resp.Stdout)
	}
	if len(resp.Tried) != 3 {
		t.Errorf("expected 3 tried, got %d", len(resp.Tried))
	}
	if len(resp.Errors) != 2 {
		t.Errorf("expected 2 errors, got %d: %v", len(resp.Errors), resp.Errors)
	}
}

func TestExecuteAllFail(t *testing.T) {
	e := New(testLogger())
	e.EnvName = ""

	cfg := &config.Config{
		Commands: map[string]config.Command{
			"bad": {
				Fallback: []string{
					"exit 1",
					"exit 2",
				},
			},
		},
	}

	resp := e.Execute(cfg, protocol.Request{Command: "bad", Wait: true})
	if resp.Success {
		t.Errorf("expected failure")
	}
	if len(resp.Errors) != 2 {
		t.Errorf("expected 2 errors, got %d", len(resp.Errors))
	}
	if resp.Errors[0].ExitCode != 1 {
		t.Errorf("expected first error exit 1, got %d", resp.Errors[0].ExitCode)
	}
	if resp.Errors[1].ExitCode != 2 {
		t.Errorf("expected second error exit 2, got %d", resp.Errors[1].ExitCode)
	}
	if resp.ExitCode != 2 {
		t.Errorf("expected last exit code 2, got %d", resp.ExitCode)
	}
}

func TestExecuteCommandNotFound(t *testing.T) {
	e := New(testLogger())

	cfg := &config.Config{
		Commands: make(map[string]config.Command),
	}

	resp := e.Execute(cfg, protocol.Request{Command: "nonexistent", Wait: true})
	if resp.Success {
		t.Errorf("expected failure for nonexistent command")
	}
	if resp.Stderr == "" {
		t.Error("expected stderr message for missing command")
	}
}

func TestExecuteTimeout(t *testing.T) {
	e := New(testLogger())
	e.EnvName = ""

	timeout := 1

	cfg := &config.Config{
		Options: config.Options{Timeout: &timeout},
		Commands: map[string]config.Command{
			"sleepy": {
				Fallback: []string{"sleep 10"},
			},
		},
	}

	start := time.Now()
	resp := e.Execute(cfg, protocol.Request{Command: "sleepy", Wait: true})
	elapsed := time.Since(start)

	if resp.Success {
		t.Errorf("expected timeout failure")
	}
	if elapsed > 5*time.Second {
		t.Errorf("timeout took too long: %v", elapsed)
	}
	if len(resp.Errors) == 0 {
		t.Fatal("expected error entry for timeout")
	}
	if resp.Errors[0].ExitCode != -1 {
		t.Errorf("expected exitCode -1 for timeout, got %d", resp.Errors[0].ExitCode)
	}
}

func TestExecuteSkipsCachedFailedCommand(t *testing.T) {
	e := New(testLogger())
	t.Cleanup(e.Close)
	e.EnvName = "test"
	ttl := 60
	cfg := &config.Config{
		Options: config.Options{Shell: "bash", FailedCommandTTL: &ttl},
		Commands: map[string]config.Command{
			"command": {
				Envs:     map[string]string{"test": "false"},
				Fallback: []string{"echo fallback"},
			},
		},
	}
	e.ConfigureFailureCache(cfg)

	first := e.Execute(cfg, protocol.Request{Command: "command", Wait: true})
	if !first.Success || len(first.Tried) != 2 || len(first.Skipped) != 0 {
		t.Fatalf("first response = %+v, want failed env command then fallback", first)
	}

	second := e.Execute(cfg, protocol.Request{Command: "command", Wait: true})
	if !second.Success || len(second.Tried) != 1 || len(second.Skipped) != 1 {
		t.Fatalf("second response = %+v, want cached env command skipped", second)
	}
	if second.Tried[0] != "echo fallback" || second.Skipped[0] != "false" {
		t.Errorf("unexpected execution result: tried=%v skipped=%v", second.Tried, second.Skipped)
	}

	e.ClearFailedCommands()
	third := e.Execute(cfg, protocol.Request{Command: "command", Wait: true})
	if !third.Success || len(third.Tried) != 2 || len(third.Skipped) != 0 {
		t.Fatalf("response after clearing cache = %+v, want both commands tried", third)
	}
}

func TestSetEnvironmentClearsFailedCommandCache(t *testing.T) {
	e := New(testLogger())
	t.Cleanup(e.Close)
	e.EnvName = "test"
	ttl := 60
	cfg := &config.Config{
		Options: config.Options{Shell: "bash", FailedCommandTTL: &ttl},
		Commands: map[string]config.Command{
			"command": {
				Envs:     map[string]string{"test": "false"},
				Fallback: []string{"echo fallback"},
			},
		},
	}
	e.ConfigureFailureCache(cfg)
	_ = e.Execute(cfg, protocol.Request{Command: "command", Wait: true})

	e.SetEnvironment("test")
	resp := e.Execute(cfg, protocol.Request{Command: "command", Wait: true})
	if !resp.Success || len(resp.Tried) != 2 || len(resp.Skipped) != 0 {
		t.Fatalf("response after env set = %+v, want failed command retried", resp)
	}
}

func TestConfigureFailureCacheSkipsUnavailableExecutable(t *testing.T) {
	e := New(testLogger())
	t.Cleanup(e.Close)
	e.EnvName = "test"
	ttl := 60
	const unavailable = "smallctl-command-that-does-not-exist"
	cfg := &config.Config{
		Options: config.Options{Shell: "bash", FailedCommandTTL: &ttl},
		Commands: map[string]config.Command{
			"command": {
				Envs:     map[string]string{"test": unavailable + " --flag"},
				Fallback: []string{"echo fallback"},
			},
		},
	}
	e.ConfigureFailureCache(cfg)

	resp := e.Execute(cfg, protocol.Request{Command: "command", Wait: true})
	if !resp.Success || len(resp.Tried) != 1 || len(resp.Skipped) != 1 {
		t.Fatalf("response = %+v, want unavailable command skipped before execution", resp)
	}
	if resp.Tried[0] != "echo fallback" || resp.Skipped[0] != unavailable+" --flag" {
		t.Errorf("unexpected execution result: tried=%v skipped=%v", resp.Tried, resp.Skipped)
	}
}

func TestExecuteOverrideArgs(t *testing.T) {
	e := New(testLogger())
	e.EnvName = ""

	cfg := &config.Config{
		Commands: map[string]config.Command{
			"echo": {
				Args:     map[string]string{"msg": "default"},
				Fallback: []string{"echo ${msg}"},
			},
		},
	}

	resp := e.Execute(cfg, protocol.Request{Command: "echo", Wait: true})
	if resp.Stdout != "default\n" {
		t.Errorf("expected 'default\\n', got %q", resp.Stdout)
	}

	resp = e.Execute(cfg, protocol.Request{Command: "echo", Wait: true, Args: map[string]string{"msg": "override"}})
	if resp.Stdout != "override\n" {
		t.Errorf("expected 'override\\n', got %q", resp.Stdout)
	}
}

func TestMergeArgs(t *testing.T) {
	tests := []struct {
		name      string
		defaults  map[string]string
		overrides map[string]string
		want      map[string]string
	}{
		{
			name:      "both nil",
			defaults:  nil,
			overrides: nil,
			want:      map[string]string{},
		},
		{
			name:      "defaults only",
			defaults:  map[string]string{"a": "1", "b": "2"},
			overrides: nil,
			want:      map[string]string{"a": "1", "b": "2"},
		},
		{
			name:      "overrides take priority",
			defaults:  map[string]string{"a": "1"},
			overrides: map[string]string{"a": "99", "b": "new"},
			want:      map[string]string{"a": "99", "b": "new"},
		},
		{
			name:      "overrides only",
			defaults:  nil,
			overrides: map[string]string{"x": "y"},
			want:      map[string]string{"x": "y"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeArgs(tt.defaults, tt.overrides)
			if len(got) != len(tt.want) {
				t.Errorf("len = %d, want %d", len(got), len(tt.want))
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("key %q: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
