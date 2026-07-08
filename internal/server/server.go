// Package server implements the Unix domain socket IPC server for smallctl.
// It listens on a Unix socket, accepts connections, reads JSON requests,
// dispatches them to the executor, and writes JSON responses.
//
// The server also handles ping requests for health checking and enforces
// a maximum request size to prevent DoS.
package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/weissmall/smallctl/internal/config"
	"github.com/weissmall/smallctl/internal/executor"
	"github.com/weissmall/smallctl/internal/notify"
	"github.com/weissmall/smallctl/internal/protocol"
)

// ShutdownTimeout is the maximum time to wait for active connections during shutdown.
const ShutdownTimeout = 5 * time.Second

// Server is the Unix socket IPC server.
type Server struct {
	// SocketPath is the filesystem path to the Unix domain socket.
	SocketPath string

	// Logger for server-specific messages.
	Logger *slog.Logger

	// ConfigMu protects access to Cfg during hot-reloads.
	// Requests take RLock; hot-reload takes Lock.
	ConfigMu sync.RWMutex

	// Cfg is the current loaded configuration (always in RAM).
	Cfg *config.Config

	// Executor handles command resolution and execution.
	Executor *executor.Executor

	// Notifier sends desktop notifications (nil if disabled).
	Notifier *notify.Notifier

	// NotifyLevel is the configured notification level.
	NotifyLevel notify.Level

	// listener is the underlying net.Listener (set after Listen).
	listener net.Listener

	// wg tracks active connections for graceful shutdown.
	wg sync.WaitGroup
}

// New creates a new Server with the given parameters.
// The server is not started until Listen() and Serve() are called.
func New(
	socketPath string,
	logger *slog.Logger,
	exec *executor.Executor,
	notif *notify.Notifier,
	notifyLevel notify.Level,
) *Server {
	return &Server{
		SocketPath:  socketPath,
		Logger:      logger,
		Executor:    exec,
		Notifier:    notif,
		NotifyLevel: notifyLevel,
	}
}

// UpdateConfig atomically swaps the current configuration.
// Called by the config watcher on successful hot-reload.
func (s *Server) UpdateConfig(cfg *config.Config) {
	s.ConfigMu.Lock()
	s.Cfg = cfg
	s.ConfigMu.Unlock()

	// Update executor runtime parameters.
	s.Executor.Shell = cfg.Options.Shell
	if cfg.Options.Timeout != nil {
		s.Executor.DefaultTimeout = time.Duration(*cfg.Options.Timeout) * time.Second
	}

	// Re-resolve the environment (env_command may have changed).
	if err := s.Executor.ResolveEnv(cfg); err != nil {
		s.Logger.Warn("failed to re-resolve environment after config reload", "error", err)
	}
}

// Listen creates the Unix domain socket and prepares to accept connections.
//
// If a stale socket file already exists, it is removed before binding.
// Returns an error if the socket cannot be created (permissions, path issues).
func (s *Server) Listen() error {
	// Clean up stale socket file.
	if err := config.CleanupPath(s.SocketPath); err != nil {
		return fmt.Errorf("removing stale socket %s: %w", s.SocketPath, err)
	}

	// Ensure parent directory exists.
	dir := filepath.Dir(s.SocketPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating socket directory %s: %w", dir, err)
	}

	listener, err := net.Listen("unix", s.SocketPath)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", s.SocketPath, err)
	}

	s.listener = listener
	s.Logger.Info("listening", "socket", s.SocketPath)
	return nil
}

// Serve begins the accept loop. It blocks until the listener is closed
// (via Shutdown) or an unrecoverable error occurs.
//
// Each connection is handled in its own goroutine.
func (s *Server) Serve() error {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// Check if listener was closed (graceful shutdown).
			if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
				return nil
			}
			// net.ErrClosed is not directly comparable, so check the string.
			if err.Error() == "accept unix "+s.SocketPath+": use of closed network connection" {
				return nil
			}
			s.Logger.Error("accept error", "error", err)
			return err
		}

		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

