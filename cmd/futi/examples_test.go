package main

import (
	"path/filepath"
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

// Каждый пример собирает ожидаемый argv; пользовательские значения проходят
// через {{quote}} и остаются одним токеном.
func TestExamplesRender(t *testing.T) {
	src := discovery.NewGlobalSource(examplesDir)
	tests := []struct {
		name     string
		ns, cmd  string
		params   map[string]string
		args     string
		argsList []string
		want     []string
	}{
		{"git switch", "git", "switch", map[string]string{"branch": "feat/x"}, "", nil,
			[]string{"git", "switch", "feat/x"}},
		{"git switch: значение с пробелом и флагом", "git", "switch", map[string]string{"branch": "--force x"}, "", nil,
			[]string{"git", "switch", "--force x"}},
		{"git log: default count", "git", "log", map[string]string{}, "", nil,
			[]string{"git", "log", "--oneline", "-n", "10"}},
		{"docker logs: follow и tail", "docker", "logs",
			map[string]string{"container": "web", "follow": "true", "tail": "50"}, "", nil,
			[]string{"docker", "logs", "--tail", "50", "--follow", "web"}},
		{"files large: size склеен в один токен", "files", "large", map[string]string{"size": "100M"}, "", nil,
			[]string{"find", ".", "-type", "f", "-size", "+100M"}},
		{"files count: позиционные не разбираются повторно", "files", "count", nil, "a b c", []string{"a b", "c"},
			[]string{"sh", "-c", `echo "$#"`, "sh", "a b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := modulerunner.Prepare(src, tt.ns, tt.cmd, tt.params, tt.args, tt.argsList, nil)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			if !slices.Equal(p.Argv, tt.want) {
				t.Fatalf("Argv = %q, want %q", p.Argv, tt.want)
			}
		})
	}
}

func TestExamplesRenderBackup(t *testing.T) {
	src := discovery.NewGlobalSource(examplesDir)
	p, err := modulerunner.Prepare(src, "backup", "", map[string]string{"src": "my docs", "dest": "/tmp/out"}, "", nil, nil)
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
