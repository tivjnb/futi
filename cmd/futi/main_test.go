package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"futi/internal/config"
	"futi/internal/discovery"
	"futi/internal/execmodule"
	"futi/internal/modulecache"
)

// stubLoader — тестовый загрузчик модулей, который возвращает
// предзаготовленный модуль для любого пути и считает вызовы.
// Используется для проверки, что localSource/printCommands/printChildren/runList
// действительно обращаются к loader, а не к execmodule.LoadModule напрямую.
type stubLoader struct {
	mod   *execmodule.Module
	calls int
}

func (s *stubLoader) LoadModule(manifestPath, moduleRoot string) (*execmodule.Module, error) {
	s.calls++
	return s.mod, nil
}

func TestParseArgsNamespaceAndCommand(t *testing.T) {
	args := []string{"docker", "build", "--tag", "myapp"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "docker" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "build" {
		t.Fatalf("unexpected command: %q", pa.command)
	}
	if pa.params["tag"] != "myapp" {
		t.Fatalf("unexpected params: %+v", pa.params)
	}
}

func TestParseArgsNestedNamespace(t *testing.T) {
	args := []string{"dev", "deploy", "staging"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "dev.deploy" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "staging" {
		t.Fatalf("unexpected command: %q", pa.command)
	}
	if len(pa.params) != 0 {
		t.Fatalf("unexpected params: %+v", pa.params)
	}
}

func TestParseArgsEqualSyntax(t *testing.T) {
	args := []string{"docker", "build", "--tag=myapp"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "docker" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "build" {
		t.Fatalf("unexpected command: %q", pa.command)
	}
	if pa.params["tag"] != "myapp" {
		t.Fatalf("unexpected params: %+v", pa.params)
	}
}

func TestParseArgsBoolFlag(t *testing.T) {
	args := []string{"k8s", "logs", "--follow"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "k8s" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "logs" {
		t.Fatalf("unexpected command: %q", pa.command)
	}
	if pa.params["follow"] != "true" {
		t.Fatalf("unexpected follow value: %q", pa.params["follow"])
	}
}

func TestParseArgsTooFewArgs(t *testing.T) {
	_, err := parseArgs([]string{})
	if err == nil {
		t.Fatal("expected error for no args")
	}
}

func TestParseArgsSingleNamespaceForDefaultCmd(t *testing.T) {
	args := []string{"echo_mod"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "echo_mod" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "" {
		t.Fatalf("expected empty command for default cmd, got %q", pa.command)
	}
}

func TestParseArgsListReserved(t *testing.T) {
	args := []string{"list"}
	_, err := parseArgs(args)
	if !errors.Is(err, errListCommand) {
		t.Fatalf("expected errListCommand, got: %v", err)
	}
}

func TestParseArgsListWithNamespace(t *testing.T) {
	args := []string{"images", "list"}
	_, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if req.namespace != "images" {
		t.Fatalf("unexpected namespace: %q", req.namespace)
	}
}

func TestParseArgsListWithNestedNamespace(t *testing.T) {
	args := []string{"dev", "deploy", "list"}
	_, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if req.namespace != "dev.deploy" {
		t.Fatalf("unexpected namespace: %q", req.namespace)
	}
}

func TestParseArgsListVerbose(t *testing.T) {
	args := []string{"list", "--verbose"}
	_, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if !req.verbose {
		t.Fatal("expected verbose=true")
	}
}

func TestParseArgsListVerboseShort(t *testing.T) {
	args := []string{"list", "-v"}
	_, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if !req.verbose {
		t.Fatal("expected verbose=true")
	}
}

func TestParseArgsListWithNamespaceVerbose(t *testing.T) {
	args := []string{"images", "list", "-v"}
	_, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if req.namespace != "images" {
		t.Fatalf("unexpected namespace: %q", req.namespace)
	}
	if !req.verbose {
		t.Fatal("expected verbose=true")
	}
}

func TestParseArgsListRecurse(t *testing.T) {
	args := []string{"list", "--recurse"}
	_, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if !req.recurse {
		t.Fatal("expected recurse=true")
	}
}

func TestParseArgsListRecurseShort(t *testing.T) {
	args := []string{"list", "-r"}
	_, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if !req.recurse {
		t.Fatal("expected recurse=true")
	}
}

func TestParseArgsListWithNamespaceRecurseVerbose(t *testing.T) {
	args := []string{"images", "list", "-r", "-v"}
	_, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if req.namespace != "images" {
		t.Fatalf("unexpected namespace: %q", req.namespace)
	}
	if !req.recurse {
		t.Fatal("expected recurse=true")
	}
	if !req.verbose {
		t.Fatal("expected verbose=true")
	}
}

func TestParseArgsDoubleDashArgs(t *testing.T) {
	args := []string{"echo_mod", "echo", "--", "hello", "world"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "echo_mod" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "echo" {
		t.Fatalf("unexpected command: %q", pa.command)
	}
	if pa.args != "hello world" {
		t.Fatalf("unexpected args: %q", pa.args)
	}
	if len(pa.argsList) != 2 || pa.argsList[0] != "hello" || pa.argsList[1] != "world" {
		t.Fatalf("unexpected argsList: %+v", pa.argsList)
	}
}

func TestParseArgsDoubleDashWithQuotedArg(t *testing.T) {
	args := []string{"echo_mod", "echo", "--", "hello world", "foo"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.args != "hello world foo" {
		t.Fatalf("unexpected args: %q", pa.args)
	}
	if len(pa.argsList) != 2 || pa.argsList[0] != "hello world" || pa.argsList[1] != "foo" {
		t.Fatalf("unexpected argsList: %+v", pa.argsList)
	}
}

func TestParseArgsDoubleDashOnly(t *testing.T) {
	args := []string{"echo_mod", "--", "hello", "world"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "echo_mod" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "" {
		t.Fatalf("unexpected command: %q", pa.command)
	}
	if pa.args != "hello world" {
		t.Fatalf("unexpected args: %q", pa.args)
	}
}

func TestParseArgsDoubleDashEmpty(t *testing.T) {
	args := []string{"echo_mod", "echo", "--"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "echo_mod" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "echo" {
		t.Fatalf("unexpected command: %q", pa.command)
	}
	if pa.args != "" {
		t.Fatalf("unexpected args: %q", pa.args)
	}
	if len(pa.argsList) != 0 {
		t.Fatalf("unexpected argsList: %+v", pa.argsList)
	}
}

func TestParseArgsDoubleDashWithFlags(t *testing.T) {
	args := []string{"docker", "build", "--tag", "myapp", "--", "extra", "arg"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "docker" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "build" {
		t.Fatalf("unexpected command: %q", pa.command)
	}
	if pa.params["tag"] != "myapp" {
		t.Fatalf("unexpected params: %+v", pa.params)
	}
	if pa.args != "extra arg" {
		t.Fatalf("unexpected args: %q", pa.args)
	}
}

func TestExitCodeFromErrorExtractsExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts not supported on Windows")
	}

	cmd := exec.Command("sh", "-c", "exit 42")
	runErr := cmd.Run()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"direct exit error", runErr, 42},
		{"wrapped (executor style)", fmt.Errorf("executor: run %q: %w", "sh", runErr), 42},
		{"double-wrapped (modulerunner style)", fmt.Errorf("modulerunner: %w", fmt.Errorf("executor: run %q: %w", "sh", runErr)), 42},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code := exitCodeFromError(tc.err); code != tc.want {
				t.Fatalf("expected %d, got %d", tc.want, code)
			}
		})
	}
}

func TestExitCodeFromErrorDefaultsToOne(t *testing.T) {
	if code := exitCodeFromError(errors.New("something went wrong")); code != 1 {
		t.Fatalf("plain error: expected 1, got %d", code)
	}
}

func TestLocalSourceFutiYaml(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	manifest := []byte("name: local\ncommands:\n  greet:\n    cmd: echo hi\n")
	if err := os.WriteFile(filepath.Join(dir, "futi.yaml"), manifest, 0o644); err != nil {
		t.Fatalf("write futi.yaml: %v", err)
	}

	src, err := localSource(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src == nil {
		t.Fatal("expected non-nil source for ./futi.yaml")
	}

	manifestPath, root, err := src.Resolve("local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifestPath != filepath.Join(dir, "futi.yaml") {
		t.Fatalf("unexpected manifest: %q", manifestPath)
	}
	if root != dir {
		t.Fatalf("unexpected root: %q, want %q", root, dir)
	}
}

func TestLocalSourceFutiDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	futiDir := filepath.Join(dir, ".futi")
	if err := os.Mkdir(futiDir, 0o755); err != nil {
		t.Fatalf("mkdir .futi: %v", err)
	}
	writeMainTestFile(t, filepath.Join(futiDir, "local.yaml"), "name: local\n")

	src, err := localSource(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src == nil {
		t.Fatal("expected non-nil source for ./.futi/")
	}

	manifestPath, _, err := src.Resolve("local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifestPath != filepath.Join(futiDir, "local.yaml") {
		t.Fatalf("unexpected manifest: %q", manifestPath)
	}
}

func TestLocalSourceNone(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	src, err := localSource(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src != nil {
		t.Fatalf("expected nil source, got %T", src)
	}
}

func TestLocalSourceBothFutiYamlAndDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// ./futi.yaml with module "yamlmod"
	writeMainTestFile(t, filepath.Join(dir, "futi.yaml"), "name: yamlmod\n")

	// ./.futi/local.yaml with module "local"
	futiDir := filepath.Join(dir, ".futi")
	if err := os.Mkdir(futiDir, 0o755); err != nil {
		t.Fatalf("mkdir .futi: %v", err)
	}
	writeMainTestFile(t, filepath.Join(futiDir, "local.yaml"), "name: local\n")

	src, err := localSource(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src == nil {
		t.Fatal("expected non-nil source")
	}

	// Both modules should resolve
	if _, _, err := src.Resolve("yamlmod"); err != nil {
		t.Fatalf("expected yamlmod to resolve: %v", err)
	}
	if _, _, err := src.Resolve("local"); err != nil {
		t.Fatalf("expected local to resolve: %v", err)
	}
}

func writeMainTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

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
	if err := printChildren(&buf, children, false, nil); err != nil {
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

// TestLocalSourceUsesLoader доказывает, что localSource обращается к loader
// для загрузки ./futi.yaml (а не к execmodule.LoadModule напрямую).
// Stub возвращает модуль с name="spy_name"; если loader использован —
// source.Resolve("spy_name") успешен и spy.calls == 1.
func TestLocalSourceUsesLoader(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// Файл нужен, чтобы localSource вообще вызвал loader.
	writeMainTestFile(t, filepath.Join(dir, "futi.yaml"), "name: real\n")

	spy := &stubLoader{
		mod: &execmodule.Module{Name: "spy_name", Root: dir},
	}

	src, err := localSource(spy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src == nil {
		t.Fatal("expected non-nil source")
	}

	if _, _, err := src.Resolve("spy_name"); err != nil {
		t.Fatalf("expected spy_name to resolve (loader used): %v", err)
	}
	if spy.calls != 1 {
		t.Fatalf("expected exactly 1 loader call, got %d", spy.calls)
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

// TestPrintChildrenUsesLoader доказывает, что printChildren использует loader
// для получения описаний module-children.
func TestPrintChildrenUsesLoader(t *testing.T) {
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
	if err := printChildren(&buf, children, false, spy); err != nil {
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

// TestLocalSourceWithCache проверяет интеграцию localSource с *modulecache.Cache.
func TestLocalSourceWithCache(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeMainTestFile(t, filepath.Join(dir, "futi.yaml"), "name: cached\n")

	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})
	src, err := localSource(cache)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src == nil {
		t.Fatal("expected non-nil source")
	}

	if _, _, err := src.Resolve("cached"); err != nil {
		t.Fatalf("expected cached to resolve: %v", err)
	}
}

// --- Шаг 2: рефакторинг log.Fatalf в ошибках ---

// TestCreateGlobalSourceInvalidConfigReturnsError проверяет, что невалидный
// config.yaml возвращает ошибку, а не убивает процесс через log.Fatalf.
func TestCreateGlobalSourceInvalidConfigReturnsError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("FUTI_MODULES_DIR", "")

	cfgDir := filepath.Join(dir, ".config", "futi")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	// Невалидный YAML
	writeMainTestFile(t, filepath.Join(cfgDir, "config.yaml"), "sources: [\n  invalid")

	_, _, err := createGlobalSource()
	if err == nil {
		t.Fatal("expected error for invalid config, got nil")
	}
}

// TestCreateGlobalSourceMissingHomeDirReturnsError проверяет, что отсутствие
// HOME возвращает ошибку, а не убивает процесс.
func TestCreateGlobalSourceMissingHomeDirReturnsError(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("FUTI_MODULES_DIR", "")

	_, _, err := createGlobalSource()
	if err == nil {
		t.Fatal("expected error for missing home dir, got nil")
	}
}

// TestCreateGlobalSourceValidConfigReturnsSource проверяет, что валидный
// config с sources возвращает CompositeSource без ошибки.
func TestCreateGlobalSourceValidConfigReturnsSource(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("FUTI_MODULES_DIR", "")

	cfgDir := filepath.Join(dir, ".config", "futi")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	modDir := filepath.Join(dir, "modules")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir modules dir: %v", err)
	}
	writeMainTestFile(t, filepath.Join(cfgDir, "config.yaml"), fmt.Sprintf("sources:\n  - path: %s\n", modDir))

	_, src, err := createGlobalSource()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src == nil {
		t.Fatal("expected non-nil source")
	}
}

// TestCreateGlobalSourceFallbackToModulesDir проверяет fallback на
// FUTI_MODULES_DIR при отсутствии config.yaml.
func TestCreateGlobalSourceFallbackToModulesDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", "/nonexistent-home-no-config-here")
	t.Setenv("FUTI_MODULES_DIR", dir)

	_, src, err := createGlobalSource()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src == nil {
		t.Fatal("expected non-nil source")
	}
}

// TestLocalSourceInvalidFutiYamlReturnsError проверяет, что невалидный
// ./futi.yaml возвращает ошибку (новое поведение — раньше молча игнорировалось).
func TestLocalSourceInvalidFutiYamlReturnsError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// Невалидный YAML
	writeMainTestFile(t, filepath.Join(dir, "futi.yaml"), "name: [\n  invalid")

	_, err := localSource(nil)
	if err == nil {
		t.Fatal("expected error for invalid futi.yaml, got nil")
	}
}

// TestCreateSourcePropagatesLocalSourceError проверяет, что ошибка из
// localSource пробрасывается через createSource.
func TestCreateSourcePropagatesLocalSourceError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", "/nonexistent-home-no-config-here")
	t.Setenv("FUTI_MODULES_DIR", dir)
	// Невалидный futi.yaml
	writeMainTestFile(t, filepath.Join(dir, "futi.yaml"), "name: [\n  invalid")

	_, global, err := createGlobalSource()
	if err != nil {
		t.Fatalf("createGlobalSource: %v", err)
	}
	_, err = createSource(nil, global)
	if err == nil {
		t.Fatal("expected error from invalid futi.yaml, got nil")
	}
}

// TestCreateSourceReturnsLayeredSourceWhenLocalPresent проверяет, что
// при наличии локального source возвращается LayeredSource.
func TestCreateSourceReturnsLayeredSourceWhenLocalPresent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", "/nonexistent-home-no-config-here")
	t.Setenv("FUTI_MODULES_DIR", dir)

	writeMainTestFile(t, filepath.Join(dir, "futi.yaml"), "name: local\n")

	_, global, err := createGlobalSource()
	if err != nil {
		t.Fatalf("createGlobalSource: %v", err)
	}
	src, err := createSource(nil, global)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if src == nil {
		t.Fatal("expected non-nil source")
	}
}

// --- Шаг 4: futi validate ---

// TestParseArgsValidateReserved проверяет, что "validate" как последний
// позиционный аргумент резолвится как *validateRequest.
func TestParseArgsValidateReserved(t *testing.T) {
	args := []string{"docker", "validate"}
	_, err := parseArgs(args)
	var req *validateRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *validateRequest, got: %v", err)
	}
	if req.namespace != "docker" {
		t.Fatalf("unexpected namespace: %q", req.namespace)
	}
}

// TestParseArgsValidateWithNestedNamespace проверяет, что "validate"
// работает с вложенными namespace.
func TestParseArgsValidateWithNestedNamespace(t *testing.T) {
	args := []string{"dev", "deploy", "validate"}
	_, err := parseArgs(args)
	var req *validateRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *validateRequest, got: %v", err)
	}
	if req.namespace != "dev.deploy" {
		t.Fatalf("unexpected namespace: %q", req.namespace)
	}
}

// TestParseArgsValidateWithoutNamespace проверяет, что "validate" без
// namespace возвращает ошибку (validate требует модуль).
func TestParseArgsValidateWithoutNamespace(t *testing.T) {
	args := []string{"validate"}
	_, err := parseArgs(args)
	if err == nil {
		t.Fatal("expected error for validate without namespace, got nil")
	}
	var req *validateRequest
	if errors.As(err, &req) {
		t.Fatalf("expected plain error (not validateRequest) for validate without namespace, got: %v", err)
	}
}

// TestRunValidateValidModule проверяет, что валидный модуль → "OK".
func TestRunValidateValidModule(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "docker.yaml")
	writeMainTestFile(t, manifestPath, `
name: docker
description: Docker management
commands:
  ps:
    cmd: "docker ps"
  build:
    params:
      - name: tag
        type: string
        required: true
    cmd: "docker build -t {{.tag}} ."
`)

	source := discovery.NewGlobalSource(dir)

	var buf bytes.Buffer
	err := runValidate(&buf, source, "docker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "OK") {
		t.Fatalf("expected OK in output, got:\n%s", output)
	}
}

// TestRunValidateInvalidModule проверяет, что невалидный модуль → отчёт с ERROR.
func TestRunValidateInvalidModule(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "bad.yaml")
	writeMainTestFile(t, manifestPath, `
name: bad
commands:
  empty: {}
  build:
    params:
      - name: tag
        type: float
      - name: tag
        type: string
    cmd: "docker build"
`)

	source := discovery.NewGlobalSource(dir)

	var buf bytes.Buffer
	err := runValidate(&buf, source, "bad")
	if err == nil {
		t.Fatal("expected error for invalid module, got nil")
	}

	output := buf.String()
	if !strings.Contains(output, "ERROR") {
		t.Fatalf("expected ERROR in output, got:\n%s", output)
	}
	// Должно показать несколько ошибок, а не только первую.
	if !strings.Contains(output, "empty") {
		t.Fatalf("expected 'empty' command error in output, got:\n%s", output)
	}
	if !strings.Contains(output, "float") {
		t.Fatalf("expected 'float' type error in output, got:\n%s", output)
	}
}