// handleConn processes a single client connection.
//
// Protocol:
//  1. Read up to protocol.MaxRequestSize bytes (single JSON line).
//  2. Unmarshal into protocol.Request.
//  3. If Request.Type == "ping": respond with {"pong": true} immediately.
//  4. If Request.Type == "command" (or empty): dispatch to executor.
//  5. If Request.Wait is false: send empty success response and close.
//  6. If Request.Wait is true: marshal Response, write to connection, close.
func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	defer s.wg.Done()

	// 1. Read a single line (newline-delimited JSON) with size limit.
	var data []byte
	buf := make([]byte, 1)
	for len(data) < protocol.MaxRequestSize {
		n, err := conn.Read(buf)
		if n == 0 || (err != nil && err != io.EOF) {
			if err != nil {
				s.Logger.Debug("error reading request", "error", err)
				s.writeError(conn, "error reading request: "+err.Error())
			}
			return
		}
		data = append(data, buf[0])
		if buf[0] == '\n' {
			break
		}
	}

	// Trim trailing whitespace/newlines.
	data = bytes.TrimRight(data, "\r\n\t ")

	if len(data) == 0 {
		s.writeError(conn, "empty request")
		return
	}

	// 2. Parse JSON.
	var req protocol.Request
	if err := json.Unmarshal(data, &req); err != nil {
		s.Logger.Debug("invalid JSON request", "error", err, "data", string(data))
		s.writeError(conn, "invalid JSON: "+err.Error())
		return
	}

	// 3. Handle ping.
	if req.Type == protocol.TypePing {
		resp := protocol.Response{Pong: true, Success: true}
		if err := writeResponse(conn, resp); err != nil {
			s.Logger.Debug("error writing ping response", "error", err)
		}
		return
	}

	// 4. Handle command (default when type is empty or "command").
	s.ConfigMu.RLock()
	cfg := s.Cfg
	s.ConfigMu.RUnlock()

	resp := s.Executor.Execute(cfg, req)

	// 5. Send response or fire-and-forget.
	if req.Wait {
		if err := writeResponse(conn, resp); err != nil {
			s.Logger.Debug("error writing response", "error", err)
		}
	}

	// 6. Maybe notify.
	s.maybeNotify(req.Command, resp)

	s.Logger.Debug("request handled",
		"command", req.Command,
		"wait", req.Wait,
		"success", resp.Success,
		"tried", len(resp.Tried),
		"errors", len(resp.Errors),
	)
}

// writeError sends a JSON error response to the client.
func (s *Server) writeError(conn net.Conn, msg string) {
	resp := protocol.Response{
		Success:  false,
		ExitCode: 1,
		Stderr:   msg,
	}
	writeResponse(conn, resp)
}

// writeResponse marshals a Response to JSON and writes it to the connection
// followed by a newline.
func writeResponse(conn net.Conn, resp protocol.Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = conn.Write(data)
	return err
}

// maybeNotify sends a desktop notification based on the configured level
// and the response outcome.
func (s *Server) maybeNotify(command string, resp protocol.Response) {
	isError := !resp.Success

	var title, body string
	if isError {
		title = fmt.Sprintf("smallctl: %s failed", command)
		body = "All attempts failed"
		if len(resp.Errors) > 0 {
			last := resp.Errors[len(resp.Errors)-1]
			if last.Stderr != "" {
				// Truncate long error messages for notification.
				if len(last.Stderr) > 200 {
					body = last.Stderr[:200] + "..."
				} else {
					body = last.Stderr
				}
			}
		}
	} else {
		title = fmt.Sprintf("smallctl: %s succeeded", command)
		if len(resp.Stdout) > 100 {
			body = resp.Stdout[:100] + "..."
		} else {
			body = resp.Stdout
		}
	}

	notify.Send(s.Notifier, s.NotifyLevel, isError, title, body)
}

// Shutdown gracefully stops the server:
//  1. Closes the listener (stops accepting new connections).
//  2. Waits for all active connections to finish (up to ShutdownTimeout).
//  3. Removes the socket file.
func (s *Server) Shutdown() error {
	s.Logger.Info("shutting down server...")

	// 1. Close the listener.
	if err := s.listener.Close(); err != nil {
		s.Logger.Warn("error closing listener", "error", err)
	}

	// 2. Wait for active connections with timeout.
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.Logger.Debug("all connections drained")
	case <-time.After(ShutdownTimeout):
		s.Logger.Warn("shutdown timeout, some connections may be dropped")
	}

	// 3. Remove the socket file.
	if err := config.CleanupPath(s.SocketPath); err != nil {
		s.Logger.Warn("error removing socket file", "error", err)
	}

	s.Logger.Info("server stopped")
	return nil
}

// Ensure types compile.
var (
	_ = json.Marshal
	_ = io.ReadAll
	_ = sync.RWMutex{}
	_ = protocol.MaxRequestSize
	_ = protocol.Request{}
	_ = protocol.TypePing
	_ = protocol.Response{}
	_ = config.Config{}
	_ = config.CleanupPath
	_ = executor.Executor{}
	_ = notify.Notifier{}
	_ = notify.LevelOff
	_ = notify.Send
	_ = filepath.Dir
	_ = os.MkdirAll
)
