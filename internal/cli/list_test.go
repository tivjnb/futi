package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
)

func TestPrintCommandsSortedAlphabetically(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "testmod.yaml")
	writeMainTestFile(t, manifestPath, `
name: testmod
commands:
  zebra:
    cmd: "echo z"
  apple:
    cmd: "echo a"
  mango:
    cmd: "echo m"
`)

	var buf bytes.Buffer
	if err := printCommands(&buf, manifestPath, dir, false, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	appleIdx := strings.Index(output, "apple")
	mangoIdx := strings.Index(output, "mango")
	zebraIdx := strings.Index(output, "zebra")
	if appleIdx < 0 || mangoIdx < 0 || zebraIdx < 0 {
		t.Fatalf("missing command names in output:\n%s", output)
	}
	if !(appleIdx < mangoIdx && mangoIdx < zebraIdx) {
		t.Fatalf("commands not sorted alphabetically:\n%s", output)
	}
}

func TestPrintChildrenSortedAlphabetically(t *testing.T) {
	children := []discovery.Child{
		{Name: "zebra", Type: "module"},
		{Name: "apple", Type: "module"},
		{Name: "mango", Type: "namespace"},
	}

	var buf bytes.Buffer
	if err := renderChildren(&buf, core.DescribeChildren(children, nil), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	appleIdx := strings.Index(output, "apple")
	mangoIdx := strings.Index(output, "mango")
	zebraIdx := strings.Index(output, "zebra")
	if appleIdx < 0 || mangoIdx < 0 || zebraIdx < 0 {
		t.Fatalf("missing names in output:\n%s", output)
	}
	if !(appleIdx < mangoIdx && mangoIdx < zebraIdx) {
		t.Fatalf("children not sorted alphabetically:\n%s", output)
	}
}

// TestPrintChildrenSameNameOrderedByType: модуль и namespace одного имени
// выводятся в одном порядке (module, затем namespace) независимо от порядка
// во входном листинге.
func TestPrintChildrenSameNameOrderedByType(t *testing.T) {
	tests := []struct {
		name     string
		children []discovery.Child
	}{
		{"module первым", []discovery.Child{{Name: "tools", Type: "module"}, {Name: "tools", Type: "namespace"}}},
		{"namespace первым", []discovery.Child{{Name: "tools", Type: "namespace"}, {Name: "tools", Type: "module"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := renderChildren(&buf, core.DescribeChildren(tt.children, nil), false); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			out := buf.String()
			moduleIdx := strings.Index(out, "│ module")
			namespaceIdx := strings.Index(out, "│ namespace")
			if moduleIdx < 0 || namespaceIdx < 0 || moduleIdx > namespaceIdx {
				t.Fatalf("want строка module перед namespace:\n%s", out)
			}
		})
	}
}

// TestRunListShowsConflict: x.yaml рядом с x/config.yaml — одна строка x
// с описанием конфликта вместо описания модуля.
func TestRunListShowsConflict(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMainTestFile(t, filepath.Join(dir, "x.yaml"), "name: x\ndescription: Описание-из-yaml\ncmd: echo x\n")
	writeMainTestFile(t, filepath.Join(dir, "x", "config.yaml"), "name: x\ndescription: Описание-из-пакета\ncmd: echo x\n")

	var buf bytes.Buffer
	if err := runList(&buf, discovery.NewGlobalSource(dir), "", false, false, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "conflict: x.yaml and x/config.yaml") {
		t.Errorf("нет описания конфликта:\n%s", out)
	}
	if strings.Contains(out, "Описание-из") {
		t.Errorf("для конфликта не должно подтягиваться описание модуля:\n%s", out)
	}
	if n := strings.Count(out, "module"); n != 1 {
		t.Errorf("строк module = %d, want 1:\n%s", n, out)
	}

	// Вложенный namespace в `list -r`: имя записи — полный путь ns.x, но в
	// тексте конфликта — имена файлов, а не ns.x.yaml и ns.x/config.yaml.
	nested := t.TempDir()
	if err := os.MkdirAll(filepath.Join(nested, "ns", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMainTestFile(t, filepath.Join(nested, "ns", "x.yaml"), "name: x\ncmd: echo x\n")
	writeMainTestFile(t, filepath.Join(nested, "ns", "x", "config.yaml"), "name: x\ncmd: echo x\n")

	buf.Reset()
	if err := runList(&buf, discovery.NewGlobalSource(nested), "", false, true, nil); err != nil {
		t.Fatalf("list -r: unexpected error: %v", err)
	}
	out = buf.String()
	if !strings.Contains(out, "ns.x") || !strings.Contains(out, "conflict: x.yaml and x/config.yaml") {
		t.Errorf("list -r: нет записи ns.x с описанием конфликта по именам файлов:\n%s", out)
	}
	if strings.Contains(out, "ns.x.yaml") {
		t.Errorf("list -r: в тексте конфликта полный путь namespace вместо имени файла:\n%s", out)
	}
}

func TestRunListSubcommands(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "db.yaml")
	writeMainTestFile(t, manifestPath, `
name: db
commands:
  migrate:
    description: Database migrations
    commands:
      up:
        description: Run migrations up
        cmd: "echo up"
      down:
        description: Run migrations down
        cmd: "echo down"
  status:
    cmd: "echo status"
`)

	source := discovery.NewGlobalSource(dir)

	var buf bytes.Buffer
	err := runList(&buf, source, "db.migrate", false, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	downIdx := strings.Index(output, "down")
	upIdx := strings.Index(output, "up")
	if downIdx < 0 || upIdx < 0 {
		t.Fatalf("missing subcommands in output:\n%s", output)
	}
	if downIdx >= upIdx {
		t.Fatalf("subcommands not sorted alphabetically:\n%s", output)
	}
}

func TestRunListModuleWithSubcommands(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "db.yaml")
	writeMainTestFile(t, manifestPath, `
name: db
commands:
  migrate:
    description: Database migrations
    commands:
      up:
        cmd: "echo up"
  status:
    description: Show status
    cmd: "echo status"
`)

	source := discovery.NewGlobalSource(dir)

	var buf bytes.Buffer
	err := runList(&buf, source, "db", false, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "migrate") {
		t.Fatalf("missing 'migrate' in output:\n%s", output)
	}
	if !strings.Contains(output, "status") {
		t.Fatalf("missing 'status' in output:\n%s", output)
	}
}

func TestRunListNestedSubcommands(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "db.yaml")
	writeMainTestFile(t, manifestPath, `
name: db
commands:
  migrate:
    commands:
      up:
        commands:
          force:
            description: Force migration
            cmd: "echo force"
          safe:
            description: Safe migration
            cmd: "echo safe"
`)

	source := discovery.NewGlobalSource(dir)

	var buf bytes.Buffer
	err := runList(&buf, source, "db.migrate.up", false, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	forceIdx := strings.Index(output, "force")
	safeIdx := strings.Index(output, "safe")
	if forceIdx < 0 || safeIdx < 0 {
		t.Fatalf("missing nested subcommands in output:\n%s", output)
	}
}

func TestRunListLeafCommandReturnsError(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "db.yaml")
	writeMainTestFile(t, manifestPath, `
name: db
commands:
  status:
    cmd: "echo status"
`)

	source := discovery.NewGlobalSource(dir)

	var buf bytes.Buffer
	err := runList(&buf, source, "db.status", false, false, nil)
	if err == nil {
		t.Fatal("expected error for listing a leaf command")
	}
}

// TestPrintCommandsUsesLoader доказывает, что printCommands использует loader.
// Stub возвращает модуль с командой "spy_cmd"; файл содержит "real".
// Если вывод содержит spy_cmd (и не содержит real) — loader использован.
func TestPrintCommandsUsesLoader(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "stub.yaml")
	writeMainTestFile(t, manifestPath, `
name: stub
commands:
  real:
    cmd: "echo real"
`)

	spy := &stubLoader{
		mod: &execmodule.Module{
			Name: "stub",
			Root: dir,
			Commands: map[string]*execmodule.Command{
				"spy_cmd": {Description: "from spy"},
			},
		},
	}

	var buf bytes.Buffer
	if err := printCommands(&buf, manifestPath, dir, false, spy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "spy_cmd") {
		t.Fatalf("expected spy_cmd in output (loader used), got:\n%s", output)
	}
	if strings.Contains(output, "real") {
		t.Fatalf("expected no 'real' in output (loader used), got:\n%s", output)
	}
	if spy.calls != 1 {
		t.Fatalf("expected exactly 1 loader call, got %d", spy.calls)
	}
}

// TestRenderChildrenUsesLoader доказывает, что рендер детей использует loader
// для получения описаний module-children (через core.DescribeChildren).
func TestRenderChildrenUsesLoader(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "stub.yaml")

	children := []discovery.Child{
		{Name: "stub", Type: "module", ManifestPath: manifestPath, ModuleRoot: dir},
	}

	spy := &stubLoader{
		mod: &execmodule.Module{
			Name:        "stub",
			Description: "SPY_DESCRIPTION",
			Root:        dir,
		},
	}

	var buf bytes.Buffer
	if err := renderChildren(&buf, core.DescribeChildren(children, spy), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "SPY_DESCRIPTION") {
		t.Fatalf("expected SPY_DESCRIPTION in output (loader used), got:\n%s", output)
	}
	if spy.calls != 1 {
		t.Fatalf("expected exactly 1 loader call, got %d", spy.calls)
	}
}

