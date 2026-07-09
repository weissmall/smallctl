// Package notify provides optional desktop notification support.
// It probes for available notification transports at startup and sends
// notifications on command completion or failure based on the configured
// notify level ("off", "error", "all").
//
// Transports are probed in order: notify-send, gdbus, dbus-send.
// If none are found, notifications are silently disabled.
package notify

import (
	"fmt"
	"log/slog"
	"os/exec"
)

// Notifier is a handle for sending desktop notifications.
// It is nil if no transport is available (notifications disabled).
type Notifier struct {
	// transport is the function used to send a notification.
	transport func(title, body string) error

	// TransportName is a human-readable label for logging.
	TransportName string

	// logger is used for debug logging from async sends.
	logger *slog.Logger
}

// Level defines when notifications are sent.
type Level string

const (
	// LevelOff disables all notifications.
	LevelOff Level = "off"
	// LevelError sends notifications only on command failure.
	LevelError Level = "error"
	// LevelAll sends notifications on every invocation (success or failure).
	LevelAll Level = "all"
)

// ParseLevel converts a string to a Level, defaulting to LevelOff.
func ParseLevel(s string) Level {
	switch s {
	case "error":
		return LevelError
	case "all":
		return LevelAll
	default:
		return LevelOff
	}
}

// Probe detects available notification transports and returns a Notifier.
//
// Probe order:
//  1. notify-send --version → use notify-send
//  2. gdbus call --session ... → use gdbus
//  3. dbus-send ... → use dbus-send
//  4. None found → return nil (notifications disabled)
//
// Each probe runs the binary with a quick version/ping check.
// If the binary is not found or returns non-zero, the next transport is tried.
func Probe(logger *slog.Logger) *Notifier {
	// 1. Try notify-send.
	if err := exec.Command("notify-send", "--version").Run(); err == nil {
		logger.Info("notification transport found", "transport", "notify-send")
		return &Notifier{
			transport:     notifySendTransport,
			TransportName: "notify-send",
			logger:        logger,
		}
	}
	logger.Debug("notify-send not available")

	// 2. Try gdbus.
	if err := exec.Command("gdbus", "call", "--session",
		"--dest", "org.freedesktop.Notifications",
		"--object-path", "/org/freedesktop/Notifications",
		"--method", "org.freedesktop.DBus.Peer.Ping",
	).Run(); err == nil {
		logger.Info("notification transport found", "transport", "gdbus")
		return &Notifier{
			transport:     gdbusTransport,
			TransportName: "gdbus",
			logger:        logger,
		}
	}
	logger.Debug("gdbus not available")

	// 3. Try dbus-send.
	if err := exec.Command("dbus-send", "--session",
		"--dest=org.freedesktop.Notifications",
		"--type=method_call", "--print-reply",
		"/org/freedesktop/Notifications",
		"org.freedesktop.DBus.Peer.Ping",
	).Run(); err == nil {
		logger.Info("notification transport found", "transport", "dbus-send")
		return &Notifier{
			transport:     dbusSendTransport,
			TransportName: "dbus-send",
			logger:        logger,
		}
	}
	logger.Debug("dbus-send not available")

	// 4. No transport found.
	logger.Warn("no notification transport found, notifications disabled")
	return nil
}

// Send fires a desktop notification if n is non-nil and the level permits it.
//
// Parameters:
//   - n: the Notifier from Probe() (may be nil).
//   - level: the configured notification level.
//   - isError: true if the command failed, false if it succeeded.
//   - title: notification title.
//   - body: notification body text.
//
// Send runs in a goroutine — it never blocks the caller.
// If the notification command fails, the error is logged at debug level.
func Send(n *Notifier, level Level, isError bool, title, body string) {
	if n == nil {
		return
	}
	if level == LevelOff {
		return
	}
	if level == LevelError && !isError {
		return
	}

	// Launch in a goroutine to avoid blocking.
	go func() {
		if err := n.transport(title, body); err != nil {
			n.logger.Debug("notification send failed",
				"transport", n.TransportName, "error", err)
		}
	}()
}

// ── Transport implementations ────────────────────────────────────

// notifySendTransport sends a notification via notify-send.
func notifySendTransport(title, body string) error {
	cmd := exec.Command("notify-send", title, body)
	return cmd.Run()
}

// gdbusTransport sends a notification via gdbus call.
//
// Uses the org.freedesktop.Notifications.Notify method with:
//
//	app_name: "smallctl"
//	replaces_id: 0
//	app_icon: ""
//	summary: title
//	body: body
//	actions: [] (empty array)
//	hints: {} (empty dict)
//	expire_timeout: 5000ms
func gdbusTransport(title, body string) error {
	cmd := exec.Command("gdbus", "call", "--session",
		"--dest", "org.freedesktop.Notifications",
		"--object-path", "/org/freedesktop/Notifications",
		"--method", "org.freedesktop.Notifications.Notify",
		"smallctl", // app_name
		"0",        // replaces_id
		"",         // app_icon
		title,      // summary
		body,       // body
		"[]",       // actions
		"{}",       // hints
		"5000",     // expire_timeout
	)
	return cmd.Run()
}

// dbusSendTransport sends a notification via dbus-send.
func dbusSendTransport(title, body string) error {
	cmd := exec.Command("dbus-send", "--session",
		"--dest=org.freedesktop.Notifications",
		"--type=method_call",
		"--print-reply",
		"/org/freedesktop/Notifications",
		"org.freedesktop.Notifications.Notify",
		"string:smallctl",
		"uint32:0",
		"string:",
		fmt.Sprintf("string:%s", title),
		fmt.Sprintf("string:%s", body),
		"array:string:",
		"dict:string:string:",
		"int32:5000",
	)
	return cmd.Run()
}
