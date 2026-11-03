package execmodule

import (
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/inputs"
)

func TestFormatCommandHelpGolden(t *testing.T) {
	tests := []struct {
		name string
		path string
		cmd  *Command
		want string
	}{
		{
			name: "docker build",
			path: "docker build",
			cmd: &Command{
				Description: "Build an image",
				Cmd:         cmdText("docker build"),
				Args:        []inputs.Spec{{Name: "context", Description: "Build context directory", Default: ".", HasDefault: true}},
				Flags: []inputs.Spec{
					{Name: "tag", Required: true, Short: "t", Flag: true},
					{Name: "file", Default: "Dockerfile", HasDefault: true, Flag: true},
				},
			},
			want: `Build an image

usage: futi docker build [flags] [context]
  context  Build context directory (default ".")
flags:
  -t, --tag string   (required)
      --file string  (default "Dockerfile")
  -h, --help
`,
		},
		{
			name: "docker run",
			path: "docker run",
			cmd: &Command{
				Cmd: cmdText("docker run"),
				Args: []inputs.Spec{
					{Name: "image", Required: true, Description: "Image to run"},
					{Name: "docker_args", Rest: true, Description: "Options of docker run"},
				},
			},
			want: `usage: futi docker run <image> [docker_args...]
  image           Image to run (required)
  docker_args...  Options of docker run (passed verbatim)
flags:
  -h, --help
`,
		},
		{
			name: "k8s logs",
			path: "k8s logs",
			cmd: &Command{
				Description: "Show pod logs",
				Cmd:         cmdText("kubectl logs"),
				Args:        []inputs.Spec{{Name: "pod", Required: true, Description: "Pod name"}},
				Flags: []inputs.Spec{
					{Name: "follow", Type: "bool", Short: "f", Description: "Stream logs", Flag: true},
					{Name: "tail", Type: "int", Default: "100", HasDefault: true, Flag: true},
				},
			},
			want: `Show pod logs

usage: futi k8s logs [flags] <pod>
  pod  Pod name (required)
flags:
  -f, --follow    Stream logs
      --tail int  (default 100)
  -h, --help
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatCommandHelp(tt.path, tt.cmd, FrontendCLI); got != tt.want {
				t.Errorf("FormatCommandHelp:\ngot:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestFormatCommandHelpShapes(t *testing.T) {
	tests := []struct {
		name string
		cmd  *Command
		want string
	}{
		{
			name: "variadic args and flag, flag name with underscore",
			cmd: &Command{
				Cmd: cmdText("x"),
				Args: []inputs.Spec{
					{Name: "src", Required: true, Variadic: true},
					{Name: "dst", Required: true},
				},
				Flags: []inputs.Spec{{Name: "ignore_case", Type: "bool", Flag: true}, {Name: "env", Variadic: true, Flag: true}},
			},
			want: `usage: futi m c [flags] <src>... <dst>
  src...  (required)
  dst     (required)
flags:
      --ignore-case
      --env string...
  -h, --help
`,
		},
		{
			name: "optional variadic",
			cmd:  &Command{Cmd: cmdText("echo"), Args: []inputs.Spec{{Name: "words", Variadic: true}}},
			want: `usage: futi m c [words...]
  words...
flags:
  -h, --help
`,
		},
		{
			name: "list-form cmd and no inputs",
			cmd:  &Command{Cmd: CmdSpec{List: []string{"echo", "a b"}, IsList: true}},
			want: `usage: futi m c
flags:
  -h, --help
`,
		},
		{
			name: "branch without cmd",
			cmd: &Command{Commands: map[string]*Command{
				"up":   {Description: "Apply", Cmd: cmdText("x")},
				"down": {Cmd: cmdText("x")},
			}},
			want: `usage: futi m c <subcommand>
subcommands:
  down
  up    Apply
`,
		},
		{
			name: "subcommands with a string arg warn",
			cmd: &Command{
				Cmd:      cmdText("x"),
				Args:     []inputs.Spec{{Name: "steps"}},
				Commands: map[string]*Command{"up": {Cmd: cmdText("x")}},
			},
			want: `usage: futi m c [steps]
  steps
flags:
  -h, --help
subcommands:
  up
warning: command "m c": words up are subcommands; pass such a value after --
`,
		},
		{
			name: "subcommands with an int arg do not warn",
			cmd: &Command{
				Cmd:      cmdText("x"),
				Args:     []inputs.Spec{{Name: "steps", Type: "int"}},
				Commands: map[string]*Command{"up": {Cmd: cmdText("x")}},
			},
			want: `usage: futi m c [steps]
  steps
flags:
  -h, --help
subcommands:
  up
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatCommandHelp("m c", tt.cmd, FrontendCLI); got != tt.want {
				t.Errorf("FormatCommandHelp:\ngot:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// TestFormatCommandHelpStripsControlChars: ESC/OSC из YAML не доходят до терминала,
// а многострочный description сохраняет переводы строк (audit F18).
func TestFormatCommandHelpStripsControlChars(t *testing.T) {
	cmd := &Command{
		Description: "line1\nline2\x1b]0;pwned\x07",
		Cmd:         cmdText("echo"),
		Args:        []inputs.Spec{{Name: "x", Default: "d\x1b[31m", HasDefault: true, Description: "pd\x1b[0m"}},
		Flags:       []inputs.Spec{{Name: "f\x1b[1m", Short: "s\x1b[1m", Flag: true}},
		Commands:    map[string]*Command{"run": {Description: "d\x1b[2J", Cmd: cmdText("echo")}},
	}
	out := FormatCommandHelp("demo\x1b[1m run", cmd, FrontendCLI)
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\x07") {
		t.Fatalf("output contains control chars: %q", out)
	}
	if !strings.HasPrefix(out, "line1\nline2]0;pwned\n\nusage: futi demo[1m run") {
		t.Fatalf("multiline description not preserved: %q", out)
	}
}

// TestFormatHelpAlignmentUsesSanitizedWidth: ширина колонки считается по
// очищенным именам (audit F18, review).
func TestFormatHelpAlignmentUsesSanitizedWidth(t *testing.T) {
	cmd := &Command{Commands: map[string]*Command{
		"a\x1b[2Jbcdef": {Description: "d1"},
		"x":             {Description: "d2"},
	}}
	out := FormatCommandHelp("m", cmd, FrontendCLI)
	if !strings.Contains(out, "  x          d2\n") || !strings.Contains(out, "  a[2Jbcdef  d1\n") {
		t.Fatalf("misaligned: %q", out)
	}
}

// TestFormatHelpInputAlignmentUsesSanitizedWidth: ESC в имени входа не
// сдвигает колонку описания.
func TestFormatHelpInputAlignmentUsesSanitizedWidth(t *testing.T) {
	cmd := &Command{
		Cmd: cmdText("echo"),
		Args: []inputs.Spec{
			{Name: "a\x1b[2Jbcdef", Description: "d1"},
			{Name: "x", Description: "d2"},
		},
	}
	// Очищенное имя "a[2Jbcdef" — 9 символов, "x" дополняется 8 пробелами.
	out := FormatCommandHelp("m c", cmd, FrontendCLI)
	if !strings.Contains(out, "  a[2Jbcdef  d1\n") || !strings.Contains(out, "  x          d2\n") {
		t.Errorf("misaligned: %q", out)
	}
}

func TestHelpChoicesAndPathTypes(t *testing.T) {
	cmd := &Command{
		Cmd: cmdText("x"),
		Args: []inputs.Spec{
			{Name: "env", Choices: []string{"dev", "prod"}},
		},
		Flags: []inputs.Spec{
			{Name: "level", Flag: true, Choices: []string{"debug", "info", "warn"}, HasDefault: true, Default: "info"},
			{Name: "out", Type: "file", Flag: true},
			{Name: "dir", Type: "dir", Flag: true, HasDefault: true, Default: "build"},
			{Name: "lv", Flag: true, Variadic: true, Choices: []string{"debug", "info"}},
		},
	}
	got := FormatCommandHelp("m c", cmd, FrontendCLI)
	for _, want := range []string{
		"--level debug|info|warn",
		`(default "info")`,
		"--out file",
		"--dir dir",
		`(default "build")`,
		"--lv debug|info...",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("help lacks %q:\n%s", want, got)
		}
	}
	var envLine string
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "env") {
			envLine = l
		}
	}
	if !strings.Contains(envLine, "(dev|prod)") {
		t.Errorf("env arg line %q lacks (dev|prod):\n%s", envLine, got)
	}
}

func TestFormatCommandHelpHidesSubcommands(t *testing.T) {
	cmd := &Command{
		Args: []inputs.Spec{{Name: "target"}},
		Commands: map[string]*Command{
			"up":   {Description: "Start", Expose: []Frontend{FrontendMCP}},
			"down": {Description: "Stop"},
		},
	}
	cli := FormatCommandHelp("m", cmd, FrontendCLI)
	if !strings.Contains(cli, "  down") || strings.Contains(cli, "  up") {
		t.Errorf("CLI help must list down and hide up:\n%s", cli)
	}
	if !strings.Contains(cli, "warning:") {
		t.Errorf("CLI help lacks the shadow warning:\n%s", cli)
	}
	if mcp := FormatCommandHelp("m", cmd, FrontendMCP); !strings.Contains(mcp, "  up") {
		t.Errorf("MCP help must list up:\n%s", mcp)
	}

	onlyHidden := &Command{Commands: map[string]*Command{"up": {Expose: []Frontend{FrontendMCP}}}}
	if got := FormatCommandHelp("m", onlyHidden, FrontendCLI); strings.Contains(got, "subcommands:") {
		t.Errorf("no visible subcommands, section printed:\n%s", got)
	}
}