// TestRunListUsesLoader доказывает, что runList использует loader при
// progressive resolution для проверки subcommands.
func TestRunListUsesLoader(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "db.yaml")
	writeMainTestFile(t, manifestPath, `
name: db
commands:
  migrate:
    commands:
      real:
        cmd: "echo real"
`)

	source := discovery.NewGlobalSource(dir)

	spy := &stubLoader{
		mod: &execmodule.Module{
			Name: "db",
			Root: dir,
			Commands: map[string]*execmodule.Command{
				"migrate": {
					Commands: map[string]*execmodule.Command{
						"spy_sub": {Description: "from spy"},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	if err := runList(&buf, source, "db.migrate", false, false, spy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "spy_sub") {
		t.Fatalf("expected spy_sub in output (loader used), got:\n%s", output)
	}
	if spy.calls != 1 {
		t.Fatalf("expected exactly 1 loader call, got %d", spy.calls)
	}
}

// --- Шаг 2: рефакторинг log.Fatalf в ошибках ---

// --- Шаг 4: futi validate ---

// TestPrintCommandsShowsDefaultCmd проверяет, что при наличии cmd на уровне
// модуля (default cmd) в выводе list появляется маркер (default).
func TestPrintCommandsShowsDefaultCmd(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "echo_mod.yaml")
	writeMainTestFile(t, manifestPath, `
name: echo_mod
cmd: "echo {{.args}}"
commands:
  upper:
    cmd: "echo {{.args}} | tr a-z A-Z"
`)
	var buf bytes.Buffer
	if err := printCommands(&buf, manifestPath, dir, false, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "(default)") {
		t.Fatalf("expected '(default)' marker in output:\n%s", output)
	}
	if !strings.Contains(output, "upper") {
		t.Fatalf("expected 'upper' command in output:\n%s", output)
	}
}

// TestPrintCommandsNoDefaultCmd проверяет, что при отсутствии cmd на уровне
// модуля маркер (default) НЕ появляется.
func TestPrintCommandsNoDefaultCmd(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "docker.yaml")
	writeMainTestFile(t, manifestPath, `
name: docker
commands:
  ps:
    cmd: "docker ps"
`)
	var buf bytes.Buffer
	if err := printCommands(&buf, manifestPath, dir, false, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if strings.Contains(output, "(default)") {
		t.Fatalf("'(default)' should not appear without module cmd:\n%s", output)
	}
}

// TestRunListSubcommandWithFallbackCmd проверяет, что subcommand с cmd
// (fallback при отсутствии явной subcommand) отображается с маркером (default).
func TestRunListSubcommandWithFallbackCmd(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "db.yaml")
	writeMainTestFile(t, manifestPath, `
name: db
commands:
  migrate:
    description: Database migrations
    cmd: "migrate"
    commands:
      up:
        cmd: "migrate up"
      down:
        cmd: "migrate down"
`)
	source := discovery.NewGlobalSource(dir)
	var buf bytes.Buffer
	if err := runList(&buf, source, "db.migrate", false, false, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "(default)") {
		t.Fatalf("expected '(default)' marker for fallback cmd:\n%s", output)
	}
	if !strings.Contains(output, "up") || !strings.Contains(output, "down") {
		t.Fatalf("expected subcommands in output:\n%s", output)
	}
}

func TestRunListConflictInSubnamespaceReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		"a.yaml":          "name: a\ncommands:\n  b:\n    commands:\n      c:\n        cmd: \"echo c\"\n",
		"a/b.yaml":        "name: b\ncmd: echo\n",
		"a/b/config.yaml": "name: b\ncmd: echo\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	err := runList(&buf, discovery.NewGlobalSource(dir), "a.b", false, false, nil)
	if err == nil {
		t.Fatalf("expected conflict error, got output:\n%s", buf.String())
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error %q does not mention conflict", err)
	}
}

func TestRunListConflictInProgressiveResolveReturnsError(t *testing.T) {
	dir := t.TempDir()
	// a.b.c: ns "a.b" конфликтен, "a" — валидный модуль с командой b.
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		"a.yaml":          "name: a\ncommands:\n  b:\n    commands:\n      c:\n        commands:\n          d:\n            cmd: \"echo d\"\n",
		"a/b.yaml":        "name: b\ncmd: echo\n",
		"a/b/config.yaml": "name: b\ncmd: echo\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	err := runList(&buf, discovery.NewGlobalSource(dir), "a.b.c", false, false, nil)
	if err == nil {
		t.Fatalf("expected conflict error, got output:\n%s", buf.String())
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error %q does not mention conflict", err)
	}
}

// Пустой namespace у runList — законный запрос корня: Resolve("") не вызывается,
// корневой config.yaml не считается модулем (audit F1/F2).
func TestRunListEmptyNamespaceListsRoot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.yaml"), "name: root\ncmd: echo ROOTCFG\n")
	writeFile(t, filepath.Join(dir, "a.yaml"), "name: a\ncmd: echo a\n")

	for _, tc := range []struct {
		name    string
		recurse bool
		verbose bool
	}{
		{"plain", false, false},
		{"recursive", true, false},
		{"verbose", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := runList(&buf, discovery.NewGlobalSource(dir), "", tc.verbose, tc.recurse, nil)
			if err != nil {
				t.Fatalf("runList: %v", err)
			}
			out := buf.String()
			if !strings.Contains(out, "a") {
				t.Fatalf("в выводе нет модуля a:\n%s", out)
			}
			if strings.Contains(out, "ROOTCFG") || strings.Contains(out, "root") {
				t.Fatalf("корневой config.yaml попал в список:\n%s", out)
			}
		})
	}
}

