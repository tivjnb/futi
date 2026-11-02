package inputs

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tivjnb/futi/internal/argvtmpl"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestSchemaGolden(t *testing.T) {
	tests := []struct {
		file string
		node node
		cmd  string
	}{
		{"k8s_logs", k8sLogs, `kubectl logs {{.pod}} {{if .follow}}-f{{end}} --tail={{.tail}}`},
		{"files_copy", filesCopy, `cp {{if .force}}-f{{end}} {{.src}} {{.dst}}`},
		{"docker_run", dockerRun, `docker run {{if .rm}}--rm{{end}} {{.image}} {{.extra}}`},
	}
	for _, tc := range tests {
		t.Run(tc.file, func(t *testing.T) {
			tpl, err := argvtmpl.Parse(tc.cmd)
			if err != nil {
				t.Fatal(err)
			}
			ti := TemplateInputs(tc.node.args, tc.node.flags)
			schema := Schema(tc.node.args, tc.node.flags, func(name string) bool { return tpl.Guarded(ti, name) })
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetIndent("", "  ")
			if err := enc.Encode(schema); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", tc.file+".json")
			if *update {
				if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(buf.Bytes(), want) {
				t.Fatalf("schema differs from %s:\n%s", path, buf.String())
			}
		})
	}
}

func TestSchemaGuardOnInt(t *testing.T) {
	schema := Schema(nil, []Spec{{Name: "n", Type: "int", Flag: true}}, func(string) bool { return true })
	b, _ := json.Marshal(schema)
	want := `{"additionalProperties":false,"properties":{"n":{"minimum":0,"type":"integer"}},"type":"object"}`
	if string(b) != want {
		t.Fatalf("got %s, want %s", b, want)
	}
}

func TestSchemaEmptyNode(t *testing.T) {
	b, _ := json.Marshal(Schema(nil, nil, func(string) bool { return false }))
	want := `{"additionalProperties":false,"properties":{},"type":"object"}`
	if string(b) != want {
		t.Fatalf("got %s, want %s", b, want)
	}
}

func TestSchemaChoicesAndPathTypes(t *testing.T) {
	no := func(string) bool { return false }
	props := func(s Spec) map[string]any {
		return Schema(nil, []Spec{s}, no)["properties"].(map[string]any)["x"].(map[string]any)
	}

	if got := props(Spec{Name: "x", Flag: true, Choices: []string{"a", "b"}})["enum"]; !reflect.DeepEqual(got, []any{"a", "b"}) {
		t.Errorf("string enum = %#v", got)
	}
	if got := props(Spec{Name: "x", Flag: true, Type: "int", Choices: []string{"1", "3"}})["enum"]; !reflect.DeepEqual(got, []any{1, 3}) {
		t.Errorf("int enum = %#v", got)
	}
	list := props(Spec{Name: "x", Flag: true, Variadic: true, Choices: []string{"a", "b"}})
	if _, ok := list["enum"]; ok {
		t.Errorf("enum on the array itself: %#v", list)
	}
	if got := list["items"].(map[string]any)["enum"]; !reflect.DeepEqual(got, []any{"a", "b"}) {
		t.Errorf("items enum = %#v", got)
	}

	for _, tc := range []struct{ typ, desc, want string }{
		{"file", "", "(path to a file)"},
		{"file", "Output", "Output (path to a file)"},
		{"dir", "", "(path to a directory)"},
		{"dir", "Where", "Where (path to a directory)"},
	} {
		p := props(Spec{Name: "x", Flag: true, Type: tc.typ, Description: tc.desc})
		if p["type"] != "string" || p["description"] != tc.want {
			t.Errorf("%s %q: type=%v description=%v, want string %q", tc.typ, tc.desc, p["type"], p["description"], tc.want)
		}
	}
}
