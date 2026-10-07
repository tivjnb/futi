package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/discovery"
)

// attachTouch — attach-хук, оставляющий маркер в каталоге модуля.
const attachTouch = "attach: \"touch {{.module_root}}/attached\"\n"

// setStdinTerminal подменяет проверку TTY на stdin на время теста.
func setStdinTerminal(t *testing.T, tty bool) {
	t.Helper()
	orig := stdinIsTerminal
	stdinIsTerminal = func() bool { return tty }
	t.Cleanup(func() { stdinIsTerminal = orig })
}

// unreadableStdin валит тест при любом чтении: ответ не должен запрашиваться.
type unreadableStdin struct{ t *testing.T }

func (r unreadableStdin) Read([]byte) (int, error) {
	r.t.Error("stdin must not be read")
	return 0, os.ErrClosed
}

// attachEnv — изолированный каталог модулей и cwd; возвращает каталог модулей.
func attachEnv(t *testing.T) string {
	t.Helper()
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())
	return global
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestInstallAttachConfirmed(t *testing.T) {
	for _, answer := range []string{"Y\n", "yes\n", " YES \n", "y"} {
		t.Run(strings.TrimSpace(answer), func(t *testing.T) {
			global := attachEnv(t)
			setStdinTerminal(t, true)
			src := makeSourceModule(t, t.TempDir(), "qr", attachTouch)

			var err error
			prompt := captureStderr(t, func() {
				err = runModulesInstall(&bytes.Buffer{}, strings.NewReader(answer), newTestCache(), []string{src})
			})
			if err != nil {
				t.Fatalf("install: %v", err)
			}
			if !exists(filepath.Join(global, "qr", "attached")) {
				t.Fatal("attach hook must run after confirmation")
			}
			want := "Module qr runs an attach hook (in " + filepath.Join(global, "qr") + "):\n" +
				"  touch {{.module_root}}/attached\n" +
				"Run it? [y/N]"
			if !strings.Contains(prompt, want) {
				t.Fatalf("prompt = %q, want it to contain %q", prompt, want)
			}
		})
	}
}

// Пустой stdin на «терминале» (/dev/null — тоже char device: docker run без
// -i, systemd StandardInput=null) — отказ с подсказкой про --yes.
func TestAttachNoAnswerHintsYes(t *testing.T) {
	t.Run("install", func(t *testing.T) {
		global := attachEnv(t)
		setStdinTerminal(t, true)
		src := makeSourceModule(t, t.TempDir(), "qr", attachTouch)

		var err error
		captureStderr(t, func() {
			err = runModulesInstall(&bytes.Buffer{}, strings.NewReader(""), newTestCache(), []string{src})
		})
		if err == nil || !strings.Contains(err.Error(), "--yes") || !strings.Contains(err.Error(), "module not installed") {
			t.Fatalf("err = %v, want hint about --yes and module not installed", err)
		}
		if exists(filepath.Join(global, "qr")) {
			t.Fatal("module must not be installed")
		}
	})
	t.Run("update", func(t *testing.T) {
		global := attachEnv(t)
		setStdinTerminal(t, true)
		dest := installThenChange(t, global, "qr")

		var err error
		captureStderr(t, func() {
			err = runModulesUpdate(&bytes.Buffer{}, strings.NewReader(""), discovery.NewGlobalSource(global), newTestCache(), []string{"qr"})
		})
		if err == nil || !strings.Contains(err.Error(), "--yes") || !strings.Contains(err.Error(), "module not updated") {
			t.Fatalf("err = %v, want hint about --yes and module not updated", err)
		}
		if data, _ := os.ReadFile(filepath.Join(dest, "config.yaml")); strings.Contains(string(data), "echo v2") {
			t.Fatal("old tree must stay when there is no answer")
		}
		if exists(dest + backupSuffix) {
			t.Fatal("backup must not be created")
		}
	})
}

func TestAttachPromptSkipsBlankLines(t *testing.T) {
	attachEnv(t)
	setStdinTerminal(t, true)
	src := makeSourceModule(t, t.TempDir(), "qr", "attach: \"echo a\\n   \\n\\techo b\\n\"\n")

	prompt := captureStderr(t, func() {
		_ = runModulesInstall(&bytes.Buffer{}, strings.NewReader("n\n"), newTestCache(), []string{src})
	})
	want := "):\n  echo a\n  \techo b\nRun it?"
	if !strings.Contains(prompt, want) {
		t.Fatalf("prompt = %q, want it to contain %q", prompt, want)
	}
}