// То же на продовой цепочке источников: Layered(Composite, ...).
func TestRunListEmptyNamespaceOnProdSourceChain(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.yaml"), "name: a\ncmd: echo a\n")
	cfg := &config.Config{Sources: []config.SourceConfig{{Path: dir}}}
	source := discovery.NewLayeredSource(discovery.NewCompositeSource(cfg), discovery.NewGlobalSource(t.TempDir()))

	var buf bytes.Buffer
	if err := runList(&buf, source, "", false, false, nil); err != nil {
		t.Fatalf("runList: %v", err)
	}
	if !strings.Contains(buf.String(), "a") {
		t.Fatalf("в выводе нет модуля a:\n%s", buf.String())
	}
}

// partialSource — Composite из читаемого source A (с x.yaml) и несуществующего B.
func partialSource(t *testing.T) (src discovery.Source, dirA, dirB string) {
	t.Helper()
	dirA = t.TempDir()
	dirB = filepath.Join(t.TempDir(), "missing")
	writeMainTestFile(t, filepath.Join(dirA, "x.yaml"), "name: x\ndescription: Икс\ncmd: echo x\n")
	cfg := &config.Config{Sources: []config.SourceConfig{{Path: dirA}, {Path: dirB}}}
	return discovery.NewCompositeSource(cfg), dirA, dirB
}

