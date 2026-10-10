package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const bashCompletionLib = "/usr/share/bash-completion/bash_completion"

// runBashComplete подключает bash-скрипт автодополнения, подменяет futi
// shell-функцией (пишет argv в файл, печатает fakeOut) и вызывает
// __futi_get_completion_results с заданным состоянием COMP_*.
// Возвращает argv, с которым вызван futi, COMPREPLY и directive.
func runBashComplete(t *testing.T, withLib bool, compWords, compLine string, compPoint, cword int, fakeOut string) (args []string, reply []string, directive string) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash не найден")
	}
	if withLib {
		if _, err := os.Stat(bashCompletionLib); err != nil {
			t.Skip("bash-completion не установлен")
		}
	}

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "futi.bash")
	outPath := filepath.Join(dir, "out.txt")
	argsPath := filepath.Join(dir, "args.txt")
	if err := os.WriteFile(scriptPath, []byte(bashCompletionScript), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outPath, []byte(fakeOut), 0o600); err != nil {
		t.Fatal(err)
	}

	lib := ""
	if withLib {
		lib = "source " + bashCompletionLib + "\n"
	}
	driver := lib + `source "$SCRIPT"
futi() { printf '%s\n' "$@" > "$ARGS"; cat "$OUT"; }
COMP_WORDS=(` + compWords + `)
COMP_CWORD=` + itoa(cword) + `
COMP_LINE='` + compLine + `'
COMP_POINT=` + itoa(compPoint) + `
__futi_get_completion_results
printf 'DIRECTIVE=%s\n' "$directive"
printf 'REPLY=%s\n' "${COMPREPLY[*]}"
`
	cmd := exec.Command(bash, "--norc", "--noprofile", "-c", driver)
	cmd.Env = append(os.Environ(), "SCRIPT="+scriptPath, "OUT="+outPath, "ARGS="+argsPath)
	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash: %v\n%s", err, got)
	}
	for _, l := range strings.Split(strings.TrimRight(string(got), "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "DIRECTIVE="):
			directive = strings.TrimPrefix(l, "DIRECTIVE=")
		case strings.HasPrefix(l, "REPLY="):
			if r := strings.TrimPrefix(l, "REPLY="); r != "" {
				reply = strings.Split(r, " ")
			}
		}
	}
	a, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("futi не был вызван: %v\n%s", err, got)
	}
	return strings.Split(strings.TrimSuffix(string(a), "\n"), "\n"), reply, directive
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestBashCompletionScript(t *testing.T) {
	cases := []struct {
		name      string
		words     string // содержимое массива COMP_WORDS
		line      string
		point     int
		cword     int
		out       string
		wantArgs  []string
		wantReply []string
		wantDir   string
	}{
		{
			name:  "слова после курсора не передаются",
			words: `futi a b c`, line: "futi a b c", point: 8, cword: 2,
			out:      "bx\n:0\n",
			wantArgs: []string{"__complete", "a", "b"}, wantReply: []string{"bx"}, wantDir: "0",
		},
		{
			name:  "флаг с равенством остаётся одним словом",
			words: `futi x --tag = val`, line: "futi x --tag=val", point: 16, cword: 4,
			out:      ":0\n",
			wantArgs: []string{"__complete", "x", "--tag=val"}, wantDir: "0",
		},
		{
			name:  "двоеточие не разбивает слово",
			words: `futi a : b`, line: "futi a:b", point: 8, cword: 3,
			out:      "a:bc\n:0\n",
			wantArgs: []string{"__complete", "a:b"}, wantReply: []string{"bc"}, wantDir: "0",
		},
		{
			name:  "кандидат с двоеточием не директива",
			words: `futi ""`, line: "futi ", point: 5, cword: 1,
			out:      ":foo\nbar\t desc\n:4\n",
			wantArgs: []string{"__complete", ""}, wantReply: []string{":foo", "bar"}, wantDir: "4",
		},
		{
			name:  "последняя строка не директива",
			words: `futi ""`, line: "futi ", point: 5, cword: 1,
			out:      "one\n:foo\n",
			wantArgs: []string{"__complete", ""}, wantReply: []string{"one", ":foo"}, wantDir: "0",
		},
	}
	for _, withLib := range []bool{false, true} {
		mode := "без bash-completion"
		if withLib {
			mode = "с bash-completion"
		}
		for _, tc := range cases {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				args, reply, dir := runBashComplete(t, withLib, tc.words, tc.line, tc.point, tc.cword, tc.out)
				if strings.Join(args, "|") != strings.Join(tc.wantArgs, "|") {
					t.Errorf("argv = %q, want %q", args, tc.wantArgs)
				}
				if strings.Join(reply, "|") != strings.Join(tc.wantReply, "|") {
					t.Errorf("COMPREPLY = %q, want %q", reply, tc.wantReply)
				}
				if dir != tc.wantDir {
					t.Errorf("directive = %q, want %q", dir, tc.wantDir)
				}
			})
		}
	}
}
