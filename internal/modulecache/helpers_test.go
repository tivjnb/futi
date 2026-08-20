package modulecache

import (
	"os"
	"testing"
)

// writeFile — хелпер для тестов: создаёт файл с содержимым.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %q: %v", path, err)
	}
}
