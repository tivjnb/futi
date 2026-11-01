package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// runFutiEnv запускает собранный futi в cwd с явным окружением и возвращает
// stdout, stderr и код выхода.
func runFutiEnv(t *testing.T, cwd string, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(builtFutiBinary(t), args...)
	cmd.Dir = cwd
	cmd.Env = env
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run futi %v: %v", args, err)
		}
		code = ee.ExitCode()
	}
	return out.String(), errBuf.String(), code
}

// cliEnv — изолированное окружение: всё в temp-каталогах.
func cliEnv(home, xdgCache, modules string) []string {
	return []string{
		"HOME=" + home,
		"USERPROFILE=" + home,
		"XDG_CACHE_HOME=" + xdgCache,
		"FUTI_MODULES_DIR=" + modules,
		"PATH=" + os.Getenv("PATH"),
	}
}

func newCLIDirs(t *testing.T) (home, cwd string, env []string) {
	t.Helper()
	root := t.TempDir()
	home = filepath.Join(root, "home")
	cwd = filepath.Join(root, "cwd")
	modules := filepath.Join(root, "modules")
	for _, d := range []string{home, cwd, modules} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home, cwd, cliEnv(home, filepath.Join(root, "xdg"), modules)
}

func TestCLIBrokenLocalFutiYaml(t *testing.T) {
	_, cwd, env := newCLIDirs(t)
	if err := os.WriteFile(filepath.Join(cwd, "futi.yaml"), []byte("name: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		args       []string
		code       int
		stderrPref string
		stderrIs   string
	}{
		{[]string{"cache", "status"}, 0, "", ""},
		{[]string{"-h"}, 0, "", ""},
		{[]string{"list"}, 1, "source: local source: ", ""},
		{[]string{"--bogus"}, 1, "parse: ", ""},
		{[]string{"--dry-run", "list"}, 1, "", "futi: --dry-run applies only to module commands\n"},
	}
	for _, c := range cases {
		_, stderr, code := runFutiEnv(t, cwd, env, c.args...)
		if code != c.code {
			t.Errorf("futi %v: code = %d, want %d (stderr: %q)", c.args, code, c.code, stderr)
		}
		if c.stderrPref != "" && !strings.HasPrefix(stderr, c.stderrPref) {
			t.Errorf("futi %v: stderr = %q, want prefix %q", c.args, stderr, c.stderrPref)
		}
		if c.stderrIs != "" && stderr != c.stderrIs {
			t.Errorf("futi %v: stderr = %q, want %q", c.args, stderr, c.stderrIs)
		}
	}
}

func TestCLICachePathErrWarning(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CACHE_HOME действует на Linux")
	}
	home, cwd, env := newCLIDirs(t)
	for i, e := range env {
		if strings.HasPrefix(e, "XDG_CACHE_HOME=") {
			env[i] = "XDG_CACHE_HOME=rel/cache"
		}
	}
	_ = home
	_, stderr, code := runFutiEnv(t, cwd, env, "list")
	if code != 0 {
		t.Fatalf("list: code = %d, stderr: %q", code, stderr)
	}
	const warn = "cache: cache path: get cache dir: "
	if n := strings.Count(stderr, warn); n != 1 {
		t.Errorf("list: %q встречается %d раз, want 1; stderr: %q", warn, n, stderr)
	}
	if !strings.Contains(stderr, "(using in-memory only)") {
		t.Errorf("list: stderr без (using in-memory only): %q", stderr)
	}
	stdout, _, code := runFutiEnv(t, cwd, env, "cache", "status")
	if code != 0 {
		t.Fatalf("cache status: code = %d", code)
	}
	if !strings.Contains(stdout, "Storage: in-memory (no persistent cache)") {
		t.Errorf("cache status: stdout = %q", stdout)
	}
}

func TestCLICacheConfigWarningOnce(t *testing.T) {
	home, cwd, env := newCLIDirs(t)
	cfgDir := filepath.Join(home, ".config", "futi")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("cache:\n  refresh: bogus\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"list"},
		{"cache", "status"},
		{"cache", "refresh"},
		{"cache", "clear"},
		{"modules", "list"},
		{"__complete", ""},
	} {
		_, stderr, code := runFutiEnv(t, cwd, env, args...)
		if code != 0 {
			t.Errorf("futi %q: code = %d, want 0; stderr: %q", args, code, stderr)
		}
		if n := strings.Count(stderr, "cache config: "); n != 1 {
			t.Errorf("futi %q: %q встречается %d раз, want 1; stderr: %q", args, "cache config: ", n, stderr)
		}
	}
}

func TestCLINoHome(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("поведение пустого HOME проверяется на Linux")
	}
	_, cwd, _ := newCLIDirs(t)
	env := cliEnv("", "", "")
	_, stderr, code := runFutiEnv(t, cwd, env, "list")
	if code != 1 {
		t.Errorf("list: code = %d, want 1 (stderr: %q)", code, stderr)
	}
	if !strings.HasPrefix(stderr, "source: config path: ") {
		t.Errorf("list: stderr = %q, want prefix %q", stderr, "source: config path: ")
	}
}

// TestCLIInvalidModuleIsNotLoggedPerCall: a module that does not load must not
// make every other call print a timestamped log line; running the module
// itself reports its error once.
func TestCLIInvalidModuleIsNotLoggedPerCall(t *testing.T) {
	_, cwd, env := newCLIDirs(t)
	modules := strings.TrimPrefix(env[3], "FUTI_MODULES_DIR=")
	if err := os.WriteFile(filepath.Join(modules, "good.yaml"), []byte("cmd: echo ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modules, "bad.yaml"), []byte("commands:\n  empty: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runFutiEnv(t, cwd, env, "good")
	if code != 0 || strings.TrimSpace(stdout) != "ok" {
		t.Fatalf("good: code = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	if strings.Contains(stderr, "modulecache:") {
		t.Errorf("good: stderr has a cache log line: %q", stderr)
	}
	_, stderr, code = runFutiEnv(t, cwd, env, "bad")
	if code == 0 {
		t.Fatalf("bad: code = 0, want failure")
	}
	if strings.Contains(stderr, "modulecache:") {
		t.Errorf("bad: stderr has a cache log line: %q", stderr)
	}
	if n := strings.Count(stderr, "empty"); n != 1 {
		t.Errorf("bad: the error mentions the command %d times, want 1; stderr: %q", n, stderr)
	}
}

// TestCLICacheSaveFailureIsWarning: a cache that cannot be saved is reported
// as a plain "warning:" line, not a timestamped log record.
func TestCLICacheSaveFailureIsWarning(t *testing.T) {
	home, cwd, env := newCLIDirs(t)
	modules := strings.TrimPrefix(env[3], "FUTI_MODULES_DIR=")
	if err := os.WriteFile(filepath.Join(modules, "good.yaml"), []byte("cmd: echo ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// XDG_CACHE_HOME points under a regular file: the cache dir cannot be made.
	blocker := filepath.Join(home, "blocker")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	env[2] = "XDG_CACHE_HOME=" + filepath.Join(blocker, "xdg")
	_, stderr, code := runFutiEnv(t, cwd, env, "good")
	if code != 0 {
		t.Fatalf("good: code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stderr, "warning: modulecache: refresh: save: ") {
		t.Errorf("stderr lacks the save warning: %q", stderr)
	}
	if regexp.MustCompile(`(?m)^\d{4}/\d\d/\d\d `).MatchString(stderr) {
		t.Errorf("stderr has a timestamped log line: %q", stderr)
	}
}
