package logging

func (w *writerCloser) Close() error {
	if w.closeFn != nil {
		return w.closeFn()
	}
	return nil
}
