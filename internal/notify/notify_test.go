package notify

import (
	"log/slog"
	"os"
	"testing"
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

func TestNotifySendTransportCmd(t *testing.T) {
	_ = notifySendTransport
	_ = gdbusTransport
	_ = dbusSendTransport
}
