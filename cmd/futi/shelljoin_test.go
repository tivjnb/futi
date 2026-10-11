package main

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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shellJoin(tt.argv); got != tt.want {
				t.Fatalf("shellJoin(%q) = %q, want %q", tt.argv, got, tt.want)
			}
		})
	}
}
