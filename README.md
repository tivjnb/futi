English | [Русский](docs/README.ru.md)

# futi

**futi** is a global manager for console utilities. It brings scattered commands, scripts and tools together into modules with a single CLI.

Instead of dozens of scripts with different syntax, you get one YAML file per module and one way to call it:

```bash
futi docker build --tag myapp
futi git commit --message "fix"
futi deploy staging
```

---

## Contents

- [Installation](#installation)
- [Quick start](#quick-start)
- [Module structure](#module-structure)
- [Discovery and namespaces](#discovery-and-namespaces)
- [Parameters and templating](#parameters-and-templating)
- [Module package with scripts](#module-package-with-scripts)
- [CLI commands](#cli-commands)
- [Validation](#validation)
- [Creating modules](#creating-modules)
- [Managing modules](#managing-modules)
- [Module cache](#module-cache)
- [Shell completion](#shell-completion)
- [Security](#security)
- [Examples](#examples)
- [Roadmap](#roadmap)
- [License](#license)

---

## Installation

Supported platforms: Linux and macOS. Windows: builds, not supported.

With the Go toolchain:

```bash
go install github.com/tivjnb/futi/cmd/futi@latest
```

Prebuilt binaries (linux and darwin, amd64 and arm64) are published on the GitHub Releases page together with a `checksums.txt` file. Download the archive for your platform, verify it against `checksums.txt` and put `futi` somewhere on your `PATH`.

Building from source:

```bash
git clone https://github.com/tivjnb/futi futi
cd futi
go build -o futi ./cmd/futi
```

Check the installation:

```bash
futi --version
futi list
# No modules found.
```

futi keeps modules in `~/.config/futi/modules/` (if a `config.yaml` exists, modules are read from the paths listed in it; `modules install` still writes to this directory). To override the directory when there is no config:

```bash
export FUTI_MODULES_DIR=/path/to/modules
```

To enable shell completion (bash, zsh, fish), see [Shell completion](#shell-completion).

---

## Quick start

Create a module template in the current project (it lands in `./.futi/`):

```bash
futi modules init hello --desc "Demo module" --command greet
```

A module from `futi modules init` has stub commands without parameters:

```bash
$ futi hello greet
greet
```

Add parameters to the YAML by hand (see [Module structure](#module-structure)). Replace the contents of `.futi/hello.yaml`:

```yaml
name: hello
description: Demo module
commands:
  greet:
    params:
      - name: name
        required: true
    cmd: "echo hello {{quote .name}}"
```

Run it and look at the list:

```bash
$ futi hello greet --name world
hello world

$ futi list
┌────────┬───────┬─────────────┐
│  TYPE  │ NAME  │ DESCRIPTION │
├────────┼───────┼─────────────┤
│ module │ hello │ Demo module │
└────────┴───────┴─────────────┘
```

To see the assembled command without running it, use `--dry-run`:

```bash
$ futi --dry-run hello greet --name 'big world'
echo hello 'big world'
```

Ready-to-use modules are in [`examples/`](examples/):

```bash
mkdir -p ~/.config/futi/modules
cp -r examples/git.yaml examples/docker.yaml examples/files.yaml examples/backup ~/.config/futi/modules/
futi list
```

---

## Module structure

A module is a YAML file that describes commands.

```yaml
name: docker                    # optional, defaults to the file name
description: Docker management  # optional
cmd: "docker ps"                # optional: default command (when no command is given)

commands:
  # A simple command without parameters
  ps:
    cmd: "docker ps"

  # A command with parameters
  build:
    description: Build Docker image
    params:
      - name: tag
        type: string            # string (default), bool or int
        required: true
      - name: file
        type: string
        default: Dockerfile
    cmd: "docker build -t {{.tag}} -f {{.file}} ."

  # A command with a bool parameter
  logs:
    params:
      - name: container
        type: string
        required: true
      - name: follow
        type: bool
        default: false
    cmd: "docker logs {{.container}} {{if .follow}}-f{{end}}"

  # Nested commands: a command may have commands together with cmd or instead of it.
  # With cmd + commands, cmd is called as a fallback when no subcommand is given.
  migrate:
    description: Database migrations
    cmd: "migrate"              # fallback when no subcommand is given
    commands:
      up:
        cmd: "migrate up"
      down:
        cmd: "migrate down"
```

### Fields

| Field | Type | Description |
|---|---|---|
| `name` | string | Module name (optional; defaults to the file name without `.yaml`, or the directory name for a directory with `config.yaml`; for `futi.yaml` it is `futi`). The value is available as `{{.module_name}}` and `FUTI_MODULE_NAME` in `cmd` and in the `attach`/`detach` hooks |
| `description` | string | Module description (optional) |
| `cmd` | string | Module-level default command (optional) |
| `params` | list | Schema of the default command: parameters (optional) |
| `positional` | block | Schema of the default command: positional arguments (optional) |
| `commands` | map | Module commands |
| `commands.<name>.description` | string | Command description (optional) |
| `commands.<name>.cmd` | string | Command template (if `commands` is present, it is a fallback) |
| `commands.<name>.params` | list | Command parameters (optional) |
| `commands.<name>.positional` | block | Requirements for positional arguments (optional) |
| `commands.<name>.commands` | map | Nested commands (optional) |
| `params[].name` | string | Parameter name (required): letters, digits, `_` and `-`, must not start with a digit or `-`. The names `args`, `args_list`, `env`, `module_name`, `module_root` are reserved and cause a schema error |
| `params[].type` | string | Type: `string` (default), `bool` or `int` |
| `params[].required` | bool | Required parameter (default `false`) |
| `params[].default` | string | Default value |
| `params[].description` | string | Parameter description (optional, shown in help) |
| `positional.min` | int | Required minimum of positional arguments after `--` (default `0`) |

A name with a hyphen, for example `dry-run`, is `--dry-run` on the command line and `{{index . "dry-run"}}` in a template (dot notation `{{.dry-run}}` does not work).

Top-level `params` and `positional` describe the default command (`cmd`): validation, defaults and help work the same way as for regular commands.

```yaml
# meta.yaml — a module as a single command with a schema
name: meta
cmd: "python scripts/meta.py --files {{quote .args}}{{if .json}} --json{{end}}"
params:
  - name: json
    type: bool
    default: "false"
positional:
  min: 1
```

```bash
futi meta --json -- photo.jpg
```

An unknown manifest key (for example, the typo `requird` in a parameter) is a schema error with a line number: `line 9: unknown key "requird" in commands.build.params[0]`. Keys with the `x-` prefix are allowed at any level of the structure and are ignored; they are handy for YAML anchors (`x-common: &c {…}` and `<<: *c`). The exception: a key inside `commands` is a command name, so `x-build` there is an ordinary command.

---

## Discovery and namespaces

futi uses the SaltStack discovery model. A module can be:

**A single YAML file:**

```
~/.config/futi/modules/docker.yaml      → futi docker <command>
~/.config/futi/modules/git.yaml         → futi git <command>
```

**A package: a directory with `config.yaml` and scripts:**

```
~/.config/futi/modules/deploy/
├── config.yaml
└── scripts/
    └── deploy.sh
```

→ `futi deploy <command>`

**Nested namespaces:**

```
~/.config/futi/modules/dev/deploy.yaml          → futi dev deploy <command>
~/.config/futi/modules/dev/deploy/config.yaml   → futi dev deploy <command>
```

The namespace is built from the path: `dev/deploy.yaml` → `dev.deploy`, called as `futi dev deploy <command>` (the last argument is the command, the rest is the namespace).

**Entry naming:** the file name (without `.yaml`) and the directory name are a namespace segment, so they must not be empty, contain a dot, or equal `config`. Entries such as `my.tool.yaml`, `.yaml`, `my.dir/`, `.git/` and `config/` (with their contents) are neither listed nor resolved: `my.tool` would resolve to `my/tool.yaml`.

**Name conflict:** if both `.yaml` and `/config.yaml` exist for one namespace, that is an error. Remove one of the two. `futi list` does not hide such a pair: it shows one entry `x` with the note `conflict: x.yaml and x/config.yaml`, and `futi x` fails with a conflict error.

A module `x.yaml` and a namespace `x/` without `config.yaml` are not a conflict: both entries are listed, `futi x <command>` calls the module, and `futi x <name> <command>` calls modules inside the namespace.

### Symlinks

Symlinks in the modules directory are supported and treated the same way as real files and directories: a symlink to a `.yaml` file, to a package directory (with `config.yaml`) and to a namespace is listed, resolved and executed everywhere.

This is convenient for dotfiles managers (stow, chezmoi) and for development: you can keep a module in a git repo and connect it with a symlink:

```bash
ln -s ~/projects/my-modules/deploy ~/.config/futi/modules/deploy
futi deploy   # a package symlink is listed and executed like a regular one
```

Details:

- Symlink cycles (`a → b → a`) are safe: cycles are cut off by ancestors. A directory (by real path) that is already on the current traversal path is not expanded again. Two symlinks to one directory (`a/x → real`, `b/y → real`) are both expanded.
- A broken symlink named `*.yaml` (target unavailable) is neither listed nor resolved, just like a FIFO or a device with such a name: only a regular file counts as a module. If the target is temporarily unavailable (for example, a volume is not mounted), the cache entry of such a module is removed on cache refresh and restored when the target is available again.
- An entry that cannot be checked because of permissions (for example, a directory without the search permission `x`) is not listed either, but it does not disappear silently: `futi list` prints `warning: …` with the path and the reason, and the cache is not cleared on such a partial listing.
- When `futi modules init` writes through a symlinked directory, it creates the file in the symlink's target directory and shows the **real** (resolved) path in its output.

### Local project modules

Besides the global directory, futi automatically picks up modules from the current project directory:

- `./.futi/` — a modules directory with the same structure (yaml files, packages, namespaces);
- `./futi.yaml` — a single project module (the name comes from the `name` field).

If both exist, `./.futi/` takes precedence. Local modules **shadow global ones** with the same name: handy for overriding behavior for a specific project (your own `deploy`, `test`, `build` in the repo, without editing global modules).

```
myproject/
├── futi.yaml          # the whole project module
├── .futi/             # or a directory of local modules
│   ├── deploy.yaml
│   └── test.yaml
└── src/
```

```bash
cd myproject
futi list              # both local and global modules are visible
futi deploy            # the local .futi/deploy.yaml is called
```

Local modules live in memory only (they do not enter the persistent cache): every run reads them again, so changes are picked up immediately. An invalid `futi.yaml` is an error at startup (early diagnostics).

### Config file: several sources

The file `~/.config/futi/config.yaml` defines module sources with mountpoints:

```yaml
sources:
  - path: ~/.config/futi/modules         # root (default)
  - path: /path/to/project-modules       # contents are mounted at ns1.ns2
    mountpoint: ns1.ns2
  - path: /path/to/another-source        # extends ns1.ns2 from another path
    mountpoint: ns1.ns2
  - path: /path/to/tools                 # wrapped as the namespace "tools" inside "dev"
    mountpoint: dev
    mount_as_namespace: tools
```

Parameters:
- `path` (required) — a directory with modules; `~/…` is a path from the home directory (the home directory itself only in quotes: `"~"`, a bare `~` in YAML is null), a relative path is counted from the directory of `config.yaml`
- `mountpoint` — the namespace to mount the contents into (default: root)
- `mount_as_namespace` — wrap the contents in an extra namespace with this name

Rules for `mountpoint` and `mount_as_namespace`: leading and trailing dots are trimmed (`.dev.` → `dev`); a value made only of dots (`.`, `..`), an empty segment (`a..b`), as well as `/`, `\`, whitespace and control characters in a name are errors when the config is loaded.

An unknown key in `config.yaml` (for example, the typo `mountpont`) is an error with a line number: `line 3: unknown key "mountpont" in sources[0]`. Keys with the `x-` prefix are allowed at any level and are ignored; they are handy for YAML anchors (`x-common: &common {…}`).

Several sources with the same mountpoint are **merged**: modules from all sources are visible in one namespace. If two sources provide a module with the same name, that is a conflict: `futi list` shows the module with the note `conflict: module in several sources: <source directories>`, and `futi x` fails with a conflict error.

A missing or unreadable source directory does not break the others: modules from the other sources work, and `warning: …` with the path and the reason is printed to stderr (with the cache policy `every_time`, on every run). Completion stays silent in this case, and the cache is not cleared on a partial listing. Without `config.yaml`, a missing modules directory (a fresh install) is not an error and produces no warnings.

Empty namespaces are created implicitly: if a source is mounted at `ns1.ns2`, then `ns1` and `ns2` exist as namespaces even without modules of their own.

If the config file is absent, futi uses a single directory `~/.config/futi/modules/` (or `FUTI_MODULES_DIR`).

---

## Parameters and templating

### Passing parameters

```bash
futi docker build --tag myapp --file Dockerfile.dev
futi docker logs --follow
futi docker logs --follow=false
futi docker logs --follow -- mycontainer
```

Formats:
- `--param value` — with a space (for string/int parameters)
- `--param=value` — with `=` (required for values that start with `--`)
- `--bool-flag` — without a value (= `true`)
- `--bool-flag=false` — explicit false

> **Important:** the order is strict — `futi <path…> [flags] [-- args]`. The path (namespace and command) ends at the first token that starts with `-` (a lone `-` is an ordinary word). After that a word is allowed only as the value of the previous string/int flag; a bool flag never consumes a word (`--force false` is an error, write `--force=false`), and an extra word is an `unexpected argument` error. Positional arguments of the command are passed after `--`. Flags before the path (`futi --tag x docker build`) are not supported.
>
> A `string`/`int` value may start with a single `-` (`--n -5`, `--msg -h`); a value that starts with `--` only works via `=` (`--msg=--x`). Short flags (`-v`, `-f`, etc.) are not accepted by regular commands: parameters are passed as `--name`. `-v`/`-r` are only flags of `list`; `--verbose`/`--recurse` on a regular command are ordinary parameters. `-h`/`--help` after a path shows help for that path.

### Templating

futi uses Go `text/template`:

| Syntax | Description |
|---|---|
| `{{.name}}` | Substitutes a parameter value |
| `{{quote .name}}` | Safely inserts the value as **one** argument (escapes quotes, spaces and metacharacters) |
| `{{.args}}` | All arguments after `--` (a string, space-joined) |
| `{{index .args_list 0}}` | The N-th argument after `--` (array access) |
| `{{if .follow}}...{{end}}` | Conditional block (for bool) |
| `{{.env.FOO}}` | Environment variable `FOO` (an error if not set) |
| `{{if index .env "FOO"}}...{{end}}` | Conditional block on the presence of an env var |

Arguments after `--` are passed to the template:

```bash
futi echo_mod echo -- hello world
# → cmd: "echo {{.args}}" → echo hello world

futi echo_mod echo -- hello world
# → cmd: "echo {{index .args_list 0}}" → echo hello
```

The module-level default command is called without an explicit command:

```bash
futi echo_mod -- hello world
# → module.cmd: "echo {{.args}}" → echo hello world
```

> **Note:** the `--` separator for args may be replaced by a hybrid mode in future versions (positional args for leaf commands without an explicit `--`).

Bool parameters accept `true`/`false`, `1`/`0`, `yes`/`no`, `on`/`off` (case-insensitive, surrounding whitespace ignored): falsy values become an empty string, truthy ones become `true`. Any other value, including an empty one (`--flag=`), is an error. A bool that is not passed and has no `default` is false.

### Parameter validation

- `required: true` and the parameter is not passed → error; an empty string/int value (`--tag=`) is an error too (for bool, an explicit `false` is allowed)
- `required: true` together with a non-empty `default` → schema error (`futi modules validate <ns>`)
- The `default` of an int parameter must be an integer, the `default` of a bool must be one of the values above, otherwise it is a schema error
- `default` is applied when the parameter is not passed
- An unknown parameter (not in the schema) → error
- `positional.min` and fewer arguments after `--` than the minimum → error

A `positional` example:

```yaml
commands:
  greet:
    cmd: "echo {{index .args_list 0}}"
    positional:
      min: 1       # requires at least 1 positional argument after --
```

### Important: spaces and quotes in values

If a parameter value contains spaces, use `{{quote}}`:

```yaml
cmd: "echo {{quote .msg}}"   # --msg "hello world" → one argument: hello world
```

`{{quote}}` escapes the value so that it becomes exactly **one** command argument, even if it contains spaces, quotes or metacharacters.

The pattern with manual quotes (`cmd: "echo \"{{.msg}}\""`) works too, but it is unsafe: the value goes through `shlex` parsing again, so a user can "break out" of the quotes and add arbitrary flags:

```bash
futi mod --msg 'x" --force "y'
# cmd: "echo \"{{.msg}}\""  → echo "x" --force "y"  → --force became a separate argument!
# cmd: "echo {{quote .msg}}" → echo 'x" --force "y' → --force stayed part of the value
```

For values in dangerous positions (`git commit -m`, `docker run`, any `--flag {{.param}}`) always use `{{quote}}`.

### Environment variables in templates

Environment variables are available via `{{.env.FOO}}` — useful for secrets (tokens, API keys) that are unsafe to pass via `--param` (they are visible in `ps`):

```yaml
commands:
  deploy:
    params:
      - name: target
        type: string
        required: true
    cmd: "deploy --token {{.env.DEPLOY_TOKEN}} --env {{quote .target}}"
```

```bash
export DEPLOY_TOKEN=secret123
futi deploy --target staging
# → deploy --token secret123 --env staging
```

Referencing a non-existent variable is an error (fail-fast). For an optional check use `index`:

```yaml
cmd: "deploy {{if index .env \"VERBOSE\"}}-v{{end}}"
```

---

## Module package with scripts

A module may contain its own scripts. futi does not set `WorkDir` for the command: it runs in the caller's cwd (`./photo.jpg` is resolved from there). Refer to the module's files explicitly through the `{{quote .module_root}}` template:

```
~/.config/futi/modules/mytool/
├── config.yaml
└── scripts/
    └── deploy.sh
```

```yaml
# config.yaml
name: mytool
commands:
  deploy:
    params:
      - name: target
        type: string
        required: true
    cmd: "{{quote .module_root}}/scripts/deploy.sh {{quote .target}}"
```

```bash
futi mytool deploy --target staging
# runs ~/.config/futi/modules/mytool/scripts/deploy.sh staging
```

Scripts are called with `WorkDir` = the cwd of the futi call. The first token of `cmd` follows ordinary Unix semantics: a relative path is resolved from the cwd, a bare name through `PATH`. The package's own scripts go explicitly through `{{quote .module_root}}/scripts/...` (the quotes escape spaces in the path if the module lives in a directory with spaces).

### `{{.module_root}}` and `{{.module_name}}`

Two variables are available in the `cmd` template: the path and the name of the module. They are used to reach the module's files from `cmd` and hooks:

```yaml
cmd: "{{quote .module_root}}/scripts/{{.name}}.sh"
```

```yaml
# A module with its own venv — the interpreter path starts from the module root:
# ~/.config/futi/modules/images/qr/
# ├── config.yaml
# ├── scripts/make.py
# └── .venv/            # python3 -m venv .venv && .venv/bin/pip install qrcode
commands:
  make:
    cmd: "{{quote .module_root}}/.venv/bin/python {{quote .module_root}}/scripts/make.py --text {{quote .text}}"
```

The variables are also exposed as `FUTI_MODULE_ROOT` and `FUTI_MODULE_NAME` in the child process environment, so scripts in any language can use them directly:

```python
# scripts/run.py — the module path from env
import os
mod_root = os.environ["FUTI_MODULE_ROOT"]
config = os.path.join(mod_root, "config.local.yaml")
```

Referencing `{{.module_root}}`/`{{.module_name}}` with an empty `ModuleRef` gives a missingkey error (`fail-fast`, like `{{.env.X}}` when the env var is absent).

### Caller's cwd and user paths

Relative paths passed by the user (`--out result.png`, files after `--`) are resolved by the OS from the cwd of the `futi` call — natural Unix semantics. A script that needs the cwd can just call `getcwd()` (it equals the cwd of the futi call):

```python
import os
out = os.path.join(os.getcwd(), "result.png")
```

> **Migration note (pre-release):** before the working-directory rework, futi ran commands with `WorkDir` = the module root and passed `FUTI_CWD` in the env. That behavior has been removed. Module fixes:
>
> - `cmd: "./scripts/x.sh ..."` → `cmd: "{{quote .module_root}}/scripts/x.sh ..."`
> - `cmd: ".venv/bin/python scripts/x.py ..."` → `cmd: "{{quote .module_root}}/.venv/bin/python {{quote .module_root}}/scripts/x.py ..."`
> - `base = os.environ.get("FUTI_CWD") or os.getcwd()` → `base = os.getcwd()`
>   (the cwd of the futi call = the script's cwd, `getcwd()` is enough)
> - `os.environ["FUTI_CWD"]` for module paths → `os.environ["FUTI_MODULE_ROOT"]`

---

## CLI commands

```bash
futi <namespace...> <command> [--param val] [-- args]   # call a module command
futi <namespace...> [<command>] -h          # help for a path (module, branch or command)
futi <namespace>                            # default command (if the module has a module-level cmd)
futi --dry-run <path...> <command>          # print the command without running it
futi list                                   # modules and namespaces at the root
futi <namespace...> list                    # contents of a namespace or module commands
futi <namespace...> list -v                 # the same, with a PATH column
futi <namespace...> list -r                 # recursive subtree traversal
futi modules validate <namespace>           # validate a module schema
futi <namespace...> help                    # help for a module/command
futi modules init <name> [flags]            # create a module from a template
futi modules install <source> [flags]       # install a module from a source (local/git/archive)
futi modules uninstall <name> [flags]       # detach + remove (with confirmation)
futi modules update [name] [flags]          # re-fetch from the lockfile source + re-attach
futi modules list                           # installed modules
futi modules info <name>                    # installation details (lockfile/about)
futi modules path <name>                    # directory of an installed module
futi modules add_command <mod> <cmd> [flags]   # add a command to the manifest
futi modules remove_command <mod> <cmd>      # remove a command from the manifest
futi cache refresh                          # explicit module cache refresh
futi cache status                           # cache statistics
futi cache clear                            # remove the persistent cache from disk
futi completion <bash|zsh|fish>             # generate a completion script
futi __complete <args...>                   # hidden: completion hints (internal)
futi -h | --help                            # help
futi --version                              # print the version
```

You cannot remove the only command of a module that has no `cmd`: the module would be empty; `futi modules remove_command` refuses and leaves the manifest unchanged.

Parsing: the path is the words up to the first flag (a token with `-`); the last word of the path is the command, everything before it is the namespace (segments joined with `.`); then flags, then `--`. Arguments after `--` are passed to the command template as `{{.args}}` (a string) and `{{.args_list}}` (an array). Path segments may be written with dots: `futi docker.build` = `futi docker build` (if a module `docker.build` exists, it wins: the longest namespace is tried first).

**Reserved names**: `list`, `cache`, `modules`, `completion`, `__complete` (intercepted by the CLI before module resolution) cannot be used in any segment of a module path — neither in a namespace nor in a name. The check runs in `futi modules init` and `futi modules install` (for `--namespace` and the resulting name); modules that are already installed are not affected. A module command named `list` is a schema error (see the validation errors section). `help` is an exception: if a module defines its own `help` command, it is called.

```bash
futi docker build --tag myapp        # namespace=docker, command=build
futi dev deploy staging              # namespace=dev.deploy, command=staging
futi k8s logs --follow -- mypod      # namespace=k8s, command=logs, follow=true, args="mypod"
futi echo_mod echo -- hello world    # namespace=echo_mod, command=echo, args="hello world"
futi echo_mod -- hello world         # default cmd of the echo_mod module, args="hello world"
```

**Stdin, signals and exit code.** A module command inherits the stdin of futi: pipes (`echo data | futi m c`) and interactive utilities (`ssh`, `psql`, `docker exec -it`) work. Ctrl+C (SIGINT) and Ctrl+\ (SIGQUIT) are delivered by the terminal straight to the command: when futi runs in the foreground of a terminal, it waits for the command to finish and does nothing itself. Without a terminal or not in the foreground (CI, background, no controlling terminal), SIGINT/SIGQUIT sent to futi (`kill -INT <pid>`, an IDE) are forwarded to the command. SIGTERM and SIGHUP received by futi are always forwarded to the command. When a signal is sent to the whole process group (`timeout`, `timeout -s INT`, systemd `KillMode=control-group`, `kill -TERM -<pgid>`), the command receives SIGTERM (and without a terminal, SIGINT/SIGQUIT too) twice: directly and from futi. If futi forwarded SIGTERM, SIGHUP or SIGINT to the command, after the command finishes futi terminates with the same signal, even if the command caught the signal and exited with 0; after a forwarded SIGQUIT the exit code is `128+3`. The futi exit code for a module command equals the command's exit code; if the command was killed by signal N, it is `128+N` (for example, 143 for SIGTERM); when the command dies from SIGINT/SIGTERM/SIGHUP, futi itself terminates with the same signal, so the shell breaks loops on Ctrl+C. The same applies to `futi modules *`: if a hook (`attach`/`detach`) is killed by an interrupting signal, futi terminates with that signal; other errors of `modules` commands give exit code 1. SIGINT and SIGHUP that were ignored when futi started (`nohup`, a shell background command) stay ignored for the command too (for SIGQUIT and SIGTERM the Go runtime installs its own handler, so nothing is inherited).

### Navigation with `list`

`list` shows the contents of the current level of the hierarchy:

```bash
$ futi list                          # root: modules and namespaces
┌───────────┬────────┬───────────────────┐
│   TYPE    │  NAME  │    DESCRIPTION    │
├───────────┼────────┼───────────────────┤
│ module    │ docker │ Docker management │
│ namespace │ images │                   │
└───────────┴────────┴───────────────────┘

$ futi images list                   # namespace: nested namespaces and modules
┌───────────┬───────────┬───────────────┐
│   TYPE    │   NAME    │  DESCRIPTION  │
├───────────┼───────────┼───────────────┤
│ namespace │ converter │               │
│ module    │ png       │ PNG utilities │
└───────────┴───────────┴───────────────┘

$ futi images converter list         # nested namespace
┌────────┬────────┬──────────────────────┐
│  TYPE  │  NAME  │     DESCRIPTION      │
├────────┼────────┼──────────────────────┤
│ module │ to_png │ Convert images to PNG │
└────────┴────────┴──────────────────────┘

$ futi images png list               # module: commands
┌─────────┬─────────────┐
│ COMMAND │ DESCRIPTION │
├─────────┼─────────────┤
│ optimize│ Optimize PNG│
│ resize  │             │
└─────────┴─────────────┘

$ futi docker list                   # module at the root: commands
┌─────────┬─────────────┐
│ COMMAND │ DESCRIPTION │
├─────────┼─────────────┤
│ ps      │             │
│ build   │ Build image │
└─────────┴─────────────┘
```

If a module has a default command (a module-level `cmd`), it is shown first with the `(default)` marker:

```bash
$ futi echo_mod list                 # module with a default cmd
┌───────────┬─────────────┐
│ COMMAND   │ DESCRIPTION │
├───────────┼─────────────┤
│ (default) │             │
│ upper     │             │
└───────────┴─────────────┘
```

The same goes for subcommands: if a command has `cmd` + `commands`, `cmd` is shown as `(default)` (the fallback when called without a subcommand).

Logic: if the namespace resolves to a module, `list` shows its commands. If it resolves to a namespace directory, it shows the direct children (nested namespaces and modules). `list` is reserved: a command named `list` (at any nesting level) in a module is a schema error.

The `-v`/`--verbose` flag adds a `PATH` column — the path to the module manifest. Useful with several module sources or when debugging:

```bash
$ futi list -v
┌───────────┬────────┬───────────────────┬──────────────────────────────────────────┐
│   TYPE    │  NAME  │    DESCRIPTION    │                   PATH                    │
├───────────┼────────┼───────────────────┼──────────────────────────────────────────┤
│ module    │ docker │ Docker management │ /home/user/.config/futi/modules/docker.yaml│
│ namespace │ images │                   │                                          │
└───────────┴────────┴───────────────────┴──────────────────────────────────────────┘
```

The `-r`/`--recurse` flag walks the whole namespace subtree recursively, showing the full namespace path from the context:

```bash
$ futi list -r
┌───────────┬──────────────────────┬──────────────────────┐
│   TYPE    │         NAME         │     DESCRIPTION      │
├───────────┼──────────────────────┼──────────────────────┤
│ module    │ docker               │ Docker management    │
│ namespace │ images               │                      │
│ namespace │ images.converter     │                      │
│ module    │ images.converter.to_png│ Convert images to PNG│
│ module    │ images.png           │ PNG utilities        │
└───────────┴──────────────────────┴──────────────────────┘

$ futi images list -r                # from the images context
┌───────────┬──────────────────┬──────────────────────┐
│   TYPE    │       NAME       │     DESCRIPTION      │
├───────────┼──────────────────┼──────────────────────┤
│ namespace │ converter        │                      │
│ module    │ converter.to_png │ Convert images to PNG│
│ module    │ png              │ PNG utilities        │
└───────────┴──────────────────┴──────────────────────┘
```

`-r` is ignored for modules (commands are shown as usual). Flags can be combined: `futi list -r -v`.

### Help with `help`

`help` is contextual help at any level, without leaving the terminal for the README:

```bash
futi demo help                 # module help
futi demo build help           # command help
futi demo migrate help         # help for a command branch
```

Module help shows the description, the default cmd and the list of commands:

```bash
$ futi demo help
demo — Demo module
default: echo default
commands:
  build    Build the project
  migrate  Database migrations
```

Command help shows USAGE with parameters (type, required, default, description), positional requirements and the final command template:

```bash
$ futi demo build help
demo build — Build the project

USAGE
  futi demo build [--tag value] [--file value] [--push] [-- args]

PARAMETERS
  --tag   (string, required)  Image tag
  --file  (string, default: Dockerfile)  Dockerfile path
  --push  (bool, default: false)

CMD
  docker build -t {{.tag}} -f {{.file}} .
```

`help` is suggested by completion at every level (module/command/subcommand). If a module defines its own `help` command, that one is called; system help does not intercept user-defined names. A user-defined `help` runs as an ordinary command: it receives flags and `-- args`, and its stderr and exit code are passed through.

### `--dry-run`

`--dry-run` is a global flag: it prints the final module command and executes nothing.

For a command with the template `cmd: "docker build -t {{quote .tag}} -f {{quote .file}} ."` (`file` defaults to `Dockerfile`):

```bash
$ futi --dry-run docker build --tag 'my app'
docker build -t 'my app' -f Dockerfile .
```

The output is one line on stdout: the command's argv after template rendering and shlex parsing, with tokens that contain spaces or special characters in single quotes (the line can be pasted into a shell). The output uses bash/zsh syntax (not POSIX sh/dash): the `$'…'` form is used only for tokens with control or non-printable characters (ESC, `\r`, bidi characters, etc.), so a foreign module cannot forge the displayed command. To paste such tokens back into a shell, use a UTF-8 locale: `\u…` inside `$'…'` depends on the locale. Errors (unknown command, missing required parameter, template error) are printed the same way as without `--dry-run`, with the same exit code.

- The flag is recognized only as the first argument of `futi`; after the path `--dry-run` is an ordinary module flag, not a reserved parameter name.
- Hooks (`attach`/`detach`) are not executed and not shown: only the command is printed.
- `-h` / `--help` after the path works as usual (help, exit code 0). A module's user-defined `help` command is shown like any other.
- `list`, `help` (system help), `modules`, `cache`, `completion`, `--version` and `-h` without a path are not module commands: with `--dry-run` they fail with the error `--dry-run applies only to module commands` (exit code 1).
- **Secrets:** the template substitutes `{{.env.X}}` values, and they will appear in the output. Do not publish `--dry-run` output for commands with tokens and passwords in the environment.

---

## Validation

`futi modules validate <ns>` checks a module schema without calling a command — useful when developing a module, in CI, or before publishing.

```bash
$ futi modules validate docker
OK
```

An invalid module prints a report with all errors and warnings in one pass:

```bash
$ futi modules validate bad
ERROR: command "build": param "tag" has invalid type "float" (valid: string, bool, int)
ERROR: command "build": duplicate param "tag"
ERROR: command "empty": must have cmd or commands
modules: validate: module "bad" has 3 error(s)
$ echo $?
1
```

Warnings do not block (exit code 0):

```bash
$ futi modules validate db
WARNING: command "migrate": has subcommands but no cmd (calling without subcommand will fail)
$ echo $?
0
```

### What is checked

**Errors** (block module loading, `futi <ns> <cmd>` fails with a clear message):

- an unknown manifest key (with a line number; such errors are printed first)
- a module with neither `cmd` nor `commands`
- a command with neither `cmd` nor `commands` (a useless command); a `cmd` consisting only of spaces counts as empty
- `params` or `positional.min` without `cmd` — at any level (module, command); parameters are not inherited by subcommands
- a template syntax error in `cmd`, `attach`, `detach` (only parsing is checked, not variable names)
- an invalid command name in `commands`: empty, starts with `-`, contains `.`, whitespace or control characters
- a command named `list` (at any level) is unreachable: `list` is intercepted as a system request
- duplicate `params` names in a command
- a parameter `type` outside `{string, bool, int}` (empty = `string`)
- a reserved parameter name (`args`, `args_list`, `env`, `module_name`, `module_root`) — otherwise it would be shadowed by a template service variable
- a parameter name that does not match `^[A-Za-z_][A-Za-z0-9_-]*$` (including an empty one)

**Warnings** (do not block, shown only in `validate`):

- a command with `commands` but without `cmd` — calling without a subcommand fails at runtime

Validation is recursive over all nested `commands`. The order of errors is deterministic: unknown keys, then `attach`/`detach` and the empty module, then `(default)`, then commands in alphabetical order (a parent before its nested commands). On a regular call (`futi <ns> <cmd>`) validation runs automatically when the module is loaded; `futi modules validate <ns>` is needed to see **all** errors in one pass and the warnings (which are not shown at runtime).

---

## Creating modules

`futi modules init` creates a YAML module from a template, so you do not have to copy it from the README by hand. The module is immediately available through auto-discovery. Path segments contain no dot (it is the namespace separator); if `<path>/config.yaml` already exists, init refuses (otherwise `.yaml` and `/config.yaml` would conflict).

```bash
futi modules init mytool                          # ./.futi/mytool.yaml
futi modules init dev/mytool                      # ./.futi/dev/mytool.yaml (namespace dev)
futi modules init mytool --global                 # ~/.config/futi/modules/mytool.yaml
```

Flags:

| Flag | Description |
|---|---|
| `--desc <text>` | Module description (default `My tool`) |
| `--default-cmd <cmd>` | Module-level default command (the `cmd` field) |
| `--command <name>` | Add a command (can be repeated) |
| `--global` | Create in `~/.config/futi/modules/` instead of `./.futi/` |

A minimal module (no flags):

```bash
$ futi modules init mytool
Created: .futi/mytool.yaml

$ futi mytool hello
hello
```

```yaml
# .futi/mytool.yaml
name: mytool
description: My tool
commands:
  hello:
    cmd: "echo hello"
```

With flags:

```bash
$ futi modules init dev/apitool --desc "API utilities" --default-cmd "echo api" --command fetch --command build
Created: .futi/dev/apitool.yaml

$ futi dev apitool           # default cmd
api

$ futi dev apitool fetch     # a command
fetch
```

```yaml
# .futi/dev/apitool.yaml
name: apitool
description: API utilities
cmd: "echo api"
commands:
  fetch:
    cmd: "echo fetch"
  build:
    cmd: "echo build"
```

If the file already exists, an error is returned (no overwrite). The created module passes schema validation.

---

## Managing modules

Install modules from any source — a local path, a git repository, a web archive:

```bash
# from a local directory
futi modules install ~/dev/my-modules/qr

# from git (branch/tag via --ref)
futi modules install https://github.com/foo/qr-mod.git --ref v1.2.0

# from an archive (.zip / .tar.gz)
futi modules install https://example.com/qr-mod.tar.gz

# into a namespace, and locally into a project
futi modules install ~/dev/qr --namespace images
futi modules install ~/dev/qr --local
```

A single `.yaml` is wrapped into a package on install: a directory `<module>/config.yaml` is created; the name without `.yaml`/`.yml` is taken from `name:` in the manifest or from the source name, it must not contain a dot, and there must be no `<module>.yaml` of the same name next to it. Install flags: `--namespace <ns>`, `--ref <git-ref>` (git sources only, an error for local/archive), `--local` (into `./.futi/` instead of the global directory), `--skip-attach`, `--yes` / `-y` (run the attach hook without asking), `--from-local` / `--from-git` / `--from-archive` (override the source type detection heuristic).

Network sources are `https://` only: `http://` and `git://` addresses are rejected, and a redirect from https to http is forbidden (for both archive and git); for a git repository, download the module manually and install it from a local path, and for an archive, download and unpack it and install the directory (install does not accept a local `.tar.gz`/`.zip`). `.zip` and `.tar.gz`/`.tgz` archives are supported; the format is detected by the file contents, the extension in the URL is taken without query and fragment, and `.tar.bz2`/`.tbz2`/`.tar.xz`/`.txz` are rejected before downloading. An archive download is limited to 100 MiB and 5 minutes. Unpacking is limited to 500 MiB in total and 10000 entries (files and directories); symlinks and other non-regular entries in `.zip` and `.tar.gz` are skipped, and file permissions are reduced to `rwx` (no setuid/setgid/sticky, the owner can always read and write). The source and `--ref` must not start with `-`.

A module can declare setup/teardown hooks — `futi` runs them on install/update and uninstall:

```yaml
attach: ".venv/bin/python -m pip install -r requirements.txt"
detach: "rm -rf .venv"
```

Hooks run in the module directory; `{{.module_name}}`/`FUTI_MODULE_NAME` is the same name as in `cmd` (the `name` field, or without it the module's directory/file name) on install, update and uninstall; `{{.module_name}}`, `{{.module_root}}`, `{{.env.FOO}}`, `{{quote ...}}` and the environment variables FUTI_MODULE_NAME / FUTI_MODULE_ROOT are available. Hooks inherit the stdin of futi and run without a timeout: a hung hook is interrupted with Ctrl+C. Without a controlling terminal (CI, cron, ssh without tty) a hook runs in its own process group: SIGINT, SIGQUIT, SIGTERM and SIGHUP received by futi are forwarded to all hook processes, including those spawned by a compound command (`sh -c`). With a controlling terminal the hook stays in the futi group (interactive hooks work), including when futi runs in the background (`&`) or with redirected input (`</dev/null`): redirection and background do not enable isolation; on a targeted `kill -TERM <futi>`, processes spawned by the hook may stay behind. An isolated hook (no controlling terminal) does not die from a signal sent to the futi group (`kill -9 -<pgid>`, `timeout -s KILL futi …`): its group is separate. A failed `attach` on install rolls the installation back (the module is removed); on update it is only a warning, and the new tree is kept. A failed `detach` on uninstall is a warning (or a refusal with `--strict-detach`).

The `attach` from a downloaded manifest is foreign code, so install and update ask for confirmation before running it (on stderr; the template is shown as is, without substituting values):

```
Module qr runs an attach hook (in /home/user/.config/futi/modules/qr):
  .venv/bin/python -m pip install -r requirements.txt
Run it? [y/N]
```

The question is asked before any changes on disk: on refusal install does not install the module, update keeps the old version (`update` without a name asks about each module; a refusal is an error for that module, the others are updated). If stdin is not a terminal (CI, scripts, a pipe), futi does not ask and fails with an error; if there is no answer (`</dev/null`, `docker run` without `-i`, systemd `StandardInput=null`), that is an error too. In both cases pass `--yes` (`-y`) to run the hook or `--skip-attach` to skip it; `--skip-attach` takes precedence over `--yes`. For update, `--yes` also confirms the replacement of files (see below): without a terminal on stdin, an update with changed files requires `--yes`, including together with `--skip-attach`. A module without `attach` and an update with an unchanged source do not require attach confirmation.

Each installation writes a sidecar `__futi_meta.yaml` next to config.yaml (source, type, ref/commit, tree hash, dates); the following commands rely on it:

```bash
futi modules list              # installed modules
futi modules info qr           # installation details
futi modules update            # re-fetch + re-attach (all modules)
futi modules update qr         # one module
futi modules uninstall qr      # detach + remove (with confirmation)
futi modules path qr           # module directory (for scripts)
```

`modules list` and `update` without a name see modules from all `sources` in `config.yaml` plus the global directory and `./.futi`; `uninstall`/`update`/`info` by name find a module only through discovery (in `sources` and `./.futi`) — a module in the global directory outside `sources` must be added to `sources` or removed by hand. Modules are shown under the name by which discovery finds them, with the mountpoint: `dev.tools.qr`; symlinks to directories are expanded (cycles are cut off, one real directory is shown once, preferably under the path without a symlink); a module whose directory is itself a symlink is skipped by `update` without a name (`Warning: skipped <name>: module directory is a symlink`): replacing the tree would turn the symlink into a copy; a symlinked namespace directory above the module does not interfere. An invalid `config.yaml` is a command error. Installation (`install`, `init --global`) writes only to the global directory (`FUTI_MODULES_DIR` or `~/.config/futi/modules/`) or, with `--local`, to `./.futi`; if the global directory is not among the configured `sources`, install and `init --global` warn `Warning: <dir> is not listed in config.yaml sources; …` (the module will not be found by name until the directory is added to `sources`), and if it is mounted with a prefix, they print `Module is available as <prefix>.<name>`.

`update` compares the hash of the source tree with the lockfile (the hash covers paths, file contents, the execute bit and symlink targets): no changes means a no-op (only `last_check` is updated), and local edits of the installed module are kept. If the source changed, update compares the files on disk with the new version: if they match, only the lockfile is rewritten; otherwise a list of changes is printed to stderr (`D` — will be removed, `M` — will change, `A` — will appear, in this order; at most 50 lines) and the question `Replace module files? [y/N]` is asked, before the `attach` question. Local edits show up as `M`/`D` and are lost after confirmation; on refusal the module is not changed. Without a terminal on stdin, `--yes` (`-y`) is required — pass it in CI and scripts. The directories `.venv`, `node_modules`, `.git`, `__pycache__` are not part of the hash or the comparison and are not preserved when the tree is replaced (the old tree is removed entirely, `attach` recreates them). The tree replacement is atomic, `installed_at` and `about` are preserved. If moving or writing the lockfile fails, update restores the old tree. If a `<module>.futi-backup` is left over from an interrupted update, update refuses and asks you to sort out the backup manually; `list` and `update` do not treat such a directory as a module (update without a name only warns about it on stderr). Update flags: `--skip-attach`, `--yes` / `-y`.

Uninstall requires confirmation (size and number of files); without the sidecar (the module was created by hand or via `modules init`) it works only with `--force`. Uninstall flags: `--yes`, `--force`, `--skip-detach`, `--strict-detach`.

Managing module commands without an editor:

```bash
futi modules add_command qr scan --desc "Scan a QR code"
futi modules remove_command qr scan
```

You cannot remove the only command of a module that has no `cmd`: the module would be empty; `remove_command` refuses and leaves the manifest unchanged.

**Known limitation:** if the futi process crashes in the middle of install/update, a partially copied tree without a sidecar may remain. Rollback works on attach errors and on validation failure, but not on a process crash — such a directory can be removed by hand or with `futi modules uninstall <name> --force`.

---

## Module cache

futi caches loaded modules to avoid reloading within one run and between runs. The persistent cache is stored in `~/.cache/futi/modulecache.gob` (gob format), the in-memory cache lives for the duration of the process.

### Behavior

- On startup the CLI loads the persistent cache from disk (global modules).
- The in-memory cache holds ALL modules (global + local from `./.futi/` or `./futi.yaml`).
- Local modules are not saved to disk — only global ones.
- On Refresh: an `mtime + size` check — only changed files are re-read.
- Modules removed from the source are purged from the cache on the next Refresh.
- Load errors of individual modules are not fatal — the module is skipped and the `skipped` counter is incremented.

### `futi cache refresh`

An explicit cache refresh: walks the source, re-reads changed modules, saves to disk.

```bash
$ futi cache refresh
Cache refreshed: 5 modules, 2 updated

# With an invalid module:
$ futi cache refresh
Cache refreshed: 5 modules, 2 updated, 1 skipped
```

`futi cache` without a subcommand, or with an unknown subcommand, returns a clear error:

```bash
$ futi cache
futi: cache requires a subcommand (try "cache refresh", "cache status", or "cache clear")

$ futi cache foo
futi: unknown cache subcommand "foo" (try "cache refresh", "cache status", or "cache clear")
```

### `futi cache status` / `futi cache clear`

```bash
$ futi cache status
Policy: every_time
Last refresh: 2026-09-14 17:24:54
Modules: 5
Cache file: /home/user/.cache/futi/modulecache.gob

$ futi cache clear
Cache cleared: /home/user/.cache/futi/modulecache.gob
```

`status` shows the active policy, the time of the last refresh, the number of modules in the cache and the file path. `clear` removes the persistent cache — it will be rebuilt from scratch on the next run (useful for odd behavior or after changing the futi version).

### Cache integrity

The cache stores a SHA-256 checksum of each module's manifest. When a module is **executed**, the checksum is compared with the file on disk: if the manifest changed or the cache entry does not match the file, the module is re-read from disk instead of being taken from the cache. This protects against two scenarios: a stale cache (the file changed after Refresh) and a corrupted or substituted gob file. The checksum is also compared in system help (`help`) and in completion. Only `list` works from the cache without the check — speed matters more there.

### Update policies

The automatic refresh strategy at CLI startup is set in `config.yaml`:

```yaml
cache:
  refresh: every_time   # every_time (default) | every_t_min | force_only
  time: "30m"            # only for every_t_min
```

| Policy | Behavior |
|---|---|
| `every_time` | Refresh on every CLI run (default). |
| `every_t_min` | Refresh once per the given `time` interval (at most 365 days; use `force_only` for more). Between runs, use the cache from disk. |
| `force_only` | Refresh only on `futi cache refresh`. After a futi upgrade or rebuild the cache is empty until `futi cache refresh`. |

The `time` format is an extension of `time.ParseDuration` with days via the `d` suffix:

| Example | Meaning |
|---|---|
| `30m` | 30 minutes |
| `2h` | 2 hours |
| `1d` | 1 day (24 hours) |
| `1d4h` | 1 day 4 hours (28 hours) |
| `1d2h30m` | 1 day 2 hours 30 minutes |

The components are summed and the total must be greater than zero (`0d12h` is valid, `0d` is not). Negative values are not supported.

### Fallback

If `~/.cache/` is unavailable (no `HOME` / `XDG_CACHE_HOME`), futi falls back to an in-memory cache with a warning on stderr. The CLI works, but the persistent cache is unavailable until the next run with a correct environment.

If the gob file is corrupted, the cache is automatically rebuilt from an empty state. The same happens on a schema version mismatch (an incompatible cache file format). The cache is also rebuilt on every upgrade or rebuild of the futi binary: the cache file is tied to the binary identity (mtime and size), and a foreign cache is ignored silently. With the `force_only` policy, after a futi upgrade the persistent cache is empty until `futi cache refresh` (modules are read directly from disk meanwhile).

---

## Shell completion

futi supports completion for **bash**, **zsh** and **fish**. Names of modules, namespaces, commands, subcommands and parameters (`--tag`, `--force`) are dynamic, so completion asks futi for hints on the fly through the hidden command `futi __complete <args...>` (a protocol compatible with cobra).

### Setup

```bash
# bash
futi completion bash > /etc/bash_completion.d/futi   # system-wide
source <(futi completion bash)                        # current session

# zsh
futi completion zsh > "${fpath[1]}/_futi"
autoload -U compinit && compinit

# fish
futi completion fish > ~/.config/fish/completions/futi.fish
```

### What is completed

- **Modules and namespaces** — `futi <TAB>`, `futi dev <TAB>`
- **Commands and subcommands** — `futi docker <TAB>`, `futi docker migrate <TAB>`
- **Parameter names** — `futi docker build --<TAB>` → `--tag`, `--force`
- **All parameters on a command** — `futi docker build <TAB>` immediately shows all flags with marks: `--tag (required)`, `--force (default: false)` (the marks are visible in zsh; bash/fish cut them off)
- **After a flag value** — `futi images qr make --out qr.png <TAB>` suggests the remaining flags (`--text`, `--scale`); a flag value does not break the hints. The path (namespace and command) always comes before the first flag, so after flags the path words and `help` are not offered
- **Paths in flag values** — when typing the value of a non-bool flag (`--out ~/<TAB>`, `--out=~/<TAB>`), the shell offers files/directories; in the future only for parameters of type `path` (Roadmap)
- **Parameters of the default command** — a single-command module (`cmd` + `params`, no `commands`) suggests its own flags: `futi meta <TAB>` → `--json`
- **After a bool flag** — `futi docker build --force <TAB>` suggests the remaining (not yet entered) flags (`--tag`); the values `true`/`false` are not offered: a bool flag takes no value, and `false` is given as `--force=false`
- **Global flags** — `futi --<TAB>` → `--dry-run`, `--help`, `--version`; after a leading `--dry-run` the hints are as for a regular call, but without builtins (`cache`, `completion`, `list`, `modules`): `futi --dry-run <TAB>` shows only modules and namespaces, `futi --dry-run dev <TAB>` works as usual
- **Builtins** — `futi cache <TAB>` → `refresh`/`status`/`clear`; `futi completion <TAB>` → `bash`/`zsh`/`fish`
- **Module validation** — `futi modules validate <TAB>` suggests full module names (`dev.deploy`), and no hints after the first argument
- **Builtin priority** — at the root, `cache`, `completion`, `list`, `modules` are suggested as builtins even when a module has the same name (the CLI executes the builtin); `futi cache <TAB>` does not show the commands of such a module
- **The `help` hint** — `help` is offered at every level (module/command/subcommand): `futi demo <TAB>`, `futi demo db <TAB>`, `futi demo greet <TAB>`. If the module already defines a (user-defined) `help` command, that one is offered and the system one is not duplicated.
- **The `list` hint** — `list` is offered where `futi <path> list` works: in a namespace, a module, a branch with subcommands and a single-command module (`futi dev <TAB>`, `futi docker <TAB>`, `futi docker migrate <TAB>`); it is not offered on a leaf command (there `list` is an error)
- **`modules` flags** — `futi modules add_command <module> --<TAB>` → `--cmd`, `--desc`; likewise for `init`, `install`, `uninstall`, `update`

Values of string/numeric parameters (usually user-supplied, for example `--tag myapp`) are not guessed or completed. After `--` (the command's args) futi allows file completion.

zsh prints descriptions of commands/modules next to the hints.

### Cache and completion

`__complete` uses the persistent module cache (like the regular CLI), so it is fast — it does not walk the source on every TAB. So that hints are not empty on a cold cache (the first run) or stale, `__complete` **refreshes** the cache itself if it has never been refreshed or was refreshed more than an hour ago. Otherwise it takes data from the cache without a Refresh.

---

## Security

### Trust model

**A module is code.** Whoever can write to the modules directory (or to the config with sources) decides which commands are executed. This is a deliberate design, like make, npm scripts or shell aliases. futi does not try to sandbox modules from their owner. Consequences:

- Symlinks in the modules directory are allowed (dotfiles managers, dev symlinks): control of the directory already gives full control.
- `futi modules init` writes only inside the modules directory and shows the real (resolved) write path.
- `futi modules install` and `futi modules update` show the `attach` hook of the module being installed and run it only after confirmation (or with `--yes`); without a terminal on stdin and without `--yes` they refuse.
- Network installs accept `https://` only and enforce download and unpack limits (see [Managing modules](#managing-modules)).
- A public registry of downloadable modules (see Roadmap) will require revisiting this model: trust in foreign modules cannot be inherited implicitly.

### Execution without a shell

Commands are executed through `exec` with a ready argv — **without** a shell. The metacharacters `;`, `$()`, `` ` `` in parameters are not executed as shell code (they pass as ordinary text in arguments). If you need a shell, write it explicitly (`sh -c '...'`), passing user input as a positional argument with `{{quote}}` (see the examples).

### `{{quote}}` for user values

Parameter values go through parsing again (shlex) when argv is built. Inserting `{{.param}}` in quotes (`"{{.msg}}"`) can be "broken out of" by a value with a quote character: it escapes the quotes and becomes separate arguments (flag injection). For values in dangerous positions use `{{quote .param}}`, which guarantees exactly one argument (see [spaces and quotes](#important-spaces-and-quotes-in-values)).

### Secrets through the environment

Pass tokens and keys through `{{.env.NAME}}`, not through `--param`: parameters are visible in `ps` and in shell history, the process environment is not.

### Cache integrity

The persistent cache verifies the SHA-256 checksum of the manifest on module execution, in system help and in completion, so a stale or substituted cache will not lead to a foreign command being executed (see [Module cache](#module-cache)).

---

## Examples

Ready-to-use modules (`git`, `docker`, `files`, `backup`) are in the [`examples/`](examples/) directory: copy them into your modules directory or connect them through `sources`; to see the assembled command without running it, use `futi --dry-run <path> …`.

### Echo module (args + default cmd)

```yaml
# ~/.config/futi/modules/echo_mod.yaml
name: echo_mod
description: Echo utilities
cmd: "echo {{.args}}"           # default command (no shell)
commands:
  upper:
    description: Uppercase echo
    # futi executes argv WITHOUT a shell: pipes, $() and loops do not work directly.
    # If you need a shell, write it explicitly (sh -c). Pass user input as a
    # positional argument of sh ($1) with {{quote}}, so that quotes in the value
    # do not break the script or inject code.
    cmd: "sh -c 'echo \"$1\" | tr a-z A-Z' sh {{quote .args}}"
  repeat:
    description: Repeat first arg N times
    params:
      - name: count
        type: int
        required: true
    cmd: "sh -c 'for i in $(seq 1 {{.count}}); do echo \"$1\"; done' sh {{quote (index .args_list 0)}}"
```

```bash
futi echo_mod -- hello world         # echo hello world (default cmd)
futi echo_mod upper -- hello world   # HELLO WORLD (via sh -c | tr)
futi echo_mod repeat --count 3 -- hi # hi\nhi\nhi
```

### Git shortcuts

```yaml
# ~/.config/futi/modules/git.yaml
name: git
description: Git shortcuts
commands:
  amend:
    cmd: "git commit --amend --no-edit"

  fresh:
    description: Reset to remote branch
    params:
      - name: branch
        type: string
        default: main
    cmd: "sh -c 'git fetch origin && git reset --hard origin/\"$1\"' sh {{quote .branch}}"

  squash:
    description: Squash last N commits
    params:
      - name: count
        type: string
        required: true
      - name: message
        type: string
        required: true
    cmd: "sh -c 'git reset --soft HEAD~\"$1\" && git commit -m \"$2\"' sh {{quote .count}} {{quote .message}}"
```

```bash
futi git amend
futi git fresh --branch develop
futi git squash --count 3 --message "feature complete"
```

### Docker wrapper

```yaml
# ~/.config/futi/modules/docker.yaml
name: docker
description: Docker utilities
commands:
  clean:
    description: Remove stopped containers and dangling images
    cmd: "sh -c 'docker container prune -f && docker image prune -f'"

  shell:
    description: Shell into running container
    params:
      - name: container
        type: string
        required: true
    cmd: "docker exec -it {{quote .container}} sh"

  build-all:
    description: Build all images in directory
    cmd: "sh -c 'for d in */; do docker build -t \"${d%/}\" \"$d\"; done'"
```

```bash
futi docker clean
futi docker shell --container myapp
futi docker build-all
```

### Project deploy package

```
~/.config/futi/modules/deploy/
├── config.yaml
└── scripts/
    ├── deploy.sh
    └── rollback.sh
```

```yaml
# config.yaml
name: deploy
description: Deployment utilities
commands:
  staging:
    params:
      - name: version
        type: string
        required: true
    cmd: "./scripts/deploy.sh staging {{.version}}"

  production:
    params:
      - name: version
        type: string
        required: true
    cmd: "./scripts/deploy.sh production {{.version}}"

  rollback:
    params:
      - name: target
        type: string
        required: true
    cmd: "./scripts/rollback.sh {{quote .target}}"
```

```bash
futi deploy staging --version v1.2.3
futi deploy production --version v1.2.3
futi deploy rollback --target production
```

---

## Roadmap

futi is early-stage software (0.x): the module format may change between minor versions. Everything below is a plan, not a promise: none of it is implemented yet.

- **Module schema** — richer parameter types (`file`, `json`, `secret`), `choices`, annotations, and hybrid argument parsing (positional args for leaf commands without an explicit `--`). Path-style completion will then be offered only for `file`/`path` parameters.
- **Server mode** — a remote MCP server over HTTP so that AI agents can call module commands on the machine where futi runs, plus a REST API for CI/CD. Auth, a namespace allowlist, output and time limits and an audit log are part of the plan.
- **Module-provided completion and interactive mode** — modules supply their own completion candidates (container names, branches, etc.); an interactive TUI for browsing and calling modules; completion descriptions in bash/fish; PowerShell support.
- **Ecosystem** — a module registry (`futi modules install <short-name>`, update checks) and module versioning.

---

## License

MIT — see [LICENSE](LICENSE).
