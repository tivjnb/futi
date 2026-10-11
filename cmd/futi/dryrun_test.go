package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// dryRunEnv создаёт каталог модулей с m.yaml и рабочий каталог, в котором
// не должно появиться файлов при dry-run.
func dryRunEnv(t *testing.T) (modules, workdir string) {
	t.Helper()
	root := t.TempDir()
	modules = filepath.Join(root, "modules")
	if err := os.MkdirAll(modules, 0o755); err != nil {
		t.Fatal(err)
	}
	workdir = filepath.Join(root, "work")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `commands:
  mk:
    params:
      - name: file
        required: true
    cmd: "touch {{quote .file}}"
  help:
    cmd: "echo helped"
  say:
    params:
      - name: file
        required: true
    cmd: "echo {{quote .file}} {{.args}}"
`
	plain := "commands:\n  go:\n    cmd: \"echo go\"\n"
	if err := os.WriteFile(filepath.Join(modules, "plain.yaml"), []byte(plain), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modules, "m.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return modules, workdir
}

func TestFutiDryRun(t *testing.T) {
	modules, workdir := dryRunEnv(t)
	run := func(args ...string) (string, string, int) {
		return runFutiIn(t, modules, workdir, args...)
	}

	t.Run("печатает команду и не исполняет", func(t *testing.T) {
		stdout, stderr, code := run("--dry-run", "m", "mk", "--file", "x y")
		if code != 0 || stdout != "touch 'x y'\n" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		if _, err := os.Stat(filepath.Join(workdir, "x y")); err == nil {
			t.Fatal("файл создан: команда исполнилась")
		}
	})

	t.Run("args после -- попадают в argv", func(t *testing.T) {
		stdout, stderr, code := run("--dry-run", "m", "say", "--file", "x", "--", "extra", "more")
		if code != 0 || stdout != "echo x extra more\n" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	})

	t.Run("ошибка Prepare как без dry-run", func(t *testing.T) {
		_, wantErr, wantCode := run("m", "mk")
		stdout, stderr, code := run("--dry-run", "m", "mk")
		if wantCode != 1 || code != wantCode || stderr != wantErr || stdout != "" {
			t.Fatalf("dry: code=%d stderr=%q stdout=%q; обычный: code=%d stderr=%q", code, stderr, stdout, wantCode, wantErr)
		}
	})

	t.Run("пользовательская help — dry-run этой команды", func(t *testing.T) {
		stdout, stderr, code := run("--dry-run", "m", "help")
		if code != 0 || stdout != "echo helped\n" {
			t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
	})

	t.Run("справка по пути", func(t *testing.T) {
		for _, f := range []string{"-h", "--help"} {
			stdout, stderr, code := run("--dry-run", "m", "mk", f)
			if code != 0 || !strings.Contains(stdout, "--file") {
				t.Fatalf("%s: code=%d stdout=%q stderr=%q", f, code, stdout, stderr)
			}
		}
	})

	t.Run("несуществующий путь — ошибка как без dry-run", func(t *testing.T) {
		_, wantErr, wantCode := run("nope", "help")
		stdout, stderr, code := run("--dry-run", "nope", "help")
		if wantCode != 1 || code != wantCode || stderr != wantErr || stdout != "" {
			t.Fatalf("dry: code=%d stderr=%q stdout=%q; обычный: code=%d stderr=%q", code, stderr, stdout, wantCode, wantErr)
		}
		if strings.Contains(stderr, "applies only") {
			t.Fatalf("ошибка отказа dry-run вместо «не найден»: %q", stderr)
		}
	})

	const wantMsg = "--dry-run applies only to module commands"
	for _, args := range [][]string{
		{"--dry-run"},
		{"--dry-run", "-h"},
		{"--dry-run", "--help"},
		{"--dry-run", "--version"},
		{"--dry-run", "list"},
		{"--dry-run", "m", "list"},
		{"--dry-run", "modules", "list"},
		{"--dry-run", "modules", "init", "zz"},
		{"--dry-run", "modules", "validate", "m"},
		{"--dry-run", "cache", "status"},
		{"--dry-run", "completion", "bash"},
		{"--dry-run", "__complete", ""},
		{"--dry-run", "plain", "help"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stdout, stderr, code := run(args...)
			if code != 1 || stdout != "" || !strings.Contains(stderr, wantMsg) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
		})
	}

	t.Run("не первый аргумент — обычный флаг модуля", func(t *testing.T) {
		_, stderr, code := run("m", "mk", "--dry-run")
		if code == 0 || strings.Contains(stderr, wantMsg) || !strings.Contains(stderr, `unknown param "dry-run"`) {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
	})
}

// runFutiIn запускает собранный futi с изолированным HOME и рабочим каталогом
// workdir; возвращает stdout, stderr и код выхода.
func runFutiIn(t *testing.T, modules, workdir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(builtFutiBinary(t), args...)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		"FUTI_MODULES_DIR="+modules,
	)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errb.String(), code
}
