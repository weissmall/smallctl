package config

import (
	"os"
	"path/filepath"
)

func ConfigPath(explicitPath string, binaryName string) string {
	if explicitPath != "" {
		return explicitPath
	}

	xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfigHome == "" {
		home, err := os.UserHomeDir()
		if err == nil {
			xdgConfigHome = filepath.Join(home, ".config")
		}
	}
	if xdgConfigHome == "" {
		return filepath.Join(".config", binaryName, "config.yaml")
	}

	return filepath.Join(xdgConfigHome, binaryName, "config.yaml")
}

func ResolveAppPaths(binaryName string, explicitConfig string) AppPaths {
	return AppPaths{
		BinaryName: binaryName,
		ConfigFile: ConfigPath(explicitConfig, binaryName),
		SocketPath: filepath.Join(runtimeDir(), binaryName+".sock"),
		LockPath:   filepath.Join(runtimeDir(), binaryName+".lock"),
	}
}

func runtimeDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return dir
	}
	return os.TempDir()
}

func cleanupPath(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Don't like this
var CleanupPath = cleanupPath

func FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
