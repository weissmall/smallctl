# smallctl

Decouple your compositor keybinds from the tools they invoke.

`smallctl` is a micro-server that reads a YAML configuration of named
commands, listens on a Unix domain socket, and executes shell commands
via `bash -c`. A companion CLI subcommand sends invocations and prints
responses.

**Why?** Change your screenshot tool or terminal emulator once in a config file
instead of hunting through your window manager keybind configuration.

## Features

- **Single binary** — `smallctl serve` and `smallctl invoke`
- **YAML config** — defines commands with args, env-specific variants, and fallback chains; split across multiple files per environment if you like
- **Hot reload** — edit any config file; the server picks up changes without restart
- **Environment awareness** — run different commands per machine (desktop vs. laptop)
- **Fallback chains** — if one tool isn't available, try the next
- **Desktop notifications** — optional notify on errors or all invocations
- **Ping/pong health check** — detect if the server is running
- **Configurable logging** — levels 0-5, stdout and/or file

## Installation

```bash
git clone https://github.com/weissmall/smallctl.git
cd smallctl
go build -o ~/.local/bin/smallctl .
```

Or with Nix:

```bash
nix build github:weissmall/smallctl
```

Requires Go 1.26+.

## Quick Start

1. Create a config file:

```bash
mkdir -p ~/.config/smallctl
cp config.example.yaml ~/.config/smallctl/config.yaml
# Edit to match your tools
```

2. Start the server:

```bash
smallctl serve
```

3. Invoke a command:

```bash
smallctl invoke brightnessIncrease
smallctl invoke brightnessIncrease --args step=20
```

## Usage

### Server

```
smallctl serve [--config <path>]
```

- `--config`: path to config.yaml (default: `$XDG_CONFIG_HOME/<binary>/config.yaml`)
- Listens on `$XDG_RUNTIME_DIR/<binary>.sock`
- Graceful shutdown on SIGINT/SIGTERM (Ctrl+C)

### Client

```
smallctl invoke <command> [--args key=value,...] [--no-wait]
```

- `<command>`: name of the command defined in config
- `--args`: comma-separated key=value overrides for command arguments
- `--no-wait`: fire-and-forget — don't wait for the command to finish

`smallctl invoke` exits with the exit code of the command that ultimately ran (fallbacks included). For `--no-wait` the CLI exits `0` once the request is queued.

### Examples

```bash
# Basic invocation
smallctl invoke lockScreen

# Override default args
smallctl invoke brightnessIncrease --args step=15

# Fire and forget
smallctl invoke screenshot --args mode=full --no-wait

# Point to an alternate config without touching the default
SMALLCTL_CONFIG=~/dotfiles/smallctl.yaml smallctl serve
```

## Configuration

### Finding the config file

`smallctl serve` looks for its YAML file in the following order:

1. `--config <path>` flag (highest priority)
2. `$SMALLCTL_CONFIG` environment variable
3. `$XDG_CONFIG_HOME/<binary>/config.yaml`
4. Fallback to `$XDG_CONFIG_HOME/smallctl/config.yaml` (handy when running `go run . serve` whose binary name is `main`)

