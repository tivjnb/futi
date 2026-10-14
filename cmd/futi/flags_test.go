package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/execmodule"
)

func TestBindFlags(t *testing.T) {
	cmd := &execmodule.Command{Params: []execmodule.Param{
		{Name: "follow", Type: "bool"},
		{Name: "msg", Type: "string"},
		{Name: "n", Type: "int"},
		{Name: "verbose", Type: "bool"},
	}}

	tests := []struct {
		name       string
		flags      []string
		want       map[string]string
		wantHelp   bool
		wantErrSub string
	}{
		{name: "bool не съедает слово", flags: []string{"--follow", "logs"}, wantErrSub: `unexpected argument "logs"`},
		{name: "голый bool", flags: []string{"--follow"}, want: map[string]string{"follow": "true"}},
		{name: "bool=false", flags: []string{"--follow=false"}, want: map[string]string{"follow": "false"}},
		{name: "string берёт -h как значение", flags: []string{"--msg", "-h"}, want: map[string]string{"msg": "-h"}},
		{name: "-h после bool — справка", flags: []string{"--follow", "-h"}, wantHelp: true},
		{name: "-h", flags: []string{"-h"}, wantHelp: true},
		{name: "--help", flags: []string{"--help"}, wantHelp: true},
		{name: "int берёт отрицательное", flags: []string{"--n", "-5"}, want: map[string]string{"n": "-5"}},
		{name: "string и следующий флаг", flags: []string{"--msg", "--follow"}, wantErrSub: "requires a value"},
		{name: "string в конце", flags: []string{"--msg"}, wantErrSub: "flag --msg requires a value (use --msg=<value>)"},
		{name: "значение с -- через =", flags: []string{"--msg=--x"}, want: map[string]string{"msg": "--x"}},
		{name: "пустое значение", flags: []string{"--msg="}, want: map[string]string{"msg": ""}},
		{name: "verbose обычный параметр", flags: []string{"--verbose"}, want: map[string]string{"verbose": "true"}},
		{name: "короткий -v", flags: []string{"-v"}, wantErrSub: `unknown flag "-v"`},
		{name: "короткий -f", flags: []string{"-f"}, wantErrSub: `unknown flag "-f"`},
		{name: "неизвестный голый", flags: []string{"--zzz"}, wantErrSub: `unknown param "zzz"`},
		{name: "неизвестный с =", flags: []string{"--zzz=1"}, wantErrSub: `unknown param "zzz"`},
		{name: "-h после неизвестного голого", flags: []string{"--zzz", "-h"}, wantHelp: true},
		{name: "-h после лишнего слова", flags: []string{"--follow", "logs", "-h"}, wantHelp: true},
		{name: "-h после неизвестного короткого", flags: []string{"-f", "-h"}, wantHelp: true},
		{name: "-h после --x=1 неизвестного", flags: []string{"--zzz=1", "--help"}, wantHelp: true},
		{name: "первая ошибка побеждает", flags: []string{"-f", "--zzz"}, wantErrSub: `unknown flag "-f"`},
		{name: "пустое имя", flags: []string{"--=v"}, wantErrSub: `invalid flag "--=v"`},
		{name: "повтор — последний выигрывает", flags: []string{"--msg", "a", "--msg=b"}, want: map[string]string{"msg": "b"}},
		{name: "-h после значения не справка", flags: []string{"--msg", "-h", "--follow"}, want: map[string]string{"msg": "-h", "follow": "true"}},
		{name: "нет флагов", flags: nil, want: map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, help, err := bindFlags(cmd, tt.flags)
			if tt.wantErrSub != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if help != tt.wantHelp {
				t.Fatalf("help = %v, want %v", help, tt.wantHelp)
			}
			if tt.wantHelp {
				return
			}
			if !reflect.DeepEqual(params, tt.want) {
				t.Fatalf("params = %v, want %v", params, tt.want)
			}
		})
	}
}

func TestBindFlagsUnknownShortHint(t *testing.T) {
	_, _, err := bindFlags(&execmodule.Command{}, []string{"-v"})
	want := `unknown flag "-v": params are passed as --name value`
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}