func TestRunListPartialListingReturnsPartialError(t *testing.T) {
	src, _, _ := partialSource(t)
	var buf bytes.Buffer
	err := runList(&buf, src, "", false, false, nil)
	var pe *discovery.PartialError
	if !errors.As(err, &pe) {
		t.Fatalf("runList err = %v, want *discovery.PartialError", err)
	}
	found := false
	for _, line := range strings.Split(buf.String(), "\n") {
		cells := strings.Split(line, "│")
		// cells: ["", TYPE, NAME, DESCRIPTION, ""]
		if len(cells) >= 4 && strings.TrimSpace(cells[1]) == "module" && strings.TrimSpace(cells[2]) == "x" {
			found = true
		}
	}
	if !found {
		t.Errorf("нет строки модуля x в листинге читаемых sources:\n%s", buf.String())
	}
}

func TestPrintChildrenConflictSources(t *testing.T) {
	children := []discovery.Child{{
		Type: "module", Name: "x", Conflict: true,
		ManifestPath:    "/a/x.yaml",
		ConflictSources: []string{"/a", "/b"},
	}}
	var buf bytes.Buffer
	if err := renderChildren(&buf, core.DescribeChildren(children, nil), false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "conflict: module in several sources: /a, /b") {
		t.Errorf("нет текста конфликта sources:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "x.yaml and") {
		t.Errorf("текст конфликта файлов вместо sources:\n%s", buf.String())
	}
}

// TestRunListErrorsHaveSingleListPrefix: все ошибки runList начинаются ровно с
// одного «list: » — вызывающий печатает их как есть, без своего префикса.
func TestRunListErrorsHaveSingleListPrefix(t *testing.T) {
	dir := t.TempDir()
	writeMainTestFile(t, filepath.Join(dir, "a.yaml"), "name: a\ncmd: echo a\n")
	writeMainTestFile(t, filepath.Join(dir, "c.yaml"), "name: c\ncmd: echo\n")
	if err := os.Mkdir(filepath.Join(dir, "c"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMainTestFile(t, filepath.Join(dir, "c", "config.yaml"), "name: c\ncmd: echo\n")

	for _, ns := range []string{"nope", "a.nope", "c"} {
		var buf bytes.Buffer
		err := runList(&buf, discovery.NewGlobalSource(dir), ns, false, false, nil)
		if err == nil {
			t.Fatalf("runList(%q) err = nil, want ошибка", ns)
		}
		msg := err.Error()
		if !strings.HasPrefix(msg, "list: ") || strings.HasPrefix(msg, "list: list: ") {
			t.Errorf("runList(%q) err = %q, want ровно один префикс «list: »", ns, msg)
		}
	}
}

// TestRunListStripsControlChars: ESC в описаниях модуля и команд не доходит до
// терминала ни в таблице модулей, ни в таблице команд (audit F18).
func TestRunListStripsControlChars(t *testing.T) {
	dir := t.TempDir()
	// YAML double-quoted: \e разворачивается в ESC.
	writeMainTestFile(t, filepath.Join(dir, "evil.yaml"), "name: evil\ndescription: \"mod\\e[2Jdesc\"\ncommands:\n  run:\n    description: \"cmd\\e]0;pwned\\adesc\"\n    cmd: echo\n")
	source := discovery.NewGlobalSource(dir)

	for _, ns := range []string{"", "evil"} {
		var buf bytes.Buffer
		if err := runList(&buf, source, ns, false, false, nil); err != nil {
			t.Fatalf("runList(%q): %v", ns, err)
		}
		out := buf.String()
		if strings.ContainsAny(out, "\x1b\x07") {
			t.Fatalf("runList(%q) output contains control chars: %q", ns, out)
		}
		want := "mod[2Jdesc"
		if ns == "evil" {
			want = "cmd]0;pwneddesc"
		}
		if !strings.Contains(out, want) {
			t.Fatalf("runList(%q) output missing %q:\n%s", ns, want, out)
		}
	}
}

// TestRunListVerboseStripsControlCharsInPath: ESC в имени каталога/файла модуля
// не доходит до терминала через колонку PATH (audit F18, review).
func TestRunListVerboseStripsControlCharsInPath(t *testing.T) {
	dir := t.TempDir()
	// Пропуск допустим только если ФС не позволяет создать имя с ESC.
	if err := os.WriteFile(filepath.Join(dir, "ev\x1b[2Jil.yaml"), []byte("name: evil\ncommands:\n  run:\n    cmd: echo\n"), 0o644); err != nil {
		t.Skipf("ФС не принимает ESC в имени файла: %v", err)
	}
	source := discovery.NewGlobalSource(dir)

	for _, ns := range []string{"", "ev\x1b[2Jil"} {
		var buf bytes.Buffer
		if err := runList(&buf, source, ns, true, false, nil); err != nil {
			t.Fatalf("runList(%q): %v", ns, err)
		}
		if strings.Contains(buf.String(), "\x1b") {
			t.Fatalf("runList(%q) -v output contains ESC: %q", ns, buf.String())
		}
	}
}
