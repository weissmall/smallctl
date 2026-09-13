package webconfig

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"smallctl/internal/config"
)

func TestCreateCommandKeepsConfigurationInMemory(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	app := New(configPath)

	form := url.Values{
		"name":                {"volumeUp"},
		"description":         {"Increase volume"},
		"args_key":            {"step", "mode"},
		"args_value":          {"5", "default"},
		"environment_name":    {"laptop"},
		"environment_command": {"pactl set-sink-volume @DEFAULT_SINK@ +${step}%"},
		"fallback_command":    {"amixer sset Master ${step}%+"},
	}
	req := httptest.NewRequest(http.MethodPost, "/commands", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, req)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "volumeUp") {
		t.Fatalf("response does not contain new command: %s", response.Body.String())
	}

	cfg, err := app.load()
	if err != nil {
		t.Fatalf("loading in-memory draft: %v", err)
	}
	command, ok := cfg.Commands["volumeUp"]
	if !ok {
		t.Fatal("saved config is missing volumeUp")
	}
	if command.Args["step"] != "5" || command.Args["mode"] != "default" || command.Envs["laptop"] == "" || len(command.Fallback) != 1 {
		t.Fatalf("unexpected saved command: %#v", command)
	}
	if config.FileExists(configPath) {
		t.Fatal("preview save wrote a configuration file")
	}
}

func TestPreparedEnvironmentIsSaved(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	app := New(configPath)
	form := url.Values{"environment": {"dms"}}
	req := httptest.NewRequest(http.MethodPost, "/environments", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, req)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	cfg, err := app.load()
	if err != nil {
		t.Fatalf("loading saved config: %v", err)
	}
	if len(cfg.Options.Environments) != 1 || cfg.Options.Environments[0] != "dms" {
		t.Fatalf("prepared environments = %#v, want [dms]", cfg.Options.Environments)
	}
}

func TestAllowsPreviewEditsForSplitConfiguration(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(filepath.Join(dir, "laptop.yaml"), []byte("commands:\n  volumeUp: pactl up\n"), 0o600); err != nil {
		t.Fatalf("creating env config: %v", err)
	}
	app := New(configPath)
	req := httptest.NewRequest(http.MethodPost, "/options", strings.NewReader("shell=bash&notify=off"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "No configuration files were changed") {
		t.Fatalf("expected in-memory preview explanation, got: %s", response.Body.String())
	}
}

func TestShowsActiveEnvironment(t *testing.T) {
	t.Setenv("SMALLCTL_ENV", "laptop")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("commands:\n  volumeUp:\n    envs:\n      laptop: pactl up\n"), 0o600); err != nil {
		t.Fatalf("creating config: %v", err)
	}
	app := New(configPath)
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, text := range []string{"Active environment", "laptop", "active"} {
		if !strings.Contains(response.Body.String(), text) {
			t.Fatalf("response is missing %q: %s", text, response.Body.String())
		}
	}
}

func TestEnvironmentCommandTestReportsOutput(t *testing.T) {
	app := New(filepath.Join(t.TempDir(), "config.yaml"))
	form := url.Values{"shell": {"sh"}, "env_command": {"printf dms"}}
	req := httptest.NewRequest(http.MethodPost, "/environment-test", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, text := range []string{"Environment command test", "dms"} {
		if !strings.Contains(response.Body.String(), text) {
			t.Fatalf("response is missing %q: %s", text, response.Body.String())
		}
	}
}
