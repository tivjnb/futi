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
- [Command annotations](#command-annotations)
- [Discovery and namespaces](#discovery-and-namespaces)
- [Inputs and templating](#inputs-and-templating)
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

A module from `futi modules init` has stub commands without inputs:

```bash
$ futi hello greet
greet
```

Add inputs to the YAML by hand (see [Module structure](#module-structure)). Replace the contents of `.futi/hello.yaml`:

```yaml
name: hello
description: Demo module
commands:
  greet:
    args:
      - name: name
        required: true
    cmd: "echo hello {{.name}}"
```

Run it and look at the list:

```bash
$ futi hello greet world
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
$ futi --dry-run hello greet 'big world'
echo hello 'big world'
```

Ready-to-use modules are in [`examples/`](examples/); the `cp` commands below work from a clone of the repository (release archives do not include `examples/`):

```bash
mkdir -p ~/.config/futi/modules
cp -r examples/git.yaml examples/docker.yaml examples/files.yaml examples/backup ~/.config/futi/modules/
futi list
```

---

## Module structure

A module is a YAML file that describes commands. A command takes **args** (positional words, no `--` needed) and **flags** (`--name value`):

```yaml
name: docker                    # optional, defaults to the file name
description: Docker management  # optional
cmd: "docker ps"                # optional: default command (when no command is given)

commands:
  # A simple command without inputs
  ps:
    cmd: "docker ps"

  # A command with an arg and flags
  build:
    description: Build Docker image
    args:
      - name: context
        default: "."
        description: Build context directory
    flags:
      - name: tag
        short: t                # -t
        required: true
      - name: file
        default: Dockerfile
    cmd: "docker build -t {{.tag}} -f {{.file}} {{.context}}"

  # A required arg and a bool flag
  logs:
    args:
      - name: container
        required: true
    flags:
      - name: follow
        type: bool
        short: f
    cmd: "docker logs {{if .follow}}-f{{end}} {{.container}}"

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

```bash
futi docker build --tag myapp          # docker build -t myapp -f Dockerfile .
futi docker build ./ctx -t myapp       # args and flags in any order
futi docker logs -f web                # docker logs -f web
```

### Fields

A node (the module or a command) has these fields:

| Field | Type | Description |
|---|---|---|
| `name` | string | Module only. Module name (optional; defaults to the file name without `.yaml`, or the directory name for a directory with `config.yaml`; for `futi.yaml` it is `futi`). The value is available as `{{.module_name}}` and `FUTI_MODULE_NAME` in `cmd` and in the `attach`/`detach` hooks |
| `description` | string | Description (optional, shown in `list` and help) |
| `cmd` | string or list | Command template (see [Templating](#templating)). On the module it is the default command; on a command with `commands` it is a fallback |
| `args` | list | Positional inputs |
| `flags` | list | Inputs passed as `--name` |
| `commands` | map | Nested commands (a node may have `cmd`, `args` and `commands` at the same time) |
| `readonly`, `destructive`, `idempotent` | bool | Annotations of a command with its own `cmd` (see [Command annotations](#command-annotations)) |
| `expose` | list | Where the node is available: `cli`, `mcp`, `http`; inherited by the subcommands (see [Command annotations](#command-annotations)) |
| `attach`, `detach` | string or list | Module only. Install/uninstall hooks (see [Managing modules](#managing-modules)) |

`args` and `flags` of a node belong to its own `cmd` only: they are not inherited by subcommands. The module-level `args` and `flags` describe the module's default command.

An input (an item of `args` or `flags`) is an object with a `name`; there is no shorthand:

| Field | Where | Default | Description |
|---|---|---|---|
| `name` | both | — | Required. `[a-z][a-z0-9_]*`, unique among the args and flags of the node. On the command line `_` becomes `-` (`ignore_case` is `--ignore-case`); in a template it is `{{.ignore_case}}`. The names `help`, `env`, `module_root`, `module_name` are reserved |
| `type` | both | `string` | `string`, `int`, `bool`, `file` or `dir`; `bool` only for flags. `file` and `dir` are strings that hint at a path (completion, help): futi never checks that the path exists or what it points to, and does not change it |
| `required` | both, not bool | `false` | The input must be given. Together with any `default` (even an empty one) it is a schema error. For variadic/rest — at least one value |
| `default` | both, not variadic/rest | — | A string; for `int` an integer, for `bool` one of `true/false/1/0/yes/no/on/off` |
| `choices` | both, `string`/`int` only | — | A non-empty list of allowed values (see [Choices](#choices)). Not for `bool`, `file`, `dir` or `rest` |
| `description` | both | — | Shown in help and completion |
| `short` | flags | — | One Latin letter, case matters (`-f` is not `-F`), unique in the node; `h` is reserved. Allowed for variadic flags |
| `variadic` | both | `false` | Args: several words in a row. Flags: the flag may be repeated and the values are collected. Not for `bool` |
| `rest` | args | `false` | The last arg only: takes all the remaining tokens verbatim (see [Rest](#rest-the-remaining-tokens)) |
| `allow_dash` | both, not bool | `false` | Lifts the [protection against values starting with `-`](#values-starting-with--) |

The args of a command have one of three forms: required then optional (`req* opt*`), required then one `variadic` then required (`req* variadic req*`), or required then one `rest` (`req* rest`). Only required args may stand before `variadic`/`rest`; an optional arg may follow required ones and may have a `default` (`context` above defaults to `.`).

#### Choices

`choices` restricts an input to a fixed set of values; it is allowed for `string` and `int` inputs (including variadic ones). The list must not be empty; for `int` every element must be an integer; duplicates (`1` and `01` count as the same integer) and control characters are a schema error. A `default` must be one of the choices.

```yaml
flags:
  - name: level
    choices: [debug, info, warn]
    default: info
args:
  - name: env
    choices: [dev, prod]
```

A value outside the list is an error; the comparison is exact and case-sensitive, and every element of a variadic input is checked:

```
ERROR: invalid value "Info" for --level (valid: debug, info, warn)
```

A choice that starts with `-` on an input under the [dash protection](#values-starting-with--) is a schema error: use `--opt={{.x}}`, a literal `--` in `cmd`, or `allow_dash: true`.

```yaml
# meta.yaml — a module as a single command with a schema
name: meta
cmd: "python3 scripts/meta.py {{if .json}}--json{{end}} --files {{.files}}"
args:
  - name: files
    variadic: true
    required: true
flags:
  - name: json
    type: bool
```

```bash
futi meta --json photo.jpg scan.png
```

An unknown manifest key (for example, the typo `requird` in an input) is a schema error with a line number: `line 9: unknown key "requird" in commands.build.flags[0]`. Keys with the `x-` prefix are allowed at any level of the structure and are ignored; they are handy for YAML anchors (`x-common: &c {…}` and `<<: *c`). The exception: a key inside `commands` is a command name, so `x-build` there is an ordinary command.

> **Old format.** `params`, `positional` and `{{.args}}`, `{{.args_list}}`, `{{quote …}}` no longer exist. A module that uses them fails to load with an error that points to the replacement (for example `"params" was replaced by "args"/"flags" (see README)`). Move parameters to `flags` (or to `args` if they were positional), replace `{{quote .x}}` with `{{.x}}`, and replace `{{.args}}` with a `rest` or `variadic` arg.

---

## Command annotations

An author can describe what a command does to the world and where it may be called from. The keys are optional and sit next to `cmd` in the module or in any command:

```yaml
# ~/.config/futi/modules/db.yaml
name: db
description: Database tools
commands:
  status:
    description: Show the server status
    readonly: true
    idempotent: true
    cmd: "pg_isready"

  drop:
    description: Drop a database
    destructive: true
    args:
      - name: name
        required: true
    cmd: ["dropdb", "{{.name}}"]

  maintenance:
    description: Maintenance tasks
    expose: [cli, mcp]          # inherited by the subcommands
    idempotent: true
    cmd: "vacuumdb --all"       # fallback when no subcommand is given
    commands:
      analyze:
        cmd: "vacuumdb --all --analyze-only"
      reindex:
        expose: [mcp]           # narrower than the parent: not available in the CLI
        cmd: "reindexdb --all"
```

| Key | Where | Meaning |
|---|---|---|
| `readonly` | a node with its own `cmd` | The command changes nothing |
| `destructive` | a node with its own `cmd` | The command deletes or overwrites data; futi asks for confirmation |
| `idempotent` | a node with its own `cmd` | A repeated call has no further effect |
| `expose` | any node | A list of frontends where the node is available: `cli`, `mcp`, `http`. Inherited; a subcommand may only narrow it |

`readonly`, `destructive` and `idempotent` have three states: `true`, `false` and unset (not stated). They are allowed only on a node that has its own `cmd`; on a pure group they are a schema error (`readonly applies only to a command with cmd`). `readonly: true` together with `destructive: true` is a schema error (`readonly and destructive are mutually exclusive`). Only `destructive: true` changes what the CLI does; the other keys are shown in `help` and are meant for tools that call futi.

`expose` defaults to all three frontends. A node without its own `expose` takes the list of its parent (the module root is exposed everywhere), and a subcommand may name only frontends that its parent has: `expose widens the parent: "mcp" is not exposed by "<parent>"`. An empty list, an unknown or a repeated value is a schema error. Inheritance works inside one module file; a namespace directory has no annotations. `mcp` and `http` are groundwork for a future server mode: today only `cli` has an effect.

A command without `cli` in its `expose` is hidden from `futi list`, from the help of its group and from completion, and a direct call, `help`, `-h` or `list` of it fails (exit code 1):

```
command "futi db maintenance reindex" is not available in the CLI (expose: mcp)
```

A hidden command still occupies its word: a path collision with it is an error, and `futi modules validate` checks it as usual.

### Confirmation of destructive commands

A command with `destructive: true` does not run until it is confirmed:

```bash
$ futi db drop main
futi db drop is destructive and will run:
  dropdb main
Run it? [y/N]
```

- **A terminal on stdin.** futi asks on stderr. `y` or `yes` (any case, spaces around are ignored) confirms; anything else and end of input give `futi: aborted` and exit code 1, the command is not started. The answer is read up to the end of the line and no further, so the rest of stdin goes to the command.
- **No terminal on stdin** (a pipe, CI, a file). futi refuses without reading stdin, because stdin belongs to the command (`futi db import < dump.sql`): `futi: "futi db drop" is destructive and needs confirmation (no terminal on stdin): rerun as "futi --yes db drop ..."`. Exit code 1.
- **`--yes` / `-y`** skips the question and runs the command. For a command that is not destructive the flag does nothing.
- **`--dry-run`** prints the command and never asks.

```bash
futi --yes db drop main        # run without asking
futi -y --dry-run db drop main # the flags go in any order; prints: dropdb main
```

`--yes` is recognized only before the path, like `--dry-run` (see [`--yes`](#--yes)).

`futi help db drop` shows the annotations that are set to `true` and, when the list differs from "everywhere", the exposure; `futi list db` adds `(destructive)` to the description:

```
$ futi help db drop
...
annotations: destructive (asks for confirmation)
```

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

## Inputs and templating

### Passing inputs

```bash
futi docker build --tag myapp --file Dockerfile.dev ./ctx
futi docker build ./ctx -t myapp           # args and flags in any order
futi docker logs --follow web
futi docker logs web --follow=false
futi k8s logs -- -weird-pod-name           # after "--" everything is an arg
```

The command line is `futi [--dry-run] [--yes] <path…> [args and flags…]`. Tokens after the path are read left to right:

- `--name value`, `--name=value` — a flag with a `string`/`int` value; `-s value` — its short form (set by `short`). Short flags are not glued (`-fi` is an error with the hint `-f -i`) and do not take `=` (`-f=x` is an error)
- `--bool-flag` — a `bool` flag is `true`; `--bool-flag=false` — explicit (`true/false/1/0/yes/no/on/off`). A bare bool flag never takes the next word: `--force false` is an error (`bool flag --force takes no separate value; use --force=false`); to pass the arg `false`, put it after `--`
- the value of a non-bool flag is the next token, **unless it is a flag of this command or `--`**: `--tail -5` and `-m -x` give the values `-5` and `-x` (when `-x` is not a flag of the command), whereas `--message --amend` with a declared `--amend` is an error: `flag --message requires a value (got --amend; use --message=--amend)`. A missing value is `flag --message requires a value`
- `-5` and `-3.2` (a dash and a number) are never flags: they are ordinary values
- any other token that starts with `-` is an unknown flag: `unknown flag --x for "futi k8s logs"` (with a suggestion of a similar name)
- everything else is an arg: args are filled in declaration order
- `--` makes all the following tokens args (flags and `-h` are not recognized). futi consumes **at most one** `--`; the next ones are ordinary values
- `-h` / `--help` before `--` and not in the value position of a flag shows help for the command
- a repeated non-variadic flag: the last one wins; a `variadic` flag collects the values in order
- an empty value `""` is a value: for an optional string arg or flag it is an empty argument; for a `required` or `int` input it is an error (`empty value for --tag`)

After the whole line is read: defaults are applied, a missing required input is an error (`missing argument <pod>`, `missing required flag --tag`), an `int` must be an integer, an extra word is `unexpected argument "extra" (futi k8s logs takes 1 argument: <pod>)` (with a hint `--x=…` when the previous token is a bool flag).

`--dry-run`, `--yes`, `-h/--help` and `--version` of futi itself are recognized only **before** the path. After the path every flag belongs to the command: `futi k8s logs x --dry-run` fails with `unknown flag --dry-run` unless the command declares such a flag.

#### Subcommand or arg

If a node has subcommands and args, the first word after the path that equals a subcommand name is the **subcommand**. To pass such a value as an arg, put it after `--`: `futi db migrate -- up`. A subcommand is recognized only right after the path: if a flag, `--` or another word comes first, the rest are inputs of the node (`futi db migrate --verbose up`: `up` is an arg of `migrate`). Adding a subcommand can therefore change the meaning of a line like `futi echo build` (it was a value, now it is a subcommand), so `futi modules validate`, the node help and `futi modules update` warn when a node has both subcommands and a string arg.

A module with a default `cmd` and the commands `logs`, `ctx` can still act as a shortcut: `futi k logs pod` runs the command, `futi k get pods` runs the default `kubectl get pods`.

#### Variadic

```yaml
commands:
  copy:
    args:
      - name: src
        variadic: true
        required: true
      - name: dst
        required: true
    cmd: "cp -- {{.src}} {{.dst}}"
```

`futi files copy a.txt b.txt dir/` — `src` takes as many words as remain after the trailing required args are reserved (`a.txt b.txt`), `dst` is `dir/`. A variadic flag is repeated: `--env A=1 --env B=2`.

#### Rest: the remaining tokens

A `rest` arg passes the remaining tokens to the wrapped tool **verbatim**, including flags and `-h`:

```yaml
commands:
  run:
    args:
      - name: image
        required: true
      - name: docker_args
        rest: true
    flags:
      - name: rm
        type: bool
    cmd: "docker run {{if .rm}}--rm{{end}} {{.image}} {{.docker_args}}"
```

```bash
futi docker run nginx -it --rm -p 80:80    # docker run nginx -it --rm -p 80:80
futi docker run --rm nginx -it             # --rm is the flag of "run"; the tool gets: nginx -it
futi docker run nginx -- -it               # the same as: futi docker run nginx -it
```

The rest starts right after the token that filled the last arg before it (or, when `rest` is the only arg, at the first token that is not a flag, or after `--`). Flags of the command go **before** it. A `--` is consumed only when it is the very first token of the rest and no `--` was consumed before; any later `--` is a value: `futi docker run nginx -- -it` gives `-it`, `futi docker run nginx -it -- x` gives `-it -- x`, and `futi docker run -- nginx -- -it` gives `nginx -- -it`. Before the rest starts, an unknown flag is an error, after it everything is the rest — so adding a flag to a command never changes the meaning of a line that was valid before. The rest is not checked for a leading `-`.

### Templating

`cmd` is a template in the syntax of Go `text/template`, but futi parses and evaluates it itself (see [cmd vs text/template](#cmd-vs-texttemplate)). The result is an argv list: the command is run without a shell.

| Syntax | Description |
|---|---|
| `{{.name}}` | The value of an arg or flag |
| `{{.env.FOO}}` | Environment variable `FOO` (an execution error if not set) |
| `{{.module_root}}`, `{{.module_name}}` | The directory and the name of the module |
| `{{if .follow}}…{{else if .x}}…{{else}}…{{end}}` | Conditional block |
| `{{range .files}}…{{.}}…{{end}}` | Repeat for each value of a `variadic` input |

The condition of `if` is `.x`, `.env.X`, or `eq A B`, `and`, `or`, `not` over them and string/number literals (`{{if and .a (not .b)}}`, `{{if eq .mode "fast"}}`). `eq` compares string forms (an `int` by its decimal form); an unset input in `eq` is false; a variadic/rest in `eq` is a load error.

**Two forms of `cmd`:**

- **A string** — whitespace (space, tab, newline) outside quotes separates arguments; a multi-line `>-`/`|` is safe and never gives empty arguments. Quotes `'…'`/`"…"` in the literal text only group (no `$`, no escapes except `\"` and `\\` inside `"…"`); `{{…}}` inside quotes is part of the same argument.
- **A list** — every item is exactly one argument; spaces are literal and quotes are not interpreted. An empty item `""` is an empty argument, whereas the item `'""'` is a literal of two quote characters (not an empty argument).

```yaml
cmd: "docker build -t {{.tag}} ."
cmd: [docker, build, -t, "{{.tag}}", .]
```

**A value is never split or reinterpreted.** A scalar value is inserted into its argument as is: spaces, quotes and metacharacters are data. `--msg "hello world"` with `cmd: "echo {{.msg}}"` gives the single argument `hello world`. There is no `quote` function and no manual quoting.

**Unset inputs.** An input without a value and without a `default` is *unset*:

- an argument that mentions an unset input outside `if` is dropped entirely (`--tail={{.tail}}` disappears when `tail` is not given);
- an argument that mentions **two or more** inputs outside `if`, of which one may be unset, is a load error — otherwise another input's value would silently vanish together with it (`{{.user}}@{{.host}}` with an optional `user`; write `{{if .user}}{{.user}}@{{end}}{{.host}}`);
- an argument that became empty only because of an empty `if` is dropped; one that is empty because of the value `""` stays;
- an environment variable with an empty value in an argument of its own (`"{{.env.FOO}}"`) gives no argument, like an unset input; to test it, use `{{if .env.FOO}}`.

**Variadic and rest.** An argument that consists only of a variadic input (`{{.src}}`) becomes N arguments (none for an empty list). A variadic in a concatenation (`--env={{.env_var}}`) is a load error: use `range`.

```yaml
cmd: "docker run {{range .env_var}}-e {{.}} {{end}}{{.image}}"    # -e A=1 -e B=2 nginx
```

`range` works only over a variadic input, has no `$i, $v` and no `{{else}}`; its body may produce any number of arguments. A `rest` is allowed only as a whole argument, not in `range` or a concatenation. A `cmd` that renders to nothing fails with `command rendered to empty argv`.

**A dangling flag** is a load error: a literal like `--tail` followed by an argument that may disappear (an optional input without a default) would swallow the next word. Write `{{if .tail}}--tail {{.tail}}{{end}}` or `--tail={{.tail}}`. This check does not apply to variadic/rest inputs.

#### `cmd` vs `text/template`

The syntax is that of Go templates, but futi allows only a part of it and differs in two places:

1. **In the string form a space in the literal text is the argument boundary**, and a value is never cut: `echo {{.msg}}` is always exactly two arguments, whatever `msg` contains. In `text/template` + shell splitting a value with spaces would become several words.
2. **`if` on a non-bool input tests that the value is set**, not that it is non-zero: `--tail=0` counts as set (Go treats `0` and `""` as false). For a `bool` it is the truth value; a variadic is true when the list is non-empty; `.env.X` is true when the variable is set and non-empty (in a condition an unset variable is false, not an error).

Everything else is a load error with a hint: `with`, `$var`, `:=`, pipes `|`, function calls (`printf`, `quote`, `index`, `len`, `html`, `js`, `call`, …), `define`/`template`/`block`, trim markers `{{-` / `-}}`, and comments inside an argument. A comment `{{/* … */}}` is accepted only when surrounded by whitespace or at the edge of the whole template; at the edge of the body of an `if`/`range` it is rejected.

### Values starting with `-`

An argument whose **first character comes from a user value** (an arg or flag, including the elements of `range`) must not start with `-`: the wrapped program would read it as an option. This covers concatenations (`{{.user}}@{{.host}}`) and `int` values (`-5`). The error:

```
value "-rf" for <src> starts with "-" and would be read as an option by cp; use --opt={{.src}}, put a literal -- before it, or set allow_dash
```

(for `git` the hint also mentions `--end-of-options`). Values of `env`, `module_root` and `module_name` are not checked. Ways out:

- put the value after an option with `=`: `--file={{.file}}` — the argument starts with the author's literal;
- put a literal `--` before the value in `cmd`: `cp -- {{.src}} {{.dst}}`. This lifts the check for all the arguments after it **only if each of them consists of exactly one input reference** (including inside `if` and `range` bodies), with no literal characters. Otherwise the check stays: `kubectl exec {{.pod}} -- ls {{.path}}` (literal `ls`), `rsync -- {{.src}} {{.host}}:{{.dst}}` (literal `:`) and `ssh {{.host}} -- tail {{.file}}` are still checked. The decision is made from the template, not from the actual values;
- `allow_dash: true` on the input lifts the check for all its occurrences (for the shell recipe below).

A `rest` is never checked. The same check works for the CLI and for other front ends (MCP/REST), and in the JSON Schema it becomes `"not": {"pattern": "^-"}` for strings and `"minimum": 0` for ints.

### Environment variables in templates

Environment variables are available via `{{.env.FOO}}` — useful for secrets (tokens, API keys) that are unsafe to pass via flags (they are visible in `ps`):

```yaml
commands:
  deploy:
    args:
      - name: target
        required: true
    cmd: "deploy --token {{.env.DEPLOY_TOKEN}} --env {{.target}}"
```

```bash
export DEPLOY_TOKEN=secret123
futi deploy staging
# → deploy --token secret123 --env staging
```

Referencing a non-existent variable is an execution error (fail-fast). For an optional variable use a condition, where an unset variable is simply false:

```yaml
cmd: "deploy {{if .env.VERBOSE}}-v{{end}}"
```

### A command that needs a shell

futi never runs `sh -c` on its own. If you really need pipes or loops, write the shell explicitly and pass user input as positional parameters of the script (`$1`, `$2`), never inside the script text:

```yaml
commands:
  upper:
    args:
      - name: text
        required: true
        allow_dash: true         # the value goes to "$1", sh does not read it as an option
    cmd: [sh, -c, 'printf "%s\n" "$1" | tr a-z A-Z', sh, "{{.text}}"]
```

`allow_dash` is needed because the item `"{{.text}}"` is a whole value and `-n` would otherwise be refused. `sh` does not read `$1` as an option (it takes everything after the script as parameters), but a command inside the script may: that is why the script uses `printf "%s\n" "$1"` and not `echo "$1"` (`echo` would take `-n` or `-e` as its own option). Inside the script always quote `"$1"` and use `--` or `printf` where the called program supports it.

---

---

## Module package with scripts

A module may contain its own scripts. futi does not set `WorkDir` for the command: it runs in the caller's cwd (`./photo.jpg` is resolved from there). Refer to the module's files explicitly through the `{{.module_root}}` template:

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
    args:
      - name: target
        required: true
    cmd: "{{.module_root}}/scripts/deploy.sh {{.target}}"
```

```bash
futi mytool deploy staging
# runs ~/.config/futi/modules/mytool/scripts/deploy.sh staging
```

Scripts are called with `WorkDir` = the cwd of the futi call. The first token of `cmd` follows ordinary Unix semantics: a relative path is resolved from the cwd, a bare name through `PATH`. The package's own scripts go explicitly through `{{.module_root}}/scripts/...` (a value is never split, so a module in a directory with spaces works without quotes).

### `{{.module_root}}` and `{{.module_name}}`

Two variables are available in the `cmd` template: the path and the name of the module. They are used to reach the module's files from `cmd` and hooks:

```yaml
cmd: "{{.module_root}}/scripts/{{.name}}.sh"   # .name is an input of the command
```

```yaml
# A module with its own venv — the interpreter path starts from the module root:
# ~/.config/futi/modules/images/qr/
# ├── config.yaml
# ├── scripts/make.py
# └── .venv/            # python3 -m venv .venv && .venv/bin/pip install qrcode
commands:
  make:
    args:
      - name: text
        required: true
    cmd: "{{.module_root}}/.venv/bin/python {{.module_root}}/scripts/make.py --text={{.text}}"
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

Relative paths passed by the user (`--out result.png`, file args) are resolved by the OS from the cwd of the `futi` call — natural Unix semantics. A script that needs the cwd can just call `getcwd()` (it equals the cwd of the futi call):

```python
import os
out = os.path.join(os.getcwd(), "result.png")
```

> **Migration note (pre-release):** before the working-directory rework, futi ran commands with `WorkDir` = the module root and passed `FUTI_CWD` in the env. That behavior has been removed. Module fixes:
>
> - `cmd: "./scripts/x.sh ..."` → `cmd: "{{.module_root}}/scripts/x.sh ..."`
> - `cmd: ".venv/bin/python scripts/x.py ..."` → `cmd: "{{.module_root}}/.venv/bin/python {{.module_root}}/scripts/x.py ..."`
> - `base = os.environ.get("FUTI_CWD") or os.getcwd()` → `base = os.getcwd()`
>   (the cwd of the futi call = the script's cwd, `getcwd()` is enough)
> - `os.environ["FUTI_CWD"]` for module paths → `os.environ["FUTI_MODULE_ROOT"]`

---

## CLI commands

```bash
futi <namespace...> <command> [args and --flags]   # call a module command
futi <path...> -h                           # help for a path (namespace, module, branch or command)
futi <namespace>                            # default command (if the module has a module-level cmd)
futi --dry-run <path...> <command>          # print the command without running it
futi --yes <path...> <command>              # run a destructive command without asking (-y)
futi help <path...>                         # help for a path
futi list                                   # modules and namespaces at the root
futi list <path...>                         # contents of a namespace or module commands
futi list <path...> -v                      # the same, with a PATH column
futi list <path...> -r                      # recursive subtree traversal
futi modules validate <namespace>           # validate a module schema
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

Parsing: the path is read word by word over the merged tree of all sources — namespace directories, modules and their commands. A word continues the path only if it is exactly the name of a child of the current node; the path ends at the first token that starts with `-` (longer than one character), at `--`, at a word that is not a child, or at the end. A word with dots is split into segments and continues the path only if all of them match: `futi docker.build` = `futi docker build`, while `file.txt` after a node with a child `file` is not a path word. After the path come the inputs of the command: args and flags in any order, and `--` to make the rest plain args (see [Inputs and templating](#inputs-and-templating)). Flags of a parent node are not inherited by its subcommands.

If the reached node has no `cmd`, the call fails: `unknown command "x" for "futi docker" (available: build, run)` when a word follows, otherwise `"futi db" requires a subcommand (available: migrate)`. If a subcommand name appears later in the line (`futi db --verbose migrate up`), the hint ` (put the full command path before flags)` is added.

**Path collision**: a path that names two things at once is an error, for example the module `dev/deploy.yaml` (`dev deploy`) and the command `deploy` of the module `dev.yaml`: `path "dev deploy" is ambiguous: command "deploy" of module dev (<manifest>) and module dev.deploy (<manifest>)`. It is checked over all sources together while the path is resolved. A local module that overrides a global module with the same name is not a collision.

**Reserved names**: `help`, `list`, `cache`, `modules`, `completion`, `__complete` are builtin commands only as the first word, so they cannot be top-level module or namespace names. Deeper in the path they are ordinary names: `futi git help` and `futi git list` call the commands `help` and `list` of the module `git`, and `tools/help.yaml` is a valid module. The check runs in `futi modules init` and `futi modules install` (for `--namespace` and, without a namespace, for the resulting name); modules that are already installed are not affected.

```bash
futi docker build --tag myapp        # module docker, command build
futi dev deploy staging              # module dev.deploy, command staging
futi k8s logs mypod --follow         # module k8s, command logs, pod=mypod, follow=true
futi echo_mod hello world            # default cmd of echo_mod, words=[hello, world]
futi echo_mod -- upper               # "--": "upper" is a word, not a subcommand
```

**Stdin, signals and exit code.** A module command inherits the stdin of futi: pipes (`echo data | futi m c`) and interactive utilities (`ssh`, `psql`, `docker exec -it`) work. Ctrl+C (SIGINT) and Ctrl+\ (SIGQUIT) are delivered by the terminal straight to the command: when futi runs in the foreground of a terminal, it waits for the command to finish and does nothing itself. Without a terminal or not in the foreground (CI, background, no controlling terminal), SIGINT/SIGQUIT sent to futi (`kill -INT <pid>`, an IDE) are forwarded to the command. SIGTERM and SIGHUP received by futi are always forwarded to the command. When a signal is sent to the whole process group (`timeout`, `timeout -s INT`, systemd `KillMode=control-group`, `kill -TERM -<pgid>`), the command receives SIGTERM (and without a terminal, SIGINT/SIGQUIT too) twice: directly and from futi. If futi forwarded SIGTERM, SIGHUP or SIGINT to the command, after the command finishes futi terminates with the same signal, even if the command caught the signal and exited with 0; after a forwarded SIGQUIT the exit code is `128+3`. The futi exit code for a module command equals the command's exit code; if the command was killed by signal N, it is `128+N` (for example, 143 for SIGTERM); when the command dies from SIGINT/SIGTERM/SIGHUP, futi itself terminates with the same signal, so the shell breaks loops on Ctrl+C. The same applies to `futi modules *`: if a hook (`attach`/`detach`) is killed by an interrupting signal, futi terminates with that signal; other errors of `modules` commands give exit code 1. SIGINT and SIGHUP that were ignored when futi started (`nohup`, a shell background command) stay ignored for the command too (for SIGQUIT and SIGTERM the Go runtime installs its own handler, so nothing is inherited).

### Navigation with `list`

`futi list [<path...>]` shows the contents of a level of the hierarchy (the path may be written with spaces or dots: `futi list images converter` = `futi list images.converter`):

```bash
$ futi list                          # root: modules and namespaces
┌───────────┬────────┬───────────────────┐
│   TYPE    │  NAME  │    DESCRIPTION    │
├───────────┼────────┼───────────────────┤
│ module    │ docker │ Docker management │
│ namespace │ images │                   │
└───────────┴────────┴───────────────────┘

$ futi list images                   # namespace: nested namespaces and modules
┌───────────┬───────────┬───────────────┐
│   TYPE    │   NAME    │  DESCRIPTION  │
├───────────┼───────────┼───────────────┤
│ namespace │ converter │               │
│ module    │ png       │ PNG utilities │
└───────────┴───────────┴───────────────┘

$ futi list images converter         # nested namespace
┌────────┬────────┬──────────────────────┐
│  TYPE  │  NAME  │     DESCRIPTION      │
├────────┼────────┼──────────────────────┤
│ module │ to_png │ Convert images to PNG │
└────────┴────────┴──────────────────────┘

$ futi list images png               # module: commands
┌─────────┬─────────────┐
│ COMMAND │ DESCRIPTION │
├─────────┼─────────────┤
│ optimize│ Optimize PNG│
│ resize  │             │
└─────────┴─────────────┘

$ futi list docker                   # module at the root: commands
┌─────────┬─────────────┐
│ COMMAND │ DESCRIPTION │
├─────────┼─────────────┤
│ ps      │             │
│ build   │ Build image │
└─────────┴─────────────┘
```

If a module has a default command (a module-level `cmd`), it is shown first with the `(default)` marker:

```bash
$ futi list echo_mod                 # module with a default cmd
┌───────────┬─────────────┐
│ COMMAND   │ DESCRIPTION │
├───────────┼─────────────┤
│ (default) │             │
│ upper     │             │
└───────────┴─────────────┘
```

The same goes for subcommands: if a command has `cmd` + `commands`, `cmd` is shown as `(default)` (the fallback when called without a subcommand).

Logic: if the namespace resolves to a module, `list` shows its commands. If it resolves to a namespace directory, it shows the direct children (nested namespaces and modules). `list` is a builtin only as the first word: `futi docker list` calls the command `list` of the module `docker` (an error if there is none).

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

$ futi list images -r                # from the images context
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

`futi help <path...>` is contextual help at any level, without leaving the terminal for the README; `-h`/`--help` after a path does the same:

```bash
futi help demo                 # module help
futi help demo build           # command help (= futi demo build -h)
futi help demo migrate         # help for a command branch
futi help images               # namespace: its children
```

Module help shows the description, the usage of the default command and the list of subcommands:

```bash
$ futi help demo
Demo module

usage: futi demo
flags:
  -h, --help
subcommands:
  build    Build the project
  migrate  Database migrations
```

Command help shows the usage line, the args (with description, default and `(required)`), the flags (type, short form, required, default) and, if the command has subcommands, their list:

```bash
$ futi help demo build
Build the project

usage: futi demo build [flags] [context]
  context  Build context directory (default ".")
flags:
  -t, --tag string   Image tag (required)
      --file string  Dockerfile path (default "Dockerfile")
      --push
  -h, --help
```

A flag with `choices` shows the values instead of the type (`--level debug|info|warn (default "info")`), an arg shows them as `(dev|prod)`; a `file` or `dir` flag shows its type (`--out file`). A `variadic` input is shown with `...` (`[words...]`), a `rest` arg as `[docker_args...]` with the note `(passed verbatim)`. If a node has subcommands and a string arg, the help adds `words up, down are subcommands; pass such a value after --`. The template is not shown in the help; to see the assembled command use [`--dry-run`](#--dry-run).

A trailing `-h`/`--help` after the path is ignored (`futi help demo -h` = `futi help demo`); `futi help --help` shows the help of futi itself, like `futi -h`. Every word after `futi help` must be a path word: `futi help docker build extra` fails with `unknown command "extra" for "futi docker build"`. `help` is a builtin only as the first word: `futi demo help` calls the command `help` of the module `demo` as an ordinary command (flags, `-- args`, its stderr and exit code), and fails if there is no such command; `futi help demo help` shows the help of that command.

### `--dry-run`

`--dry-run` is a global flag: it prints the final module command and executes nothing.

For a command with the template `cmd: "docker build -t {{.tag}} -f {{.file}} ."` (`file` defaults to `Dockerfile`):

```bash
$ futi --dry-run docker build --tag 'my app'
docker build -t 'my app' -f Dockerfile .
```

The output is one line on stdout: the command's argv after template evaluation, with tokens that contain spaces or special characters in single quotes (the line can be pasted into a shell). The output uses bash/zsh syntax (not POSIX sh/dash): the `$'…'` form is used only for tokens with control or non-printable characters (ESC, `\r`, bidi characters, etc.), so a foreign module cannot forge the displayed command. To paste such tokens back into a shell, use a UTF-8 locale: `\u…` inside `$'…'` depends on the locale. Errors (unknown command, missing required input, template error) are printed the same way as without `--dry-run`, with the same exit code.

- The flag is recognized only before the path (together with [`--yes`](#--yes), in any order); after the path `--dry-run` is an ordinary module flag, not a reserved name. Repeating it is an error: `futi: duplicate flag --dry-run`.
- A destructive command is printed without a confirmation question.
- Hooks (`attach`/`detach`) are not executed and not shown: only the command is printed.
- `-h` / `--help` after the path works as usual (help, exit code 0). A module command named `help` (`futi --dry-run demo help`) is shown like any other.
- `list`, `help`, `modules`, `cache`, `completion`, `--version` and `-h` without a path are not module commands: with `--dry-run` they fail with the error `--dry-run applies only to module commands` (exit code 1).
- **Secrets:** the template substitutes `{{.env.X}}` values, and they will appear in the output. Do not publish `--dry-run` output for commands with tokens and passwords in the environment.

### `--yes`

`--yes` (short `-y`) is a global flag: it confirms a [destructive command](#confirmation-of-destructive-commands) in advance, so futi does not ask.

```bash
futi --yes db drop main
futi -y --dry-run db drop main
```

- Like `--dry-run`, the flag is recognized only before the path; after the path `--yes` belongs to the command (`futi db drop main --yes` fails with `unknown flag --yes` if the command has no such flag). The order of `--yes` and `--dry-run` does not matter; repeating a flag is an error (`futi: duplicate flag --yes`).
- For a command that is not `destructive: true` the flag does nothing.
- With builtins (`list`, `help`, `modules`, `cache`, `completion`, `--version`, `-h`) it fails with `futi: --yes applies only to module commands` (exit code 1). The own `--yes` of `futi modules install|update|uninstall` is given after the subcommand and is not affected.

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
ERROR: line 10: "positional" was replaced by "args"/"flags" (see README)
ERROR: command "build": flag "tag": invalid type "float" (valid: string, int, bool, file, dir)
ERROR: command "build": flag "tag": duplicate name "tag"
ERROR: command "build": cmd: 1:14: "-t" would be left without its value when "tag" is unset; use {{if .tag}}-t {{.tag}}{{end}} or -t={{.tag}}
ERROR: command "empty": must have cmd or commands
ERROR: command "old": cmd: 1:8: {{.args}} was replaced by a rest arg (see README)
modules: validate: module "bad" has 6 error(s)
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
- `args` or `flags` without `cmd` — at any level (module, command); inputs are not inherited by subcommands
- an input schema error: a `type` outside `{string, int, bool, file, dir}`, wrong `choices` (on `bool`, `file`, `dir` or `rest`, empty, duplicates, a non-integer for `int`, a `default` outside the list), `bool` with `required`/`variadic`/`allow_dash`, `required` together with a `default` (even an empty one), a `default` of the wrong type, a duplicate name among the args and flags of a node, a name that does not match `[a-z][a-z0-9_]*`, a reserved name (`help`, `env`, `module_root`, `module_name`), an invalid `short` (not one Latin letter, duplicate, `h`), `rest` not on the last arg, and an invalid form of the args (only `required… optional…`, `required… variadic required…` or `required… rest` are allowed)
- a template error in `cmd`, `attach`, `detach`: syntax, a construct that is not allowed (see [`cmd` vs `text/template`](#cmd-vs-texttemplate)), a reference to an undeclared input, a bool outside a condition, a variadic/rest in a concatenation without `range`, `range` over a non-variadic input; hooks may refer only to `env`, `module_root` and `module_name`
- an input that `cmd` never mentions (`input "port" is not used in cmd`)
- a dangling flag: a literal `--tail` followed by an argument that may disappear (`"--tail" would be left without its value when "tail" is unset; use {{if .tail}}--tail {{.tail}}{{end}} or --tail={{.tail}}`); not checked for variadic/rest
- an argument that mentions two or more inputs, one of which may be unset (`{{.user}}@{{.host}}`)
- an invalid command name in `commands`: empty, starts with `-`, contains `.`, whitespace or control characters
- the old format: `params`/`positional` keys (`"params" was replaced by "args"/"flags" (see README)`), the function `quote` and `{{.args}}`/`{{.args_list}}` in a template (each with a hint)
- a path collision: one path addresses two entities of the merged tree, e.g. the command `deploy` of module `dev.yaml` and the module `dev/deploy.yaml` (`path "dev deploy" is ambiguous: …`). Checked across all sources; `futi list` prints it as a `Warning:` line on stderr and still shows the listing

**Warnings** (do not block, shown only in `validate`):

- a command with `commands` but without `cmd` — calling without a subcommand fails at runtime
- a node with subcommands and a string arg (including `variadic` and `rest`) — a word equal to a subcommand name is taken as the subcommand; pass such a value after `--` (an arg of type `int` does not trigger it). The same warning is shown in the help of the node and by `futi modules update`
- a hook with a separate `&&`, `||`, `|`, `>`, `<` or `>>` word — hooks run without a shell, so it is passed to the program as an argument; use `[sh, -c, "…"]`

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
detach: [rm, -rf, .venv]
```

A hook is a template in the same two forms as `cmd` (a string split into words, or a list with one argument per item); it is evaluated to argv and executed without a shell, so `&&`, `|`, `>`, `;`, `$VAR` and globs are not interpreted: they reach the program as plain text. `futi modules validate` warns when a hook has a separate `&&`, `||`, `|`, `>`, `<` or `>>` word. A compound hook is written explicitly with a shell:

```yaml
attach: [sh, -c, "python3 -m venv .venv && .venv/bin/pip install -r requirements.txt"]
```

Hooks run in the module directory; `{{.module_name}}`/`FUTI_MODULE_NAME` is the same name as in `cmd` (the `name` field, or without it the module's directory/file name) on install, update and uninstall; `{{.module_name}}`, `{{.module_root}}`, `{{.env.FOO}}` (there are no inputs) and the environment variables FUTI_MODULE_NAME / FUTI_MODULE_ROOT are available. A program written with a path (`scripts/x.sh`, `./x.sh`) is looked up from the module directory, a bare name (`python3`) through `PATH`. Hooks inherit the stdin of futi and run without a timeout: a hung hook is interrupted with Ctrl+C. Without a controlling terminal (CI, cron, ssh without tty) a hook runs in its own process group: SIGINT, SIGQUIT, SIGTERM and SIGHUP received by futi are forwarded to all hook processes, including those spawned by a compound command (`[sh, -c, "…"]`). With a controlling terminal the hook stays in the futi group (interactive hooks work), including when futi runs in the background (`&`) or with redirected input (`</dev/null`): redirection and background do not enable isolation; on a targeted `kill -TERM <futi>`, processes spawned by the hook may stay behind. An isolated hook (no controlling terminal) does not die from a signal sent to the futi group (`kill -9 -<pgid>`, `timeout -s KILL futi …`): its group is separate. A failed `attach` on install rolls the installation back (the module is removed); on update it is only a warning, and the new tree is kept. A failed `detach` on uninstall is a warning (or a refusal with `--strict-detach`).

The `attach` from a downloaded manifest is foreign code, so install and update ask for confirmation before running it (on stderr; the hook is shown as written, without substituting values; a list is shown as `[a, "b c"]`):

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

`update` compares the hash of the source tree with the lockfile (the hash covers paths, file contents, the execute bit and symlink targets): no changes means a no-op (only `last_check` is updated), and local edits of the installed module are kept. If the source changed, update compares the files on disk with the new version: if they match, only the lockfile is rewritten; otherwise a list of changes is printed to stderr (`D` — will be removed, `M` — will change, `A` — will appear, in this order; at most 50 lines) and the question `Replace module files? [y/N]` is asked, before the `attach` question. Local edits show up as `M`/`D` and are lost after confirmation; on refusal the module is not changed. Without a terminal on stdin, `--yes` (`-y`) is required — pass it in CI and scripts. The directories `.venv`, `node_modules`, `.git`, `__pycache__` are not part of the hash or the comparison and are not preserved when the tree is replaced (the old tree is removed entirely, `attach` recreates them). The tree replacement is atomic, `installed_at` and `about` are preserved. If moving or writing the lockfile fails, update restores the old tree. If a `<module>.futi-backup` is left over from an interrupted update, update refuses and asks you to sort out the backup manually; `list` and `update` do not treat such a directory as a module (update without a name only warns about it on stderr). If the new version adds a subcommand to a node that has a string positional arg, update warns on stderr (`Warning: module <name>: command "…": words … are subcommands; pass such a value after --`): a word equal to the subcommand name is now taken as the subcommand. Update flags: `--skip-attach`, `--yes` / `-y`.

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

futi supports completion for **bash**, **zsh** and **fish**. Names of modules, namespaces, commands, subcommands, flags (`--tag`, `--force`) are dynamic, so completion asks futi for hints on the fly through the hidden command `futi __complete <args...>` (a protocol compatible with cobra).

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

- **Path words** — the children of the current node in the merged tree: `futi <TAB>` (modules, namespaces, builtins), `futi dev <TAB>`, `futi docker <TAB>`, `futi docker migrate <TAB>`. A subcommand is suggested only right after the path: once a flag or another word follows, the rest are the inputs of the node
- **Flags** — only for a word starting with `-`: `futi k8s logs -<TAB>` → `--follow`, `-f`, `--tail`, `--help`; flags already used are not offered again (except variadic ones). The marks `(required)` and `(default: …)` are visible in zsh; bash/fish cut them off
- **Values** — an input with `choices` suggests its choices; a `file` input suggests files and directories, a `dir` input suggests directories only. For any other input (`futi docker build --tag <TAB>`) nothing is suggested, and the shell does not add files either. After `--flag=<TAB>` only choices are completed, never paths. When a node has subcommands and its first arg has `choices`, the matching subcommands win; the choices are offered only if no subcommand matches. A bool flag takes no value, so nothing is offered after it
- **Rest arg** — once the rest arg has started (`futi docker run nginx <TAB>`) or when it is the next input of a node (`futi run <TAB>`; on a node with subcommands, only if the typed prefix matches no subcommand), the tokens are passed to the tool as they are, and the shell completes files as usual. Everything after `--` is data: no flags or subcommands are suggested
- **`help` and `list`** — these are builtins only as the first word; after them the command path is completed: `futi help do<TAB>` → `docker`, `futi list dev <TAB>`. Inside a path `help` and `list` are suggested only if the module defines such commands
- **Global flags** — `futi --<TAB>` → `--dry-run`, `--help`, `--version`, `--yes`; after the leading `--dry-run` / `--yes` / `-y` (in any combination) the hints are as for a regular call, but without builtins (`cache`, `completion`, `list`, `modules`): `futi --dry-run <TAB>` shows only modules and namespaces. Commands without `cli` in `expose` are not suggested
- **Builtins** — `futi cache <TAB>` → `refresh`/`status`/`clear`; `futi completion <TAB>` → `bash`/`zsh`/`fish`. At the root, `cache`, `completion`, `list`, `modules` are suggested as builtins even when a module has the same name (the CLI executes the builtin)
- **Module validation** — `futi modules validate <TAB>` suggests full module names (`dev.deploy`), and no hints after the first argument
- **`modules` flags** — `futi modules add_command <module> --<TAB>` → `--cmd`, `--desc`; likewise for `init`, `install`, `uninstall`, `update`

A module that cannot be loaded, or a path that addresses two entities (a path collision), gives no hints and no error output.

zsh prints descriptions of commands/modules next to the hints.

### Cache and completion

`__complete` uses the persistent module cache (like the regular CLI), so it does not walk the sources on every TAB. So that hints are not empty on a cold cache (the first run) or stale, `__complete` **refreshes** the cache itself if it has never been refreshed or was refreshed more than an hour ago.

A cached entry is not trusted blindly: the modules on the typed path are loaded **with a freshness check** — futi hashes the manifest on disk and compares it with the hash stored in the cache (the same check that runs before a command is executed). If the file changed, it is read again, so a stale cache does not hide the flags and subcommands that you have just added to a module. Hints for the path itself (the children of a namespace) still come from the cache and the source listing.

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

Commands are executed through `exec` with a ready argv — **without** a shell. The metacharacters `;`, `$()`, `` ` `` in inputs are not executed as shell code (they pass as ordinary text in arguments). If you need a shell, write it explicitly (`[sh, -c, '…', sh, "{{.x}}"]`), passing user input as a positional parameter of the script (`$1`) and never inside the script text (see [A command that needs a shell](#a-command-that-needs-a-shell) and the examples).

### Batch files on Windows

On Windows, when the program of a command resolves (after the `PATH` lookup) to a `.bat` or `.cmd` file, Go cannot quote arguments safely for `cmd.exe`. futi therefore refuses to run the command if any argument element containing a user value (an arg, a flag or `rest`) contains `"`, `%`, `!`, `&`, `|`, `<`, `>`, `^` or a line break; the error names the input, for example `value for <x> contains "&", unsafe for batch file deploy.cmd`. Prefer an `.exe` for commands that take user input. The check is skipped by `--dry-run`.

### One value is one argument

A value from an arg or a flag is never split or parsed again: it is inserted into the argument as is, with its spaces, quotes and metacharacters, so it cannot add arguments to the command. There is no manual quoting (the `quote` function was removed): a value is always exactly one argument.

A value that starts with `-` could still be read as an option by the wrapped program, so futi refuses such a value when it would be the first character of an argument, unless the template guards it with a literal `--` or the input has `allow_dash: true` (see [Values starting with `-`](#values-starting-with--)). Use `--opt={{.x}}` or a literal `--` for values in dangerous positions (`git commit -m`, `docker run`, `rm`). The tokens of a `rest` arg are passed verbatim and are not checked: do not give a `rest` to a program that interprets its arguments as a dangerous option unless that is the point of the command.

### Secrets through the environment

Pass tokens and keys through `{{.env.NAME}}`, not through flags: arguments are visible in `ps` and in shell history, the process environment is not.

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
cmd: "echo {{.words}}"           # default command (no shell)
args:
  - name: words
    variadic: true
commands:
  upper:
    description: Uppercase echo
    # futi executes argv WITHOUT a shell: pipes, $() and loops do not work directly.
    # If you need a shell, write it explicitly (sh -c) and pass user input as
    # a positional parameter of sh ($1): the value never becomes part of the script.
    args:
      - name: text
        required: true
        allow_dash: true       # goes to "$1": sh does not read it as an option
    cmd: [sh, -c, 'printf "%s\n" "$1" | tr a-z A-Z', sh, "{{.text}}"]
  repeat:
    description: Repeat the text N times
    args:
      - name: text
        required: true
        allow_dash: true
    flags:
      - name: count
        type: int
        short: n
        required: true
    cmd: [sh, -c, 'for i in $(seq 1 "$2"); do printf "%s\n" "$1"; done', sh, "{{.text}}", "{{.count}}"]
```

```bash
futi echo_mod hello world            # echo hello world (default cmd)
futi echo_mod upper "hello world"    # HELLO WORLD (via sh -c | tr)
futi echo_mod repeat hi -n 3         # hi\nhi\nhi
futi echo_mod -- upper               # prints "upper": after "--" it is a word, not a subcommand
```

`futi modules validate echo_mod` prints a warning here: the default command takes string words, and `upper`/`repeat` are subcommands, so such a word as the first one is read as a subcommand (the node help shows it too). The same warning is printed for a module with a string positional arg that has child modules (module `dev` with an arg and `dev/deploy.yaml`): `futi dev deploy` goes to the child module, and the warning lists the child module names.

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
    args:
      - name: branch
        default: main
    cmd: [sh, -c, 'git fetch origin && git reset --hard "origin/$1"', sh, "{{.branch}}"]

  squash:
    description: Squash last N commits
    args:
      - name: count
        type: int
        required: true
    flags:
      - name: message
        short: m
        required: true
        allow_dash: true
    cmd: [sh, -c, 'git reset --soft "HEAD~$1" && git commit -m "$2"', sh, "{{.count}}", "{{.message}}"]
```

```bash
futi git amend
futi git fresh develop
futi git squash 3 --message "feature complete"
```

### Docker wrapper

```yaml
# ~/.config/futi/modules/docker.yaml
name: docker
description: Docker utilities
commands:
  clean:
    description: Remove stopped containers and dangling images
    cmd: [sh, -c, "docker container prune -f && docker image prune -f"]

  shell:
    description: Shell into running container
    args:
      - name: container
        required: true
    cmd: "docker exec -it {{.container}} sh"

  run:
    description: Run an image; everything after the image goes to docker verbatim
    args:
      - name: image
        required: true
      - name: docker_args
        rest: true
    flags:
      - name: rm
        type: bool
    cmd: "docker run {{if .rm}}--rm{{end}} {{.image}} {{.docker_args}}"

  build-all:
    description: Build all images in directory
    cmd: [sh, -c, 'for d in */; do docker build -t "${d%/}" "$d"; done']
```

```bash
futi docker clean
futi docker shell myapp
futi docker run --rm nginx -p 8080:80
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
    args:
      - name: version
        required: true
    cmd: ["{{.module_root}}/scripts/deploy.sh", staging, "{{.version}}"]

  production:
    args:
      - name: version
        required: true
    cmd: ["{{.module_root}}/scripts/deploy.sh", production, "{{.version}}"]

  rollback:
    args:
      - name: target
        required: true
    cmd: ["{{.module_root}}/scripts/rollback.sh", "{{.target}}"]
```

```bash
futi deploy staging v1.2.3
futi deploy production v1.2.3
futi deploy rollback production
```

---

## Roadmap

futi is early-stage software (0.x): the module format may change between minor versions. Everything below is a plan, not a promise: none of it is implemented yet.

- **Module schema** — richer input types (`json`, `secret`).
- **Server mode** — a remote MCP server over HTTP so that AI agents can call module commands on the machine where futi runs, plus a REST API for CI/CD. Auth, a namespace allowlist, output and time limits and an audit log are part of the plan.
- **Module-provided completion and interactive mode** — modules supply their own completion candidates (container names, branches, etc.); an interactive TUI for browsing and calling modules; completion descriptions in bash/fish; PowerShell support.
- **Ecosystem** — a module registry (`futi modules install <short-name>`, update checks) and module versioning.

---

## License

MIT — see [LICENSE](LICENSE).
