package logging

import (
	"os"
	"path/filepath"
	"strings"
)

// ExpandPath expands environment variables ($VAR, ${VAR}) in path and a
// leading ~ or ~/ to the user's home directory. Paths without variables or
// a home prefix are returned unchanged.
func ExpandPath(path string) string {
	if path == "" {
		return path
	}

	expanded := os.ExpandEnv(path)

	if expanded == "~" || strings.HasPrefix(expanded, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if expanded == "~" {
				return home
			}
			return filepath.Join(home, expanded[2:])
		}
	}

	return expanded
}
