package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/metayaml"
)

func TestMainVersion(t *testing.T) {
	var out, errb bytes.Buffer
	code := Main([]string{"futi", "--version"}, nil, &out, &errb, "v9.9.9")
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if out.String() != "futi v9.9.9\n" {
		t.Fatalf("stdout = %q, want %q", out.String(), "futi v9.9.9\n")
	}
	if errb.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", errb.String())
	}
}

func TestMainNoArgs(t *testing.T) {
	var out, errb bytes.Buffer
	code := Main([]string{"futi"}, nil, &out, &errb, "")
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "USAGE\n  futi <namespace...>") {
		t.Fatalf("stderr = %q, want usage", errb.String())
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
}

func TestMainVersionExtraArg(t *testing.T) {
	var out, errb bytes.Buffer
	code := Main([]string{"futi", "--version", "x"}, nil, &out, &errb, "v1")
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if errb.String() != "futi: --version takes no arguments\n" {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestMainModulesErrorToStderrParam(t *testing.T) {
	t.Chdir(t.TempDir())
	var out, errb bytes.Buffer
	code := Main([]string{"futi", "modules", "init", "--no-such-flag"}, nil, &out, &errb, "")
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.HasPrefix(errb.String(), "modules init: ") {
		t.Fatalf("stderr = %q, want prefix %q", errb.String(), "modules init: ")
	}
}

func TestCLIStreamsUsesGivenWriters(t *testing.T) {
	var o, e bytes.Buffer
	r := strings.NewReader("")
	s := cliStreams(&o, &e, r)
	if s.Err != &e {
		t.Fatalf("Err = %v, want given writer", s.Err)
	}
	if s.Stdin != r {
		t.Fatalf("Stdin = %v, want given reader", s.Stdin)
	}
}

func TestMainDryRunRejected(t *testing.T) {
	var out, errb bytes.Buffer
	code := Main([]string{"futi", "--dry-run", "list"}, nil, &out, &errb, "")
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if errb.String() != errDryRunScope+"\n" {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestMainModulesUpdateWarningGoesToGivenStderr(t *testing.T) {
	root := t.TempDir()
	target := installFixtureModule(t, t.TempDir(), "qr", &metayaml.Meta{
		Lockfile: metayaml.Lockfile{Source: "/src/qr", Type: "local", Hash: "sha256:ab"},
	})
	if err := os.Symlink(filepath.Join(target, "qr"), filepath.Join(root, "qr")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv("FUTI_MODULES_DIR", root)
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())

	var out, errb bytes.Buffer
	code := Main([]string{"futi", "modules", "update"}, nil, &out, &errb, "")
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr = %q", code, errb.String())
	}
	if want := "Warning: skipped qr: module directory is a symlink\n"; errb.String() != want {
		t.Fatalf("stderr = %q, want %q", errb.String(), want)
	}
}