The resolved file is the **main config**; every other `*.yaml` file placed in
the same directory is loaded and combined with it. See
[Multiple config files](#multiple-config-files) below and
[docs/CONFIGURATION.md](./docs/CONFIGURATION.md) for the full rules.

### Multiple config files

Instead of keeping everything in `config.yaml`, you can split the config per
environment. A file named `dms.yaml` with a `commands` section is an **env
file**: its file name becomes the key under `envs`, so commands can be written
as plain strings:

```
~/.config/smallctl/
├── config.yaml       # main: options + shared definitions (description, args, fallback)
├── dms.yaml          # env "dms"
└── noctalia.yaml     # env "noctalia"
```

```yaml
# config.yaml                  # dms.yaml
commands:                      commands:
  volumeMute:                    volumeMute: "dms ipc call audio mute"
    description: "Toggle audio mute"
```

```yaml
# noctalia.yaml
commands:
  volumeMute: "noctalia-shell ipc volume mute"
```

The three files combine into:

```yaml
commands:
  volumeMute:
    description: "Toggle audio mute"
    envs:
      dms: "dms ipc call audio mute"
      noctalia: "noctalia-shell ipc volume mute"
```

Rules in brief:

- `*.yaml` files with a `commands` section are env files (file name = env key;
  full command objects are also accepted).
- `*.yaml` files without a `commands` section merge their `options`.
- Hidden files, other extensions, and subdirectories are ignored.
- All `envs` maps are union-merged; defining the same command+env twice is an
  error naming both files.
- Any change to any of these files triggers a hot reload.

See [docs/CONFIGURATION.md](./docs/CONFIGURATION.md) for details.

### Schema

```yaml
options:
  env_command: "hostname"     # optional: command whose output sets env name
  log_level: 3                # 0-5 (default: 3; 0 = quiet, 5 = verbose); read at startup
  log_file: ""                # empty = no file (default: $XDG_DATA_HOME/<binary>/log); ~ and $VAR are expanded; read at startup
  notify: "off"               # off | error | all
  timeout: 30                 # seconds, 0 = no limit (no timeout)
  shell: "bash"               # shell executable for -c execution

commands:
  <name>:
    description: "..."        # optional human-readable description
    args:                     # optional default values for ${var} substitution
      key: value
    envs:                     # optional env-specific commands
      <env_name>: "shell command with ${args}"
    fallback:                 # optional fallback chain (tried in order)
      - "command 1"
      - "command 2"
```

### Variable Substitution

```
${varname} → resolves to --args override > command.args default > ""
$$         → literal $
```

### Environment Resolution

1. `options.env_command` is executed; trimmed stdout becomes the environment name.
2. If the command is unset or fails, `$SMALLCTL_ENV` (if present) is used.
3. If neither is set the environment is empty and `fallback` commands are invoked directly.

### Hot reload

All config files are re-read on change (see [Multiple config files](#multiple-config-files)). The `notify` level is re-applied on reload. Logging options (`log_level`, `log_file`) are read at startup — restart the server to change them.

`log_file` (and `$SMALLCTL_LOG_FILE`) expand `~` and environment variables, e.g. `$HOME/.config/smallctl/log`.

### Execution Model

```
invoke cmd ──► Server looks up cmd in config
                   │
            ┌──────┴──────┐
            ▼              ▼
       env match?     no env match
            │              │
            ▼              │
     run env command       │
       │       │           │
       ▼       ▼           │
      ok     fail ─────────┤
       │                   ▼
       ▼            walk fallback[]
    return ok           │     │
                        ▼     ▼
                       ok   fail ──► return all errors
```

## Environment Variables

| Variable | Values | Default | Purpose |
|----------|--------|---------|---------|
| `SMALLCTL_CONFIG` | file path | *(unset)* | Override path to `config.yaml` without using `--config`. |
| `SMALLCTL_ENV` | string | `""` | Static environment name when `env_command` is unset or fails. |
| `SMALLCTL_LOG_LEVEL` | `0`–`5` | `3` | Log verbosity (`0` = quiet, `3` = info, `5` = verbose/debug). |
| `SMALLCTL_LOG_FILE` | path | `$XDG_DATA_HOME/<binary>/log` | Log file destination (empty disables file logging). |
| `SMALLCTL_NOTIFY` | `off`, `error`, `all` | `off` | Desktop notification level (`error`/`all` require `notify-send`, `gdbus`, or `dbus-send`). |

## Notifications

Desktop notifications are sent via the first available transport:

1. `notify-send` (libnotify)
2. `gdbus` (GLib D-Bus)
3. `dbus-send` (D-Bus CLI)

If none are found, `smallctl` logs a warning and continues without notifications.

## Health & diagnostics

- Starting `smallctl serve` while another instance is running triggers an internal ping and prints `smallctl server already running (PID …)` if the existing server responds.
- You can issue a manual ping with:
  ```bash
  printf '{"type":"ping"}\n' | socat - UNIX-CONNECT:"$XDG_RUNTIME_DIR/smallctl.sock"
  ```
- Adjust log verbosity at runtime with `SMALLCTL_LOG_LEVEL=5` (verbose) or suppress logs entirely with `SMALLCTL_LOG_LEVEL=0`.

## Integrating with niri

In your `~/.config/niri/config.kdl`, replace hardcoded tool paths with
`smallctl invoke`:

```kdl
// Instead of:
// binds { XF86AudioRaiseVolume.spawn "pactl set-sink-volume @DEFAULT_SINK@ +5%"; }

// Use:
binds { XF86AudioRaiseVolume.spawn "smallctl invoke volumeUp"; }
binds { XF86AudioLowerVolume.spawn "smallctl invoke volumeDown"; }
binds { XF86MonBrightnessUp.spawn "smallctl invoke brightnessIncrease"; }
binds { XF86MonBrightnessDown.spawn "smallctl invoke brightnessDecrease"; }
```

Now changing your volume backend or screenshot tool is a one-line config edit
instead of updating every keybind.

## License

MIT