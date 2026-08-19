package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/weissmall/smallctl/internal/general"
)

func TestSubstitute(t *testing.T) {
	tests := []struct {
		name      string
		cmd       string
		defaults  map[string]string
		overrides map[string]string
		want      string
	}{
		{
			name:      "simple substitution",
			cmd:       "brightnessctl set +${step}%",
			defaults:  map[string]string{"step": "10"},
			overrides: nil,
			want:      "brightnessctl set +10%",
		},
		{
			name:      "override takes priority",
			cmd:       "brightnessctl set +${step}%",
			defaults:  map[string]string{"step": "10"},
			overrides: map[string]string{"step": "20"},
			want:      "brightnessctl set +20%",
		},
		{
			name:      "missing var becomes empty",
			cmd:       "echo ${nonexistent}",
			defaults:  nil,
			overrides: nil,
			want:      "echo ",
		},
		{
			name:      "dollar escape",
			cmd:       "echo $$HOME is ${HOME}",
			defaults:  nil,
			overrides: map[string]string{"HOME": "/home/user"},
			want:      "echo $HOME is /home/user",
		},
		{
			name:      "double dollar at end",
			cmd:       "end$$",
			defaults:  nil,
			overrides: nil,
			want:      "end$",
		},
		{
			name:      "multiple vars",
			cmd:       "${a} ${b} ${a}",
			defaults:  map[string]string{"a": "1", "b": "2"},
			overrides: nil,
			want:      "1 2 1",
		},
		{
			name:      "unclosed brace treated as literal",
			cmd:       "${open",
			defaults:  nil,
			overrides: nil,
			want:      "${open",
		},
		{
			name:      "no vars",
			cmd:       "echo hello",
			defaults:  nil,
			overrides: nil,
			want:      "echo hello",
		},
		{
			name:      "override adds new var",
			cmd:       "use ${tool}",
			defaults:  nil,
			overrides: map[string]string{"tool": "brightnessctl"},
			want:      "use brightnessctl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Substitute(tt.cmd, tt.defaults, tt.overrides)
			if got != tt.want {
				t.Errorf("Substitute() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadExampleConfig(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	examplePath := filepath.Join(dir, "..", "..", "config.example.yaml")

	cfg, err := Load(examplePath)
	if err != nil {
		t.Fatalf("Load(%q) failed: %v", examplePath, err)
	}

	if len(cfg.Commands) == 0 {
		t.Error("expected commands to be loaded from example config")
	}

	if cfg.Options.Shell != "bash" {
		t.Errorf("expected default Shell='bash', got %q", cfg.Options.Shell)
	}

	if cfg.Options.Notify != "off" {
		t.Errorf("expected default Notify='off', got %q", cfg.Options.Notify)
	}

	if cfg.Options.Timeout == nil || *cfg.Options.Timeout != general.DefaultTimeout {
		t.Errorf("expected Timeout=%d, got %v", general.DefaultTimeout, cfg.Options.Timeout)
	}

	cmd, ok := cfg.Commands["brightnessIncrease"]
	if !ok {
		t.Fatal("expected brightnessIncrease command in config")
	}
	if cmd.Args["step"] != "10" {
		t.Errorf("expected brightnessIncrease args.step=10, got %q", cmd.Args["step"])
	}
	if len(cmd.Fallback) == 0 {
		t.Error("expected fallback commands for brightnessIncrease")
	}

	t.Logf("Loaded %d commands from example config", len(cfg.Commands))
}

func TestLoadNonexistent(t *testing.T) {
	cfg, err := Load("/tmp/smallctl-nonexistent-config-test.yaml")
	if err != nil {
		t.Fatalf("Load of nonexistent file should not error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config from nonexistent file")
	}
	if len(cfg.Commands) != 0 {
		t.Errorf("expected 0 commands from nonexistent file, got %d", len(cfg.Commands))
	}
	if cfg.Options.Shell != general.DefaultShell {
		t.Errorf("expected default shell, got %q", cfg.Options.Shell)
	}
}

func TestConfigPath(t *testing.T) {
	got := ConfigPath("/custom/path.yaml", "myapp")
	if got != "/custom/path.yaml" {
		t.Errorf("explicit path: got %q, want %q", got, "/custom/path.yaml")
	}

	os.Setenv("XDG_CONFIG_HOME", "/custom/config")
	got = ConfigPath("", "myapp")
	if got != "/custom/config/myapp/config.yaml" {
		t.Errorf("XDG_CONFIG_HOME: got %q, want %q", got, "/custom/config/myapp/config.yaml")
	}
	os.Unsetenv("XDG_CONFIG_HOME")
}

func TestResolveAppPaths(t *testing.T) {
	os.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	defer os.Unsetenv("XDG_RUNTIME_DIR")

	paths := ResolveAppPaths("smallctl", "")
	if paths.BinaryName != "smallctl" {
		t.Errorf("BinaryName: got %q, want %q", paths.BinaryName, "smallctl")
	}
	if paths.SocketPath != "/run/user/1000/smallctl.sock" {
		t.Errorf("SocketPath: got %q", paths.SocketPath)
	}
	if paths.LockPath != "/run/user/1000/smallctl.lock" {
		t.Errorf("LockPath: got %q", paths.LockPath)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: &Config{
				Options: Options{Shell: "bash", Notify: "off"},
				Commands: map[string]Command{
					"test": {Fallback: []string{"echo hello"}},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid notify",
			cfg: &Config{
				Options: Options{Shell: "bash", Notify: "invalid"},
			},
			wantErr: true,
		},
		{
			name: "empty command string",
			cfg: &Config{
				Options: Options{Shell: "bash"},
				Commands: map[string]Command{
					"test": {Fallback: []string{""}},
				},
			},
			wantErr: true,
		},
		{
			name: "empty shell",
			cfg: &Config{
				Options: Options{Shell: ""},
			},
			wantErr: true,
		},
		{
			name: "negative timeout",
			cfg: &Config{
				Options: Options{Shell: "bash"},
			},
			wantErr: false, // Timeout nil → default applied, no error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cfg.Options.Timeout == nil {
				d := general.DefaultTimeout
				tt.cfg.Options.Timeout = &d
			}
			err := Validate(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestCleanupPath(t *testing.T) {
	err := cleanupPath("/tmp/smallctl-nonexistent-cleanup-test")
	if err != nil {
		t.Errorf("cleanup of nonexistent file should not error: %v", err)
	}

	f, err := os.CreateTemp("", "smallctl-cleanup-test-*")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()

	err = cleanupPath(path)
	if err != nil {
		t.Errorf("cleanup of real file failed: %v", err)
	}
	if FileExists(path) {
		t.Errorf("file should not exist after cleanup: %s", path)
	}
}
