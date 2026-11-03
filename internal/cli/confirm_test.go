package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/core"
)

func TestStripGlobalFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantRest []string
		wantG    globalFlags
		wantErr  string
	}{
		{"yes then dry-run", []string{"futi", "--yes", "--dry-run", "db", "drop"}, []string{"futi", "db", "drop"}, globalFlags{dryRun: true, yes: true}, ""},
		{"dry-run then short yes", []string{"futi", "--dry-run", "-y", "db", "drop"}, []string{"futi", "db", "drop"}, globalFlags{dryRun: true, yes: true}, ""},
		{"duplicate yes", []string{"futi", "--yes", "--yes", "x"}, nil, globalFlags{}, "futi: duplicate flag --yes"},
		{"short and long yes", []string{"futi", "-y", "--yes", "x"}, nil, globalFlags{}, "futi: duplicate flag --yes"},
		{"flag after path is not global", []string{"futi", "db", "drop", "--yes"}, []string{"futi", "db", "drop", "--yes"}, globalFlags{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rest, g, err := stripGlobalFlags(tc.args)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("stripGlobalFlags(%q) err = %v, want %q", tc.args, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("stripGlobalFlags(%q) err = %v", tc.args, err)
			}
			if !reflect.DeepEqual(rest, tc.wantRest) || g != tc.wantG {
				t.Fatalf("stripGlobalFlags(%q) = %q, %+v; want %q, %+v", tc.args, rest, g, tc.wantRest, tc.wantG)
			}
		})
	}
}

func TestYesScope(t *testing.T) {
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	const yesMsg = "futi: --yes applies only to module commands\n"
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"list", []string{"futi", "--yes", "list"}, yesMsg},
		{"help", []string{"futi", "--yes", "help", "x"}, yesMsg},
		{"modules", []string{"futi", "--yes", "modules", "list"}, yesMsg},
		{"version", []string{"futi", "--yes", "--version"}, yesMsg},
		{"short flag", []string{"futi", "-y", "list"}, yesMsg},
		{"both flags report dry-run", []string{"futi", "--yes", "--dry-run", "list"}, errDryRunScope + "\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			code := Main(tc.args, strings.NewReader(""), &out, &errb, "")
			if code != 1 || errb.String() != tc.want {
				t.Fatalf("Main(%q) = %d, stderr %q; want 1, %q", tc.args, code, errb.String(), tc.want)
			}
		})
	}
}

// countingReader records whether it was read.
type countingReader struct {
	r    io.Reader
	read bool
}

func (c *countingReader) Read(p []byte) (int, error) {
	c.read = true
	return c.r.Read(p)
}

func TestConfirmPrompt(t *testing.T) {
	req := core.ConfirmRequest{Path: []string{"db", "drop"}, Argv: []string{"dropdb", "main"}}
	const prompt = "futi db drop is destructive and will run:\n  dropdb main\nRun it? [y/N] "

	t.Run("yes skips everything", func(t *testing.T) {
		in := &countingReader{r: strings.NewReader("n\n")}
		var errb bytes.Buffer
		ok, err := newConfirm(true, in, &errb, func() bool { return true })(req)
		if err != nil || !ok || in.read || errb.Len() != 0 {
			t.Fatalf("ok=%v err=%v read=%v stderr=%q, want true, nil, no read, no output", ok, err, in.read, errb.String())
		}
	})

	t.Run("no terminal refuses", func(t *testing.T) {
		in := &countingReader{r: strings.NewReader("y\n")}
		var errb bytes.Buffer
		ok, err := newConfirm(false, in, &errb, func() bool { return false })(req)
		const want = `"futi db drop" is destructive and needs confirmation (no terminal on stdin): rerun as "futi --yes db drop ..."`
		if ok || !errors.Is(err, errNeedsConfirmation) || err.Error() != want {
			t.Fatalf("ok=%v err=%v, want false and %q", ok, err, want)
		}
		if in.read {
			t.Fatal("stdin was read")
		}
	})

	t.Run("answer leaves the rest of stdin", func(t *testing.T) {
		in := strings.NewReader("y\nDATA")
		var errb bytes.Buffer
		ok, err := newConfirm(false, in, &errb, func() bool { return true })(req)
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v, want true", ok, err)
		}
		if errb.String() != prompt {
			t.Fatalf("stderr = %q, want %q", errb.String(), prompt)
		}
		rest, _ := io.ReadAll(in)
		if string(rest) != "DATA" {
			t.Fatalf("rest of stdin = %q, want DATA", rest)
		}
	})

	answers := []struct {
		name, input string
		want        bool
	}{
		{"YES with spaces", " YES \n", true},
		{"n", "n\n", false},
		{"empty line", "\n", false},
		{"EOF", "", false},
		{"y without newline", "y", true},
		{"garbage", "maybe\n", false},
	}
	for _, a := range answers {
		t.Run(a.name, func(t *testing.T) {
			var errb bytes.Buffer
			ok, err := newConfirm(false, strings.NewReader(a.input), &errb, func() bool { return true })(req)
			if err != nil || ok != a.want {
				t.Fatalf("input %q: ok=%v err=%v, want %v", a.input, ok, err, a.want)
			}
		})
	}
}

