# Roadmap

- [ ] Add semantic-release to project with github actions, make branch rules, update `CONTRIBUTING.md` docs with proper instructions on issues, PRs, branches, commits and etc
- [ ] Review and refactor wrong or bad AI code. This also implies to write proper documentation and make harness for that project in maner so *agents* will write proper code (or less shit code).
- [ ] Make install script for other distros. Simple curl with shell script will be enough for now. Get latest release, download archive or binary and install in in .local/bin user directory.
- [ ] Add `config.yaml` populate function to let `smallctl` create *dummy* config.
- [ ] Add `validate` and/or `explain` command which will show in one format which commands have which envs, their descriptions and arguments. Useful to see that one of your envs is not fully configured. For example you will be able to see that some `command` doesn't have `fallback` value and not configured for current `env`. Good for debuging and writing config
- [ ] Cover code and project with proper `use` documentation. Now it's not quite clear how to use and why to use. `README.md` should have this information at first while documentation can cover how projet works.
- [ ] Add picker feature. Instead of `smallctl invoke volumeMute` you should be able to call `smalctl picker volumeMute` and get list of all available envs to execute. This can be useful for testing purposes.
- [ ] Think about embedding some scripting into it (like `lua`). Don't know yet if it's necessary because you can use shell scripts or just use `lua script.lua` for executable. This only requires to have `lua` installed.
- [ ] Think about plugin system of some kind. This can be useful to not write commans like `noctalia-shell ipc volume increase ${step}` but have some different approach without manual googling for commands.
- [ ] Add pre-checks to exclude non-existing commands on startup/reload. For example run `which noctalia-shell` and if it's not found then all commands with `noctalia-shell` can be excluded. Can be configurable from user side. 
- [ ] Improve parsing, storing parsed data and execution. Current approach sucks because on execute it does a lot of things which could be done on startup/hot-reload.