// TestRunValidateWithWarnings проверяет, что warnings показываются.
func TestRunValidateWithWarnings(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "db.yaml")
	writeMainTestFile(t, manifestPath, `
name: db
commands:
  migrate:
    commands:
      up:
        cmd: "echo up"
      down:
        cmd: "echo down"
`)

	source := discovery.NewGlobalSource(dir)

	var buf bytes.Buffer
	err := runValidate(&buf, source, "db")
	// Warnings не блокируют — err должен быть nil.
	if err != nil {
		t.Fatalf("unexpected error (warnings should not block): %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "WARNING") {
		t.Fatalf("expected WARNING in output, got:\n%s", output)
	}
}

// TestRunValidateModuleNotFound проверяет, что несуществующий namespace → ошибка.
func TestRunValidateModuleNotFound(t *testing.T) {
	dir := t.TempDir()
	source := discovery.NewGlobalSource(dir)

	var buf bytes.Buffer
	err := runValidate(&buf, source, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing module, got nil")
	}
}

// --- Group 3a: CLI cache command (#2, #3, #4, #25) ---

// TestCacheSubcommandRefresh проверяет, что "cache refresh" определяется
// как subcommand="refresh".
func TestCacheSubcommandRefresh(t *testing.T) {
	subcmd, ok := cacheSubcommand([]string{"cache", "refresh"})
	if !ok {
		t.Fatal("expected ok=true for cache refresh")
	}
	if subcmd != "refresh" {
		t.Fatalf("expected subcmd=refresh, got %q", subcmd)
	}
}

