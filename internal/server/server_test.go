package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/weissmall/smallctl/internal/config"
	"github.com/weissmall/smallctl/internal/executor"
	"github.com/weissmall/smallctl/internal/general"
	"github.com/weissmall/smallctl/internal/notify"
	"github.com/weissmall/smallctl/internal/protocol"
)

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.Level(99)}))
}

func setupTestServer(t *testing.T) (*Server, string) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "smallctl-server-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	socketPath := filepath.Join(tmpDir, "test.sock")

	exec := executor.New(silentLogger())
	exec.EnvName = ""

	defaultTimeout := general.DefaultTimeout
	cfg := &config.Config{
		Options: config.Options{
			Shell:   general.DefaultShell,
			Timeout: &defaultTimeout,
		},
		Commands: map[string]config.Command{
			"echo": {
				Fallback: []string{"echo hello"},
			},
			"fail": {
				Fallback: []string{"exit 1"},
			},
		},
	}

	srv := New(socketPath, silentLogger(), exec, nil, notify.LevelOff)
	srv.UpdateConfig(cfg)

	return srv, socketPath
}

// readResponse reads all data from conn and unmarshals into a Response.
func readResponse(t *testing.T, conn net.Conn) protocol.Response {
	t.Helper()
	respData, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	var resp protocol.Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("Unmarshal failed: %v (data=%q)", err, string(respData))
	}
	return resp
}

func TestServerListenAndServe(t *testing.T) {
	srv, socketPath := setupTestServer(t)

	err := srv.Listen()
	if err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}

	go func() {
		_ = srv.Serve()
	}()

	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}

	pingReq := protocol.Request{Type: protocol.TypePing}
	data, _ := json.Marshal(pingReq)
	data = append(data, '\n')
	_, err = conn.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	resp := readResponse(t, conn)
	conn.Close()

	if !resp.Pong {
		t.Error("expected Pong=true")
	}
	if !resp.Success {
		t.Error("expected Success=true for ping")
	}

	if err := srv.Shutdown(); err != nil {
		t.Errorf("Shutdown() error: %v", err)
	}
}

func TestServerExecuteCommand(t *testing.T) {
	srv, socketPath := setupTestServer(t)

	err := srv.Listen()
	if err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}

	go func() {
		_ = srv.Serve()
	}()

	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}

	req := protocol.Request{
		Type:    protocol.TypeCommand,
		Command: "echo",
		Wait:    true,
	}
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	conn.Write(data)

	resp := readResponse(t, conn)
	conn.Close()

	if !resp.Success {
		t.Errorf("expected success, got errors: %v", resp.Errors)
	}
	if resp.Stdout != "hello\n" {
		t.Errorf("expected stdout='hello\\n', got %q", resp.Stdout)
	}

	if err := srv.Shutdown(); err != nil {
		t.Errorf("Shutdown() error: %v", err)
	}
}

func TestServerNoWait(t *testing.T) {
	srv, socketPath := setupTestServer(t)

	err := srv.Listen()
	if err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}

	go func() {
		_ = srv.Serve()
	}()

	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}

	req := protocol.Request{
		Type:    protocol.TypeCommand,
		Command: "echo",
		Wait:    false,
	}
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	conn.Write(data)
	conn.Close()

	time.Sleep(100 * time.Millisecond)

	if err := srv.Shutdown(); err != nil {
		t.Errorf("Shutdown() error: %v", err)
	}
}

func TestServerCommandNotFound(t *testing.T) {
	srv, socketPath := setupTestServer(t)

	err := srv.Listen()
	if err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}

	go func() {
		_ = srv.Serve()
	}()

	time.Sleep(50 * time.Millisecond)

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}

	req := protocol.Request{
		Type:    protocol.TypeCommand,
		Command: "nonexistent",
		Wait:    true,
	}
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	conn.Write(data)

	resp := readResponse(t, conn)
	conn.Close()

	if resp.Success {
		t.Error("expected failure for nonexistent command")
	}

	if err := srv.Shutdown(); err != nil {
		t.Errorf("Shutdown() error: %v", err)
	}
}

// recordingDispatcher captures Dispatch calls for assertions. It mirrors
// the level filtering of notify.Notifier so that only notifications that
// would actually be sent are recorded.
type recordingDispatcher struct {
	mu    sync.Mutex
	calls []dispatchCall
}

type dispatchCall struct {
	level   notify.Level
	isError bool
	title   string
}

func (d *recordingDispatcher) Dispatch(level notify.Level, isError bool, title, body string) {
	if level == notify.LevelOff {
		return
	}
	if level == notify.LevelError && !isError {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, dispatchCall{level: level, isError: isError, title: title})
}

func (d *recordingDispatcher) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

func (d *recordingDispatcher) last() dispatchCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.calls) == 0 {
		return dispatchCall{}
	}
	return d.calls[len(d.calls)-1]
}

