package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
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
