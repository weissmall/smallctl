// Package config (watcher) handles hot-reloading the YAML configuration
// file using fsnotify. When the config file changes, it re-parses and
// invokes a callback with the new Config. On parse errors, the old config
// is preserved and an error is logged.
package config

import (
	"log/slog"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// Watch starts a file watcher on the given config path. When the file is
// modified, it re-parses via Load() and calls onReload with the new config.
// If parsing fails, the old config is kept and the error is logged.
//
// The watcher goroutine runs until the returned done channel is closed.
// The caller should close(done) to stop watching (typically during shutdown).
//
// The watcher watches the parent directory (not the file directly) to handle
// atomic save patterns where editors replace the file via rename+create.
// Events are filtered to only react to changes on the target filename.
//
// Parameters:
//   - path: absolute path to the YAML config file to watch.
//   - logger: structured logger for reporting reload successes/errors.
//   - onReload: callback invoked with the new Config after a successful reload.
//     Called synchronously from the watcher goroutine. The caller should
//     swap the config behind its sync.RWMutex inside this callback.
//
// Returns a done channel (close to stop) and an error if watcher init failed.
func Watch(path string, logger *slog.Logger, onReload func(*Config)) (done chan struct{}, err error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(path)
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return nil, err
	}

	done = make(chan struct{})
	targetName := filepath.Base(path)

	go func() {
		defer watcher.Close()

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if filepath.Base(event.Name) != targetName {
					continue
				}

				switch {
				case event.Has(fsnotify.Create) || event.Has(fsnotify.Write):
					logger.Debug("config file changed, reloading", "path", path)
					newCfg, loadErr := Load(path)
					if loadErr != nil {
						logger.Error("config reload failed, keeping old config",
							"error", loadErr, "path", path)
						continue
					}
					logger.Info("config reloaded successfully",
						"commands", len(newCfg.Commands))
					onReload(newCfg)

				case event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename):
					logger.Warn("config file removed, keeping old config until recreation",
						"path", path)
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				logger.Error("config watcher error", "error", err)

			case <-done:
				logger.Debug("config watcher stopped")
				return
			}
		}
	}()

	return done, nil
}
