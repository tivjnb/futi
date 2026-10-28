package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/discovery"
)

// TestPrintPartialWarningsDedupSamePathError: одна и та же проблема чтения,
// пришедшая из листингов с разной обёрткой (рекурсивный и нерекурсивный),
// печатается один раз.
func TestPrintPartialWarningsDedupSamePathError(t *testing.T) {
	if os.Geteuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("chmod 000 не ограничивает доступ для root и в Windows")
	}
	dirA, dirB := t.TempDir(), t.TempDir()
	writeMainTestFile(t, filepath.Join(dirA, "x.yaml"), "name: x\ncmd: echo x\n")
	if err := os.Chmod(dirB, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dirB, 0o755) })
	src := discovery.NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: dirA}, {Path: dirB}}})

	_, errRec := src.ListChildrenRecursive("")
	_, errFlat := src.ListChildren("")
	var buf bytes.Buffer
	for _, err := range []error{errRec, errFlat} {
		if !printPartialWarnings(&buf, err) {
			t.Fatalf("printPartialWarnings(%v) = false, want true", err)
		}
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "warning: ") || !strings.Contains(lines[0], dirB) {
		t.Errorf("предупреждения:\n%s\nwant ровно одну строку warning: с путём %s", buf.String(), dirB)
	}
}

func TestPrintPartialWarnings(t *testing.T) {
	src, _, dirB := partialSource(t)
	_, err := src.ListChildren("")
	var buf bytes.Buffer
	if !printPartialWarnings(&buf, err) {
		t.Fatalf("printPartialWarnings(%v) = false, want true", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "warning: discovery: source \""+dirB+"\"") {
		t.Errorf("предупреждение = %q, want префикс warning: discovery: source %q", out, dirB)
	}
	// Повторный вызов с той же ошибкой ничего не добавляет.
	buf.Reset()
	if !printPartialWarnings(&buf, err) {
		t.Fatalf("повторный вызов вернул false")
	}
	if buf.Len() != 0 {
		t.Errorf("повторное предупреждение напечатано: %q", buf.String())
	}
	// Не PartialError — false и тишина.
	buf.Reset()
	if printPartialWarnings(&buf, errors.New("boom")) || buf.Len() != 0 {
		t.Errorf("обычная ошибка: ожидалось false и пустой вывод, got %q", buf.String())
	}
	if printPartialWarnings(&buf, nil) {
		t.Errorf("nil: ожидалось false")
	}
}
