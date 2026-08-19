package config

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"smallctl/internal/general"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func loadFromDir(t *testing.T, dir string) (*Config, error) {
	t.Helper()
	return Load(filepath.Join(dir, MainConfigName))
}

func TestLoadMultiFileUserExample(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `
commands:
  volumeMute:
    description: "Toggle audio mute"
`)
	writeFile(t, dir, "dms.yaml", `
commands:
  volumeMute: "dms ipc call audio mute"
`)
	writeFile(t, dir, "noctalia.yaml", `
commands:
  volumeMute: "noctalia-shell ipc volume mute"
`)

	cfg, err := loadFromDir(t, dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cfg.Commands))
	}

	cmd, ok := cfg.Commands["volumeMute"]
	if !ok {
		t.Fatal("expected volumeMute command")
	}
	if cmd.Description != "Toggle audio mute" {
		t.Errorf("description: got %q, want %q", cmd.Description, "Toggle audio mute")
	}
	if cmd.Envs["dms"] != "dms ipc call audio mute" {
		t.Errorf("envs.dms: got %q", cmd.Envs["dms"])
	}
	if cmd.Envs["noctalia"] != "noctalia-shell ipc volume mute" {
		t.Errorf("envs.noctalia: got %q", cmd.Envs["noctalia"])
	}
	if len(cmd.Envs) != 2 {
		t.Errorf("expected 2 envs, got %d", len(cmd.Envs))
	}
}

func TestLoadMultiFileEnvsUnionWithMain(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `
commands:
  volumeMute:
    description: "Toggle audio mute"
    envs:
      workstation: "pactl set-sink-mute @DEFAULT_SINK@ toggle"
`)
	writeFile(t, dir, "dms.yaml", `
commands:
  volumeMute: "dms ipc call audio mute"
`)

	cfg, err := loadFromDir(t, dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	cmd := cfg.Commands["volumeMute"]
	if cmd.Envs["workstation"] == "" || cmd.Envs["dms"] == "" {
		t.Errorf("expected union of envs from main and env file, got %v", cmd.Envs)
	}
}

func TestLoadEnvFileFullObject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `
commands:
  volumeMute:
    description: "Main description"
    args:
      step: "10"
    fallback:
      - "echo main-fallback"
`)
	writeFile(t, dir, "dms.yaml", `
commands:
  volumeMute:
    description: "DMS description"
    args:
      step: "20"
      extra: "x"
    fallback:
      - "echo dms-fallback"
    envs:
      dms: "dms ipc call audio mute"
      other: "other ipc call"
`)

	cfg, err := loadFromDir(t, dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	cmd := cfg.Commands["volumeMute"]
	if cmd.Description != "DMS description" {
		t.Errorf("description: got %q, want env file value", cmd.Description)
	}
	if cmd.Args["step"] != "20" {
		t.Errorf("args.step: got %q, want env file override", cmd.Args["step"])
	}
	if cmd.Args["extra"] != "x" {
		t.Errorf("args.extra: got %q, want env file addition", cmd.Args["extra"])
	}
	if len(cmd.Args) != 2 {
		t.Errorf("expected 2 args, got %d (%v)", len(cmd.Args), cmd.Args)
	}
	if len(cmd.Fallback) != 1 || cmd.Fallback[0] != "echo dms-fallback" {
		t.Errorf("fallback: got %v, want env file replacement", cmd.Fallback)
	}
	if cmd.Envs["dms"] != "dms ipc call audio mute" || cmd.Envs["other"] != "other ipc call" {
		t.Errorf("envs: got %v", cmd.Envs)
	}
}

func TestLoadEnvFileDescriptionOrder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `
commands:
  cmd:
    description: "From main"
`)
	writeFile(t, dir, "a.yaml", `
commands:
  cmd:
    description: "From a"
`)
	writeFile(t, dir, "b.yaml", `
commands:
  cmd:
    description: "From b"
`)

	cfg, err := loadFromDir(t, dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if got := cfg.Commands["cmd"].Description; got != "From b" {
		t.Errorf("description: got %q, want later file value %q", got, "From b")
	}
}

func TestLoadSingleFileBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `
options:
  shell: fish
commands:
  volumeMute:
    description: "Toggle audio mute"
    envs:
      dms: "dms ipc call audio mute"
    fallback:
      - "pactl set-sink-mute @DEFAULT_SINK@ toggle"
`)

	cfg, err := loadFromDir(t, dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Options.Shell != "fish" {
		t.Errorf("shell: got %q", cfg.Options.Shell)
	}
	cmd := cfg.Commands["volumeMute"]
	if cmd.Description != "Toggle audio mute" || cmd.Envs["dms"] != "dms ipc call audio mute" {
		t.Errorf("unexpected command: %+v", cmd)
	}
}

func TestLoadEnvFilesWithoutMainConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "dms.yaml", `
commands:
  volumeMute: "dms ipc call audio mute"
`)

	cfg, err := loadFromDir(t, dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	cmd, ok := cfg.Commands["volumeMute"]
	if !ok {
		t.Fatal("expected volumeMute command from env file alone")
	}
	if cmd.Envs["dms"] != "dms ipc call audio mute" {
		t.Errorf("envs.dms: got %q", cmd.Envs["dms"])
	}
	if cfg.Options.Shell != general.DefaultShell {
		t.Errorf("expected default shell, got %q", cfg.Options.Shell)
	}
}

func TestLoadOptionFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `
commands:
  volumeMute:
    description: "Toggle audio mute"
`)
	writeFile(t, dir, "options.yaml", `
options:
  shell: fish
  timeout: 15
  notify: error
`)

	cfg, err := loadFromDir(t, dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Options.Shell != "fish" {
		t.Errorf("shell: got %q", cfg.Options.Shell)
	}
	if cfg.Options.Timeout == nil || *cfg.Options.Timeout != 15 {
		t.Errorf("timeout: got %v", cfg.Options.Timeout)
	}
	if cfg.Options.Notify != "error" {
		t.Errorf("notify: got %q", cfg.Options.Notify)
	}
}

func TestLoadOptionFilesIdenticalValues(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `
options:
  shell: fish
`)
	writeFile(t, dir, "more-options.yaml", `
options:
  shell: fish
  notify: all
`)

	cfg, err := loadFromDir(t, dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Options.Shell != "fish" || cfg.Options.Notify != "all" {
		t.Errorf("unexpected options: %+v", cfg.Options)
	}
}

func TestLoadOptionFileConflict(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `
options:
  shell: bash
`)
	writeFile(t, dir, "options.yaml", `
options:
  shell: fish
`)

	_, err := loadFromDir(t, dir)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	for _, want := range []string{"options.shell", "config.yaml", "options.yaml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestLoadOptionFileConflictBetweenOptionFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a-options.yaml", `
options:
  timeout: 10
`)
	writeFile(t, dir, "b-options.yaml", `
options:
  timeout: 20
`)

	_, err := loadFromDir(t, dir)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "options.timeout") {
		t.Errorf("error %q does not mention options.timeout", err)
	}
}

func TestLoadNullCommandsSectionIsOptionFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `
commands:
  volumeMute:
    description: "Toggle audio mute"
`)
	writeFile(t, dir, "extra.yaml", "commands:\noptions:\n  shell: zsh\n")

	cfg, err := loadFromDir(t, dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Options.Shell != "zsh" {
		t.Errorf("shell: got %q, want zsh from file with null commands", cfg.Options.Shell)
	}
	if len(cfg.Commands) != 1 {
		t.Errorf("expected 1 command, got %d", len(cfg.Commands))
	}
}

func TestLoadEmptyCommandsMapping(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `
commands:
  volumeMute:
    description: "Toggle audio mute"
`)
	writeFile(t, dir, "empty.yaml", "commands: {}\n")

	cfg, err := loadFromDir(t, dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.Commands) != 1 {
		t.Errorf("expected 1 command, got %d", len(cfg.Commands))
	}
}

func TestLoadEmptyFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `
commands:
  volumeMute:
    description: "Toggle audio mute"
`)
	writeFile(t, dir, "empty.yaml", "")

	cfg, err := loadFromDir(t, dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.Commands) != 1 {
		t.Errorf("expected 1 command, got %d", len(cfg.Commands))
	}
}

func TestLoadUnrelatedFileWithoutCommands(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `
commands:
  volumeMute:
    description: "Toggle audio mute"
`)
	writeFile(t, dir, "notes.yaml", "notes: just some notes\n")

	cfg, err := loadFromDir(t, dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(cfg.Commands) != 1 {
		t.Errorf("expected 1 command, got %d", len(cfg.Commands))
	}
}

func TestLoadEnvNameFromFileName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "my.env.yaml", `
commands:
  volumeMute: "some command"
`)

	cfg, err := loadFromDir(t, dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if got := cfg.Commands["volumeMute"].Envs["my.env"]; got != "some command" {
		t.Errorf("envs[my.env]: got %q", got)
	}
}

func TestLoadCustomMainFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "custom.yaml", `
commands:
  customCmd:
    description: "Custom"
`)
	writeFile(t, dir, "config.yaml", `
commands:
  mainOnly:
    description: "From config.yaml"
`)
	writeFile(t, dir, "dms.yaml", `
commands:
  customCmd: "dms ipc call audio mute"
`)

	cfg, err := Load(filepath.Join(dir, "custom.yaml"))
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if _, ok := cfg.Commands["mainOnly"]; ok {
		t.Error("sibling config.yaml must not be loaded when main file is custom.yaml")
	}
	cmd, ok := cfg.Commands["customCmd"]
	if !ok {
		t.Fatal("expected customCmd from custom main file")
	}
	if cmd.Envs["dms"] != "dms ipc call audio mute" {
		t.Errorf("envs.dms: got %q", cmd.Envs["dms"])
	}
}

func TestLoadCommandsWithoutMainSection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", "options:\n  shell: bash\n")
	writeFile(t, dir, "dms.yaml", `
commands:
  volumeMute: "dms ipc call audio mute"
`)
	writeFile(t, dir, "noctalia.yaml", `
commands:
  brightnessUp: "noctalia brightness up"
`)

	cfg, err := loadFromDir(t, dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Commands["volumeMute"].Envs["dms"] != "dms ipc call audio mute" {
		t.Errorf("envs.dms: got %q", cfg.Commands["volumeMute"].Envs["dms"])
	}
	if cfg.Commands["brightnessUp"].Envs["noctalia"] != "noctalia brightness up" {
		t.Errorf("envs.noctalia: got %q", cfg.Commands["brightnessUp"].Envs["noctalia"])
	}
}

func TestLoadLoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		wantErr []string
	}{
		{
			name: "duplicate env between main and env file",
			files: map[string]string{
				"config.yaml": "commands:\n  volumeMute:\n    envs:\n      dms: \"from main\"\n",
				"dms.yaml":    "commands:\n  volumeMute: \"from env file\"\n",
			},
			wantErr: []string{"commands.volumeMute", `"dms"`, "config.yaml", "dms.yaml"},
		},
		{
			name: "duplicate env between two env files",
			files: map[string]string{
				"noctalia.yaml": "commands:\n  volumeMute:\n    envs:\n      dms: \"explicit in noctalia\"\n",
				"dms.yaml":      "commands:\n  volumeMute: \"from dms file\"\n",
			},
			wantErr: []string{"commands.volumeMute", "dms.yaml", "noctalia.yaml"},
		},
		{
			name: "options in env file",
			files: map[string]string{
				"dms.yaml": "options:\n  shell: fish\ncommands:\n  volumeMute: \"cmd\"\n",
			},
			wantErr: []string{"options are not allowed in env files", "dms.yaml"},
		},
		{
			name: "shorthand not a string",
			files: map[string]string{
				"dms.yaml": "commands:\n  volumeMute: 5\n",
			},
			wantErr: []string{"commands.volumeMute", "command string or a mapping", "dms.yaml"},
		},
		{
			name: "shorthand is null",
			files: map[string]string{
				"dms.yaml": "commands:\n  volumeMute:\n",
			},
			wantErr: []string{"commands.volumeMute", "command string or a mapping"},
		},
		{
			name: "shorthand is a sequence",
			files: map[string]string{
				"dms.yaml": "commands:\n  volumeMute:\n    - a\n    - b\n",
			},
			wantErr: []string{"commands.volumeMute", "command string or a mapping"},
		},
		{
			name: "empty shorthand string",
			files: map[string]string{
				"dms.yaml": "commands:\n  volumeMute: \"\"\n",
			},
			wantErr: []string{"must not be empty", "dms.yaml"},
		},
		{
			name: "string command in main file",
			files: map[string]string{
				"config.yaml": "commands:\n  volumeMute: \"just a string\"\n",
			},
			wantErr: []string{"parsing config file"},
		},
		{
			name: "invalid yaml in env file",
			files: map[string]string{
				"dms.yaml": "commands: {volumeMute: [unclosed\n",
			},
			wantErr: []string{"dms.yaml"},
		},
		{
			name: "commands not a mapping",
			files: map[string]string{
				"dms.yaml": "commands:\n  - a\n  - b\n",
			},
			wantErr: []string{"commands must be a mapping", "dms.yaml"},
		},
		{
			name: "invalid option value in option file",
			files: map[string]string{
				"options.yaml": "options:\n  timeout: not-a-number\n",
			},
			wantErr: []string{"options.yaml"},
		},
		{
			name: "invalid option value in main file",
			files: map[string]string{
				"config.yaml": "options:\n  notify: sometimes\n",
			},
			wantErr: []string{"validating config", "notify"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, content := range tt.files {
				writeFile(t, dir, name, content)
			}

			cfg, err := loadFromDir(t, dir)
			if err == nil {
				t.Fatalf("expected error, got config: %+v", cfg)
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

func TestLoadIgnoresNonConfigFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `
commands:
  volumeMute:
    description: "Toggle audio mute"
`)
	writeFile(t, dir, "dms.yaml", `
commands:
  volumeMute: "dms ipc call audio mute"
`)
	writeFile(t, dir, ".hidden.yaml", "commands:\n  volumeMute: \"from hidden\"\n")
	writeFile(t, dir, "notes.txt", "commands:\n  volumeMute: \"from txt\"\n")
	writeFile(t, dir, "extra.yml", "commands:\n  volumeMute: \"from yml\"\n")
	if err := os.Mkdir(filepath.Join(dir, "subdir.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadFromDir(t, dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	cmd := cfg.Commands["volumeMute"]
	if len(cmd.Envs) != 1 || cmd.Envs["dms"] != "dms ipc call audio mute" {
		t.Errorf("expected only the dms env, got %v", cmd.Envs)
	}
}

func TestIsConfigFileName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"dms.yaml", true},
		{"config.yaml", true},
		{"my.env.yaml", true},
		{"dms.yml", false},
		{"dms.txt", false},
		{".hidden.yaml", false},
		{"yaml", false},
		{".yaml", false},
		{"dms.yaml.bak", false},
	}
	for _, tt := range tests {
		if got := isConfigFileName(tt.name); got != tt.want {
			t.Errorf("isConfigFileName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestHasConfig(t *testing.T) {
	t.Run("main file exists", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "config.yaml", "commands: {}\n")
		if !HasConfig(filepath.Join(dir, "config.yaml")) {
			t.Error("expected true when main file exists")
		}
	})

	t.Run("only env file exists", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "dms.yaml", "commands: {}\n")
		if !HasConfig(filepath.Join(dir, "config.yaml")) {
			t.Error("expected true when only env file exists")
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		if HasConfig(filepath.Join(dir, "config.yaml")) {
			t.Error("expected false for empty directory")
		}
	})

	t.Run("only hidden and non-yaml files", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, ".hidden.yaml", "commands: {}\n")
		writeFile(t, dir, "notes.txt", "hi\n")
		if HasConfig(filepath.Join(dir, "config.yaml")) {
			t.Error("expected false when only hidden/non-yaml files exist")
		}
	})

	t.Run("missing directory", func(t *testing.T) {
		dir := t.TempDir()
		if HasConfig(filepath.Join(dir, "missing", "config.yaml")) {
			t.Error("expected false for missing directory")
		}
	})
}

