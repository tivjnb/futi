package modules

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/metayaml"
)

func TestRunModulesUninstallPackage(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	// Модуль с detach, пишущим маркер за пределы модуля.
	detachedMarker := filepath.Join(t.TempDir(), "detached.marker")
	moduleDir := filepath.Join(global, "qr")
	os.MkdirAll(moduleDir, 0o755)
	body := "name: qr\ndetach: \"touch " + detachedMarker + "\"\ncommands:\n  run:\n    cmd: echo\n"
	os.WriteFile(filepath.Join(moduleDir, "config.yaml"), []byte(body), 0o644)
	metayaml.Write(filepath.Join(moduleDir, metayaml.FileName), &metayaml.Meta{})

	var b bytes.Buffer
	if err := newTestManager(t, &b, strings.NewReader("\n")).Uninstall(context.Background(), UninstallOptions{Name: "qr", Yes: true}); err != nil {
		t.Fatalf("runModulesUninstall: %v", err)
	}
	if _, err := os.Stat(moduleDir); !os.IsNotExist(err) {
		t.Fatal("module must be removed")
	}
	if _, err := os.Stat(detachedMarker); err != nil {
		t.Fatalf("detach hook did not run: %v", err)
	}
}

func TestRunModulesUninstallNoMetaRequiresForce(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	moduleDir := filepath.Join(global, "hand")
	os.MkdirAll(moduleDir, 0o755)
	os.WriteFile(filepath.Join(moduleDir, "config.yaml"), []byte("name: hand\ncommands:\n  run:\n    cmd: echo\n"), 0o644)

	err := newTestManager(t, &bytes.Buffer{}, strings.NewReader("\n")).Uninstall(context.Background(), UninstallOptions{Name: "hand", Yes: true})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected no-meta error, got %v", err)
	}

	if err := newTestManager(t, &bytes.Buffer{}, strings.NewReader("\n")).Uninstall(context.Background(), UninstallOptions{Name: "hand", Yes: true, Force: true}); err != nil {
		t.Fatalf("force uninstall: %v", err)
	}
	if _, err := os.Stat(moduleDir); !os.IsNotExist(err) {
		t.Fatal("module must be removed with --force")
	}
}

func TestRunModulesUninstallConfirmDeclined(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	moduleDir := filepath.Join(global, "qr")
	os.MkdirAll(moduleDir, 0o755)
	os.WriteFile(filepath.Join(moduleDir, "config.yaml"), []byte("name: qr\ncommands:\n  run:\n    cmd: echo\n"), 0o644)
	metayaml.Write(filepath.Join(moduleDir, metayaml.FileName), &metayaml.Meta{})

	var b bytes.Buffer
	if err := newTestManager(t, &b, strings.NewReader("n\n")).Uninstall(context.Background(), UninstallOptions{Name: "qr"}); err != nil {
		t.Fatalf("runModulesUninstall: %v", err)
	}
	if !strings.Contains(b.String(), "Aborted") {
		t.Fatalf("expected abort: %s", b.String())
	}
	if _, err := os.Stat(moduleDir); err != nil {
		t.Fatal("module must survive declined confirmation")
	}
}

func TestRunModulesUninstallStrictDetachAborts(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	moduleDir := filepath.Join(global, "qr")
	os.MkdirAll(moduleDir, 0o755)
	body := "name: qr\ndetach: \"exit 2\"\ncommands:\n  run:\n    cmd: echo\n"
	os.WriteFile(filepath.Join(moduleDir, "config.yaml"), []byte(body), 0o644)
	metayaml.Write(filepath.Join(moduleDir, metayaml.FileName), &metayaml.Meta{})

	err := newTestManager(t, &bytes.Buffer{}, strings.NewReader("\n")).Uninstall(context.Background(), UninstallOptions{Name: "qr", Yes: true, StrictDetach: true})
	if err == nil || !strings.Contains(err.Error(), "strict") {
		t.Fatalf("expected strict-detach error, got %v", err)
	}
	if _, statErr := os.Stat(moduleDir); statErr != nil {
		t.Fatal("module must NOT be deleted on strict detach failure")
	}
}

func TestRunModulesUninstallDetachFailureWarns(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	moduleDir := filepath.Join(global, "qr")
	os.MkdirAll(moduleDir, 0o755)
	body := "name: qr\ndetach: \"exit 2\"\ncommands:\n  run:\n    cmd: echo\n"
	os.WriteFile(filepath.Join(moduleDir, "config.yaml"), []byte(body), 0o644)
	metayaml.Write(filepath.Join(moduleDir, metayaml.FileName), &metayaml.Meta{})

	if err := newTestManager(t, &bytes.Buffer{}, strings.NewReader("\n")).Uninstall(context.Background(), UninstallOptions{Name: "qr", Yes: true}); err != nil {
		t.Fatalf("detach failure must be warning, not error: %v", err)
	}
	if _, err := os.Stat(moduleDir); !os.IsNotExist(err) {
		t.Fatal("module must be removed despite detach failure")
	}
}

