package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/discovery"
)

// runUsageBin запускает собранный futi в изолированном окружении и возвращает
// stdout, stderr и код выхода.
func runUsageBin(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	bin := builtFutiBinary(t)
	home := t.TempDir()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		"FUTI_MODULES_DIR="+filepath.Join(home, "modules"),
	)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errb.String(), code
}

// Явный запрос справки — usage в stdout, код 0, stderr пуст; без аргументов —
// usage в stderr, код 1; флаги без пути и без -h остаются ошибкой.
func TestUsageStreams(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantOut    bool // usage в stdout
		wantErrUse bool // usage в stderr
		wantCode   int
	}{
		{"без аргументов", nil, false, true, 1},
		{"-h", []string{"-h"}, true, false, 0},
		{"--help", []string{"--help"}, true, false, 0},
		{"help", []string{"help"}, true, false, 0},
		{"list -h", []string{"list", "-h"}, true, false, 0},
		{"--foo без пути", []string{"--foo"}, false, false, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runUsageBin(t, tc.args...)
			if code != tc.wantCode {
				t.Errorf("code = %d, want %d (stderr: %s)", code, tc.wantCode, stderr)
			}
			if got := strings.Contains(stdout, "\nUSAGE\n"); got != tc.wantOut {
				t.Errorf("usage в stdout = %v, want %v\nstdout: %s", got, tc.wantOut, stdout)
			}
			if got := strings.Contains(stderr, "\nUSAGE\n"); got != tc.wantErrUse {
				t.Errorf("usage в stderr = %v, want %v\nstderr: %s", got, tc.wantErrUse, stderr)
			}
			if tc.wantOut && stderr != "" {
				t.Errorf("stderr не пуст: %s", stderr)
			}
		})
	}
}

// Примеры в usage не должны учить небезопасным шаблонам: значение без quote
// и пайп (исполнение идёт без shell).
func TestUsageExamplesAreSafe(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	usage := buf.String()
	if !strings.Contains(usage, "{{quote .msg}}") {
		t.Errorf("usage не показывает {{quote .msg}}")
	}
	if strings.Contains(usage, "| tr") {
		t.Errorf("usage содержит пример с пайпом (без shell он не работает)")
	}
	if strings.Contains(usage, `"{{.msg}}"`) {
		t.Errorf("usage советует оборачивать значение в кавычки вместо quote")
	}
	for _, name := range []string{"{{.module_root}}", "{{.module_name}}"} {
		if !strings.Contains(usage, name) {
			t.Errorf("usage не упоминает %s", name)
		}
	}
}

// runHelp для корня печатает usage в переданный stdout.
func TestRunHelpRootPrintsUsageToStdout(t *testing.T) {
	rt := helpTestSource(t)
	code, stdout, stderr := runHelpFor(t, rt, helpArgs())
	if code != 0 {
		t.Fatalf("code = %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "\nUSAGE\n") {
		t.Errorf("usage не в stdout:\n%s", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr не пуст: %s", stderr)
	}
}

// Модуль, который не загрузился, в list не теряет причину: описание
// заменяется пометкой (error: ...), строка модуля остаётся.
func TestPrintChildrenLoadError(t *testing.T) {
	dir := t.TempDir()
	writeMainTestFile(t, filepath.Join(dir, "bad.yaml"), "name: bad\ncommands: [\n")
	writeMainTestFile(t, filepath.Join(dir, "good.yaml"), "name: good\ndescription: Fine module\ncmd: echo x\n")

	var buf bytes.Buffer
	if err := runList(&buf, discovery.NewGlobalSource(dir), "", false, false, nil); err != nil {
		t.Fatal(err)
	}
	var badLine string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "bad") {
			badLine = line
		}
	}
	const want = "(error: cannot load module; run 'futi modules validate <namespace>')"
	if !strings.Contains(badLine, want) {
		t.Errorf("строка bad без пометки %q:\n%s", want, buf.String())
	}
	if strings.Contains(buf.String(), dir) {
		t.Errorf("в выводе без -v есть путь манифеста:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "Fine module") {
		t.Errorf("описание good потеряно:\n%s", buf.String())
	}
}

// Описания в секции USAGE выровнены по одной колонке (строка --dry-run — тоже).
func TestUsageDescriptionsAligned(t *testing.T) {
	var buf bytes.Buffer
	printUsage(&buf)
	re := regexp.MustCompile(`^(  futi \S.*?\S)( {2,})(\S.*)$`)
	col := -1
	var sawDryRun bool
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.HasPrefix(line, "The last positional") {
			break
		}
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		c := len(m[1]) + len(m[2])
		if col == -1 {
			col = c
		}
		if c != col {
			t.Errorf("описание в колонке %d, want %d: %q", c, col, line)
		}
		if strings.Contains(line, "--dry-run") {
			sawDryRun = true
		}
	}
	if !sawDryRun {
		t.Error("строка --dry-run с описанием не найдена в USAGE")
	}
}

// Сообщения об ошибках main() начинаются с префикса "futi:".
func TestMainErrorsHaveFutiPrefix(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"completion"}, "futi: completion requires a shell"},
		{[]string{"cache"}, "futi: cache requires a subcommand"},
		{[]string{"cache", "bogus"}, "futi: unknown cache subcommand"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			_, stderr, code := runUsageBin(t, tt.args...)
			if code == 0 {
				t.Errorf("код выхода 0, ожидался ненулевой")
			}
			if !strings.HasPrefix(stderr, tt.want) {
				t.Errorf("stderr = %q, want prefix %q", stderr, tt.want)
			}
		})
	}
}