func TestLoadMissingDirectory(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "missing", "config.yaml"))
	if err != nil {
		t.Fatalf("Load from missing directory should not error: %v", err)
	}
	if cfg == nil || len(cfg.Commands) != 0 {
		t.Errorf("expected empty config, got %+v", cfg)
	}
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func waitForReload(t *testing.T, reloaded <-chan *Config, check func(*Config) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case cfg := <-reloaded:
			if check(cfg) {
				return
			}
		case <-time.After(200 * time.Millisecond):
		}
	}
	t.Fatal("timed out waiting for expected reload")
}

func TestWatchReloadsOnAnyConfigFileChange(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "config.yaml")
	writeFile(t, dir, "config.yaml", "commands:\n  volumeMute:\n    description: \"Toggle audio mute\"\n")
	writeFile(t, dir, "dms.yaml", "commands:\n  volumeMute: \"dms ipc call audio mute\"\n")

	reloaded := make(chan *Config, 16)
	done, err := Watch(mainPath, silentLogger(), func(cfg *Config) {
		select {
		case reloaded <- cfg:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer close(done)

	writeFile(t, dir, "noctalia.yaml", "commands:\n  volumeMute: \"noctalia-shell ipc volume mute\"\n")
	waitForReload(t, reloaded, func(cfg *Config) bool {
		cmd := cfg.Commands["volumeMute"]
		return cmd.Description == "Toggle audio mute" &&
			cmd.Envs["dms"] == "dms ipc call audio mute" &&
			cmd.Envs["noctalia"] == "noctalia-shell ipc volume mute"
	})

	if err := os.Remove(filepath.Join(dir, "dms.yaml")); err != nil {
		t.Fatal(err)
	}
	waitForReload(t, reloaded, func(cfg *Config) bool {
		cmd := cfg.Commands["volumeMute"]
		_, dmsGone := cmd.Envs["dms"]
		return !dmsGone && cmd.Envs["noctalia"] == "noctalia-shell ipc volume mute"
	})
}