func TestRunModulesUninstallSingleYamlFile(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	// Два одиночных yaml в одном каталоге; удаляем один — второй цел.
	os.WriteFile(filepath.Join(global, "one.yaml"), []byte("name: one\ncommands:\n  run:\n    cmd: echo\n"), 0o644)
	os.WriteFile(filepath.Join(global, "two.yaml"), []byte("name: two\ncommands:\n  run:\n    cmd: echo\n"), 0o644)

	if err := newTestManager(t, &bytes.Buffer{}, strings.NewReader("\n")).Uninstall(context.Background(), UninstallOptions{Name: "one", Yes: true, Force: true}); err != nil {
		t.Fatalf("runModulesUninstall: %v", err)
	}
	if _, err := os.Stat(filepath.Join(global, "one.yaml")); !os.IsNotExist(err) {
		t.Fatal("one.yaml must be removed")
	}
	if _, err := os.Stat(filepath.Join(global, "two.yaml")); err != nil {
		t.Fatalf("two.yaml must survive: %v", err)
	}
}

func TestRunModulesUninstallRefusesOutsideRoot(t *testing.T) {
	skipSymlinksOnWindows(t)
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	// Модуль лежит ВНЕ allowed roots; в каталоге модулей на него указывает симлинк.
	outside := t.TempDir()
	moduleDir := filepath.Join(outside, "qr")
	os.MkdirAll(moduleDir, 0o755)
	os.WriteFile(filepath.Join(moduleDir, "config.yaml"), []byte("name: qr\ncommands:\n  run:\n    cmd: echo\n"), 0o644)
	if err := os.Symlink(moduleDir, filepath.Join(global, "qr")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	err := newTestManager(t, &bytes.Buffer{}, strings.NewReader("\n")).Uninstall(context.Background(), UninstallOptions{Name: "qr", Yes: true, Force: true})
	if err == nil || !strings.Contains(err.Error(), "outside known futi modules") {
		t.Fatalf("expected outside-root refusal, got %v", err)
	}
	if _, statErr := os.Stat(moduleDir); statErr != nil {
		t.Fatal("module outside roots must survive")
	}
}

func TestUninstall_EmptyNameKeepsRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "m")
	t.Setenv("FUTI_MODULES_DIR", root)
	t.Chdir(t.TempDir())

	moduleDir := filepath.Join(root, "qr")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "config.yaml"), []byte("name: qr\ncommands:\n  run:\n    cmd: echo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := newTestManager(t, &bytes.Buffer{}, strings.NewReader("\n")).Uninstall(context.Background(), UninstallOptions{Name: "", Force: true, Yes: true})
	if err == nil || !strings.Contains(err.Error(), "invalid module name") {
		t.Fatalf("err = %v, want invalid module name", err)
	}
	if _, statErr := os.Stat(filepath.Join(moduleDir, "config.yaml")); statErr != nil {
		t.Fatalf("корень со содержимым должен остаться цел: %v", statErr)
	}
}

func TestUninstall_SingleModuleKeepsRootWithSlash(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "m2")
	t.Setenv("FUTI_MODULES_DIR", root+"/")
	t.Chdir(t.TempDir())

	moduleDir := filepath.Join(root, "n", "m")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "config.yaml"), []byte("name: m\ncommands:\n  run:\n    cmd: echo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := newTestManager(t, &bytes.Buffer{}, strings.NewReader("\n")).Uninstall(context.Background(), UninstallOptions{Name: "n.m", Force: true, Yes: true})
	if err != nil {
		t.Fatalf("runModulesUninstall: %v", err)
	}
	if _, statErr := os.Stat(root); statErr != nil {
		t.Fatalf("каталог m2 должен остаться: %v", statErr)
	}
	if _, statErr := os.Stat(moduleDir); !os.IsNotExist(statErr) {
		t.Fatal("модуль должен быть удалён")
	}
}

// "dev/config" раньше резолвился в манифест пакета dev и удалял весь dev/.
func TestUninstall_SlashNameRejectedKeepsPackage(t *testing.T) {
	root := filepath.Join(t.TempDir(), "m")
	t.Setenv("FUTI_MODULES_DIR", root)
	t.Chdir(t.TempDir())

	pkgDir := filepath.Join(root, "dev")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "config.yaml"), []byte("name: dev\ncommands:\n  run:\n    cmd: echo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := newTestManager(t, &bytes.Buffer{}, strings.NewReader("\n")).Uninstall(context.Background(), UninstallOptions{Name: "dev/config", Force: true, Yes: true})
	if err == nil || !strings.Contains(err.Error(), "invalid module name") {
		t.Fatalf("err = %v, want invalid module name", err)
	}
	if _, statErr := os.Stat(filepath.Join(pkgDir, "config.yaml")); statErr != nil {
		t.Fatalf("пакет должен остаться цел: %v", statErr)
	}
}