const destructiveDB = `commands:
  drop:
    destructive: true
    flags:
      - {name: marker, required: true}
      - {name: out, required: true}
    cmd: ["sh", "-c", 'touch "$1"; cat > "$2"', "sh", "{{.marker}}", "{{.out}}"]
  ping:
    flags:
      - {name: marker, required: true}
    cmd: ["touch", "{{.marker}}"]
`

// S29: confirmation of destructive commands.
func TestScenarioS29Confirmation(t *testing.T) {
	setup := func(t *testing.T) (marker, out string) {
		t.Helper()
		mods := scenarioDirWith(t, map[string]string{"db.yaml": destructiveDB})
		t.Setenv("FUTI_MODULES_DIR", mods)
		t.Setenv("HOME", t.TempDir())
		t.Chdir(t.TempDir())
		dir := t.TempDir()
		return filepath.Join(dir, "marker"), filepath.Join(dir, "out")
	}
	run := func(stdin string, args ...string) (string, string, int) {
		var o, e bytes.Buffer
		code := Main(append([]string{"futi"}, args...), strings.NewReader(stdin), &o, &e, "")
		return o.String(), e.String(), code
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	t.Run("no terminal refuses", func(t *testing.T) {
		marker, out := setup(t)
		setStdinTerminal(t, false)
		_, stderr, code := run("y\n", "db", "drop", "--marker", marker, "--out", out)
		if code != 1 || !strings.HasPrefix(stderr, `futi: "futi db drop" is destructive`) || exists(marker) {
			t.Fatalf("code=%d stderr=%q marker=%v, want 1, hint, no marker", code, stderr, exists(marker))
		}
	})
	t.Run("yes runs", func(t *testing.T) {
		marker, out := setup(t)
		setStdinTerminal(t, false)
		_, stderr, code := run("", "--yes", "db", "drop", "--marker", marker, "--out", out)
		if code != 0 || !exists(marker) {
			t.Fatalf("code=%d stderr=%q marker=%v, want 0 and marker", code, stderr, exists(marker))
		}
	})
	t.Run("terminal no aborts", func(t *testing.T) {
		marker, out := setup(t)
		setStdinTerminal(t, true)
		_, stderr, code := run("n\n", "db", "drop", "--marker", marker, "--out", out)
		if code != 1 || !strings.HasSuffix(stderr, "futi: aborted\n") || exists(marker) {
			t.Fatalf("code=%d stderr=%q marker=%v, want 1, aborted, no marker", code, stderr, exists(marker))
		}
	})
	t.Run("terminal yes passes the rest of stdin to the child", func(t *testing.T) {
		marker, out := setup(t)
		setStdinTerminal(t, true)
		_, stderr, code := run("y\nDATA", "db", "drop", "--marker", marker, "--out", out)
		got, _ := os.ReadFile(out)
		if code != 0 || string(got) != "DATA" {
			t.Fatalf("code=%d stderr=%q out=%q, want 0 and DATA", code, stderr, got)
		}
	})
	t.Run("dry-run needs no terminal", func(t *testing.T) {
		marker, out := setup(t)
		setStdinTerminal(t, false)
		stdout, stderr, code := run("", "--dry-run", "db", "drop", "--marker", marker, "--out", out)
		if code != 0 || !strings.HasPrefix(stdout, "sh -c 'touch \"$1\"; cat > \"$2\"' sh ") || exists(marker) {
			t.Fatalf("code=%d stdout=%q stderr=%q marker=%v", code, stdout, stderr, exists(marker))
		}
	})
	t.Run("yes after the path is an unknown flag", func(t *testing.T) {
		marker, out := setup(t)
		setStdinTerminal(t, false)
		_, stderr, code := run("", "db", "drop", "--marker", marker, "--out", out, "--yes")
		if code != 1 || !strings.Contains(stderr, "unknown flag --yes") || exists(marker) {
			t.Fatalf("code=%d stderr=%q marker=%v, want 1 and unknown flag", code, stderr, exists(marker))
		}
	})
	t.Run("non-destructive runs without a question", func(t *testing.T) {
		marker, _ := setup(t)
		setStdinTerminal(t, false)
		_, stderr, code := run("", "db", "ping", "--marker", marker)
		if code != 0 || !exists(marker) {
			t.Fatalf("code=%d stderr=%q marker=%v, want 0 and marker", code, stderr, exists(marker))
		}
	})
}
