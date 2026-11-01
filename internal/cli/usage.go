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
  futi <namespace...> <command> [flags] [args...]
  futi <namespace>                    Invoke module's default command (if defined)
  futi help <path...>                 Show help for a module, command or namespace
  futi list [<path...>] [flags]       List modules, namespaces or commands
  futi modules validate <namespace>   Validate module schema
  futi modules init <name> [flags]    Create a new module from template
  futi modules <subcommand>           Manage modules (install, uninstall, ...)
  futi cache refresh                  Refresh module cache
  futi completion <bash|zsh|fish>     Generate a shell completion script
  futi --dry-run <path...> <command>  Print the command without running it
  futi -h | --help
  futi --version                      Print version

The path is read left to right over the merged tree of namespaces, modules
and their commands. Example: "futi dev deploy staging" runs command "staging"
of module "dev.deploy" (dev/deploy.yaml). A dotted word is the same as
separate words: "futi dev.deploy staging". A path that names two things at
once (a module command and a module or namespace) is an error.

The path ends at the first token starting with "-" (a lone "-" does not
count), at "--", or at the first word that is not a child of the current node.
After the path, flags and arguments may be mixed. "--" makes every following
token an argument (a value starting with "-", or one equal to a subcommand
name). "-h" / "--help" after the path shows help for that path.

Reserved words: "help", "list", "cache", "modules", "completion",
"__complete". They are builtin commands only as the first word, so they cannot
be top-level module or namespace names; deeper in the path they are ordinary
names ("futi git help" runs command "help" of module "git").

A command declares its inputs as "args" (positional) and "flags" (--name);
see MODULE SCHEMA. Example:
  futi k8s logs mypod --tail 50      →  kubectl logs --tail=50 -- mypod

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
  futi list <ns>                   Show children of a namespace or commands
                                   of a module
  futi list <ns> <cmd>             Show subcommands of a command
  futi list -r, --recurse          Recursively traverse the subtree
  futi list -v, --verbose          Add a PATH column (manifest location)
  futi list <ns> -r -v             Flags combine

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
                                   commands, subcommands and flag names.

VALIDATION
  futi modules validate <ns>       Validate module schema (all commands and
                                   inputs, recursively). Prints a report with
                                   ERROR and WARNING lines, or "OK" if clean.
                                   Exit code: 0 if no errors, 1 otherwise.

                                   Errors (block module loading):
                                     - command without cmd and commands
                                     - invalid args/flags schema (names,
                                       types, the form of args)
                                     - cmd template errors (unknown or unused
                                       inputs, unsupported constructs)
                                     - params/positional (old format)

                                   Warnings (do not block):
                                     - command with subcommands but no cmd
                                       (calling without subcommand fails)
                                     - a subcommand name could be meant as a
                                       value of a string arg

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

INPUTS OF A COMMAND
  <arg>                            Positional arguments, in declaration order
  --flag value, --flag=value       Flag with a string or int value (the value
                                   is the next token unless it is a flag of
                                   the command or "--")
  -f value                         Short form, set by "short" in the module
  --bool-flag                      Boolean flag: true when bare, never takes
                                   the next word; --bool-flag=false for false
  --                               Everything after it is an argument

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
      args:
        - name: context            a-z, 0-9 and _ ("_" is "-" in flags)
          default: "."
          description: Build context directory
      flags:
        - name: tag
          type: string              string (default), int, or bool (flags only)
          required: true
          short: t                  -t, one letter
        - name: file
          default: Dockerfile
      cmd: "docker build -t {{.tag}} -f {{.file}} {{.context}}"

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
          args:
            - name: name
              required: true
          cmd: "migrate create {{.name}}"

  # Module with default command + args (echo-like):
  #   name: echo_mod
  #   cmd: "echo {{.words}}"
  #   args:
  #     - name: words
  #       variadic: true            several words in a row; "rest: true"
  #                                 takes all remaining tokens verbatim

TEMPLATING
  cmd is a string (split into arguments at whitespace; quotes group) or a
  list (every item is exactly one argument):
    cmd: "docker build -t {{.tag}} ."
    cmd: [docker, build, -t, "{{.tag}}", .]
  Inside cmd:
    {{.name}}                       Value of an arg or flag
    {{.env.FOO}}                    Environment variable FOO (error if unset)
    {{if .follow}}...{{end}}        Conditional: a bool flag is true, any
                                    other input is set; .env.FOO is set
                                    and non-empty
    {{range .files}}...{{.}}...{{end}}  Repeat for each value of a variadic input
    {{.module_root}}                Directory of the module
    {{.module_name}}                Name of the module

  Commands run without a shell, and a value is never split again: it is
  exactly one argument, whatever it contains:
    cmd: "echo {{.msg}}"            --msg "hello world" is passed as one argument
  An argument whose first character comes from a value starting with "-" is
  refused (the program would read it as an option), unless the input has
  "allow_dash: true", or cmd has a literal "--" before it and every argument
  after that "--" is exactly one input, with no literal characters.
  Shell features (pipes, redirects, globs) are not available in "cmd".

FLAGS
  -h, --help                        Show this help (after a path: help for
                                    that path)
  -v, --verbose                     Show module paths (flag of "list" only)
  -r, --recurse                     Recursively list namespace subtree
                                    (flag of "list" only)

  Short flags of a command are only those its module declares ("short").

EXAMPLES
  futi docker build -h             Show help for "docker build"
  futi docker build --tag myapp    Run "build" command in "docker" module
  futi help docker build           Show help for "docker build"
  futi dev deploy staging           Module "dev.deploy", command "staging"
  futi k8s logs --follow            Bool flag --follow
  futi db migrate up                Nested command: "migrate up" in "db" module
  futi list db migrate              List subcommands of "migrate"
  futi echo_mod echo hello world    Pass arguments to command "echo"
  futi echo_mod hello world         Invoke module's default command with args
  futi echo_mod -- build            "--": "build" is an argument, not a command
  futi list images -r               Recursively list all image modules
  futi list docker -v               Show docker commands with manifest path

  Exit codes: futi propagates the exit code of the called command.
  If docker build exits with 5, futi exits with 5.

ENVIRONMENT
  FUTI_MODULES_DIR                  Override modules directory (discovery
                                    uses it when no config.yaml exists;
                                    install without --local writes there;
                                    default ~/.config/futi/modules/)
`)
}
