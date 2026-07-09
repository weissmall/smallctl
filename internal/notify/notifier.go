package notify

import "log/slog"

// Notifier is a handle for sending desktop notifications.
type Notifier struct {
	transport     func(title, body string) error
	TransportName string
	logger        *slog.Logger
}
