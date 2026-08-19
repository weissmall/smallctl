# smallctl — Q&A

A quick rundown of what this project is and how it's built.
For usage details see the [README](../README.md).

## What is smallctl?

A micro-server that decouples compositor keybinds from the tools they invoke.
It reads a YAML config of named shell commands, listens on a Unix domain
socket, and runs commands via `bash -c`. A companion CLI (`smallctl invoke`)
sends requests to the server and prints the result.

**Why?** So changing your screenshot tool or volume backend is a one-line
config edit instead of rewriting every WM keybind.

## How is the codebase laid out?

Single Go binary (`main.go`) plus focused packages under `internal/`:

| Package | Responsibility |
|---|---|
| `internal/config` | Multi-file YAML loading (main config + per-env files + options files), validation, `${var}` substitution, fsnotify hot-reload watcher, XDG path resolution |
| `internal/server` | Unix socket listener; accepts newline-delimited JSON requests, holds the current config behind a mutex |
| `internal/executor` | Resolves env-specific commands and fallback chains, runs them with the configured shell and timeout |
| `internal/protocol` | Request/Response types (`command` and `ping`) |
| `internal/notify` | Desktop notifications via `notify-send` / `gdbus` / `dbus-send` (first available wins) |
| `internal/logging` | slog-based setup with levels 0–5, stdout and/or file |
| `internal/ports` | `ExecutionEngine` and `NotificationDispatcher` interfaces — the server depends on these, not concrete implementations |
| `internal/general` | Shared constants (log levels, etc.) |

## How does a keybind press flow through the system?

```
smallctl invoke <name>
  → dial unix socket ($XDG_RUNTIME_DIR/smallctl.sock)
  → server looks up <name> in the hot-reloaded config
  → executor substitutes ${args}, picks the env-specific variant or walks the fallback chain
  → command runs via <shell> -c with optional timeout
  → JSON response (stdout/stderr/exit code) returned to the CLI, which exits with the command's code
```

## What are the key design decisions?

- **Interfaces in `internal/ports`** keep the server decoupled from execution
  and notification mechanics, which simplifies testing with fakes.
- **Hot reload via fsnotify**: editing any `*.yaml` file in the config
  directory re-reads and recombines them all, swapping the server's config
  atomically (`UpdateConfig`) with no restart; env is re-resolved on reload.
  See [docs/CONFIGURATION.md](./CONFIGURATION.md) for the multi-file rules.
- **Env resolution order**: `options.env_command` output → `$SMALLCTL_ENV` →
  empty (fallback commands run directly).
- **Single-instance guard**: a PID lock file plus an internal `ping` request;
  stale locks/sockets are detected and cleaned up on startup.
- **Graceful shutdown** on SIGINT/SIGTERM; `--no-wait` invocations are
  fire-and-forget.

## What dependencies does it use?

Deliberately minimal (Go 1.26+):

- `gopkg.in/yaml.v3` — config parsing
- `github.com/fsnotify/fsnotify` — config watching
- `golang.org/x/sys` — (indirect)

Everything else (logging via `log/slog`, JSON, Unix sockets) is stdlib.

## How is it tested?

Unit tests live next to the code (`*_test.go`) in `internal/config`,
`internal/server`, `internal/executor`, and `internal/notify`. Run with:

```bash
go test ./...
```

## How do I build and run it?

```bash
go build            # produces ./smallctl
./smallctl serve    # start the server
./smallctl invoke volumeUp --args step=5
```

There's also a `flake.nix` for Nix-based development shells.
