package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"futi/internal/discovery"
)

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

	src := localSource()
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

	src := localSource()
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

	src := localSource()
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

	src := localSource()
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
	if err := printCommands(&buf, manifestPath, dir, false); err != nil {
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
	if err := printChildren(&buf, children, false); err != nil {
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
	err := runList(&buf, source, "db.migrate", false, false)
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
	err := runList(&buf, source, "db", false, false)
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
	err := runList(&buf, source, "db.migrate.up", false, false)
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
	err := runList(&buf, source, "db.status", false, false)
	if err == nil {
		t.Fatal("expected error for listing a leaf command")
	}
}