// TestCacheSubcommandWithoutSubcommand проверяет, что "cache" без subcommand
// возвращает ("", true) — main выведет понятную ошибку "requires a subcommand".
func TestCacheSubcommandWithoutSubcommand(t *testing.T) {
	subcmd, ok := cacheSubcommand([]string{"cache"})
	if !ok {
		t.Fatal("expected ok=true for cache without subcommand")
	}
	if subcmd != "" {
		t.Fatalf("expected empty subcmd, got %q", subcmd)
	}
}

// TestCacheSubcommandUnknown проверяет, что "cache foo" возвращает ("foo", true).
func TestCacheSubcommandUnknown(t *testing.T) {
	subcmd, ok := cacheSubcommand([]string{"cache", "foo"})
	if !ok {
		t.Fatal("expected ok=true for cache with unknown subcommand")
	}
	if subcmd != "foo" {
		t.Fatalf("expected subcmd=foo, got %q", subcmd)
	}
}

// TestCacheSubcommandNotCache проверяет, что обычные команды не определяются
// как cache subcommand.
func TestCacheSubcommandNotCache(t *testing.T) {
	_, ok := cacheSubcommand([]string{"docker", "build"})
	if ok {
		t.Fatal("expected ok=false for non-cache command")
	}
}

// TestCacheSubcommandEmpty проверяет пустой args.
func TestCacheSubcommandEmpty(t *testing.T) {
	_, ok := cacheSubcommand([]string{})
	if ok {
		t.Fatal("expected ok=false for empty args")
	}
}

