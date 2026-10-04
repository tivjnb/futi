// Package testutil — общие хелперы для тестов проекта.
package testutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ReplaceStdin подменяет os.Stdin файлом с content до конца теста.
func ReplaceStdin(t testing.TB, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdin
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = orig
		f.Close()
	})
}

// WaitForFile ждёт появления файла path (опрос с дедлайном timeout).
func WaitForFile(t testing.TB, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("файл %s не появился за %v", path, timeout)
}
