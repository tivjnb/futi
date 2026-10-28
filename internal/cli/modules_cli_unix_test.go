//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package cli

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Хук, завершившийся ненулевым кодом (не сигналом), даёт код выхода futi 1,
// а не код хука: README обещает 1 для прочих ошибок modules-команд.
func TestFutiModulesInstallFailedAttachExitsWithOne(t *testing.T) {
	tmp := t.TempDir()
	src := makeSourceModule(t, t.TempDir(), "a", "attach: \"exit 3\"\n")
	cmd := exec.Command(builtFutiBinary(t), "modules", "install", src, "--yes")
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(),
		"FUTI_MODULES_DIR="+filepath.Join(tmp, "mods"),
		"HOME="+tmp, "XDG_CACHE_HOME="+filepath.Join(tmp, "cache"))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err == nil {
		t.Fatal("ожидалась ошибка attach")
	}
	if code := cmd.ProcessState.ExitCode(); code != 1 {
		t.Fatalf("код выхода = %d, want 1", code)
	}
}