// waitForDispatch polls until the dispatcher recorded want calls or the
// deadline passes.
func waitForDispatch(t *testing.T, d *recordingDispatcher, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if d.count() >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d dispatches, got %d", want, d.count())
}

func invokeCommand(t *testing.T, socketPath, name string) protocol.Response {
	t.Helper()
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	req := protocol.Request{Type: protocol.TypeCommand, Command: name, Wait: true}
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	if _, err := conn.Write(data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	return readResponse(t, conn)
}

func TestUpdateConfigUpdatesNotifyLevel(t *testing.T) {
	t.Setenv("SMALLCTL_NOTIFY", "")

	defaultTimeout := general.DefaultTimeout
	cfg := &config.Config{
		Options: config.Options{Shell: general.DefaultShell, Timeout: &defaultTimeout},
	}

	srv := New("/tmp/nonexistent.sock", silentLogger(), executor.New(silentLogger()), nil, notify.LevelOff)

	cfg.Options.Notify = "error"
	srv.UpdateConfig(cfg)
	if srv.NotifyLevel != notify.LevelError {
		t.Errorf("after error config: NotifyLevel = %q, want %q", srv.NotifyLevel, notify.LevelError)
	}

	cfg.Options.Notify = "all"
	srv.UpdateConfig(cfg)
	if srv.NotifyLevel != notify.LevelAll {
		t.Errorf("after all config: NotifyLevel = %q, want %q", srv.NotifyLevel, notify.LevelAll)
	}

	cfg.Options.Notify = ""
	srv.UpdateConfig(cfg)
	if srv.NotifyLevel != notify.LevelOff {
		t.Errorf("after empty config: NotifyLevel = %q, want %q", srv.NotifyLevel, notify.LevelOff)
	}
}

func TestUpdateConfigNotifyLevelEnvOverride(t *testing.T) {
	t.Setenv("SMALLCTL_NOTIFY", "all")

	defaultTimeout := general.DefaultTimeout
	cfg := &config.Config{
		Options: config.Options{Shell: general.DefaultShell, Timeout: &defaultTimeout, Notify: "error"},
	}

	srv := New("/tmp/nonexistent.sock", silentLogger(), executor.New(silentLogger()), nil, notify.LevelOff)
	srv.UpdateConfig(cfg)

	if srv.NotifyLevel != notify.LevelAll {
		t.Errorf("expected env override to win, got %q", srv.NotifyLevel)
	}
}

func TestServerNotifiesOnFailure(t *testing.T) {
	t.Setenv("SMALLCTL_NOTIFY", "")

	tmpDir, err := os.MkdirTemp("", "smallctl-notify-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	socketPath := filepath.Join(tmpDir, "test.sock")

	exec := executor.New(silentLogger())
	exec.EnvName = ""

	defaultTimeout := general.DefaultTimeout
	cfg := &config.Config{
		Options: config.Options{
			Shell:   general.DefaultShell,
			Timeout: &defaultTimeout,
			Notify:  "error",
		},
		Commands: map[string]config.Command{
			"echo": {Fallback: []string{"echo hello"}},
			"fail": {Fallback: []string{"exit 1"}},
		},
	}

	dispatcher := &recordingDispatcher{}
	srv := New(socketPath, silentLogger(), exec, dispatcher, notify.LevelOff)
	srv.UpdateConfig(cfg)

	if err := srv.Listen(); err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Shutdown() })
	time.Sleep(50 * time.Millisecond)

	if resp := invokeCommand(t, socketPath, "fail"); resp.Success {
		t.Fatal("expected fail command to fail")
	}
	waitForDispatch(t, dispatcher, 1)
	call := dispatcher.last()
	if !call.isError || call.level != notify.LevelError {
		t.Errorf("expected error dispatch at level error, got %+v", call)
	}
	if call.title != "smallctl: fail failed" {
		t.Errorf("unexpected title: %q", call.title)
	}

	if resp := invokeCommand(t, socketPath, "echo"); !resp.Success {
		t.Fatal("expected echo command to succeed")
	}
	time.Sleep(150 * time.Millisecond)
	if got := dispatcher.count(); got != 1 {
		t.Errorf("success must not dispatch at level error, got %d dispatches", got)
	}
}

func TestServerStaleSocketCleanup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "smallctl-stale-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	socketPath := filepath.Join(tmpDir, "stale.sock")

	if err := os.WriteFile(socketPath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := New(socketPath, silentLogger(), executor.New(silentLogger()), nil, notify.LevelOff)

	if err := srv.Listen(); err != nil {
		t.Fatalf("Listen() failed on stale socket: %v", err)
	}

	fi, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if fi.Mode()&os.ModeSocket == 0 {
		t.Error("expected Unix socket after Listen")
	}

	srv.Shutdown()
}
