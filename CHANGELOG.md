# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- Command modules described in YAML, with nested commands, parameters and templating; invoked as `futi <namespace> <command>`.
- Namespaces and layered discovery: global, local (`./.futi`) and `config.yaml` sources.
- Module cache for global modules with update policies and integrity checks.
- Shell completion for bash, zsh and fish.
- Module management: `modules install`, `update`, `uninstall`, `info`, `list`, `path`, `init`, `add_command`, `remove_command`, `validate`.
- `--dry-run` to print the command that would be executed.
- `--version`.
- `examples/` with sample modules.

### Changed

Breaking relative to development builds:

- `futi <ns> validate` is now `futi modules validate <ns>`.
- Unknown keys in module manifests and in `config.yaml` are an error; keys starting with `x-` are allowed.
- Boolean values accept only `true/false/1/0/yes/no/on/off`.
- An empty value for a required parameter is an error; `required` together with `default` is a schema error.

Other behavior changes:

- `futi a.b` is equivalent to `futi a b`.
- `modules update` without a terminal requires `-y` when module files changed.
- `modules install` without `--local` and `modules init --global` fail on an invalid `config.yaml`.
- `--ref` with a local or archive source is an error; bz2 and xz archives are rejected explicitly.
- The default module name on install is derived from the source path.
- A module's own `help` command receives flags and arguments.
- Usage printed for `-h` goes to stdout.
- All user-facing output is in English.
- Module hashes are recomputed once on the first run after upgrading (lockfile and metadata are refreshed).

### Fixed

- Help and completion see module edits without `cache refresh`.
- Completion works after boolean flags of `modules` subcommands.

### Security

- Commands run without a shell; user input should be passed through `{{quote .param}}` to prevent flag injection.
- Integrity checks for the module cache.
- Confirmation before `attach` runs.
- `install` accepts https sources only and enforces size limits.

### Notes

- The 0.x format of modules and `config.yaml` may change between releases.
- Supported platforms: Linux and macOS (amd64, arm64). Windows: builds, not supported.