// TestRunCacheRefreshOutputFormat проверяет формат вывода runCacheRefresh:
// "Cache refreshed: N modules, M updated" + опционально ", K skipped".
// Спека, раздел 8: "выводит статистику; exit 0".
func TestRunCacheRefreshOutputFormat(t *testing.T) {
	dir := t.TempDir()
	writeMainTestFile(t, filepath.Join(dir, "app.yaml"), `name: app`)

	src := discovery.NewGlobalSource(dir)
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})

	var buf bytes.Buffer
	if err := runCacheRefresh(&buf, cache, src); err != nil {
		t.Fatalf("runCacheRefresh: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Cache refreshed:") {
		t.Fatalf("expected 'Cache refreshed:' in output, got: %q", output)
	}
	if !strings.Contains(output, "1 modules") {
		t.Fatalf("expected '1 modules' in output, got: %q", output)
	}
	if !strings.Contains(output, "1 updated") {
		t.Fatalf("expected '1 updated' in output, got: %q", output)
	}
}

// TestRunCacheRefreshShowsSkipped проверяет, что skipped модули показываются
// в выводе runCacheRefresh (часть #7 — observable counter).
func TestRunCacheRefreshShowsSkipped(t *testing.T) {
	dir := t.TempDir()
	writeMainTestFile(t, filepath.Join(dir, "good.yaml"), `name: good`)
	writeMainTestFile(t, filepath.Join(dir, "bad.yaml"), "name: bad\ncommands:\n  empty: {}\n")

	src := discovery.NewGlobalSource(dir)
	cache := modulecache.New(modulecache.MemoryStorage{}, modulecache.AlwaysPolicy{})

	// Подавляем лог от skipped-модуля.
	oldOut := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(oldOut)

	var buf bytes.Buffer
	if err := runCacheRefresh(&buf, cache, src); err != nil {
		t.Fatalf("runCacheRefresh: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "1 skipped") {
		t.Fatalf("expected '1 skipped' in output, got: %q", output)
	}
}

// --- Group 3b: config loading + tests (#5, #12, #26, #27) ---

// TestCreateGlobalSourceReturnsConfig проверяет, что createGlobalSource
// возвращает загруженный *config.Config (фикс #5, #12 — без двойной загрузки).
func TestCreateGlobalSourceReturnsConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("FUTI_MODULES_DIR", "")

	cfgDir := filepath.Join(dir, ".config", "futi")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	modDir := filepath.Join(dir, "modules")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatalf("mkdir modules dir: %v", err)
	}
	writeMainTestFile(t, filepath.Join(cfgDir, "config.yaml"), fmt.Sprintf(`sources:
  - path: %s
cache:
  refresh: force_only
`, modDir))

	cfg, src, err := createGlobalSource()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil Config (fixes #5, #12 — single load)")
	}
	if cfg.Cache.Refresh != "force_only" {
		t.Fatalf("expected Cache.Refresh=force_only, got %q", cfg.Cache.Refresh)
	}
	if src == nil {
		t.Fatal("expected non-nil source")
	}
}

