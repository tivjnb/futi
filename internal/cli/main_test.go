package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
)

var (
	futiBinOnce sync.Once
	futiBinDir  string // каталог с собранным бинарём futi; удаляется в TestMain
	futiBinErr  error
	origHome    string // HOME до подмены в TestMain — для go build
)

// TestMain подменяет HOME временным каталогом: команды modules читают
// ~/.config/futi/config.yaml, и без подмены тесты (в том числе update без
// имени) обходили бы sources реального конфига пользователя. В конце удаляет
// этот каталог и каталог общего бинаря futi, собранного builtFutiBinary.
func TestMain(m *testing.M) {
	origHome = os.Getenv("HOME")
	home, err := os.MkdirTemp("", "futi-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	code := m.Run()
	os.RemoveAll(home)
	if futiBinDir != "" {
		os.RemoveAll(futiBinDir)
	}
	os.Exit(code)
}

// builtFutiBinary собирает futi один раз на запуск тестов и возвращает путь
// к бинарю (файл с именем futi в каталоге futiBinDir). Ошибка сборки — Fatal
// в каждом вызывающем тесте.
func builtFutiBinary(t testing.TB) string {
	t.Helper()
	futiBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "futi-test-bin-")
		if err != nil {
			futiBinErr = err
			return
		}
		futiBinDir = dir
		// go build — с настоящим HOME: кэш сборки и модулей Go лежат в нём.
		build := exec.Command("go", "build", "-o", filepath.Join(dir, "futi"), "github.com/tivjnb/futi/cmd/futi")
		build.Env = append(os.Environ(), "HOME="+origHome)
		if out, err := build.CombinedOutput(); err != nil {
			futiBinErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if futiBinErr != nil {
		t.Fatal(futiBinErr)
	}
	return filepath.Join(futiBinDir, "futi")
}

// stubLoader — тестовый загрузчик модулей, который возвращает
// предзаготовленный модуль для любого пути и считает вызовы.
// Используется для проверки, что printCommands/renderChildren/runList
// действительно обращаются к loader, а не к execmodule.LoadModule напрямую.
type stubLoader struct {
	mod   *execmodule.Module
	calls int
}

func (s *stubLoader) LoadModule(manifestPath, moduleRoot string) (*execmodule.Module, error) {
	s.calls++
	return s.mod, nil
}

func writeMainTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// runFutiBin запускает собранный futi в изолированном окружении с модулем m
// (config.yaml = manifest) и возвращает stdout, stderr и код выхода.
func runFutiBin(t *testing.T, manifest string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	tmp := t.TempDir()
	mods := filepath.Join(tmp, "mods", "m")
	if err := os.MkdirAll(mods, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mods, "config.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(builtFutiBinary(t), args...)
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(),
		"FUTI_MODULES_DIR="+filepath.Join(tmp, "mods"),
		"HOME="+tmp, "XDG_CACHE_HOME="+filepath.Join(tmp, "cache"))
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("запуск futi: %v", err)
		}
		code = ee.ExitCode()
	}
	return out.String(), errb.String(), code
}

const strictOrderManifest = `
name: m
commands:
  logs:
    description: Show logs
    params:
      - name: follow
        type: bool
        default: "false"
    cmd: "echo follow={{.follow}}"
  say:
    description: Say a message
    params:
      - name: msg
        type: string
    cmd: "echo msg={{quote .msg}}"
  grp:
    commands:
      leaf:
        description: Leaf command
        cmd: "echo leaf"
`

func TestFutiBoolFlagBareIsTrue(t *testing.T) {
	out, errOut, code := runFutiBin(t, strictOrderManifest, "m", "logs", "--follow")
	if code != 0 || out != "follow=true\n" {
		t.Fatalf("code=%d out=%q stderr=%q, want 0 and follow=true", code, out, errOut)
	}
}

func TestFutiBoolFlagDoesNotEatWord(t *testing.T) {
	out, errOut, code := runFutiBin(t, strictOrderManifest, "m", "logs", "--follow", "extra")
	if code != 1 || out != "" || !strings.Contains(errOut, `unexpected argument "extra"`) {
		t.Fatalf("code=%d out=%q stderr=%q, want 1 and unexpected argument", code, out, errOut)
	}
	// Флаг до команды: "logs" больше не значение флага и не команда — путь [m].
	out, errOut, code = runFutiBin(t, strictOrderManifest, "m", "--follow", "logs")
	if code != 1 || out != "" || !strings.Contains(errOut, `module "m" has no default command`) ||
		!strings.Contains(errOut, "the command path goes before flags") {
		t.Fatalf("code=%d out=%q stderr=%q, want no-default-command error with path hint", code, out, errOut)
	}
}

