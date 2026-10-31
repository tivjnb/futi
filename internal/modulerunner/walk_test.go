package modulerunner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tivjnb/futi/internal/config"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/execmodule"
)

// writeTree writes files (relative path → content) under a new temp dir.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const dockerYAML = `commands:
  build:
    cmd: echo build
  run:
    cmd: echo run
`

func TestWalk(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"docker.yaml":     dockerYAML,
		"file.yaml":       "cmd: echo file\n",
		"tools/lint.yaml": "cmd: echo lint\n",
		"kube.yaml":       "cmd: echo kube\ncommands:\n  ctx:\n    cmd: echo ctx\n",
		"kube/extra.yaml": "cmd: echo extra\n",
		"tool.yaml":       "cmd: echo tool\ncommands:\n  file:\n    cmd: echo sub\n",
		"db.yaml":         "commands:\n  migrate:\n    cmd: echo migrate\n",
	})
	src := discovery.NewGlobalSource(dir)

	tests := []struct {
		name      string
		words     []string
		wantN     int
		wantPath  []string
		wantCmd   string // Command.Cmd; "" for a node without cmd
		wantNoMod bool   // namespace node
	}{
		{"module then command then value", []string{"docker", "build", "x"}, 2, []string{"docker", "build"}, "echo build", false},
		{"dotted word is one path word", []string{"docker.build", "x"}, 1, []string{"docker", "build"}, "echo build", false},
		{"dotted word with unmatched segment is a value", []string{"file.txt"}, 0, nil, "", true},
		{"dotted word after a node with child file", []string{"tool", "file.txt"}, 1, []string{"tool"}, "echo tool", false},
		{"stops at a flag", []string{"docker", "-x", "build"}, 1, []string{"docker"}, "", false},
		{"stops at double dash", []string{"docker", "--", "build"}, 1, []string{"docker"}, "", false},
		{"namespace node", []string{"tools"}, 1, []string{"tools"}, "", true},
		{"namespace then module", []string{"tools", "lint", "a"}, 2, []string{"tools", "lint"}, "echo lint", false},
		{"module and namespace of one name: module node", []string{"kube"}, 1, []string{"kube"}, "echo kube", false},
		{"module and namespace of one name: subcommand", []string{"kube", "ctx"}, 2, []string{"kube", "ctx"}, "echo ctx", false},
		{"module and namespace of one name: namespace child", []string{"kube", "extra"}, 2, []string{"kube", "extra"}, "echo extra", false},
		{"subcommand only right after the path", []string{"db", "--verbose", "migrate"}, 1, []string{"db"}, "", false},
		{"empty segment stops", []string{"docker..build"}, 0, nil, "", true},
		{"unknown first word", []string{"nope"}, 0, nil, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, n, err := Walk(src, nil, tt.words, WalkOptions{})
			if err != nil {
				t.Fatalf("Walk(%q): %v", tt.words, err)
			}
			if n != tt.wantN {
				t.Errorf("consumed = %d, want %d", n, tt.wantN)
			}
			if !slices.Equal(node.Path, tt.wantPath) {
				t.Errorf("Path = %q, want %q", node.Path, tt.wantPath)
			}
			var cmd string
			if node.Command != nil {
				cmd = node.Command.Cmd
			}
			if cmd != tt.wantCmd {
				t.Errorf("Command.Cmd = %q, want %q", cmd, tt.wantCmd)
			}
			if (node.Module == nil) != tt.wantNoMod {
				t.Errorf("Module = %v, want nil: %v", node.Module, tt.wantNoMod)
			}
		})
	}
}

func TestWalkNodeLocation(t *testing.T) {
	dir := writeTree(t, map[string]string{"docker.yaml": dockerYAML})
	node, _, err := Walk(discovery.NewGlobalSource(dir), nil, []string{"docker", "build"}, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if node.Namespace != "docker.build" || node.ManifestPath != filepath.Join(dir, "docker.yaml") ||
		node.ModuleRoot != dir || !slices.Equal(node.CmdPath, []string{"build"}) {
		t.Errorf("node = %+v, want namespace docker.build, manifest docker.yaml, root %s, CmdPath [build]", node, dir)
	}
}

func TestWalkCollisionInOneSource(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		words []string
		want  string // %s is the modules dir
	}{
		{
			"command and module",
			map[string]string{
				"dev.yaml":        "commands:\n  deploy:\n    cmd: echo a\n",
				"dev/deploy.yaml": "cmd: echo b\n",
			},
			[]string{"dev", "deploy"},
			`path "dev deploy" is ambiguous: command "deploy" of module dev (%[1]s/dev.yaml) and module dev.deploy (%[1]s/dev/deploy.yaml)`,
		},
		{
			"command and namespace via dotted word",
			map[string]string{
				"dev.yaml":       "commands:\n  ops:\n    cmd: echo a\n",
				"dev/ops/x.yaml": "cmd: echo b\n",
			},
			[]string{"dev.ops"},
			`path "dev ops" is ambiguous: command "ops" of module dev (%[1]s/dev.yaml) and namespace dev.ops`,
		},
		{
			"module inside a namespace",
			map[string]string{
				"ns/dev.yaml":        "commands:\n  deploy:\n    cmd: echo a\n",
				"ns/dev/deploy.yaml": "cmd: echo b\n",
			},
			[]string{"ns", "dev", "deploy"},
			`path "ns dev deploy" is ambiguous: command "deploy" of module ns.dev (%[1]s/ns/dev.yaml) and module ns.dev.deploy (%[1]s/ns/dev/deploy.yaml)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeTree(t, tt.files)
			_, _, err := Walk(discovery.NewGlobalSource(dir), nil, tt.words, WalkOptions{})
			var ce *CollisionError
			if !errors.As(err, &ce) {
				t.Fatalf("err = %v, want *CollisionError", err)
			}
			if want := fmt.Sprintf(filepath.FromSlash(tt.want), dir); err.Error() != want {
				t.Errorf("err = %q\nwant  %q", err.Error(), want)
			}
		})
	}
}

