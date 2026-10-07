package termtext

import "testing"

func TestLine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"ESC удаляется", "a\x1b[31mb", "a[31mb"},
		{"пробельные управляющие -> пробел, края обрезаются", "x\ty\nz\r", "x y z"},
		{"BEL и C1 CSI удаляются", "\x07\x9bq", "q"},
		{"DEL удаляется", "a\x7fb", "ab"},
		{"Unicode не меняется", "привет", "привет"},
		{"пустая строка", "", ""},
		{"bidi-управляющие удаляются", "a\u202eb\u2066c", "abc"},
		{"ZWJ в emoji сохраняется", "👨\u200d👩", "👨\u200d👩"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Line(tc.in); got != tc.want {
				t.Errorf("Line(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestBlock(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"\\n и \\t сохраняются, ESC удаляется", "l1\nl2\t\x1b", "l1\nl2\t"},
		{"\\r удаляется", "a\r\nb", "a\nb"},
		{"C1 удаляется", "a\x9bb", "ab"},
		{"Unicode не меняется", "привет\nмир", "привет\nмир"},
		{"bidi-управляющие удаляются", "a\u202eb\u2066c\nd\u200fe", "abc\nde"},
		{"ZWJ в emoji сохраняется", "👨\u200d👩", "👨\u200d👩"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Block(tc.in); got != tc.want {
				t.Errorf("Block(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
