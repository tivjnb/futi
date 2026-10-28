package cli

import (
	"fmt"
	"io"
)

// printUsage печатает общую справку в w: stdout по явному запросу (-h, help),
// stderr — когда futi запущен без аргументов.
func printUsage(w io.Writer) {
	fmt.Fprint(w, `futi — universal CLI utility manager

Turns scattered commands, scripts and tools into YAML modules with a single
CLI interface. Install a module once — use it everywhere.

USAGE
  futi <namespace...> <command> [--param value] [--bool-flag] [-- args]
  futi <namespace>                    Invoke module's default command (if defined)
  futi <namespace...> list [flags]
  futi list [flags]
  futi modules validate <namespace>   Validate module schema
  futi modules init <name> [flags]    Create a new module from template
  futi modules <subcommand>           Manage modules (install, uninstall, ...)
  futi cache refresh                  Refresh module cache
  futi completion <bash|zsh|fish>     Generate a shell completion script
  futi --dry-run <path...> <command>  Print the command without running it
  futi -h | --help
  futi --version                      Print version

The last positional argument (without --) is the command; everything before
it is the namespace, joined with dots. Example: "futi dev deploy staging"
resolves to namespace "dev.deploy", command "staging".

The order is strict: path, then flags, then "-- args". The path ends at the
first token starting with "-"; a word after a flag is only accepted as the
value of the preceding string/int flag. "-h" / "--help" after the path shows
help for that path.

Reserved words: "list", "cache", "modules", "completion", "__complete". They
cannot be used as a module name or as any segment of a module path, and "list"
cannot be a command name in a module.

Arguments after "--" are passed to the command's template as {{.args}}
(space-joined string) and {{.args_list}} (array). Example:
  futi echo_mod echo -- hello world  →  cmd: "echo {{.args}}"  → echo hello world

MODULES
  Modules live in ~/.config/futi/modules/ by default (override with
  FUTI_MODULES_DIR). With a config.yaml, sources can be anywhere —
  see CONFIGURATION below.
  A module is either a single YAML file (docker.yaml) or a package directory
  with a config.yaml inside (deploy/config.yaml). Nested namespaces map to
  directory paths: dev/deploy.yaml → namespace "dev.deploy".

  Auto-discovery: futi also searches the current working directory for
  ./futi.yaml (single module) or ./.futi/ (module directory). Local modules
  override global ones with the same name.

CONFIGURATION
  A config file at ~/.config/futi/config.yaml can define multiple module
  sources with mountpoints:

    sources:
      - path: ~/.config/futi/modules         # root mount (default)
      - path: /path/to/project-modules       # mountpoint: ns1.ns2
        mountpoint: ns1.ns2
      - path: /path/to/tools                 # mounted as namespace "tools"
        mountpoint: dev
        mount_as_namespace: tools

  path              Module directory; ~/... is relative to the home dir (the bare
                    home dir needs quotes: "~", a bare ~ is YAML null), a
                    relative path is resolved from the directory of config.yaml
  mountpoint       Mount source contents under this namespace (default: root)
  mount_as_namespace  Wrap source contents under an extra namespace level

  When no config file exists, futi falls back to a single source at
  ~/.config/futi/modules/ (or FUTI_MODULES_DIR).

LISTING
  futi list                        Show top-level modules and namespaces
  futi <ns> list                   Show children of a namespace or commands
                                   of a module
  futi <ns> <cmd> list             Show subcommands of a command
  futi list -r, --recurse          Recursively traverse the subtree
  futi list -v, --verbose          Add a PATH column (manifest location)
  futi <ns> list -r -v             Flags combine

  The (default) marker in command listings indicates the module's default
  command (cmd field), invoked when no command is specified.

CACHE
  futi cache refresh               Reload modules from source into cache
  futi cache refresh -v, --verbose Show which modules were updated/skipped
  futi cache status                Show cache policy, last refresh, module count
  futi cache clear                 Remove the persistent cache file from disk

COMPLETION
  futi completion bash|zsh|fish    Print a shell completion script to stdout.
                                   Pipe it into your shell's completion setup:
                                     bash:  futi completion bash > /etc/bash_completion.d/futi
                                     zsh:   futi completion zsh > "${fpath[1]}/_futi"
                                     fish:  futi completion fish > ~/.config/fish/completions/futi.fish
                                   Completion suggests modules, namespaces,
                                   commands, subcommands, parameter names and
                                   boolean values (true/false).

VALIDATION
  futi modules validate <ns>       Validate module schema (all commands and
                                   params, recursively). Prints a report with
                                   ERROR and WARNING lines, or "OK" if clean.
                                   Exit code: 0 if no errors, 1 otherwise.

                                   Errors (block module loading):
                                     - command without cmd and commands
                                     - duplicate param names in a command
                                     - invalid param type (not string/bool/int)

                                   Warnings (do not block):
                                     - command with subcommands but no cmd
                                       (calling without subcommand fails)

MODULES
  futi modules init <name>         Create a new module in ./.futi/<name>.yaml
  futi modules init <ns>/<name>    Create with namespace (./.futi/<ns>/<name>.yaml)
  futi modules init <name> --global
                                   Create in ~/.config/futi/modules/<name>.yaml

  init flags:
  --desc <text>                    Module description (default: "My tool")
  --default-cmd <cmd>              Add cmd at module level (default command)
  --command <name>                 Add a command (repeatable)
  --global                         Create in global modules directory

  The generated module is valid and immediately discoverable by futi.
  If the file already exists, an error is returned (no overwrite).

  futi modules install <source>    Install a module from a local path, git URL
                                   or archive URL. Writes __futi_meta.yaml
                                   next to config.yaml and runs the module's
                                   attach: hook (if defined).
  futi modules uninstall <name>    Run detach: hook (if defined) and remove
                                   the installed module. Asks for confirmation.
  futi modules update [name]       Re-fetch the source and re-run attach:
                                   (all installed modules when no name given)
  futi modules info <name>         Show installed module details (from
                                   __futi_meta.yaml)
  futi modules list                List installed modules (with __futi_meta.yaml)
  futi modules path <name>         Print the module's directory (for scripts)
  futi modules add_command <module> <command>
                                   Add a command to the module's config.yaml
  futi modules remove_command <module> <command>
                                   Remove a command from config.yaml

  list, update and uninstall see modules in all config.yaml sources (names
  include the mountpoint); install and init --global write only to the global
  modules directory (or ./.futi/ with --local).

  install flags:
  --local                          Install into ./.futi/ instead of the
                                   global modules directory
  --namespace <ns>                 Install under a namespace (ns/name)
  --ref <branch|tag>               Git branch or tag to install from
  --from-local/--from-git/--from-archive
                                   Force source type detection
  --skip-attach                    Skip the attach: hook
  --yes, -y                        Run the attach: hook without asking
                                   (it asks for confirmation otherwise)

  update flags:
  --skip-attach                    Skip the attach: hook
  --yes, -y                        Replace changed module files and run the
                                   attach: hook without asking

  uninstall flags:
  --force                          Allow removing modules without __futi_meta.yaml
  --yes, -y                        Skip the confirmation prompt
  --skip-detach                    Skip the detach: hook
  --strict-detach                  Abort (do not delete) if detach: fails

PARAMETERS
  --param value                    Pass a string/int parameter
  --param=value                    Alternative syntax (required for values
                                   starting with "--")
  --bool-flag                      Boolean flag (true when bare; never takes
                                   the next word as its value)
  --bool-flag=false                Explicit false

  Boolean values: true/false, 1/0, yes/no, on/off (case-insensitive); anything
  else is an error. Use {{if .flag}}...{{end}} in templates.

MODULE SCHEMA (YAML)
  name: docker                     Optional, defaults to file/dir name
  description: Docker management    Optional
  cmd: "docker ps"                  Optional default command (invoked when
                                   no command is specified)
  commands:
    build:
      description: Build image      Optional
      params:
        - name: tag
          type: string              string (default), bool, or int
          required: true
          default: Dockerfile
      cmd: "docker build -t {{.tag}} ."

    # Nested commands: a command can have "commands" alongside or instead
    # of "cmd". When both are present, "cmd" is a fallback (invoked when
    # no subcommand matches).
    migrate:
      description: Database migrations
      cmd: "migrate"                 # fallback if no subcommand given
      commands:
        up:
          cmd: "migrate up"
        down:
          cmd: "migrate down"
        create:
          params:
            - name: name
              type: string
              required: true
          cmd: "migrate create {{.name}}"

  # Module with default command + args (echo-like):
  #   name: echo_mod
  #   cmd: "echo {{.args}}"
  #   commands:
  #     shout:
  #       cmd: "echo {{quote .args}}"

TEMPLATING
  Commands use Go text/template syntax:
    {{.name}}                       Substitute a parameter value
    {{.args}}                       All args after "--" (space-joined string)
    {{index .args_list 0}}          Nth arg after "--" (array access)
    {{.env.FOO}}                    Environment variable FOO (error if unset)
    {{if index .env "FOO"}}...{{end}}  Conditional on env var presence
    {{if .follow}}...{{end}}        Conditional block (for bool params)

    {{.module_root}}                Directory of the module (use {{quote .module_root}})
    {{.module_name}}                Name of the module

  Commands run without a shell: the rendered "cmd" is split into words (like
  shlex), so a substituted value is split again. Always wrap user input in
  quote, otherwise spaces split it into several arguments and a value starting
  with "-" becomes a flag:
    cmd: "echo {{quote .msg}}"      --msg "hello world" is passed as one argument
  Shell features (pipes, redirects, globs) are not available in "cmd".

FLAGS
  -h, --help                        Show this help (after a path: help for
                                    that path)
  -v, --verbose                     Show module paths (flag of "list" only)
  -r, --recurse                     Recursively list namespace subtree
                                    (flag of "list" only)

  Other short flags (-x) are not accepted: pass parameters as --name value.

EXAMPLES
  futi docker build -h             Show help for "docker build"
  futi docker build --tag myapp    Run "build" command in "docker" module
  futi dev deploy staging           Namespace "dev.deploy", command "staging"
  futi k8s logs --follow            Bool flag --follow
  futi db migrate up                Nested command: "migrate up" in "db" module
  futi db migrate list              List subcommands of "migrate"
  futi echo_mod echo -- hello world Pass args to command (via {{.args}})
  futi echo_mod -- hello world      Invoke module's default command with args
  futi images list -r               Recursively list all image modules
  futi docker list -v               Show docker commands with manifest path

  Exit codes: futi propagates the exit code of the called command.
  If docker build exits with 5, futi exits with 5.

ENVIRONMENT
  FUTI_MODULES_DIR                  Override modules directory (discovery
                                    uses it when no config.yaml exists;
                                    install without --local writes there;
                                    default ~/.config/futi/modules/)
`)
}
