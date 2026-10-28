package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// TestNoCyrillicInUserFacingStrings — сторож: пользовательский вывод (сообщения
// об ошибках, help, скрипты completion) только на английском. Комментарии
// не проверяются (парсер их отбрасывает), тестовые файлы пропускаются.
func TestNoCyrillicInUserFacingStrings(t *testing.T) {
	var found []string
	for _, root := range []string{"../../cmd", "../../internal"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				if strings.IndexFunc(lit.Value, func(r rune) bool { return unicode.Is(unicode.Cyrillic, r) }) >= 0 {
					found = append(found, fset.Position(lit.Pos()).String()+": "+lit.Value)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range found {
		t.Error(s)
	}
}
