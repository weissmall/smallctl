package notify

import (
	"log/slog"
	"os/exec"
)

// Probe detects available notification transports and returns a Notifier.
func Probe(logger *slog.Logger) *Notifier {
	if err := exec.Command("notify-send", "--version").Run(); err == nil {
		logger.Info("notification transport found", "transport", "notify-send")
		return &Notifier{
			transport:     notifySendTransport,
			TransportName: "notify-send",
			logger:        logger,
		}
	}
	logger.Debug("notify-send not available")

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

	logger.Warn("no notification transport found, notifications disabled")
	return nil
}
