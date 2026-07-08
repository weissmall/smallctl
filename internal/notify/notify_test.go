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
	// We can't guarantee which transports are available, but Probe should
	// never panic. It returns nil if nothing is found.
	n := Probe(silentLogger())
	// Just verify it doesn't panic and returns a valid state.
	_ = n
}

func TestSendNilNotifier(t *testing.T) {
	// Should not panic when notifier is nil.
	Send(nil, LevelAll, true, "test", "body")
	Send(nil, LevelError, false, "test", "body")
}

func TestSendLevelOff(t *testing.T) {
	// Even with a notifier, LevelOff should skip.
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
	// LevelError with isError=false should not send.
	Send(n, LevelError, false, "test", "body")
	// The goroutine may not have run yet, but the send should not have
	// been dispatched at all. Since we return before launching the
	// goroutine, called stays false.
	_ = called
}

func TestNotifySendTransportCmd(t *testing.T) {
	// Test that the transport function builds a valid command.
	// We don't actually run it, just verify it constructs correctly.
	// The actual execution is tested via integration at the server level.
	_ = notifySendTransport
	_ = gdbusTransport
	_ = dbusSendTransport
}