func TestInstallAttachDeclinedLeavesNothing(t *testing.T) {
	for _, answer := range []string{"n\n", "\n", "", "sure\n"} {
		t.Run(strings.TrimSpace(answer), func(t *testing.T) {
			global := attachEnv(t)
			setStdinTerminal(t, true)
			src := makeSourceModule(t, t.TempDir(), "qr", attachTouch)

			var err error
			captureStderr(t, func() {
				err = runModulesInstall(&bytes.Buffer{}, strings.NewReader(answer), newTestCache(), []string{src, "--namespace", "ns"})
			})
			if err == nil || !strings.Contains(err.Error(), "not confirmed") {
				t.Fatalf("err = %v, want attach hook not confirmed", err)
			}
			if entries, _ := os.ReadDir(global); len(entries) != 0 {
				t.Fatalf("modules dir must stay empty, got %d entries", len(entries))
			}
		})
	}
}

func TestInstallAttachWithoutTerminalRefusesWithoutReading(t *testing.T) {
	global := attachEnv(t)
	setStdinTerminal(t, false)
	src := makeSourceModule(t, t.TempDir(), "qr", attachTouch)

	err := runModulesInstall(&bytes.Buffer{}, unreadableStdin{t}, newTestCache(), []string{src})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v, want hint about --yes", err)
	}
	if exists(filepath.Join(global, "qr")) {
		t.Fatal("module must not be installed")
	}
}

func TestInstallAttachYesRunsWithoutPrompt(t *testing.T) {
	for _, flag := range []string{"--yes", "-y"} {
		t.Run(flag, func(t *testing.T) {
			global := attachEnv(t)
			setStdinTerminal(t, true)
			src := makeSourceModule(t, t.TempDir(), "qr", attachTouch)

			var err error
			prompt := captureStderr(t, func() {
				err = runModulesInstall(&bytes.Buffer{}, unreadableStdin{t}, newTestCache(), []string{src, flag})
			})
			if err != nil {
				t.Fatalf("install: %v", err)
			}
			if !exists(filepath.Join(global, "qr", "attached")) {
				t.Fatal("attach hook must run with " + flag)
			}
			if strings.Contains(prompt, "Run it?") {
				t.Fatalf("no prompt expected with %s, got %q", flag, prompt)
			}
		})
	}
}

func TestInstallSkipAttachWinsWithoutPrompt(t *testing.T) {
	for _, args := range [][]string{{"--skip-attach"}, {"--skip-attach", "--yes"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			global := attachEnv(t)
			setStdinTerminal(t, true)
			src := makeSourceModule(t, t.TempDir(), "qr", attachTouch)

			var err error
			prompt := captureStderr(t, func() {
				err = runModulesInstall(&bytes.Buffer{}, unreadableStdin{t}, newTestCache(), append([]string{src}, args...))
			})
			if err != nil {
				t.Fatalf("install: %v", err)
			}
			if !exists(filepath.Join(global, "qr", "config.yaml")) {
				t.Fatal("module must be installed")
			}
			if exists(filepath.Join(global, "qr", "attached")) {
				t.Fatal("attach hook must be skipped")
			}
			if strings.Contains(prompt, "Run it?") {
				t.Fatalf("no prompt expected, got %q", prompt)
			}
		})
	}
}