// Detach, убитый SIGINT (Ctrl+C), прерывает удаление: без --strict-detach
// каталог модуля остаётся на месте.
func TestRunModulesUninstallInterruptedDetachKeepsModule(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	moduleDir := filepath.Join(global, "qr")
	os.MkdirAll(moduleDir, 0o755)
	body := "name: qr\ndetach: \"kill -INT $$\"\ncommands:\n  run:\n    cmd: echo\n"
	os.WriteFile(filepath.Join(moduleDir, "config.yaml"), []byte(body), 0o644)
	metayaml.Write(filepath.Join(moduleDir, metayaml.FileName), &metayaml.Meta{})

	err := newTestManager(t, &bytes.Buffer{}, strings.NewReader("\n")).Uninstall(context.Background(), UninstallOptions{Name: "qr", Yes: true})
	if err == nil {
		t.Fatal("ожидалась ошибка прерывания detach")
	}
	if _, statErr := os.Stat(moduleDir); statErr != nil {
		t.Fatalf("модуль не должен удаляться при прерванном detach: %v", statErr)
	}
}

func installRawModule(t *testing.T, global, body string) string {
	t.Helper()
	moduleDir := filepath.Join(global, "qr")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := metayaml.Write(filepath.Join(moduleDir, metayaml.FileName), &metayaml.Meta{}); err != nil {
		t.Fatal(err)
	}
	return moduleDir
}

func TestRunModulesUninstallInvalidSchemaStillDetaches(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	marker := filepath.Join(t.TempDir(), "detached.marker")
	// Неизвестный ключ делает модуль невалидным для LoadModule.
	moduleDir := installRawModule(t, global, "name: qr\nbogus_key: 1\ndetach: \"touch "+marker+"\"\ncommands:\n  run:\n    cmd: echo\n")

	err := newTestManager(t, &bytes.Buffer{}, strings.NewReader("\n")).Uninstall(context.Background(), UninstallOptions{Name: "qr", Yes: true, StrictDetach: true})
	if err != nil {
		t.Fatalf("runModulesUninstall: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("detach must run for schema-invalid module: %v", err)
	}
	if _, err := os.Stat(moduleDir); !os.IsNotExist(err) {
		t.Fatal("module must be removed")
	}
}

func TestRunModulesUninstallBrokenYAMLWarnsAndRemoves(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	moduleDir := installRawModule(t, global, "name: [unclosed\n")

	var runErr error
	stderr := captureStderr(t, func() {
		runErr = newTestManager(t, &bytes.Buffer{}, strings.NewReader("\n")).Uninstall(context.Background(), UninstallOptions{Name: "qr", Yes: true})
	})
	if runErr != nil {
		t.Fatalf("runModulesUninstall: %v", runErr)
	}
	if !strings.Contains(stderr, "Warning: cannot load manifest, detach skipped:") {
		t.Fatalf("stderr = %q, want warning about skipped detach", stderr)
	}
	if _, err := os.Stat(moduleDir); !os.IsNotExist(err) {
		t.Fatal("module must be removed")
	}
}

func TestRunModulesUninstallBrokenYAMLStrictDetachRefuses(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	moduleDir := installRawModule(t, global, "name: [unclosed\n")

	err := newTestManager(t, &bytes.Buffer{}, strings.NewReader("\n")).Uninstall(context.Background(), UninstallOptions{Name: "qr", Yes: true, StrictDetach: true})
	if err == nil || !strings.Contains(err.Error(), "strict") {
		t.Fatalf("expected strict-detach error, got %v", err)
	}
	if _, statErr := os.Stat(moduleDir); statErr != nil {
		t.Fatal("module must NOT be deleted")
	}
}

// F62: stdout хука detach идёт в writer вызывающей функции.
func TestModulesUninstallHookOutputToWriter(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	moduleDir := filepath.Join(global, "qr")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "name: qr\ndetach: \"echo detach-hook-says-hi\"\ncommands:\n  run:\n    cmd: echo\n"
	if err := os.WriteFile(filepath.Join(moduleDir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := metayaml.Write(filepath.Join(moduleDir, metayaml.FileName), &metayaml.Meta{}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := newTestManager(t, &buf, strings.NewReader("\n")).Uninstall(context.Background(), UninstallOptions{Name: "qr", Yes: true}); err != nil {
		t.Fatalf("runModulesUninstall: %v", err)
	}
	if !strings.Contains(buf.String(), "detach-hook-says-hi") {
		t.Fatalf("writer output = %q, want hook stdout", buf.String())
	}
}

func TestUninstallRejectsSlashName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "m")
	t.Setenv("FUTI_MODULES_DIR", root)
	t.Chdir(t.TempDir())

	pkgDir := filepath.Join(root, "dev", "config")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "config.yaml"), []byte("name: dev\ncommands:\n  run:\n    cmd: echo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := newTestManager(t, io.Discard, nil).Uninstall(context.Background(), UninstallOptions{Name: "dev/config", Yes: true})
	if err == nil || !strings.Contains(err.Error(), "write the namespace with a dot") {
		t.Fatalf("err = %v, want hint to write the namespace with a dot", err)
	}
	if _, statErr := os.Stat(pkgDir); statErr != nil {
		t.Fatalf("module directory must survive: %v", statErr)
	}
}