func TestFutiCommandHelpFlagPrintsCommandHelp(t *testing.T) {
	out, errOut, code := runFutiBin(t, strictOrderManifest, "m", "say", "-h")
	if code != 0 || !strings.Contains(out, "Say a message") || !strings.Contains(out, "msg") {
		t.Fatalf("code=%d out=%q stderr=%q, want 0 and command help", code, out, errOut)
	}
}

func TestFutiStringFlagTakesDashHAsValue(t *testing.T) {
	out, errOut, code := runFutiBin(t, strictOrderManifest, "m", "say", "--msg", "-h")
	if code != 0 || out != "msg=-h\n" {
		t.Fatalf("code=%d out=%q stderr=%q, want 0 and msg=-h", code, out, errOut)
	}
}

func TestFutiBranchHelpFlagListsChildren(t *testing.T) {
	out, errOut, code := runFutiBin(t, strictOrderManifest, "m", "grp", "-h")
	if code != 0 || !strings.Contains(out, "leaf") {
		t.Fatalf("code=%d out=%q stderr=%q, want 0 and children list", code, out, errOut)
	}
}

func TestFutiUnknownShortFlagIsError(t *testing.T) {
	_, errOut, code := runFutiBin(t, strictOrderManifest, "m", "logs", "-f")
	if code != 1 || !strings.Contains(errOut, `unknown flag "-f"`) {
		t.Fatalf("code=%d stderr=%q, want 1 and unknown flag", code, errOut)
	}
}

func TestFutiListUnknownFlagIsError(t *testing.T) {
	_, errOut, code := runFutiBin(t, strictOrderManifest, "list", "--foo")
	if code != 1 || !strings.Contains(errOut, `list: unknown flag "--foo"`) {
		t.Fatalf("code=%d stderr=%q, want 1 and list unknown flag", code, errOut)
	}
}

func TestFutiFlagsBeforePathIsUsageError(t *testing.T) {
	_, errOut, code := runFutiBin(t, strictOrderManifest, "--msg", "x", "m", "say")
	if code != 1 || !strings.Contains(errOut, "usage:") {
		t.Fatalf("code=%d stderr=%q, want 1 and usage", code, errOut)
	}
}

func TestFutiUnknownParamStillRejectedByValidate(t *testing.T) {
	_, errOut, code := runFutiBin(t, strictOrderManifest, "m", "logs", "--zzz=1")
	if code != 1 || !strings.Contains(errOut, `unknown param "zzz"`) {
		t.Fatalf("code=%d stderr=%q, want 1 and unknown param", code, errOut)
	}
}

// --zzz и --zzz=1 дают одну и ту же строку ошибки.
func TestFutiUnknownParamSameMessageBothForms(t *testing.T) {
	_, bare, code1 := runFutiBin(t, strictOrderManifest, "m", "logs", "--zzz")
	_, eq, code2 := runFutiBin(t, strictOrderManifest, "m", "logs", "--zzz=1")
	if code1 != 1 || code2 != 1 {
		t.Fatalf("codes = %d, %d, want 1, 1", code1, code2)
	}
	if bare != eq || bare != "unknown param \"zzz\"\n" {
		t.Fatalf("stderr bare=%q eq=%q, want both %q", bare, eq, "unknown param \"zzz\"\n")
	}
}

func TestFutiModulesValidateHelpFlag(t *testing.T) {
	out, errOut, code := runFutiBin(t, strictOrderManifest, "modules", "validate", "-h")
	if code != 1 || out != "" || !strings.Contains(errOut, "usage: futi modules validate <namespace>") {
		t.Fatalf("code=%d out=%q stderr=%q, want 1 and modules validate usage", code, out, errOut)
	}
}

// runFutiBinary запускает собранный futi в изолированном окружении
// (модули — mods, HOME и кэш — во временном каталоге) и возвращает stdout, stderr, код выхода.
func runFutiBinary(t *testing.T, mods string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	tmp := t.TempDir()
	cmd := exec.Command(builtFutiBinary(t), args...)
	cmd.Env = append(os.Environ(),
		"FUTI_MODULES_DIR="+mods,
		"HOME="+tmp, "XDG_CACHE_HOME="+filepath.Join(tmp, "cache"))
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("запуск futi: %v", err)
		}
		code = ee.ExitCode()
	}
	return out.String(), errb.String(), code
}

