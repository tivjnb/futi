package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
	"github.com/tivjnb/futi/internal/modulerunner"
)

const examplesDir = "../../examples"

// Примеры из examples/ должны проходить строгую схему: ни ошибок, ни предупреждений.
func TestExamplesValid(t *testing.T) {
	src := discovery.NewGlobalSource(examplesDir)
	children, err := src.ListChildrenRecursive("")
	if err != nil {
		t.Fatalf("ListChildrenRecursive: %v", err)
	}
	var modules int
	for _, c := range children {
		if c.Type != "module" {
			continue
		}
		modules++
		t.Run(c.Name, func(t *testing.T) {
			if c.Conflict {
				t.Fatal("конфликт манифестов")
			}
			m, err := execmodule.ParseModule(c.ManifestPath, c.ModuleRoot)
			if err != nil {
				t.Fatalf("ParseModule: %v", err)
			}
			errs, warns := m.Validate()
			if len(errs) != 0 || len(warns) != 0 {
				t.Fatalf("errors=%v warnings=%q", errs, warns)
			}
		})
	}
	if modules < 4 {
		t.Fatalf("модулей в examples: %d, want >= 4", modules)
	}
}

// Каждый пример собирает ожидаемый argv; значения пользователя остаются
// отдельными элементами argv.
func TestExamplesRender(t *testing.T) {
	src := discovery.NewGlobalSource(examplesDir)
	tests := []struct {
		name    string
		ns, cmd string
		inputs  map[string]any
		want    []string
	}{
		{"git switch", "git", "switch", map[string]any{"branch": "feat/x"},
			[]string{"git", "switch", "--", "feat/x"}},
		{"git switch: значение с пробелом и флагом", "git", "switch", map[string]any{"branch": "--force x"},
			[]string{"git", "switch", "--", "--force x"}},
		{"git log: default count", "git", "log", nil,
			[]string{"git", "log", "--oneline", "-n", "10"}},
		{"docker logs: follow и tail", "docker", "logs",
			map[string]any{"container": "web", "follow": "true", "tail": "50"},
			[]string{"docker", "logs", "--tail=50", "--follow", "--", "web"}},
		{"docker ps: без --all", "docker", "ps", nil,
			[]string{"docker", "ps"}},
		{"files large: size склеен в один элемент", "files", "large", map[string]any{"size": "100M"},
			[]string{"find", ".", "-type", "f", "-size", "+100M"}},
		{"files count: значения не режутся", "files", "count", map[string]any{"files": []string{"a b", "c"}},
			[]string{"wc", "-l", "--", "a b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := prepareExample(src, tt.ns, tt.cmd, tt.inputs)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if !slices.Equal(p.Argv, tt.want) {
				t.Fatalf("Argv = %q, want %q", p.Argv, tt.want)
			}
		})
	}
}

// prepareExample resolves, validates and renders a module command without running it.
func prepareExample(src discovery.Source, ns, cmd string, inputs map[string]any) (*modulerunner.Prepared, error) {
	words := strings.Split(ns, ".")
	if cmd != "" {
		words = append(words, cmd)
	}
	r, _, err := modulerunner.ResolveWords(src, nil, words, "")
	if err != nil {
		return nil, err
	}
	vals, err := r.Validate(inputs)
	if err != nil {
		return nil, err
	}
	return r.Prepare(vals)
}

func TestExamplesRenderBackup(t *testing.T) {
	src := discovery.NewGlobalSource(examplesDir)
	p, err := prepareExample(src, "backup", "", map[string]any{"src": "my docs", "dest": "/tmp/out"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(p.Argv) != 3 || p.Argv[1] != "my docs" || p.Argv[2] != "/tmp/out" {
		t.Fatalf("Argv = %q", p.Argv)
	}
	if !strings.HasSuffix(filepath.ToSlash(p.Argv[0]), "/examples/backup/scripts/backup.sh") {
		t.Fatalf("Argv[0] = %q, want suffix /examples/backup/scripts/backup.sh", p.Argv[0])
	}
}

func TestExamplesBackupScriptExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("бит исполнения не определён на Windows")
	}
	fi, err := os.Stat(filepath.Join(examplesDir, "backup", "scripts", "backup.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Fatalf("backup.sh не исполняемый: %v", fi.Mode())
	}
}
