package cli

import "testing"

func TestShellJoin(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{"простые токены", []string{"echo", "hi"}, "echo hi"},
		{"пробел в токене", []string{"echo", "a b"}, "echo 'a b'"},
		{"одинарная кавычка", []string{"echo", "it's"}, `echo 'it'\''s'`},
		{"пустой токен", []string{"echo", ""}, "echo ''"},
		{"переменная shell", []string{"echo", "$HOME"}, "echo '$HOME'"},
		{"безопасные спецсимволы", []string{"scp", "a@h:/p/x_1.txt", "--k=v,w+%"}, "scp a@h:/p/x_1.txt --k=v,w+%"},
		{"пустой argv", nil, ""},
		{"ESC", []string{"echo", "a\x1b[2Jb"}, `echo $'a\x1b[2Jb'`},
		{"возврат каретки", []string{"echo", "ok\rEVIL"}, `echo $'ok\x0dEVIL'`},
		{"перевод строки", []string{"echo", "a\nb"}, `echo $'a\x0ab'`},
		{"табуляция", []string{"echo", "a\tb"}, `echo $'a\x09b'`},
		{"DEL", []string{"echo", "a\x7fb"}, `echo $'a\x7fb'`},
		{"bidi override", []string{"echo", "a\u202eb"}, `echo $'a\u202eb'`},
		{"непечатаемая вне BMP", []string{"echo", "a\U000e0001b"}, `echo $'a\U000e0001b'`},
		{"смесь с кавычкой и слэшем", []string{"echo", "it's\\\x1b"}, `echo $'it\'s\\\x1b'`},
		{"невалидный UTF-8", []string{"echo", "a\xffb"}, `echo $'a\xffb'`},
		{"печатный юникод как раньше", []string{"echo", "привет"}, "echo 'привет'"},
		{"= в argv[0]", []string{"A=1", "x"}, "'A=1' x"},
		{"= в argv[0] с путём", []string{"./a=b", "x"}, "'./a=b' x"},
		{"токен начинается с =", []string{"echo", "=ls"}, "echo '=ls'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shellJoin(tt.argv); got != tt.want {
				t.Fatalf("shellJoin(%q) = %q, want %q", tt.argv, got, tt.want)
			}
		})
	}
}