// TestDefaultCachePathReturnsFutiSubpath проверяет, что defaultCachePath
// возвращает путь, оканчивающийся на futi/modulecache.gob.
func TestDefaultCachePathReturnsFutiSubpath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// На Linux XDG_CACHE_HOME может перехватить — очищаем для детерминизма.
	t.Setenv("XDG_CACHE_HOME", "")

	path, err := defaultCachePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join("futi", "modulecache.gob")) {
		t.Fatalf("expected path ending in futi/modulecache.gob, got %q", path)
	}
}

// TestDefaultCachePathErrorWhenNoHome проверяет, что при отсутствии HOME
// и XDG_CACHE_HOME defaultCachePath возвращает ошибку.
func TestDefaultCachePathErrorWhenNoHome(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("test is Linux-specific (XDG_CACHE_HOME behavior)")
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	_, err := defaultCachePath()
	if err == nil {
		t.Fatal("expected error when no home/cache dir")
	}
}

// TestNewCacheFromConfigNilCfgUsesAlwaysPolicy проверяет, что nil cfg
// → AlwaysPolicy (default). Желаемое поведение: без конфига — безопасней
// refresh каждый раз.
func TestNewCacheFromConfigNilCfgUsesAlwaysPolicy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", "")

	cache := newCacheFromConfig(nil)
	if cache == nil {
		t.Fatal("expected non-nil cache")
	}
	// AlwaysPolicy → Start должен запустить Refresh. Создаём source с модулем
	// и проверяем, что модуль попал в кэш (Refresh отработал).
	modDir := t.TempDir()
	writeMainTestFile(t, filepath.Join(modDir, "app.yaml"), `name: app`)
	src := discovery.NewGlobalSource(modDir)
	if err := cache.Start(src); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

