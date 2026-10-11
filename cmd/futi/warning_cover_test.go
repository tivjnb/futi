package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/discovery"
)

// resetPrintedWarnings очищает глобальное состояние напечатанных предупреждений.
func resetPrintedWarnings(t *testing.T) {
	t.Helper()
	clear := func() {
		printedWarningsMu.Lock()
		printedWarnings = map[string]bool{}
		warnedReads = nil
		printedWarningsMu.Unlock()
	}
	clear()
	t.Cleanup(clear)
}

func partialWith(pe *fs.PathError) error {
	return &discovery.PartialError{Errs: []error{fmt.Errorf("discovery: %w", pe)}}
}

func TestCoveredByWarning(t *testing.T) {
	warn := &fs.PathError{Op: "stat", Path: "/m/a/config.yaml", Err: fs.ErrPermission}
	tests := []struct {
		name string
		warn *fs.PathError // nil — предупреждение не печатается
		err  error
		want bool
	}{
		{"тот же путь, другая операция", warn,
			fmt.Errorf("list: %w", &fs.PathError{Op: "open", Path: "/m/a/config.yaml", Err: fs.ErrPermission}), true},
		{"путь предупреждения внутри пути ошибки", warn,
			fmt.Errorf("help: %w", &fs.PathError{Op: "open", Path: "/m/a", Err: fs.ErrPermission}), true},
		{"путь ошибки внутри пути предупреждения", &fs.PathError{Op: "open", Path: "/m/a", Err: fs.ErrPermission},
			fmt.Errorf("run: %w", &fs.PathError{Op: "stat", Path: "/m/a/b.yaml", Err: fs.ErrPermission}), true},
		{"другая причина", warn,
			&fs.PathError{Op: "stat", Path: "/m/a/config.yaml", Err: fs.ErrNotExist}, false},
		{"несвязанный путь", warn,
			&fs.PathError{Op: "stat", Path: "/m/ab/config.yaml", Err: fs.ErrPermission}, false},
		{"нет PathError", warn, errors.New("boom"), false},
		{"предупреждение не печаталось", nil,
			&fs.PathError{Op: "stat", Path: "/m/a/config.yaml", Err: fs.ErrPermission}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetPrintedWarnings(t)
			if tt.warn != nil {
				var buf bytes.Buffer
				if !printPartialWarnings(&buf, partialWith(tt.warn)) {
					t.Fatal("printPartialWarnings = false")
				}
			}
			if got := coveredByWarning(tt.err); got != tt.want {
				t.Errorf("coveredByWarning(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// unreadableNamespace готовит каталог модулей с нечитаемым a/ и напечатанным
// предупреждением (как это делает cache.Start до разбора команды).
func unreadableNamespace(t *testing.T) (discovery.Source, string) {
	t.Helper()
	if os.Geteuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("chmod 000 не ограничивает доступ для root и в Windows")
	}
	resetPrintedWarnings(t)
	mods := t.TempDir()
	if err := os.MkdirAll(filepath.Join(mods, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMainTestFile(t, filepath.Join(mods, "a", "b.yaml"), "cmd: echo hi\n")
	writeMainTestFile(t, filepath.Join(mods, "ok.yaml"), "cmd: echo ok\n")
	dirA := filepath.Join(mods, "a")
	if err := os.Chmod(dirA, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dirA, 0o755) })
	src := discovery.NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: mods}}})
	_, err := src.ListChildrenRecursive("")
	var warn bytes.Buffer
	if !printPartialWarnings(&warn, err) || !strings.Contains(warn.String(), "permission denied") {
		t.Fatalf("предупреждение не получено: err=%v", err)
	}
	return src, mods
}

func TestListErrorShortWhenCoveredByWarning(t *testing.T) {
	src, _ := unreadableNamespace(t)
	err := runList(&bytes.Buffer{}, src, "a", false, false, nil)
	if err == nil {
		t.Fatal("runList: ожидалась ошибка")
	}
	want := `list: cannot read namespace "a" (see warning above)`
	if got := listErrorText(err, "a"); got != want {
		t.Errorf("listErrorText = %q, want %q", got, want)
	}
}

func TestListErrorFullWhenDifferentCause(t *testing.T) {
	_, mods := unreadableNamespace(t)
	for _, tt := range []struct {
		name string
		err  error
	}{
		{"та же причина, несвязанный путь", fmt.Errorf("list: %w", &fs.PathError{Op: "stat", Path: filepath.Join(mods, "other", "config.yaml"), Err: fs.ErrPermission})},
		{"другая причина, путь внутри нечитаемого a", fmt.Errorf("list: %w", &fs.PathError{Op: "stat", Path: filepath.Join(mods, "a", "config.yaml"), Err: fs.ErrNotExist})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := listErrorText(tt.err, "a"); got != tt.err.Error() {
				t.Errorf("listErrorText = %q, want полное %q", got, tt.err.Error())
			}
		})
	}
}

func TestHelpErrorShortWhenCoveredByWarning(t *testing.T) {
	src, _ := unreadableNamespace(t)
	var stdout, stderr bytes.Buffer
	code := systemHelpCode(&stdout, &stderr, src, nil, []string{"a"})
	want := "help: cannot read namespace \"a\" (see warning above)\n"
	if code != 1 || stderr.String() != want {
		t.Errorf("code=%d stderr=%q, want 1 и %q", code, stderr.String(), want)
	}
}

func TestRunErrorShortWhenCoveredByWarning(t *testing.T) {
	for _, tt := range []struct {
		name string
		pa   *parsedArgs
		want string
	}{
		{"a b", &parsedArgs{namespace: "a", command: "b"}, "run: cannot read namespace \"a.b\" (see warning above)\n"},
		{"a", &parsedArgs{namespace: "a"}, "run: cannot read namespace \"a\" (see warning above)\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			src, _ := unreadableNamespace(t)
			var stdout, stderr bytes.Buffer
			code, runErr := runParsed(t.Context(), &stdout, &stderr, src, nil, tt.pa)
			if code != 1 || runErr == nil || stderr.String() != tt.want {
				t.Errorf("code=%d runErr=%v stderr=%q, want 1 и %q", code, runErr, stderr.String(), tt.want)
			}
		})
	}
}

func TestRunErrorFullWhenNotFoundInReadableDir(t *testing.T) {
	src, _ := unreadableNamespace(t)
	var stdout, stderr bytes.Buffer
	code, _ := runParsed(t.Context(), &stdout, &stderr, src, nil, &parsedArgs{namespace: "nope", command: "x"})
	if code != 1 || !strings.HasPrefix(stderr.String(), "run: ") || strings.Contains(stderr.String(), "see warning above") {
		t.Errorf("code=%d stderr=%q, want полное сообщение без сокращения", code, stderr.String())
	}
}
