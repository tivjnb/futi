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
func runBashComplete(t *testing.T, withLib bool, compWords, compLine string, compPoint, cword int, fakeOut, wordbreaks string) (args []string, reply []string, directive string) {
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
	wb := ""
	if wordbreaks != "" {
		wb = "COMP_WORDBREAKS='" + wordbreaks + "'\n"
	}
	if withLib {
		lib = "source " + bashCompletionLib + "\n"
	}
	driver := lib + `source "$SCRIPT"
futi() { printf '%s\n' "$@" > "$ARGS"; cat "$OUT"; }
` + wb + `COMP_WORDS=(` + compWords + `)
COMP_CWORD=` + strconv.Itoa(cword) + `
COMP_LINE='` + compLine + `'
COMP_POINT=` + strconv.Itoa(compPoint) + `
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
		wordbreak string // значение COMP_WORDBREAKS ("" — по умолчанию bash)
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
			name:  "пустое слово после равенства",
			words: `futi x --tag = ""`, line: "futi x --tag=", point: 13, cword: 4,
			out:      ":0\n",
			wantArgs: []string{"__complete", "x", "--tag="}, wantDir: "0",
		},
		{
			name:  "пустое слово после равенства и пробела",
			words: `futi x --tag = ""`, line: "futi x --tag= ", point: 14, cword: 4,
			out:      ":0\n",
			wantArgs: []string{"__complete", "x", "--tag=", ""}, wantDir: "0",
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
		{
			name:  "двоеточие вне COMP_WORDBREAKS: префикс не режется",
			words: `futi a:b`, line: "futi a:b", point: 8, cword: 1,
			out:      "a:bc\n:0\n",
			wantArgs: []string{"__complete", "a:b"}, wantReply: []string{"a:bc"}, wantDir: "0",
			wordbreak: " ",
		},
		{
			name:  "курсор внутри слова: передаётся часть до курсора",
			words: `futi dockxr ps`, line: "futi dockxr ps", point: 9, cword: 1,
			out:      "docker\n:0\n",
			wantArgs: []string{"__complete", "dock"}, wantReply: []string{"docker"}, wantDir: "0",
		},
	}
	for _, withLib := range []bool{false, true} {
		mode := "без bash-completion"
		if withLib {
			mode = "с bash-completion"
		}
		for _, tc := range cases {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				args, reply, dir := runBashComplete(t, withLib, tc.words, tc.line, tc.point, tc.cword, tc.out, tc.wordbreak)
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

// TestBashCompletionNoLeakedCur: __futi_cur не утекает в окружение вызывающего
// интерактивного shell после _futi.
func TestBashCompletionNoLeakedCur(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash не найден")
	}
	scriptPath := filepath.Join(t.TempDir(), "futi.bash")
	if err := os.WriteFile(scriptPath, []byte(bashCompletionScript), 0o600); err != nil {
		t.Fatal(err)
	}
	driver := `source "$SCRIPT"
futi() { printf ':0\n'; }
COMP_WORDS=(futi a)
COMP_CWORD=1
COMP_LINE='futi a'
COMP_POINT=6
_futi
echo "cur=${__futi_cur+SET}"
echo "args=${__futi_args+SET}"
`
	cmd := exec.Command(bash, "--norc", "--noprofile", "-c", driver)
	cmd.Env = append(os.Environ(), "SCRIPT="+scriptPath)
	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash: %v\n%s", err, got)
	}
	if string(got) != "cur=\nargs=\n" {
		t.Errorf("переменные утекли в вызывающий shell: %q", got)
	}
}
