package modules

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/execmodule"
)

// writeManifestFixture создаёт пакет <dir>/<name>/config.yaml с комментарием
// и существующими полями (GlobalSource резолвит name → name/config.yaml).
// Возвращает путь к манифесту.
func writeManifestFixture(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name, "config.yaml")
	body := `# module comment
name: qr
description: QR codes
attach: "true" # keep me
commands:
  make:
    description: Make a QR code
    cmd: "echo make"
  keep:
    cmd: "echo keep"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAddCommandPreservesCommentsAndFields(t *testing.T) {
	dir := t.TempDir()
	path := writeManifestFixture(t, dir, "qr")

	if err := addCommandToManifest(path, "scan", "echo scan", "Scan a QR code"); err != nil {
		t.Fatalf("addCommandToManifest: %v", err)
	}

	data, _ := os.ReadFile(path)
	text := string(data)
	for _, want := range []string{"# module comment", "name: qr", "description: QR codes",
		"attach: \"true\"", "# keep me", "scan", "echo scan"} {
		if !strings.Contains(text, want) {
			t.Fatalf("result missing %q:\n%s", want, text)
		}
	}

	// Результат — валидный манифест, команда доступна.
	mod, err := execmodule.LoadModule(path, filepath.Dir(path))
	if err != nil {
		t.Fatalf("LoadModule after edit: %v", err)
	}
	if _, ok := mod.Commands["scan"]; !ok {
		t.Fatal("command scan not found after edit")
	}
	if mod.Attach != "true" {
		t.Fatalf("attach must survive edit, got %q", mod.Attach)
	}
}

func TestAddCommandDuplicateError(t *testing.T) {
	dir := t.TempDir()
	path := writeManifestFixture(t, dir, "qr")
	err := addCommandToManifest(path, "make", "echo dup", "")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestRemoveCommandKeepsOthers(t *testing.T) {
	dir := t.TempDir()
	path := writeManifestFixture(t, dir, "qr")

	if err := removeCommandFromManifest(path, "make"); err != nil {
		t.Fatalf("removeCommandFromManifest: %v", err)
	}

	mod, err := execmodule.LoadModule(path, filepath.Dir(path))
	if err != nil {
		t.Fatalf("LoadModule after edit: %v", err)
	}
	if _, ok := mod.Commands["make"]; ok {
		t.Fatal("command make must be removed")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# module comment") {
		t.Fatalf("comments must survive removal:\n%s", string(data))
	}
}

func TestRemoveCommandMissingError(t *testing.T) {
	dir := t.TempDir()
	path := writeManifestFixture(t, dir, "qr")
	err := removeCommandFromManifest(path, "nope")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

// Удаление единственной команды модуля без cmd даёт невалидный манифест:
// ошибка, файл не меняется.
func TestRemoveOnlyCommandRestoresManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.yaml")
	body := "name: m\ncommands:\n  only:\n    cmd: echo\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := removeCommandFromManifest(path, "only")
	if err == nil || !strings.Contains(err.Error(), "no cmd and no commands") {
		t.Fatalf("err = %v, want invalid-manifest error", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != body {
		t.Fatalf("manifest changed:\n%s\nwant:\n%s", data, body)
	}
}

func TestRunModulesAddCommandCLI(t *testing.T) {
	dir := t.TempDir()
	writeManifestFixture(t, dir, "qr")
	t.Setenv("FUTI_MODULES_DIR", dir)

	var b bytes.Buffer
	err := newTestManager(t, &b, nil).AddCommand(AddCommandOptions{Module: "qr", Command: "scan", Desc: "Scan QR"})
	if err != nil {
		t.Fatalf("AddCommand: %v", err)
	}
	if !strings.Contains(b.String(), `Added command "scan"`) {
		t.Fatalf("unexpected output: %s", b.String())
	}

	mod, err := execmodule.LoadModule(filepath.Join(dir, "qr", "config.yaml"), filepath.Join(dir, "qr"))
	if err != nil {
		t.Fatalf("LoadModule after edit: %v", err)
	}
	if _, ok := mod.Commands["scan"]; !ok {
		t.Fatal("command scan not found")
	}
	if mod.Commands["scan"].Description != "Scan QR" {
		t.Fatalf("unexpected description: %q", mod.Commands["scan"].Description)
	}
}

func TestRunModulesAddCommandDefaultCmd(t *testing.T) {
	dir := t.TempDir()
	writeManifestFixture(t, dir, "qr")
	t.Setenv("FUTI_MODULES_DIR", dir)

	if err := newTestManager(t, &bytes.Buffer{}, nil).AddCommand(AddCommandOptions{Module: "qr", Command: "scan"}); err != nil {
		t.Fatalf("AddCommand: %v", err)
	}
	mod, err := execmodule.LoadModule(filepath.Join(dir, "qr", "config.yaml"), filepath.Join(dir, "qr"))
	if err != nil {
		t.Fatalf("LoadModule: %v", err)
	}
	if got := mod.Commands["scan"].Cmd; got != "echo scan" {
		t.Fatalf("default cmd must be \"echo scan\", got %q", got)
	}
}

// writeRawManifest пишет manifest с произвольным содержимым.
func writeRawManifest(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAddCommandEmptyManifest(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"empty", ""},
		{"only comment", "# c\n"},
		{"null document", "---\n"},
		{"tilde", "~\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeRawManifest(t, tc.body)
			if err := addCommandToManifest(path, "x", "echo x", ""); err != nil {
				t.Fatalf("addCommandToManifest(%q): %v", tc.body, err)
			}
			mod, err := execmodule.LoadModule(path, filepath.Dir(path))
			if err != nil {
				t.Fatalf("LoadModule after add: %v", err)
			}
			if _, ok := mod.Commands["x"]; !ok {
				t.Fatalf("command x not found after add to %q", tc.body)
			}
		})
	}
}

func TestRemoveCommandEmptyManifest(t *testing.T) {
	for _, body := range []string{"", "# c\n", "---\n", "~\n"} {
		path := writeRawManifest(t, body)
		err := removeCommandFromManifest(path, "x")
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("remove on %q: expected not-found error, got %v", body, err)
		}
	}
}

func TestAddCommandNonMappingRoot(t *testing.T) {
	for _, body := range []string{"- a\n", "scalar\n"} {
		path := writeRawManifest(t, body)
		err := addCommandToManifest(path, "x", "echo x", "")
		if err == nil || !strings.Contains(err.Error(), "not a mapping") {
			t.Fatalf("add on %q: expected not-a-mapping error, got %v", body, err)
		}
		if err := removeCommandFromManifest(path, "x"); err == nil || !strings.Contains(err.Error(), "not a mapping") {
			t.Fatalf("remove on %q: expected not-a-mapping error, got %v", body, err)
		}
		data, _ := os.ReadFile(path)
		if string(data) != body {
			t.Fatalf("file changed: %q, want %q", data, body)
		}
	}
}

// listDir возвращает имена файлов каталога (для проверки отсутствия temp-файлов).
func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// Невалидный результат правки не трогает оригинал: байты и права на месте,
// временных файлов в каталоге нет.
func TestEditInvalidResultKeepsOriginal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("права 0600 не проверяются на Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "m.yaml")
	body := "name: m\ncommands:\n  only:\n    cmd: echo\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	// Пустой cmd у единственной команды после удаления — невалидный манифест.
	err := removeCommandFromManifest(path, "only")
	if err == nil {
		t.Fatal("expected invalid-manifest error")
	}
	if strings.Contains(err.Error(), ".m.yaml-") || !strings.Contains(err.Error(), path) {
		t.Fatalf("error must name %s, not a temp file: %v", path, err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != body {
		t.Fatalf("manifest changed:\n%s\nwant:\n%s", data, body)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	if got := listDir(t, dir); len(got) != 1 || got[0] != "m.yaml" {
		t.Fatalf("dir contents = %v, want only m.yaml", got)
	}
}

// CreateTemp создаёт файл с 0600, поэтому берём и 0600, и 0640: второй ловит
// отсутствие явного Chmod.
func TestAddCommandPreservesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("права не проверяются на Windows")
	}
	for _, mode := range []os.FileMode{0o600, 0o640} {
		t.Run(mode.String(), func(t *testing.T) {
			dir := t.TempDir()
			path := writeManifestFixture(t, dir, "qr")
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}

			if err := addCommandToManifest(path, "scan", "echo scan", ""); err != nil {
				t.Fatalf("addCommandToManifest: %v", err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != mode {
				t.Fatalf("mode = %v, want %v", info.Mode().Perm(), mode)
			}
			if got := listDir(t, filepath.Dir(path)); len(got) != 1 {
				t.Fatalf("dir contents = %v, want only config.yaml", got)
			}
		})
	}
}

// config.yaml — симлинк: правка идёт в цель, сам симлинк остаётся симлинком.
func TestAddCommandThroughSymlinkEditsTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("симлинки требуют привилегий на Windows")
	}
	dir := t.TempDir()
	target := writeManifestFixture(t, dir, "real")
	link := filepath.Join(dir, "linked.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := addCommandToManifest(link, "scan", "echo scan", ""); err != nil {
		t.Fatalf("addCommandToManifest: %v", err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link must stay a symlink, got %v, %v", fi, err)
	}
	data, _ := os.ReadFile(target)
	if !strings.Contains(string(data), "scan:") {
		t.Fatalf("target not edited:\n%s", data)
	}
}

func TestAddCommandInvalidatesCache(t *testing.T) {
	dir := t.TempDir()
	writeManifestFixture(t, dir, "qr")
	t.Setenv("FUTI_MODULES_DIR", dir)

	rt := newTestRuntime(t)
	cache := rt.Cache()
	stranger := makeSourceModule(t, t.TempDir(), "stranger", "")
	if _, err := cache.LoadModule(filepath.Join(stranger, "config.yaml"), stranger); err != nil {
		t.Fatal(err)
	}

	if err := newManagerWithRuntime(&bytes.Buffer{}, nil, rt).AddCommand(AddCommandOptions{Module: "qr", Command: "scan"}); err != nil {
		t.Fatalf("AddCommand: %v", err)
	}
	if got := cache.ModuleCount(); got != 0 {
		t.Fatalf("cache must be invalidated after add_command, got %d entries", got)
	}
}

func TestRemoveCommandInvalidatesCache(t *testing.T) {
	dir := t.TempDir()
	writeManifestFixture(t, dir, "qr")
	t.Setenv("FUTI_MODULES_DIR", dir)

	rt := newTestRuntime(t)
	cache := rt.Cache()
	stranger := makeSourceModule(t, t.TempDir(), "stranger", "")
	if _, err := cache.LoadModule(filepath.Join(stranger, "config.yaml"), stranger); err != nil {
		t.Fatal(err)
	}

	if err := newManagerWithRuntime(&bytes.Buffer{}, nil, rt).RemoveCommand(RemoveCommandOptions{Module: "qr", Command: "make"}); err != nil {
		t.Fatalf("RemoveCommand: %v", err)
	}
	if got := cache.ModuleCount(); got != 0 {
		t.Fatalf("cache must be invalidated after remove_command, got %d entries", got)
	}
}

// Результат правки больше MaxManifestSize отвергается, оригинал не меняется.
func TestAddCommandOversizedResultRejected(t *testing.T) {
	dir := t.TempDir()
	path := writeManifestFixture(t, dir, "qr")
	before, _ := os.ReadFile(path)

	huge := strings.Repeat("x", execmodule.MaxManifestSize)
	err := addCommandToManifest(path, "scan", "echo scan", huge)
	if err == nil || !strings.Contains(err.Error(), strconv.Itoa(execmodule.MaxManifestSize)) {
		t.Fatalf("err = %v, want size-limit error", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("manifest changed")
	}
	if got := listDir(t, filepath.Dir(path)); len(got) != 1 {
		t.Fatalf("dir contents = %v, want only config.yaml", got)
	}
}

func TestRemoveCommandRejectsSlashModule(t *testing.T) {
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	err := newTestManager(t, &bytes.Buffer{}, nil).RemoveCommand(RemoveCommandOptions{Module: "a/b", Command: "x"})
	if err == nil || !strings.Contains(err.Error(), "write the namespace with a dot") {
		t.Fatalf("RemoveCommand err = %v, want namespace-with-a-dot error", err)
	}
}