// TestSystemHelpForNamespaceDirectory: futi <ns> -h и futi <ns> help для
// каталога-namespace (не модуля) печатают дочерние элементы, код 0 (audit F70).
func TestSystemHelpForNamespaceDirectory(t *testing.T) {
	mods := t.TempDir()
	if err := os.MkdirAll(filepath.Join(mods, "docker"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(mods, "docker", "build.yaml"), "name: build\ncmd: \"echo built\"\n")

	for _, args := range [][]string{{"docker", "-h"}, {"docker", "help"}} {
		stdout, stderr, code := runFutiBinary(t, mods, args...)
		if code != 0 || !strings.Contains(stdout, "build") {
			t.Fatalf("futi %v: code=%d stdout=%q stderr=%q, want code 0 and stdout with %q", args, code, stdout, stderr, "build")
		}
	}

	_, _, code := runFutiBinary(t, mods, "nope", "-h")
	if code == 0 {
		t.Fatal("futi nope -h: code 0, want error for nonexistent path")
	}
}

// TestBindFlagsErrorKeepsPathHint: futi k8s --follow logs — ошибка привязки флага
// не должна скрывать подсказку о порядке пути и флагов (audit F70).
func TestBindFlagsErrorKeepsPathHint(t *testing.T) {
	mods := t.TempDir()
	writeFile(t, filepath.Join(mods, "k8s.yaml"), "name: k8s\ncmd: \"echo k8s\"\n")

	_, stderr, code := runFutiBinary(t, mods, "k8s", "--follow", "logs")
	if code == 0 {
		t.Fatal("code 0, want error")
	}
	if !strings.Contains(stderr, `unknown param "follow"`) || strings.Count(stderr, "the command path goes before flags") != 1 {
		t.Fatalf("stderr = %q, want unknown param error with a single path hint", stderr)
	}
}

func TestRunSystemHelpPartialListing(t *testing.T) {
	dirA := t.TempDir()
	dirB := filepath.Join(t.TempDir(), "missing")
	if err := os.MkdirAll(filepath.Join(dirA, "ns"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMainTestFile(t, filepath.Join(dirA, "ns", "x.yaml"), "name: x\ncmd: echo x\n")
	src := discovery.NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: dirA}, {Path: dirB}}})
	var buf bytes.Buffer
	err := runSystemHelp(&buf, src, nil, []string{"ns"})
	var pe *discovery.PartialError
	if !errors.As(err, &pe) {
		t.Fatalf("runSystemHelp err = %v, want *discovery.PartialError", err)
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
		t.Errorf("нет строки модуля x в выводе:\n%s", buf.String())
	}
}

func TestRunCompleteMainPartialListingIsSilent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// Два source в config.yaml: читаемый (x.yaml) и несуществующий.
	dirA := t.TempDir()
	dirB := filepath.Join(t.TempDir(), "missing")
	writeMainTestFile(t, filepath.Join(dirA, "x.yaml"), "name: x\ndescription: Икс\ncmd: echo x\n")
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	writeMainTestFile(t, cfgPath, fmt.Sprintf("sources:\n  - path: %s\n  - path: %s\n", dirA, dirB))
	rt, err := core.NewRuntime(core.Options{
		ConfigPath: cfgPath,
		CachePath:  filepath.Join(t.TempDir(), "futi", "modulecache.gob"),
	})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	var out, errBuf bytes.Buffer
	if code := runCompleteMain(&out, &errBuf, rt, []string{""}); code != 0 {
		t.Fatalf("exit = %d, stderr:\n%s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "x\n") && !strings.Contains(out.String(), "x\t") {
		t.Errorf("нет подсказки x:\n%s", out.String())
	}
	// runCompleteMain передаёт stderr в runComplete, который штатно пишет туда
	// строку директивы; кроме неё stderr должен быть пуст.
	var rest []string
	for _, line := range strings.Split(errBuf.String(), "\n") {
		if line != "" && !strings.HasPrefix(line, "Completion ended with directive") {
			rest = append(rest, line)
		}
	}
	if len(rest) > 0 {
		t.Errorf("stderr не молчит:\n%s", strings.Join(rest, "\n"))
	}
}