func TestWalkCollisionBetweenSources(t *testing.T) {
	a := writeTree(t, map[string]string{"dev.yaml": "commands:\n  deploy:\n    cmd: echo a\n"})
	b := writeTree(t, map[string]string{"dev/deploy.yaml": "cmd: echo b\n"})
	src := discovery.NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: a}, {Path: b}}})

	_, _, err := Walk(src, nil, []string{"dev", "deploy", "x"}, WalkOptions{Verified: true})
	var ce *CollisionError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want *CollisionError", err)
	}
	want := `path "dev deploy" is ambiguous: command "deploy" of module dev (` + filepath.Join(a, "dev.yaml") +
		`) and module dev.deploy (` + filepath.Join(b, "dev", "deploy.yaml") + `)`
	if err.Error() != want {
		t.Errorf("err = %q\nwant  %q", err.Error(), want)
	}
	if !slices.Equal(ce.Path, []string{"dev", "deploy"}) {
		t.Errorf("Path = %q, want [dev deploy]", ce.Path)
	}
}

func TestWalkLayeredOverrideIsNotCollision(t *testing.T) {
	global := writeTree(t, map[string]string{"docker.yaml": dockerYAML})
	local := writeTree(t, map[string]string{"docker.yaml": "commands:\n  build:\n    cmd: echo local\n"})
	src := discovery.NewLayeredSource(discovery.NewGlobalSource(local), discovery.NewGlobalSource(global))

	node, n, err := Walk(src, nil, []string{"docker", "build"}, WalkOptions{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if n != 2 || node.Command.Cmd != "echo local" {
		t.Errorf("n = %d, cmd = %q, want 2, %q", n, node.Command.Cmd, "echo local")
	}
}

// loadSpy records which loader method Walk called.
type loadSpy struct{ plain, verified int }

func (s *loadSpy) LoadModule(manifestPath, moduleRoot string) (*execmodule.Module, error) {
	s.plain++
	return execmodule.LoadModule(manifestPath, moduleRoot)
}

func (s *loadSpy) LoadModuleVerified(manifestPath, moduleRoot string) (*execmodule.Module, error) {
	s.verified++
	return execmodule.LoadModule(manifestPath, moduleRoot)
}

func TestWalkLoaderByOption(t *testing.T) {
	dir := writeTree(t, map[string]string{"docker.yaml": dockerYAML})
	src := discovery.NewGlobalSource(dir)
	for _, tt := range []struct {
		verified           bool
		wantPlain, wantVer int
	}{
		{false, 1, 0},
		{true, 0, 1},
	} {
		spy := &loadSpy{}
		if _, _, err := Walk(src, spy, []string{"docker", "build"}, WalkOptions{Verified: tt.verified}); err != nil {
			t.Fatal(err)
		}
		if spy.plain != tt.wantPlain || spy.verified != tt.wantVer {
			t.Errorf("Verified=%v: LoadModule=%d LoadModuleVerified=%d, want %d %d",
				tt.verified, spy.plain, spy.verified, tt.wantPlain, tt.wantVer)
		}
	}
}

func TestNodeChildren(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"docker.yaml":    dockerYAML,
		"kube.yaml":      "cmd: echo kube\ncommands:\n  logs:\n    cmd: echo logs\n",
		"kube/ctx.yaml":  "cmd: echo ctx\n",
		"tools/a.yaml":   "cmd: echo a\n",
		"tools/b/c.yaml": "cmd: echo c\n",
	})
	src := discovery.NewGlobalSource(dir)
	for _, tt := range []struct {
		words []string
		want  []string
	}{
		{[]string{"docker"}, []string{"build", "run"}},
		{[]string{"kube"}, []string{"ctx", "logs"}},
		{[]string{"tools"}, []string{"a", "b"}},
		{[]string{"docker", "build"}, nil},
	} {
		node, _, err := Walk(src, nil, tt.words, WalkOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := node.Children(src); !slices.Equal(got, tt.want) {
			t.Errorf("Children(%q) = %q, want %q", tt.words, got, tt.want)
		}
	}
}

func TestResolveWordsErrors(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"docker.yaml":  dockerYAML,
		"tools/a.yaml": "cmd: echo a\n",
	})
	src := discovery.NewGlobalSource(dir)
	tests := []struct {
		words []string
		want  string
		is    error
	}{
		{[]string{"nope"}, `unknown command "nope" for "futi"`, ErrModuleNotFound},
		{[]string{"docker", "x"}, `unknown command "x" for "futi docker" (available: build, run)`, execmodule.ErrCommandNotFound},
		{[]string{"docker"}, `"futi docker" requires a subcommand (available: build, run)`, execmodule.ErrRequiresSubcommand},
		{[]string{"tools", "--v"}, `"futi tools" requires a subcommand (available: a)`, execmodule.ErrRequiresSubcommand},
		{[]string{"docker", "--v", "run"}, `"futi docker" requires a subcommand (available: build, run) (put the full command path before flags)`, execmodule.ErrRequiresSubcommand},
	}
	for _, tt := range tests {
		_, _, err := ResolveWords(src, nil, tt.words)
		if err == nil || err.Error() != tt.want {
			t.Errorf("ResolveWords(%q) err = %v\nwant %q", tt.words, err, tt.want)
			continue
		}
		if !errors.Is(err, tt.is) {
			t.Errorf("ResolveWords(%q) err = %v, want errors.Is %v", tt.words, err, tt.is)
		}
	}
}

func TestResolveWordsRest(t *testing.T) {
	dir := writeTree(t, map[string]string{"docker.yaml": dockerYAML})
	r, rest, err := ResolveWords(discovery.NewGlobalSource(dir), nil, []string{"docker.build", "--tag", "x", "--", "a"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Command.Cmd != "echo build" || r.ModuleName() != "docker" ||
		!slices.Equal(rest, []string{"--tag", "x", "--", "a"}) {
		t.Errorf("cmd = %q, module = %q, rest = %q", r.Command.Cmd, r.ModuleName(), rest)
	}
}

// A missing config source must not turn an unknown word into a source error.
func TestWalkUnreadableSourceDoesNotHideUnknownWord(t *testing.T) {
	a := writeTree(t, map[string]string{"docker.yaml": dockerYAML})
	missing := filepath.Join(t.TempDir(), "missing")
	src := discovery.NewCompositeSource(&config.Config{Sources: []config.SourceConfig{{Path: a}, {Path: missing}}})

	_, _, err := ResolveWords(src, nil, []string{"docker", "x"})
	want := `unknown command "x" for "futi docker" (available: build, run)`
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}