func TestInstallWithoutAttachDoesNotAsk(t *testing.T) {
	global := attachEnv(t)
	setStdinTerminal(t, false)
	src := makeSourceModule(t, t.TempDir(), "qr", "")

	if err := runModulesInstall(&bytes.Buffer{}, unreadableStdin{t}, newTestCache(), []string{src}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !exists(filepath.Join(global, "qr", "config.yaml")) {
		t.Fatal("module must be installed")
	}
}

func TestAttachPromptStripsControlCharacters(t *testing.T) {
	attachEnv(t)
	setStdinTerminal(t, true)
	src := makeSourceModule(t, t.TempDir(), "qr", "attach: \"echo \\e[2J cleared\"\n")

	prompt := captureStderr(t, func() {
		_ = runModulesInstall(&bytes.Buffer{}, strings.NewReader("n\n"), newTestCache(), []string{src})
	})
	if strings.Contains(prompt, "\x1b") {
		t.Fatalf("prompt must not contain ESC: %q", prompt)
	}
	if !strings.Contains(prompt, "  echo [2J cleared\n") {
		t.Fatalf("prompt = %q, want sanitized attach line", prompt)
	}
}

// installThenChange ставит модуль name без хука и меняет источник так, что
// новая версия содержит attach-хук. Возвращает каталог установленного модуля.
func installThenChange(t *testing.T, global, name string) string {
	t.Helper()
	src := makeSourceModule(t, t.TempDir(), name, "")
	if err := runModulesInstall(&bytes.Buffer{}, nil, newTestCache(), []string{src}); err != nil {
		t.Fatalf("install %s: %v", name, err)
	}
	writeFile(t, filepath.Join(src, "config.yaml"),
		"name: "+name+"\n"+attachTouch+"commands:\n  run:\n    cmd: echo v2\n")
	return filepath.Join(global, name)
}

func TestUpdateAttachDeclinedKeepsOldTree(t *testing.T) {
	global := attachEnv(t)
	setStdinTerminal(t, true)
	dest := installThenChange(t, global, "qr")

	var err error
	prompt := captureStderr(t, func() {
		err = runModulesUpdate(&bytes.Buffer{}, strings.NewReader("n\n"), discovery.NewGlobalSource(global), newTestCache(), []string{"qr"})
	})
	if err == nil || !strings.Contains(err.Error(), "not confirmed") {
		t.Fatalf("err = %v, want attach hook not confirmed", err)
	}
	if !strings.Contains(prompt, "  touch {{.module_root}}/attached\n") {
		t.Fatalf("prompt = %q, want raw attach template", prompt)
	}
	data, _ := os.ReadFile(filepath.Join(dest, "config.yaml"))
	if strings.Contains(string(data), "echo v2") {
		t.Fatal("old tree must stay after declined attach")
	}
	if exists(dest + backupSuffix) {
		t.Fatal("backup must not be created")
	}
	if exists(filepath.Join(dest, "attached")) {
		t.Fatal("attach hook must not run")
	}
}

func TestUpdateAttachWithoutTerminalRefuses(t *testing.T) {
	global := attachEnv(t)
	setStdinTerminal(t, false)
	dest := installThenChange(t, global, "qr")

	err := runModulesUpdate(&bytes.Buffer{}, unreadableStdin{t}, discovery.NewGlobalSource(global), newTestCache(), []string{"qr"})
	if err == nil || !strings.Contains(err.Error(), "--skip-attach") {
		t.Fatalf("err = %v, want hint about --skip-attach", err)
	}
	data, _ := os.ReadFile(filepath.Join(dest, "config.yaml"))
	if strings.Contains(string(data), "echo v2") {
		t.Fatal("old tree must stay")
	}
}

func TestUpdateAttachYesRuns(t *testing.T) {
	global := attachEnv(t)
	setStdinTerminal(t, false)
	dest := installThenChange(t, global, "qr")

	if err := runModulesUpdate(&bytes.Buffer{}, unreadableStdin{t}, discovery.NewGlobalSource(global), newTestCache(), []string{"qr", "-y"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !exists(filepath.Join(dest, "attached")) {
		t.Fatal("attach hook must run with -y")
	}
}

func TestUpdateUpToDateDoesNotAsk(t *testing.T) {
	global := attachEnv(t)
	setStdinTerminal(t, true)
	src := makeSourceModule(t, t.TempDir(), "qr", attachTouch)
	if err := runModulesInstall(&bytes.Buffer{}, nil, newTestCache(), []string{src, "--yes"}); err != nil {
		t.Fatalf("install: %v", err)
	}

	var out bytes.Buffer
	if err := runModulesUpdate(&out, unreadableStdin{t}, discovery.NewGlobalSource(global), newTestCache(), []string{"qr"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Fatalf("output = %q, want up to date", out.String())
	}
}

// Ответы читаются из одного буфера на весь вызов: отдельный reader на
// модуль съел бы ответ второго модуля, и тот получил бы EOF (отказ).
func TestUpdateAllAsksPerModule(t *testing.T) {
	cases := []struct {
		answers string
		updated string
		failed  string
	}{
		{"y\nn\n", "a", "b"},
		{"n\ny\n", "b", "a"},
	}
	for _, tc := range cases {
		t.Run(tc.updated, func(t *testing.T) {
			global := attachEnv(t)
			setStdinTerminal(t, true)
			installThenChange(t, global, "a")
			installThenChange(t, global, "b")

			var err error
			captureStderr(t, func() {
				err = runModulesUpdate(&bytes.Buffer{}, strings.NewReader(tc.answers), discovery.NewGlobalSource(global), newTestCache(), nil)
			})
			if err == nil || err.Error() != "failed to update: "+tc.failed {
				t.Fatalf("err = %v, want failed to update: %s", err, tc.failed)
			}
			if !exists(filepath.Join(global, tc.updated, "attached")) {
				t.Fatalf("%s: attach hook must run after y", tc.updated)
			}
			data, _ := os.ReadFile(filepath.Join(global, tc.failed, "config.yaml"))
			if strings.Contains(string(data), "echo v2") {
				t.Fatalf("%s: old tree must stay after n", tc.failed)
			}
		})
	}
}
