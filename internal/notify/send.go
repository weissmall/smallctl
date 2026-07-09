package notify

// Send fires a desktop notification if n is non-nil and the level permits it.
func Send(n *Notifier, level Level, isError bool, title, body string) {
	if n == nil {
		return
	}
	n.Dispatch(level, isError, title, body)
}

// Dispatch sends a notification according to the configured level.
func (n *Notifier) Dispatch(level Level, isError bool, title, body string) {
	if n == nil {
		return
	}
	if level == LevelOff {
		return
	}
	if level == LevelError && !isError {
		return
	}

	go func() {
		if err := n.transport(title, body); err != nil {
			n.logger.Debug("notification send failed",
				"transport", n.TransportName, "error", err)
		}
	}()
}
