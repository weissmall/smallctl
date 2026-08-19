# Configuration

smallctl loads its configuration from a **directory**, not a single file.
The resolved config file (by default `$XDG_CONFIG_HOME/<binary>/config.yaml`)
is the *main* file, and every other `*.yaml` file placed next to it is picked
up and combined with it into one configuration.

This page documents which files are read, how they are interpreted, and how
they are combined. For the config schema itself (options, `envs`, `fallback`,
`${var}` substitution) see the [README](../README.md#configuration) and
[config.example.yaml](../config.example.yaml).

## File roles

Files in the config directory are classified by name and content:

| File | Role |
|---|---|
| the resolved main file (`config.yaml` by default) | **Main config**: `options` plus full-format `commands` (with `description`, `args`, `envs`, `fallback`). Optional — loading continues without it. |
| `*.yaml` with a `commands` section | **Env file**: the file name (without `.yaml`) becomes the environment key for its commands. |
| `*.yaml` without a `commands` section | **Options file**: its `options` section merges into the config. |
| anything else | Ignored. |

Classification rules:

- Only non-hidden files with a `.yaml` extension participate. Dotfiles
  (`.foo.yaml`), other extensions (`.yml`, `.txt`), and subdirectories are
  ignored — you can keep unrelated files in the same directory safely.
- A file counts as an env file when it has a top-level `commands` key whose
  value is a mapping. `commands:` (null) or no `commands` key at all makes it
  an options file.
- `config.yaml` is never treated as an env file. It is only loaded when it is
  the resolved main config path.
- When `--config` / `$SMALLCTL_CONFIG` points at a file, that file becomes
  the main file (whatever its name) and its directory is scanned the same
  way. A `config.yaml` sitting next to it is skipped, since the explicit
  path replaces the default main file.

## Env files and the filename convention

The file name (without `.yaml`) is used as the key under `envs`, so most env
files only need command strings:

```yaml
# ~/.config/smallctl/dms.yaml
commands:
  volumeMute: "dms ipc call audio mute"
  brightnessUp: "dms brightness increment 10"
```

This is equivalent to writing in the main file:

```yaml
commands:
  volumeMute:
    envs:
      dms: "dms ipc call audio mute"
  brightnessUp:
    envs:
      dms: "dms brightness increment 10"
```

Full command objects are also accepted in env files — useful to set
`description`, `args`, or `fallback` alongside the env command, or to define
envs explicitly:

```yaml
# ~/.config/smallctl/dms.yaml
commands:
  volumeMute: "dms ipc call audio mute"
  brightnessUp:
    args:
      step: 10
    envs:
      dms: "dms brightness increment ${step}"
```

The env name is the file name with `.yaml` stripped — case-sensitive, dots
allowed (`my.env.yaml` → env `my.env`). It must match what `env_command` or
`$SMALLCTL_ENV` produces.

## How files are combined

Files are read in a fixed order: the main file first, then options files
alphabetically, then env files alphabetically. Contributions are combined as
follows:

| Field | Rule |
|---|---|
| `options` | Merged field by field across the main and options files. The same field may be set in several files only if the values are **identical**. |
| command presence | A command defined in any file exists in the result. |
| `description` | Set by whichever file defines it last (main first, then env files alphabetically). |
| `args` | Merged key by key; later files win per key. |
| `envs` | **Union** of all files. A duplicate command+env pair is an error. |
| `fallback` | Replaced entirely by the later file's list, if it sets one. |

A `commands` string value in an env file is shorthand for exactly one entry:
`envs: {<file name>: <string>}`.

## Conflicts

Loading fails with an error naming both files when:

- the same command+env pair is defined twice — e.g. `envs.dms` in
  `config.yaml` *and* the shorthand in `dms.yaml`, or explicit envs crossing
  files (`dms.yaml` defining `envs.noctalia` while `noctalia.yaml` also
  defines it);
- an `options` field is set to different values by two files;
- an env file contains an `options` section (options belong in the main file
  or in files without a `commands` section);
- any file is invalid YAML or fails validation.

When a conflict or error happens during hot-reload, the previous config stays
active and the error is logged.

## Hot reload

The watcher monitors the whole config directory. Creating, editing, renaming,
or removing any participating `*.yaml` file reloads the full configuration
(all files are re-read and recombined) without a restart. Removing a file
takes effect immediately: its commands/envs disappear from the running config.

The `notify` level is re-applied on every reload. Logging options
(`log_level`, `log_file`) are read at startup; restart the server to change
them. `log_file` supports `~` and `$VAR` expansion.

## Complete example

```
~/.config/smallctl/
├── config.yaml       # main: options + shared command definitions
├── options.yaml      # optional: options only
├── dms.yaml          # env "dms"
└── noctalia.yaml     # env "noctalia"
```

```yaml
# config.yaml
commands:
  volumeMute:
    description: "Toggle audio mute"
```

```yaml
# dms.yaml
commands:
  volumeMute: "dms ipc call audio mute"
```

```yaml
# noctalia.yaml
commands:
  volumeMute: "noctalia-shell ipc volume mute"
```

Combined, this is exactly:

```yaml
commands:
  volumeMute:
    description: "Toggle audio mute"
    envs:
      dms: "dms ipc call audio mute"
      noctalia: "noctalia-shell ipc volume mute"
```

A single `config.yaml` with everything in it keeps working exactly as
before — multi-file setup is entirely optional.
