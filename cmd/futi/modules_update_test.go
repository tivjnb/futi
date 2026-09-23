package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"futi/internal/discovery"
	"futi/internal/metayaml"
)

func TestRunModulesUpdateNoChange(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src}); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Файл, созданный ПОСЛЕ установки (локальная модификация — не источник).
	dest := filepath.Join(global, "qr")
	os.WriteFile(filepath.Join(dest, "local_mod.txt"), []byte("keep\n"), 0o644)

	var b bytes.Buffer
	if err := runModulesUpdate(&b, discovery.NewGlobalSource(global), cache, []string{"qr"}); err != nil {
		t.Fatalf("runModulesUpdate: %v", err)
	}
	if !strings.Contains(b.String(), "up to date") {
		t.Fatalf("unexpected output: %s", b.String())
	}
	// Дерево не тронуто.
	if _, err := os.Stat(filepath.Join(dest, "local_mod.txt")); err != nil {
		t.Fatal("tree must be untouched when up to date")
	}
	// last_check обновлён.
	meta, _ := metayaml.Read(filepath.Join(dest, metayaml.FileName))
	if meta.Lockfile.LastCheck.Before(time.Now().Add(-time.Minute)) {
		t.Fatal("last_check must be updated")
	}
}

func TestRunModulesUpdateSourceChanged(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src}); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Источник изменился.
	os.WriteFile(filepath.Join(src, "config.yaml"),
		[]byte("name: qr\ncommands:\n  run:\n    cmd: echo v2\n"), 0o644)

	oldMeta, _ := metayaml.Read(filepath.Join(global, "qr", metayaml.FileName))
	oldInstalledAt := oldMeta.Lockfile.InstalledAt

	var b bytes.Buffer
	if err := runModulesUpdate(&b, discovery.NewGlobalSource(global), cache, []string{"qr"}); err != nil {
		t.Fatalf("runModulesUpdate: %v", err)
	}
	if !strings.Contains(b.String(), "updated") {
		t.Fatalf("unexpected output: %s", b.String())
	}

	data, _ := os.ReadFile(filepath.Join(global, "qr", "config.yaml"))
	if !strings.Contains(string(data), "echo v2") {
		t.Fatalf("new tree must be installed:\n%s", string(data))
	}
	newMeta, _ := metayaml.Read(filepath.Join(global, "qr", metayaml.FileName))
	if newMeta.Lockfile.Hash == oldMeta.Lockfile.Hash {
		t.Fatal("hash must change after update")
	}
	if !newMeta.Lockfile.InstalledAt.Equal(oldInstalledAt) {
		t.Fatal("InstalledAt must be preserved on update")
	}
	if _, err := os.Stat(filepath.Join(global, "qr.futi-backup")); !os.IsNotExist(err) {
		t.Fatal("backup must be removed after update")
	}
}

func TestRunModulesUpdateAttachFailureWarns(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, cache, []string{src, "--skip-attach"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Источник изменился: новая версия с failing attach.
	os.WriteFile(filepath.Join(src, "config.yaml"),
		[]byte("name: qr\nattach: \"exit 5\"\ncommands:\n  run:\n    cmd: echo v2\n"), 0o644)

	if err := runModulesUpdate(&bytes.Buffer{}, discovery.NewGlobalSource(global), cache, []string{"qr"}); err != nil {
		t.Fatalf("attach failure on update must be warning, not error: %v", err)
	}
	// Новое дерево сохранено (НЕ rollback).
	data, _ := os.ReadFile(filepath.Join(global, "qr", "config.yaml"))
	if !strings.Contains(string(data), "echo v2") {
		t.Fatal("new tree must survive attach failure (no rollback)")
	}
}

func TestRunModulesUpdateNoMetaError(t *testing.T) {
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	installFixtureModule(t, global, "hand", nil) // без meta

	err := runModulesUpdate(&bytes.Buffer{}, discovery.NewGlobalSource(global), newTestCache(), []string{"hand"})
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("expected no-meta error, got %v", err)
	}
}
