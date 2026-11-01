# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- `futi help <path>`: contextual help at any level of the command tree.
- Inputs of a command: `args` (positional words) and `flags` (`--name value`, `-s` short form via `short`), with `string`, `int` and `bool` types, `required`, `default` and `variadic`.
- A `rest` arg that passes the remaining tokens to the wrapped tool verbatim, including flags and `-h`.
- Protection against values that start with `-` (the program would read them as options), with ways out: `--opt={{.x}}`, a literal `--` in `cmd`, or `allow_dash: true` on an input.
- A path collision (one path addressing two entities, such as a command `deploy` of module `dev` and the module `dev/deploy`) is an error in `modules validate` and a warning in `list`.
- Warnings when a subcommand or a child module shadows a string arg value: in `modules validate`, in the node help and in `modules update`.
- Shell completion follows the new command-line grammar.

### Changed

Breaking relative to 0.1.0:

- `params` and `positional` are replaced by `args` and `flags`. A module that still uses them fails to load with an error that names the replacement. See [Migration](#migration-from-010).
- New `cmd` template: a string (split into words) or a list (one argument per item), evaluated straight into argv without a second parse. The functions `quote`, `.args` and `.args_list` are removed; a value is always exactly one argument.
- New command-line grammar: the command path, then flags, then args and the rest. A `--` makes everything after it args. `-h` after the rest or after `--` is data, not a help request. `--dry-run`, `-h` and `--version` of futi itself are recognized only before the path.
- A trailing `help` or `list` after a path is no longer a system request: use `futi help <path>` and `futi list <path>`. `futi <module> help` calls the module's own command named `help`.
- `attach` and `detach` hooks run without a shell. A compound hook is written explicitly as `[sh, -c, "..."]`; a program given with a path (`scripts/x.sh`) is looked up from the module directory.
- `modules add_command` writes `cmd` as a list (`[echo, <name>]`) by default.

### Fixed

- A cache file written by an older futi no longer prints a `modulecache` warning on the first run after an upgrade: the cache is rebuilt silently.
- A module that fails to load is no longer reported on every call of any command, and cache problems are printed as plain `warning:` lines instead of timestamped log lines.

### Security

- Hooks (`attach`, `detach`) run without a shell, like commands; metacharacters in them are plain text.
- A value is never split or parsed again, so the manual `{{quote .param}}` is no longer needed (and no longer exists).
- Values that would start an argument with `-` are refused unless guarded (see `allow_dash` above), which replaces the old advice to pass input through `quote`.
- On Windows, a command that resolves to a `.bat` or `.cmd` file is refused when a user value contains characters that `cmd.exe` would interpret (`"`, `%`, `!`, `&`, `|`, `<`, `>`, `^`, a line break).

### Migration from 0.1.0

Rewrite modules as follows (details: [Module structure](README.md#module-structure), [Inputs and templating](README.md#inputs-and-templating), [A command that needs a shell](README.md#a-command-that-needs-a-shell)):

- `params` become `flags`; `positional` become `args`.
- `{{quote .x}}` becomes `{{.x}}`.
- `{{.args}}` and `{{.args_list}}` become a `rest` (or `variadic`) arg, used as `{{.name}}`.
- A hook with `&&`, pipes or redirects becomes `[sh, -c, "..."]`.
- Callers: replace `futi <path> help` with `futi help <path>` and `futi <path> list` with `futi list <path>`.
- The cache is rebuilt automatically.

## [0.1.0] - 2026-10-14

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
