package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// TestParseArgsCommandWords: anything that is not a builtin first word is the
// command path with its inputs, kept as given (the path is found by the walk).
func TestParseArgsCommandWords(t *testing.T) {
	for _, args := range [][]string{
		{"docker", "build", "--tag", "myapp"},
		{"dev", "deploy", "staging"},
		{"echo_mod", "--", "hello", "world"},
		{"docker", "list", "-v"},
		{"docker", "help", "--x"},
		{"validate", "-h"},
		{"a", "-", "b"},
	} {
		pa, err := parseArgs(args)
		if err != nil {
			t.Fatalf("parseArgs(%q): %v", args, err)
		}
		if !reflect.DeepEqual(pa.words, args) {
			t.Errorf("parseArgs(%q).words = %q, want unchanged", args, pa.words)
		}
	}
}

func TestSplitInputs(t *testing.T) {
	tests := []struct {
		rest          []string
		flags, argsLs []string
	}{
		{nil, nil, nil},
		{[]string{"--tag", "x"}, []string{"--tag", "x"}, nil},
		{[]string{"--tag", "x", "--", "a", "--b"}, []string{"--tag", "x"}, []string{"a", "--b"}},
		{[]string{"--", "--", "a"}, nil, []string{"--", "a"}},
		{[]string{"--follow", "logs"}, []string{"--follow", "logs"}, nil},
		{[]string{"x", "--"}, []string{"x"}, []string{}},
	}
	for _, tt := range tests {
		flags, argsList := splitInputs(tt.rest)
		if !reflect.DeepEqual(flags, tt.flags) || len(argsList) != len(tt.argsLs) ||
			(len(argsList) > 0 && !reflect.DeepEqual(argsList, tt.argsLs)) {
			t.Errorf("splitInputs(%q) = %q, %q, want %q, %q", tt.rest, flags, argsList, tt.flags, tt.argsLs)
		}
	}
}

func TestSplitPathFlags(t *testing.T) {
	tests := []struct {
		args        []string
		path, flags []string
	}{
		{[]string{"docker", "build"}, []string{"docker", "build"}, nil},
		{[]string{"k8s", "--follow", "logs"}, []string{"k8s"}, []string{"--follow", "logs"}},
		{[]string{"a", "-", "b", "-v"}, []string{"a", "-", "b"}, []string{"-v"}},
		{[]string{"docker", "--", "-h"}, []string{"docker"}, nil},
	}
	for _, tt := range tests {
		path, flags := splitPathFlags(tt.args)
		if !reflect.DeepEqual(path, tt.path) || !reflect.DeepEqual(flags, tt.flags) {
			t.Errorf("splitPathFlags(%q) = %q, %q, want %q, %q", tt.args, path, flags, tt.path, tt.flags)
		}
	}
}

func TestParseArgsTooFewArgs(t *testing.T) {
	if _, err := parseArgs([]string{}); err == nil {
		t.Fatal("expected error for no args")
	}
}

func TestParseArgsListRequest(t *testing.T) {
	tests := []struct {
		args             []string
		namespace        string
		verbose, recurse bool
	}{
		{[]string{"list"}, "", false, false},
		{[]string{"list", "images"}, "images", false, false},
		{[]string{"list", "dev", "deploy"}, "dev.deploy", false, false},
		{[]string{"list", "dev.deploy"}, "dev.deploy", false, false},
		{[]string{"list", "--verbose"}, "", true, false},
		{[]string{"list", "-v"}, "", true, false},
		{[]string{"list", "images", "-v"}, "images", true, false},
		{[]string{"list", "--recurse"}, "", false, true},
		{[]string{"list", "-r"}, "", false, true},
		{[]string{"list", "images", "-r", "-v"}, "images", true, true},
	}
	for _, tt := range tests {
		_, err := parseArgs(tt.args)
		var req *listRequest
		if !errors.As(err, &req) || !errors.Is(err, errListCommand) {
			t.Errorf("parseArgs(%q) err = %v, want *listRequest", tt.args, err)
			continue
		}
		if req.namespace != tt.namespace || req.verbose != tt.verbose || req.recurse != tt.recurse {
			t.Errorf("parseArgs(%q) = %+v, want namespace %q verbose %v recurse %v",
				tt.args, req, tt.namespace, tt.verbose, tt.recurse)
		}
	}
}

func TestParseArgsListFlags(t *testing.T) {
	_, err := parseArgs([]string{"list", "--foo"})
	want := `list: unknown flag "--foo" (supported: -v, --verbose, -r, --recurse)`
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	var lr *listRequest
	if errors.As(err, &lr) {
		t.Fatal("unknown flag must not yield listRequest")
	}

	for _, args := range [][]string{{"list", "-h"}, {"list", "docker", "--help"}} {
		if _, err = parseArgs(args); !errors.Is(err, errHelp) {
			t.Fatalf("parseArgs(%q) err = %v, want errHelp", args, err)
		}
	}
}

func TestParseArgsHelpRequest(t *testing.T) {
	for _, tt := range []struct{ args, path []string }{
		{[]string{"help"}, []string{}},
		{[]string{"help", "docker", "build"}, []string{"docker", "build"}},
		{[]string{"help", "docker.build", "--x"}, []string{"docker.build", "--x"}},
		{[]string{"help", "docker", "-h"}, []string{"docker"}},
		{[]string{"help", "docker", "build", "--help"}, []string{"docker", "build"}},
		{[]string{"help", "docker", "-h", "--help"}, []string{"docker"}},
	} {
		_, err := parseArgs(tt.args)
		var req *helpRequest
		if !errors.As(err, &req) || len(req.path) != len(tt.path) ||
			(len(tt.path) > 0 && !reflect.DeepEqual(req.path, tt.path)) {
			t.Errorf("parseArgs(%q) err = %v, want helpRequest{%q}", tt.args, err, tt.path)
		}
	}
}

func TestParseArgsHelpHelpIsFutiHelp(t *testing.T) {
	for _, args := range [][]string{{"help", "-h"}, {"help", "--help"}, {"help", "--help", "-h"}} {
		if _, err := parseArgs(args); !errors.Is(err, errHelp) {
			t.Errorf("parseArgs(%q) err = %v, want errHelp", args, err)
		}
	}
}

func TestParseArgsFlagsBeforePathIsUsageError(t *testing.T) {
	_, err := parseArgs([]string{"--tag", "x", "docker", "build"})
	if err == nil || !strings.HasPrefix(err.Error(), "usage: ") || !strings.Contains(err.Error(), "path goes before flags") {
		t.Fatalf("err = %v, want usage error with path hint", err)
	}
	if errors.Is(err, errHelp) {
		t.Fatal("must not be errHelp")
	}
}

func TestParseArgsHelpFlag(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}, {"--tag", "x", "-h"}} {
		if _, err := parseArgs(args); !errors.Is(err, errHelp) {
			t.Fatalf("parseArgs(%v) err = %v, want errHelp", args, err)
		}
	}
}

// TestParseArgsEmptyPathHintOnlyWithFlags: подсказка про порядок пути — только
// когда флаги действительно есть (futi -- a её не заслуживает).
func TestParseArgsEmptyPathHintOnlyWithFlags(t *testing.T) {
	_, err := parseArgs([]string{"--", "a"})
	if err == nil || !strings.HasPrefix(err.Error(), "usage: ") {
		t.Fatalf("err = %v, want usage error", err)
	}
	if strings.Contains(err.Error(), "path goes before flags") {
		t.Fatalf("err = %q, must not mention flags when none given", err)
	}
}
