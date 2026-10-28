package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	bi := func(v string, settings ...debug.BuildSetting) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Version: v}, Settings: settings}
	}
	rev := debug.BuildSetting{Key: "vcs.revision", Value: "84ec624abcdef0123"}
	dirty := debug.BuildSetting{Key: "vcs.modified", Value: "true"}
	cases := []struct {
		name string
		ld   string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{"ldflags выигрывает", "v0.1.0", bi("(devel)", rev), true, "futi v0.1.0"},
		{"go install @tag", "", bi("v0.1.0"), true, "futi v0.1.0"},
		{"локальная сборка", "", bi("(devel)", rev), true, "futi (devel) 84ec624"},
		{"локальная сборка с правками", "", bi("(devel)", rev, dirty), true, "futi (devel) 84ec624+dirty"},
		{"без vcs", "", bi("(devel)"), true, "futi (devel)"},
		{"нет build info", "", nil, false, "futi (unknown)"},
	}
	for _, c := range cases {
		if got := versionString(c.ld, c.info, c.ok); got != c.want {
			t.Errorf("%s: versionString = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestFutiVersionFlag: быстрый путь не читает конфиг — битый config.yaml не мешает.
func TestFutiVersionFlag(t *testing.T) {
	bin := builtFutiBinary(t)
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "futi")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("sources: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, string, int) {
		cmd := exec.Command(bin, args...)
		cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "FUTI_MODULES_DIR=")
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		code := 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return out.String(), errb.String(), code
	}

	// Контроль: битый конфиг действительно ломает обычный путь.
	if _, _, code := run("list"); code == 0 {
		t.Fatal("futi list with broken config.yaml: exit 0, test setup does not break config")
	}

	stdout, stderr, code := run("--version")
	if code != 0 || !strings.HasPrefix(stdout, "futi ") || stderr != "" {
		t.Errorf("futi --version: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	_, stderr, code = run("--version", "extra")
	if code != 1 || !strings.Contains(stderr, "--version takes no arguments") {
		t.Errorf("futi --version extra: code=%d stderr=%q", code, stderr)
	}
}
