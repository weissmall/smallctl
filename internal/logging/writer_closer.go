package logging

import "io"

// writerCloser wraps an io.Writer with a Close method for cleanup.
type writerCloser struct {
	io.Writer
	closeFn func() error
}
