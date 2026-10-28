package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
				err = newTestManager(t, &bytes.Buffer{}, strings.NewReader(answer)).Install(context.Background(), InstallOptions{Source: src})
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
			err = newTestManager(t, &bytes.Buffer{}, strings.NewReader("")).Install(context.Background(), InstallOptions{Source: src})
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
			err = newTestManager(t, &bytes.Buffer{}, strings.NewReader("")).Update(context.Background(), UpdateOptions{Name: "qr"})
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
		_ = newTestManager(t, &bytes.Buffer{}, strings.NewReader("n\n")).Install(context.Background(), InstallOptions{Source: src})
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
				err = newTestManager(t, &bytes.Buffer{}, strings.NewReader(answer)).Install(context.Background(), InstallOptions{Source: src, Namespace: "ns"})
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

	err := newTestManager(t, &bytes.Buffer{}, unreadableStdin{t}).Install(context.Background(), InstallOptions{Source: src})
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
				err = newTestManager(t, &bytes.Buffer{}, unreadableStdin{t}).Install(context.Background(), InstallOptions{Source: src, Yes: true})
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
				err = newTestManager(t, &bytes.Buffer{}, unreadableStdin{t}).Install(context.Background(), InstallOptions{Source: src, SkipAttach: true, Yes: slices.Contains(args, "--yes")})
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

	if err := newTestManager(t, &bytes.Buffer{}, unreadableStdin{t}).Install(context.Background(), InstallOptions{Source: src}); err != nil {
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
		_ = newTestManager(t, &bytes.Buffer{}, strings.NewReader("n\n")).Install(context.Background(), InstallOptions{Source: src})
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
	if err := newTestManager(t, &bytes.Buffer{}, nil).Install(context.Background(), InstallOptions{Source: src}); err != nil {
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
		// Сначала подтверждаем замену файлов, затем отказываемся от хука.
		err = newTestManager(t, &bytes.Buffer{}, strings.NewReader("y\nn\n")).Update(context.Background(), UpdateOptions{Name: "qr"})
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

func TestUpdateAttachYesRuns(t *testing.T) {
	global := attachEnv(t)
	setStdinTerminal(t, false)
	dest := installThenChange(t, global, "qr")

	// -y подтверждает и замену файлов, и хук: без TTY ни о чём не спрашиваем.
	var err error
	prompt := captureStderr(t, func() {
		err = newTestManager(t, &bytes.Buffer{}, unreadableStdin{t}).Update(context.Background(), UpdateOptions{Name: "qr", Yes: true})
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !exists(filepath.Join(dest, "attached")) {
		t.Fatal("attach hook must run with -y")
	}
	if strings.Contains(prompt, "Run it?") || strings.Contains(prompt, "Replace module files?") {
		t.Fatalf("no prompt expected with -y, got %q", prompt)
	}
}

func TestUpdateUpToDateDoesNotAsk(t *testing.T) {
	attachEnv(t)
	setStdinTerminal(t, true)
	src := makeSourceModule(t, t.TempDir(), "qr", attachTouch)
	if err := newTestManager(t, &bytes.Buffer{}, nil).Install(context.Background(), InstallOptions{Source: src, Yes: true}); err != nil {
		t.Fatalf("install: %v", err)
	}

	var out bytes.Buffer
	if err := newTestManager(t, &out, unreadableStdin{t}).Update(context.Background(), UpdateOptions{Name: "qr"}); err != nil {
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
		// На модуль два вопроса: замена файлов, затем attach-хук.
		{"y\ny\ny\nn\n", "a", "b"},
		{"y\nn\ny\ny\n", "b", "a"},
	}
	for _, tc := range cases {
		t.Run(tc.updated, func(t *testing.T) {
			global := attachEnv(t)
			setStdinTerminal(t, true)
			installThenChange(t, global, "a")
			installThenChange(t, global, "b")

			var err error
			captureStderr(t, func() {
				err = newTestManager(t, &bytes.Buffer{}, strings.NewReader(tc.answers)).Update(context.Background(), UpdateOptions{})
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

// F62: stdout хука attach идёт в writer вызывающей функции, а не в os.Stdout.
func TestModulesInstallHookOutputToWriter(t *testing.T) {
	attachEnv(t)
	setStdinTerminal(t, true)
	src := makeSourceModule(t, t.TempDir(), "qr", "attach: \"echo hook-says-hi\"\n")

	var buf bytes.Buffer
	if err := newTestManager(t, &buf, unreadableStdin{t}).Install(context.Background(), InstallOptions{Source: src, Yes: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(buf.String(), "hook-says-hi") {
		t.Fatalf("writer output = %q, want hook stdout", buf.String())
	}
}

// F62: stdout хука attach при update идёт в writer вызывающей функции.
func TestModulesUpdateHookOutputToWriter(t *testing.T) {
	attachEnv(t)
	setStdinTerminal(t, false)
	src := makeSourceModule(t, t.TempDir(), "qr", "")
	if err := newTestManager(t, &bytes.Buffer{}, nil).Install(context.Background(), InstallOptions{Source: src}); err != nil {
		t.Fatalf("install: %v", err)
	}
	writeFile(t, filepath.Join(src, "config.yaml"),
		"name: qr\nattach: \"echo update-hook-says-hi\"\ncommands:\n  run:\n    cmd: echo v2\n")

	var buf bytes.Buffer
	if err := newTestManager(t, &buf, unreadableStdin{t}).Update(context.Background(), UpdateOptions{Name: "qr", Yes: true}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !strings.Contains(buf.String(), "update-hook-says-hi") {
		t.Fatalf("writer output = %q, want hook stdout", buf.String())
	}
}

// hookNameModule — модуль, чьи attach/detach пишут {{.module_name}} и
// FUTI_MODULE_NAME в файлы-маркеры в каталоге out. nameLine — строка name:
// (пустая — без неё).
func hookNameModule(nameLine, out string) string {
	hook := func(file string) string {
		return "\"sh -c 'echo {{.module_name}} $FUTI_MODULE_NAME > " + filepath.Join(out, file) + "'\"\n"
	}
	return nameLine + "attach: " + hook("attach") + "detach: " + hook("detach") +
		"commands:\n  run:\n    cmd: echo run\n"
}

func readTrim(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("hook marker missing: %v", err)
	}
	return strings.TrimSpace(string(data))
}

// Хуки install, update и uninstall получают одно имя модуля: name: из
// манифеста, а без него — имя каталога установки.
func TestHooksGetSameModuleName(t *testing.T) {
	for _, tt := range []struct {
		name     string
		nameLine string
		ns       string // --namespace установки ("" — корень)
		dotName  string // имя для update/uninstall
	}{
		{"без name", "", "", "qr"},
		{"с name", "name: qr\n", "", "qr"},
		{"без name в namespace", "", "ns", "ns.qr"},
		{"с name в namespace", "name: qr\n", "ns", "ns.qr"},
	} {
		const want = "qr qr"
		t.Run(tt.name, func(t *testing.T) {
			attachEnv(t)
			out := t.TempDir()
			srcDir := filepath.Join(t.TempDir(), "qr")
			if err := os.MkdirAll(srcDir, 0o755); err != nil {
				t.Fatal(err)
			}
			cfgPath := filepath.Join(srcDir, "config.yaml")
			writeFile(t, cfgPath, hookNameModule(tt.nameLine, out))
			if err := newTestManager(t, &bytes.Buffer{}, nil).Install(context.Background(), InstallOptions{Source: srcDir, Yes: true, Namespace: tt.ns}); err != nil {
				t.Fatalf("install: %v", err)
			}
			if got := readTrim(t, filepath.Join(out, "attach")); got != want {
				t.Fatalf("install hook name = %q, want %q", got, want)
			}

			if err := os.Remove(filepath.Join(out, "attach")); err != nil {
				t.Fatal(err)
			}
			writeFile(t, cfgPath, hookNameModule(tt.nameLine, out)+"description: v2\n")
			if err := newTestManager(t, &bytes.Buffer{}, nil).Update(context.Background(), UpdateOptions{Name: tt.dotName, Yes: true}); err != nil {
				t.Fatalf("update: %v", err)
			}
			if got := readTrim(t, filepath.Join(out, "attach")); got != want {
				t.Fatalf("update hook name = %q, want %q", got, want)
			}

			if err := newTestManager(t, &bytes.Buffer{}, nil).Uninstall(context.Background(), UninstallOptions{Name: tt.dotName, Yes: true}); err != nil {
				t.Fatalf("uninstall: %v", err)
			}
			if got := readTrim(t, filepath.Join(out, "detach")); got != want {
				t.Fatalf("uninstall hook name = %q, want %q", got, want)
			}
		})
	}
}

// Модуль с name: x в каталоге y: detach получает x, а не имя из аргумента.
func TestUninstallDetachGetsManifestName(t *testing.T) {
	global := attachEnv(t)
	out := t.TempDir()
	installRawModule(t, global, hookNameModule("name: x\n", out)) // каталог qr

	err := newTestManager(t, &bytes.Buffer{}, nil).Uninstall(context.Background(), UninstallOptions{Name: "qr", Yes: true})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if got := readTrim(t, filepath.Join(out, "detach")); got != "x x" {
		t.Fatalf("detach name = %q, want %q", got, "x x")
	}
}

// Источник без name: ставится под fallbackName, а не под именем временного
// каталога fetch; хук получает то же имя.
func TestInstallWithoutNameUsesFallbackName(t *testing.T) {
	global := attachEnv(t)
	out := t.TempDir()
	srcDir := filepath.Join(t.TempDir(), "mymod")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(srcDir, "config.yaml"), hookNameModule("", out))

	if err := newTestManager(t, &bytes.Buffer{}, nil).Install(context.Background(), InstallOptions{Source: srcDir, Yes: true}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !exists(filepath.Join(global, "mymod", "config.yaml")) {
		t.Fatal("module must be installed under fallbackName")
	}
	if got := readTrim(t, filepath.Join(out, "attach")); got != "mymod mymod" {
		t.Fatalf("hook name = %q, want %q", got, "mymod mymod")
	}
}