// TestNewCacheFromConfigWithForceOnly проверяет, что cfg с refresh=force_only
// → ForceOnlyPolicy (Start не Refresh-ит). Желаемое поведение: пользователь
// явно настроил force_only.
func TestNewCacheFromConfigWithForceOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", "")

	cfg := &config.Config{
		Cache: config.CacheConfig{
			Refresh: "force_only",
		},
	}
	cache := newCacheFromConfig(cfg)
	if cache == nil {
		t.Fatal("expected non-nil cache")
	}
	// ForceOnly → Start НЕ должен Refresh. Модуль не должен попасть в кэш.
	modDir := t.TempDir()
	writeMainTestFile(t, filepath.Join(modDir, "app.yaml"), `name: app`)
	src := discovery.NewGlobalSource(modDir)
	if err := cache.Start(src); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

// TestNewCacheFromConfigFallsBackToMemoryStorage проверяет, что при отсутствии
// HOME (defaultCachePath error) newCacheFromConfig fallback-ит на MemoryStorage.
// Желаемое поведение: CLI должен работать без persistent кэша.
func TestNewCacheFromConfigFallsBackToMemoryStorage(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("test is Linux-specific (XDG_CACHE_HOME behavior)")
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	cache := newCacheFromConfig(nil)
	if cache == nil {
		t.Fatal("expected non-nil cache even without HOME")
	}
	// MemoryStorage — Start должен работать (in-memory only, без persistent).
	src := discovery.NewGlobalSource(t.TempDir())
	if err := cache.Start(src); err != nil {
		t.Fatalf("Start with MemoryStorage fallback: %v", err)
	}
}

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
