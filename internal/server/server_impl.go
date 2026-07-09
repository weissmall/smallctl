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
	"time"

	"github.com/weissmall/smallctl/internal/config"
	"github.com/weissmall/smallctl/internal/notify"
	"github.com/weissmall/smallctl/internal/ports"
	"github.com/weissmall/smallctl/internal/protocol"
)

func New(
	socketPath string,
	logger *slog.Logger,
	exec ports.ExecutionEngine,
	notif ports.NotificationDispatcher,
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

func (s *Server) UpdateConfig(cfg *config.Config) {
	s.ConfigMu.Lock()
	s.Cfg = cfg
	s.ConfigMu.Unlock()

	s.Executor.SetShell(cfg.Options.Shell)
	if cfg.Options.Timeout != nil {
		s.Executor.SetDefaultTimeout(time.Duration(*cfg.Options.Timeout) * time.Second)
	}

	if err := s.Executor.ResolveEnv(cfg); err != nil {
		s.Logger.Warn("failed to re-resolve environment after config reload", "error", err)
	}
}

func (s *Server) Listen() error {
	if err := config.CleanupPath(s.SocketPath); err != nil {
		return fmt.Errorf("removing stale socket %s: %w", s.SocketPath, err)
	}

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

func (s *Server) Serve() error {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && opErr.Err.Error() == "use of closed network connection" {
				return nil
			}
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

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	defer s.wg.Done()

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

	data = bytes.TrimRight(data, "\r\n\t ")

	if len(data) == 0 {
		s.writeError(conn, "empty request")
		return
	}

	var req protocol.Request
	if err := json.Unmarshal(data, &req); err != nil {
		s.Logger.Debug("invalid JSON request", "error", err, "data", string(data))
		s.writeError(conn, "invalid JSON: "+err.Error())
		return
	}

	if req.Type == protocol.TypePing {
		resp := protocol.Response{Pong: true, Success: true}
		if err := writeResponse(conn, resp); err != nil {
			s.Logger.Debug("error writing ping response", "error", err)
		}
		return
	}

	s.ConfigMu.RLock()
	cfg := s.Cfg
	s.ConfigMu.RUnlock()

	resp := s.Executor.Execute(cfg, req)

	if req.Wait {
		if err := writeResponse(conn, resp); err != nil {
			s.Logger.Debug("error writing response", "error", err)
		}
	}

	s.maybeNotify(req.Command, resp)

	s.Logger.Debug("request handled",
		"command", req.Command,
		"wait", req.Wait,
		"success", resp.Success,
		"tried", len(resp.Tried),
		"errors", len(resp.Errors),
	)
}

func (s *Server) writeError(conn net.Conn, msg string) {
	resp := protocol.Response{
		Success:  false,
		ExitCode: 1,
		Stderr:   msg,
	}
	writeResponse(conn, resp)
}

func writeResponse(conn net.Conn, resp protocol.Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = conn.Write(data)
	return err
}

func (s *Server) maybeNotify(command string, resp protocol.Response) {
	if s.Notifier == nil {
		return
	}

	isError := !resp.Success

	var title, body string
	if isError {
		title = fmt.Sprintf("smallctl: %s failed", command)
		body = "All attempts failed"
		if len(resp.Errors) > 0 {
			last := resp.Errors[len(resp.Errors)-1]
			if last.Stderr != "" {
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

	s.Notifier.Dispatch(s.NotifyLevel, isError, title, body)
}

func (s *Server) Shutdown() error {
	s.Logger.Info("shutting down server...")

	if err := s.listener.Close(); err != nil {
		s.Logger.Warn("error closing listener", "error", err)
	}

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

	if err := config.CleanupPath(s.SocketPath); err != nil {
		s.Logger.Warn("error removing socket file", "error", err)
	}

	s.Logger.Info("server stopped")
	return nil
}
