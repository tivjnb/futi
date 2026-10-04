package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"futi/internal/discovery"
	"futi/internal/metayaml"
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

	src := discovery.NewGlobalSource(global)
	var b bytes.Buffer
	if err := runModulesUninstall(&b, strings.NewReader("\n"), src, newTestCache(), []string{"qr", "--yes"}); err != nil {
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

	src := discovery.NewGlobalSource(global)
	err := runModulesUninstall(&bytes.Buffer{}, strings.NewReader("\n"), src, newTestCache(), []string{"hand", "--yes"})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected no-meta error, got %v", err)
	}

	if err := runModulesUninstall(&bytes.Buffer{}, strings.NewReader("\n"), src, newTestCache(), []string{"hand", "--yes", "--force"}); err != nil {
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
	if err := runModulesUninstall(&b, strings.NewReader("n\n"), discovery.NewGlobalSource(global), newTestCache(), []string{"qr"}); err != nil {
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

	err := runModulesUninstall(&bytes.Buffer{}, strings.NewReader("\n"), discovery.NewGlobalSource(global), newTestCache(), []string{"qr", "--yes", "--strict-detach"})
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

	if err := runModulesUninstall(&bytes.Buffer{}, strings.NewReader("\n"), discovery.NewGlobalSource(global), newTestCache(), []string{"qr", "--yes"}); err != nil {
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

	src := discovery.NewGlobalSource(global)
	if err := runModulesUninstall(&bytes.Buffer{}, strings.NewReader("\n"), src, newTestCache(), []string{"one", "--yes", "--force"}); err != nil {
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
	t.Setenv("FUTI_MODULES_DIR", t.TempDir())
	t.Chdir(t.TempDir())

	// Source указывает на каталог ВНЕ allowed roots.
	outside := t.TempDir()
	moduleDir := filepath.Join(outside, "qr")
	os.MkdirAll(moduleDir, 0o755)
	os.WriteFile(filepath.Join(moduleDir, "config.yaml"), []byte("name: qr\ncommands:\n  run:\n    cmd: echo\n"), 0o644)

	err := runModulesUninstall(&bytes.Buffer{}, strings.NewReader("\n"), discovery.NewGlobalSource(outside), newTestCache(), []string{"qr", "--yes", "--force"})
	if err == nil || !strings.Contains(err.Error(), "outside known futi modules") {
		t.Fatalf("expected outside-root refusal, got %v", err)
	}
	if _, statErr := os.Stat(moduleDir); statErr != nil {
		t.Fatal("module outside roots must survive")
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

	err := runModulesUninstall(&bytes.Buffer{}, strings.NewReader("\n"), discovery.NewGlobalSource(root), newTestCache(), []string{"", "--force", "--yes"})
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

	err := runModulesUninstall(&bytes.Buffer{}, strings.NewReader("\n"), discovery.NewGlobalSource(root+"/"), newTestCache(), []string{"n.m", "--force", "--yes"})
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

	err := runModulesUninstall(&bytes.Buffer{}, strings.NewReader("\n"), discovery.NewGlobalSource(root), newTestCache(), []string{"dev/config", "--force", "--yes"})
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

	src := discovery.NewGlobalSource(global)
	err := runModulesUninstall(&bytes.Buffer{}, strings.NewReader("\n"), src, newTestCache(), []string{"qr", "--yes"})
	if err == nil {
		t.Fatal("ожидалась ошибка прерывания detach")
	}
	if _, statErr := os.Stat(moduleDir); statErr != nil {
		t.Fatalf("модуль не должен удаляться при прерванном detach: %v", statErr)
	}
}
