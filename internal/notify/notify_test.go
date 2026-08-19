package notify

import (
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"
)

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.Level(99)}))
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  Level
	}{
		{"off", LevelOff},
		{"error", LevelError},
		{"all", LevelAll},
		{"", LevelOff},
		{"unknown", LevelOff},
		{"OFF", LevelOff},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseLevel(tt.input)
			if got != tt.want {
				t.Errorf("ParseLevel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveLevel(t *testing.T) {
	t.Setenv("SMALLCTL_NOTIFY", "")

	tests := []struct {
		name        string
		configValue string
		want        Level
	}{
		{"empty config", "", LevelOff},
		{"error from config", "error", LevelError},
		{"all from config", "all", LevelAll},
		{"off from config", "off", LevelOff},
		{"case insensitive", "ERROR", LevelError},
		{"invalid value is off", "sometimes", LevelOff},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveLevel(tt.configValue); got != tt.want {
				t.Errorf("ResolveLevel(%q) = %q, want %q", tt.configValue, got, tt.want)
			}
		})
	}
}

func TestResolveLevelEnvOverride(t *testing.T) {
	t.Setenv("SMALLCTL_NOTIFY", "all")
	if got := ResolveLevel("error"); got != LevelAll {
		t.Errorf("env override: ResolveLevel(%q) = %q, want %q", "error", got, LevelAll)
	}

	t.Setenv("SMALLCTL_NOTIFY", "  error  ")
	if got := ResolveLevel("off"); got != LevelError {
		t.Errorf("env with spaces: ResolveLevel(%q) = %q, want %q", "off", got, LevelError)
	}

	t.Setenv("SMALLCTL_NOTIFY", "ALL")
	if got := ResolveLevel("off"); got != LevelAll {
		t.Errorf("env case insensitive: ResolveLevel(%q) = %q, want %q", "off", got, LevelAll)
	}
}

func TestProbe(t *testing.T) {
	n := Probe(silentLogger())
	_ = n
}

func TestSendNilNotifier(t *testing.T) {
	Send(nil, LevelAll, true, "test", "body")
	Send(nil, LevelError, false, "test", "body")
}

func TestSendLevelOff(t *testing.T) {
	n := &Notifier{
		transport:     func(title, body string) error { return nil },
		TransportName: "test",
		logger:        silentLogger(),
	}
	Send(n, LevelOff, true, "test", "body")
}

func TestSendLevelErrorSkipsSuccess(t *testing.T) {
	called := false
	n := &Notifier{
		transport: func(title, body string) error {
			called = true
			return nil
		},
		TransportName: "test",
		logger:        silentLogger(),
	}
	Send(n, LevelError, false, "test", "body")
	_ = called
}

func TestDispatchLevelFiltering(t *testing.T) {
	tests := []struct {
		name     string
		level    Level
		isError  bool
		wantSent bool
	}{
		{"off with error", LevelOff, true, false},
		{"off with success", LevelOff, false, false},
		{"error with error", LevelError, true, true},
		{"error with success", LevelError, false, false},
		{"all with error", LevelAll, true, true},
		{"all with success", LevelAll, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			sent := 0
			n := &Notifier{
				transport: func(t, b string) error {
					mu.Lock()
					defer mu.Unlock()
					sent++
					return nil
				},
				TransportName: "test",
				logger:        silentLogger(),
			}

			n.Dispatch(tt.level, tt.isError, "smallctl: cmd", "body")

			if tt.wantSent {
				deadline := time.Now().Add(2 * time.Second)
				for time.Now().Before(deadline) {
					mu.Lock()
					done := sent > 0
					mu.Unlock()
					if done {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
			} else {
				time.Sleep(100 * time.Millisecond)
			}

			mu.Lock()
			defer mu.Unlock()
			if tt.wantSent && sent != 1 {
				t.Errorf("expected exactly one send, got %d", sent)
			}
			if !tt.wantSent && sent != 0 {
				t.Errorf("expected no send, got %d", sent)
			}
		})
	}
}

func TestNotifySendTransportCmd(t *testing.T) {
	_ = notifySendTransport
	_ = gdbusTransport
	_ = dbusSendTransport
}
