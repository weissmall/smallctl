package logging

import (
	"path/filepath"
	"testing"
)

func TestExpandPath(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("EXPAND_TEST_VAR", "/tmp/expanded")

	tests := []struct {
		name string
		path string
		want string
	}{
		{"empty", "", ""},
		{"absolute unchanged", "/var/log/app", "/var/log/app"},
		{"relative unchanged", "logs/app", "logs/app"},
		{"tilde only", "~", "/home/testuser"},
		{"tilde slash", "~/", "/home/testuser"},
		{"tilde prefix", "~/.local/share/log", "/home/testuser/.local/share/log"},
		{"env var", "$HOME/.config/app/log", "/home/testuser/.config/app/log"},
		{"braced env var", "${EXPAND_TEST_VAR}/log", "/tmp/expanded/log"},
		{"env var then tilde", "~/${EXPAND_TEST_VAR}/x", "/home/testuser/tmp/expanded/x"},
		{"undefined var drops segment", "/opt/$UNDEFINED_VAR/log", "/opt//log"},
		{"tilde with username unchanged", "~otheruser/log", "~otheruser/log"},
		{"dollar literal without var", "/tmp/a$log", "/tmp/a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExpandPath(tt.path); got != tt.want {
				t.Errorf("ExpandPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestExpandPathJoinsCleanly(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")

	got := ExpandPath("~/.config///smallctl//log")
	if got != filepath.Join("/home/testuser", ".config", "smallctl", "log") {
		t.Errorf("ExpandPath did not clean the path: %q", got)
	}
}
