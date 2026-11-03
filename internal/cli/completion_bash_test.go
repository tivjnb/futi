package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
printf 'REPLY=%s\n' "${COMPREPLY[@]}"
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
				reply = append(reply, r)
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
			name:  "candidates are escaped for the shell",
			words: `futi ""`, line: "futi ", point: 5, cword: 1,
			out:      "us east\nplain\n:4",
			wantArgs: []string{"__complete", ""}, wantReply: []string{`us\ east`, "plain"}, wantDir: "4",
		},
		{
			name:  "prefix is stripped before escaping",
			words: `futi mod --level=us`, line: "futi mod --level=us", point: 19, cword: 2,
			out:      "--level=us east\n:4",
			wantArgs: []string{"__complete", "mod", "--level=us"}, wantReply: []string{`us\ east`}, wantDir: "4",
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

// TestBashCompletionStripsEqualsPrefix: "=" is in COMP_WORDBREAKS, so bash
// replaces only the part after the last "=": candidates lose the prefix.
func TestBashCompletionStripsEqualsPrefix(t *testing.T) {
	cases := []struct {
		name       string
		withLib    bool
		wordbreaks string
		want       []string
	}{
		{"default wordbreaks", false, "", []string{"debug"}},
		{"default wordbreaks with bash-completion", true, "", []string{"debug"}},
		{"no equals in wordbreaks", false, " :", []string{"--level=debug"}},
		{"no equals in wordbreaks with bash-completion", true, " :", []string{"--level=debug"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wb := tc.wordbreaks
			if wb == "" {
				wb = " =:"
			}
			_, reply, _ := runBashComplete(t, tc.withLib, "futi mod --level=de", "futi mod --level=de", 19, 2, "--level=debug\n:4", wb)
			if !slices.Equal(reply, tc.want) {
				t.Errorf("COMPREPLY = %q, want %q", reply, tc.want)
			}
		})
	}
}

// TestBashCompletionDirsOnly: directive 16 offers directories only and
// removes "-o default", otherwise bash would add files on an empty COMPREPLY.
func TestBashCompletionDirsOnly(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found")
	}
	tmp := t.TempDir()
	for _, d := range []string{"sub", "my dir", "a:b"} {
		if err := os.Mkdir(filepath.Join(tmp, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, "f.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(tmp, "futi.bash")
	if err := os.WriteFile(scriptPath, []byte(bashCompletionScript), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		words string // COMP_WORDS elements after futi mk
		line  string
		want  []string
	}{
		{"empty word", `""`, "futi mk ", []string{"a:b", "my dir", "sub"}},
		{"colon prefix is stripped", `"a:"`, "futi mk a:", []string{"b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compoptLog := filepath.Join(t.TempDir(), "compopt.txt")
			// compopt only works inside a real completion, so it is replaced by a recorder.
			driver := `source "$SCRIPT"
futi() { printf ':16\n'; }
compopt() { echo "$*" >> "$LOG"; }
COMP_WORDS=(futi mk ` + tc.words + `); COMP_CWORD=2; COMP_LINE='` + tc.line + `'; COMP_POINT=${#COMP_LINE}
_futi
printf 'REPLY=%s\n' "${COMPREPLY[@]}"
`
			cmd := exec.Command(bash, "--norc", "--noprofile", "-c", driver)
			cmd.Dir = tmp
			cmd.Env = append(os.Environ(), "SCRIPT="+scriptPath, "LOG="+compoptLog)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("bash: %v\n%s", err, out)
			}
			var got []string
			for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				got = append(got, strings.TrimSuffix(strings.TrimPrefix(l, "REPLY="), "/"))
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("COMPREPLY = %q, want %q", got, tc.want)
			}
			log, _ := os.ReadFile(compoptLog)
			for _, w := range []string{"+o default", "-o filenames"} {
				if !strings.Contains(string(log), w) {
					t.Errorf("compopt calls %q lack %q", log, w)
				}
			}
		})
	}
}

// exposeModules writes: module ops (root cli+mcp; status inherits, prod is
// mcp only) and ns/hidden (mcp only) next to ns/shown.
func exposeModules(t *testing.T, modules string) {
	t.Helper()
	writeFile(t, filepath.Join(modules, "ops.yaml"), "expose: [cli, mcp]\ncommands:\n  status:\n    cmd: echo status\n  prod:\n    expose: [mcp]\n    cmd: echo prod\n")
	writeFile(t, filepath.Join(modules, "ns", "hidden.yaml"), "expose: [mcp]\ncmd: echo hidden\n")
	writeFile(t, filepath.Join(modules, "ns", "shown.yaml"), "cmd: echo shown\n")
}

// TestBashCompletionRealFuti runs the bash script against the built binary:
// --yes is skipped as a global flag, commands not exposed to the CLI are hidden.
func TestBashCompletionRealFuti(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash не найден")
	}
	tmp := t.TempDir()
	modules := filepath.Join(tmp, "modules")
	exposeModules(t, modules)
	scriptPath := filepath.Join(tmp, "futi.bash")
	if err := os.WriteFile(scriptPath, []byte(bashCompletionScript), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Dir(builtFutiBinary(t))

	cases := []struct {
		name  string
		words string // COMP_WORDS contents
		cword int
		want  []string
	}{
		{"module after --yes", `futi --yes o`, 2, []string{"ops"}},
		{"module after -y", `futi -y o`, 2, []string{"ops"}},
		{"subcommands hide prod", `futi ops ""`, 2, []string{"status"}},
		{"namespace hides hidden", `futi ns ""`, 2, []string{"shown"}},
		{"flags of hidden command", `futi ops prod -`, 3, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			driver := `source "$SCRIPT"
COMP_WORDS=(` + tc.words + `)
COMP_CWORD=` + strconv.Itoa(tc.cword) + `
COMP_LINE="${COMP_WORDS[*]}"
COMP_POINT=${#COMP_LINE}
__futi_get_completion_results
printf 'REPLY=%s\n' "${COMPREPLY[@]}"
`
			cmd := exec.Command(bash, "--norc", "--noprofile", "-c", driver)
			cmd.Env = append(os.Environ(),
				"SCRIPT="+scriptPath,
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"FUTI_MODULES_DIR="+modules,
				"HOME="+tmp,
				"XDG_CACHE_HOME="+filepath.Join(tmp, "cache"),
				"XDG_CONFIG_HOME="+filepath.Join(tmp, "config"),
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("bash: %v\n%s", err, out)
			}
			var got []string
			for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
				if r := strings.TrimPrefix(l, "REPLY="); r != "" && strings.HasPrefix(l, "REPLY=") {
					got = append(got, r)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("COMPREPLY = %q, want %q (output %q)", got, tc.want, out)
			}
		})
	}
}
