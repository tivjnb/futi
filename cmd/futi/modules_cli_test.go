package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/metayaml"
)

func TestCLIStreamsWiring(t *testing.T) {
	var out bytes.Buffer
	setStdinTerminal(t, true)
	s := cliStreams(&out, strings.NewReader("y\n"))
	if s.Out != io.Writer(&out) || s.Err != io.Writer(os.Stderr) || s.Stdin != io.Reader(os.Stdin) {
		t.Fatalf("cliStreams wiring: Out=%v Err=%v Stdin=%v", s.Out, s.Err, s.Stdin)
	}
	if !s.Prompt.Interactive() {
		t.Fatal("Interactive() must follow stdinIsTerminal")
	}
	if line, _ := s.Prompt.ReadLine(); line != "y\n" {
		t.Fatalf("ReadLine() = %q, want answers from stdin argument", line)
	}
}

func TestRunModulesInfoWithMeta(t *testing.T) {
	root := t.TempDir()
	meta := &metayaml.Meta{
		Lockfile: metayaml.Lockfile{
			Source: "/src/qr", Type: "local", Hash: "sha256:ab",
			InstalledAt: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC),
		},
		About: &metayaml.About{Author: "foo@example.com", License: "MIT"},
	}
	installFixtureModule(t, root, "qr", meta)

	t.Setenv("FUTI_MODULES_DIR", root)
	t.Chdir(t.TempDir())

	var b bytes.Buffer
	if err := runModulesInfo(&b, newTestRuntime(t), []string{"qr"}); err != nil {
		t.Fatalf("runModulesInfo: %v", err)
	}
	out := b.String()
	for _, want := range []string{"Path:", "Source: /src/qr", "Type: local", "Hash: sha256:ab",
		"Installed at: 2026-09-21 10:00:00", "Author: foo@example.com", "License: MIT"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunModulesInfoWithoutMeta(t *testing.T) {
	root := installFixtureModule(t, t.TempDir(), "qr", nil)
	t.Setenv("FUTI_MODULES_DIR", root)
	t.Chdir(t.TempDir())

	var b bytes.Buffer
	if err := runModulesInfo(&b, newTestRuntime(t), []string{"qr"}); err != nil {
		t.Fatalf("runModulesInfo: %v", err)
	}
	if !strings.Contains(b.String(), "Installed: no") {
		t.Fatalf("expected 'Installed: no' note:\n%s", b.String())
	}
}

func TestRunModulesPathUsageError(t *testing.T) {
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Chdir(t.TempDir())
	if err := runModulesPath(nil, newTestRuntime(t), nil); err == nil {
		t.Fatal("expected usage error")
	}
}

// TestRunModulesInfoStripsControlChars: ESC в lockfile/about не доходит до терминала (audit F18).

func TestRunModulesInfoStripsControlChars(t *testing.T) {
	root := t.TempDir()
	meta := &metayaml.Meta{
		Lockfile: metayaml.Lockfile{Source: "/src\x1b]0;pwned\x07/qr", Type: "local"},
		About:    &metayaml.About{Author: "ev\x1b[2Jil", Tags: []string{"t\x1b[1m"}},
	}
	installFixtureModule(t, root, "qr", meta)

	t.Setenv("FUTI_MODULES_DIR", root)
	t.Chdir(t.TempDir())

	var b bytes.Buffer
	if err := runModulesInfo(&b, newTestRuntime(t), []string{"qr"}); err != nil {
		t.Fatalf("runModulesInfo: %v", err)
	}
	out := b.String()
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\x07") {
		t.Fatalf("output contains control chars: %q", out)
	}
	for _, want := range []string{"Source: /src]0;pwned/qr", "Author: ev[2Jil"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunModulesListOutput(t *testing.T) {
	root := t.TempDir()
	installFixtureModule(t, root, "qr", &metayaml.Meta{Lockfile: metayaml.Lockfile{Source: "/src/qr"}})
	t.Setenv("FUTI_MODULES_DIR", root)
	t.Chdir(t.TempDir()) // без ./.futi

	var b bytes.Buffer
	if err := runModulesList(&b, newTestRuntime(t), nil); err != nil {
		t.Fatalf("runModulesList: %v", err)
	}
	out := b.String()
	if !strings.Contains(out, "qr") || !strings.Contains(out, "/src/qr") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestRunModulesListEmpty(t *testing.T) {
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Chdir(t.TempDir())
	var b bytes.Buffer
	if err := runModulesList(&b, newTestRuntime(t), nil); err != nil {
		t.Fatalf("runModulesList: %v", err)
	}
	if !strings.Contains(b.String(), "No installed modules found") {
		t.Fatalf("unexpected output:\n%s", b.String())
	}
}

// TestRunModulesListStripsControlChars: ESC в lockfile не доходит до терминала (audit F18).

func TestRunModulesListStripsControlChars(t *testing.T) {
	root := t.TempDir()
	installFixtureModule(t, root, "qr", &metayaml.Meta{Lockfile: metayaml.Lockfile{
		Source: "/src\x1b]0;pwned\x07/qr", Type: "git", Ref: "v1\x1b[2J",
	}})
	t.Setenv("FUTI_MODULES_DIR", root)
	t.Chdir(t.TempDir())

	var b bytes.Buffer
	if err := runModulesList(&b, newTestRuntime(t), nil); err != nil {
		t.Fatalf("runModulesList: %v", err)
	}
	out := b.String()
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\x07") {
		t.Fatalf("output contains control chars: %q", out)
	}
	if !strings.Contains(out, "/src]0;pwned/qr") || !strings.Contains(out, "v1[2J") {
		t.Fatalf("output lost visible text:\n%s", out)
	}
}

// F62: ref из lockfile не-git источника не показывается как версия.

func TestModulesListRefOnlyForGit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", root)
	t.Chdir(t.TempDir())
	installFixtureModule(t, root, "old", &metayaml.Meta{Lockfile: metayaml.Lockfile{Source: "/src/old", Type: "local", Ref: "v9"}})
	installFixtureModule(t, root, "gitm", &metayaml.Meta{Lockfile: metayaml.Lockfile{Source: "https://h/r.git", Type: "git", Ref: "v7"}})

	var b bytes.Buffer
	if err := runModulesList(&b, newTestRuntime(t), nil); err != nil {
		t.Fatalf("runModulesList: %v", err)
	}
	if strings.Contains(b.String(), "v9") {
		t.Fatalf("list shows ref of local module:\n%s", b.String())
	}
	if !strings.Contains(b.String(), "v7") {
		t.Fatalf("list must show ref of git module:\n%s", b.String())
	}
}

func TestModulesInfoRefOnlyForGit(t *testing.T) {
	root := installFixtureModule(t, t.TempDir(), "old", &metayaml.Meta{Lockfile: metayaml.Lockfile{Source: "/src/old", Type: "archive", Ref: "v9"}})
	t.Setenv("FUTI_MODULES_DIR", root)
	t.Chdir(t.TempDir())

	var b bytes.Buffer
	if err := runModulesInfo(&b, newTestRuntime(t), []string{"old"}); err != nil {
		t.Fatalf("runModulesInfo: %v", err)
	}
	if strings.Contains(b.String(), "v9") {
		t.Fatalf("info shows ref of archive module:\n%s", b.String())
	}
}

func TestModulesRootsInvalidConfig(t *testing.T) {
	writeFutiConfig(t, "sources: [\n")
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Chdir(t.TempDir())

	opts, err := core.DefaultOptions()
	if err != nil {
		t.Fatal(err)
	}
	_, err = core.NewRuntime(opts)
	if err == nil || !strings.Contains(err.Error(), "config:") {
		t.Fatalf("NewRuntime err = %v, want config: error", err)
	}
}

// TestRunInitNoArgsError проверяет, что без path возвращается ошибка.
func TestRunInitNoArgsError(t *testing.T) {
	t.Chdir(t.TempDir())

	var buf bytes.Buffer
	err := runModulesInit(&buf, []string{})
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

// --- #48: --desc captures next flag ---

// TestParseInitArgsDescDoesNotCaptureFlag проверяет, что --desc --global
// возвращает ошибку, а не захватывает --global как значение (фикс #48).
func TestParseInitArgsDescDoesNotCaptureFlag(t *testing.T) {
	_, err := parseInitArgs([]string{"mytool", "--desc", "--global"})
	if err == nil {
		t.Fatal("expected error when --desc is followed by --global")
	}
	if !strings.Contains(err.Error(), "--desc") {
		t.Fatalf("expected error mentioning --desc, got: %v", err)
	}
}

// TestParseInitArgsDefaultCmdDoesNotCaptureFlag проверяет тоже для --default-cmd.
func TestParseInitArgsDefaultCmdDoesNotCaptureFlag(t *testing.T) {
	_, err := parseInitArgs([]string{"mytool", "--default-cmd", "--global"})
	if err == nil {
		t.Fatal("expected error when --default-cmd is followed by --global")
	}
}

// TestParseInitArgsCommandDoesNotCaptureFlag проверяет тоже для --command.
func TestParseInitArgsCommandDoesNotCaptureFlag(t *testing.T) {
	_, err := parseInitArgs([]string{"mytool", "--command", "--global"})
	if err == nil {
		t.Fatal("expected error when --command is followed by --global")
	}
}

func TestParseUninstallArgs_InvalidName(t *testing.T) {
	for _, name := range []string{"", ".", "..", "a/../b", "/abs", "a//b"} {
		_, err := parseUninstallArgs([]string{name, "--force", "--yes"})
		if err == nil {
			t.Errorf("parseUninstallArgs(%q) должен вернуть ошибку", name)
			continue
		}
		if !strings.Contains(err.Error(), "invalid module name") {
			t.Errorf("parseUninstallArgs(%q) err = %v, want invalid module name", name, err)
		}
	}
}

func TestParseInstallArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    InstallOptions
		wantErr string
	}{
		{"short yes", []string{"src", "-y"}, InstallOptions{Source: "src", Yes: true}, ""},
		{"all flags", []string{"--local", "--skip-attach", "--yes", "--from-git", "--ref=v1", "--namespace", "a.b", "src"},
			InstallOptions{Source: "src", Local: true, SkipAttach: true, Yes: true, FromGit: true, Ref: "v1", Namespace: "a.b"}, ""},
		{"namespace equals form", []string{"src", "--namespace=ns"}, InstallOptions{Source: "src", Namespace: "ns"}, ""},
		{"reserved namespace", []string{"src", "--namespace", "a.list"}, InstallOptions{}, "--namespace: "},
		{"ref with leading dash", []string{"src", "--ref", "-x"}, InstallOptions{}, `must not start with "-"`},
		{"missing source", []string{"--yes"}, InstallOptions{}, "usage: futi modules install <source> [flags]"},
		{"two sources", []string{"a", "b"}, InstallOptions{}, "expected one source argument, got 2"},
		{"unknown flag", []string{"src", "--bogus"}, InstallOptions{}, "unknown flag: --bogus"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseInstallArgs(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("parseInstallArgs(%q) err = %v, want containing %q", tc.args, err, tc.wantErr)
				}
				return
			}
			if err != nil || *got != tc.want {
				t.Fatalf("parseInstallArgs(%q) = (%+v, %v), want %+v", tc.args, got, err, tc.want)
			}
		})
	}
}
