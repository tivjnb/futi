package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseArgsNamespaceAndCommand(t *testing.T) {
	args := []string{"docker", "build", "--tag", "myapp"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "docker" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "build" {
		t.Fatalf("unexpected command: %q", pa.command)
	}
	if !reflect.DeepEqual(pa.flags, []string{"--tag", "myapp"}) {
		t.Fatalf("unexpected flags: %+v", pa.flags)
	}
}

func TestParseArgsNestedNamespace(t *testing.T) {
	args := []string{"dev", "deploy", "staging"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "dev.deploy" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "staging" {
		t.Fatalf("unexpected command: %q", pa.command)
	}
	if len(pa.flags) != 0 {
		t.Fatalf("unexpected flags: %+v", pa.flags)
	}
}

func TestParseArgsEqualSyntax(t *testing.T) {
	args := []string{"docker", "build", "--tag=myapp"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "docker" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "build" {
		t.Fatalf("unexpected command: %q", pa.command)
	}
	if !reflect.DeepEqual(pa.flags, []string{"--tag=myapp"}) {
		t.Fatalf("unexpected flags: %+v", pa.flags)
	}
}

func TestParseArgsBoolFlag(t *testing.T) {
	args := []string{"k8s", "logs", "--follow"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "k8s" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "logs" {
		t.Fatalf("unexpected command: %q", pa.command)
	}
	if !reflect.DeepEqual(pa.flags, []string{"--follow"}) {
		t.Fatalf("unexpected flags: %+v", pa.flags)
	}
}

func TestParseArgsTooFewArgs(t *testing.T) {
	_, err := parseArgs([]string{})
	if err == nil {
		t.Fatal("expected error for no args")
	}
}

func TestParseArgsSingleNamespaceForDefaultCmd(t *testing.T) {
	args := []string{"echo_mod"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "echo_mod" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "" {
		t.Fatalf("expected empty command for default cmd, got %q", pa.command)
	}
}

func TestParseArgsListReserved(t *testing.T) {
	args := []string{"list"}
	_, err := parseArgs(args)
	if !errors.Is(err, errListCommand) {
		t.Fatalf("expected errListCommand, got: %v", err)
	}
}

func TestParseArgsListWithNamespace(t *testing.T) {
	args := []string{"images", "list"}
	_, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if req.namespace != "images" {
		t.Fatalf("unexpected namespace: %q", req.namespace)
	}
}

func TestParseArgsListWithNestedNamespace(t *testing.T) {
	args := []string{"dev", "deploy", "list"}
	_, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if req.namespace != "dev.deploy" {
		t.Fatalf("unexpected namespace: %q", req.namespace)
	}
}

func TestParseArgsListVerbose(t *testing.T) {
	args := []string{"list", "--verbose"}
	_, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if !req.verbose {
		t.Fatal("expected verbose=true")
	}
}

func TestParseArgsListVerboseShort(t *testing.T) {
	args := []string{"list", "-v"}
	_, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if !req.verbose {
		t.Fatal("expected verbose=true")
	}
}

func TestParseArgsListWithNamespaceVerbose(t *testing.T) {
	args := []string{"images", "list", "-v"}
	_, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if req.namespace != "images" {
		t.Fatalf("unexpected namespace: %q", req.namespace)
	}
	if !req.verbose {
		t.Fatal("expected verbose=true")
	}
}

func TestParseArgsListRecurse(t *testing.T) {
	args := []string{"list", "--recurse"}
	_, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if !req.recurse {
		t.Fatal("expected recurse=true")
	}
}

func TestParseArgsListRecurseShort(t *testing.T) {
	args := []string{"list", "-r"}
	_, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if !req.recurse {
		t.Fatal("expected recurse=true")
	}
}

func TestParseArgsListWithNamespaceRecurseVerbose(t *testing.T) {
	args := []string{"images", "list", "-r", "-v"}
	_, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if req.namespace != "images" {
		t.Fatalf("unexpected namespace: %q", req.namespace)
	}
	if !req.recurse {
		t.Fatal("expected recurse=true")
	}
	if !req.verbose {
		t.Fatal("expected verbose=true")
	}
}

func TestParseArgsDoubleDashArgs(t *testing.T) {
	args := []string{"echo_mod", "echo", "--", "hello", "world"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "echo_mod" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "echo" {
		t.Fatalf("unexpected command: %q", pa.command)
	}
	if len(pa.argsList) != 2 || pa.argsList[0] != "hello" || pa.argsList[1] != "world" {
		t.Fatalf("unexpected argsList: %+v", pa.argsList)
	}
}

func TestParseArgsDoubleDashWithQuotedArg(t *testing.T) {
	args := []string{"echo_mod", "echo", "--", "hello world", "foo"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pa.argsList) != 2 || pa.argsList[0] != "hello world" || pa.argsList[1] != "foo" {
		t.Fatalf("unexpected argsList: %+v", pa.argsList)
	}
}

func TestParseArgsDoubleDashOnly(t *testing.T) {
	args := []string{"echo_mod", "--", "hello", "world"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "echo_mod" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "" {
		t.Fatalf("unexpected command: %q", pa.command)
	}
	if len(pa.argsList) != 2 || pa.argsList[0] != "hello" || pa.argsList[1] != "world" {
		t.Fatalf("unexpected argsList: %+v", pa.argsList)
	}
}

func TestParseArgsDoubleDashEmpty(t *testing.T) {
	args := []string{"echo_mod", "echo", "--"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "echo_mod" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "echo" {
		t.Fatalf("unexpected command: %q", pa.command)
	}
	if len(pa.argsList) != 0 {
		t.Fatalf("unexpected argsList: %+v", pa.argsList)
	}
}

func TestParseArgsDoubleDashWithFlags(t *testing.T) {
	args := []string{"docker", "build", "--tag", "myapp", "--", "extra", "arg"}
	pa, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pa.namespace != "docker" {
		t.Fatalf("unexpected namespace: %q", pa.namespace)
	}
	if pa.command != "build" {
		t.Fatalf("unexpected command: %q", pa.command)
	}
	if !reflect.DeepEqual(pa.flags, []string{"--tag", "myapp"}) {
		t.Fatalf("unexpected flags: %+v", pa.flags)
	}
	if len(pa.argsList) != 2 || pa.argsList[0] != "extra" || pa.argsList[1] != "arg" {
		t.Fatalf("unexpected argsList: %+v", pa.argsList)
	}
}

// TestParseArgsValidateIsOrdinaryCommand проверяет, что "validate" больше не
// системное слово: путь разбирается как обычная команда (audit F81).
func TestParseArgsValidateIsOrdinaryCommand(t *testing.T) {
	cases := []struct {
		args        []string
		ns, command string
	}{
		{[]string{"dev", "validate"}, "dev", "validate"},
		{[]string{"dev", "deploy", "validate"}, "dev.deploy", "validate"},
		{[]string{"validate"}, "validate", ""},
	}
	for _, c := range cases {
		pa, err := parseArgs(c.args)
		if err != nil {
			t.Fatalf("parseArgs(%v): %v", c.args, err)
		}
		if pa.namespace != c.ns || pa.command != c.command {
			t.Errorf("parseArgs(%v) = ns %q cmd %q, want ns %q cmd %q", c.args, pa.namespace, pa.command, c.ns, c.command)
		}
	}
	pa, err := parseArgs([]string{"docker", "validate", "--verbose"})
	if err != nil || len(pa.flags) != 1 || pa.flags[0] != "--verbose" {
		t.Fatalf("validate с флагом: pa=%+v err=%v, want обычный флаг", pa, err)
	}
}

// TestParseArgsShortFlagsStayRawForRegularCommands проверяет, что -v/-r у
// обычной команды не системные: разбор оставляет их сырыми флагами (отвергнет
// bindFlags), а --verbose/--recurse — обычные параметры.
func TestParseArgsShortFlagsStayRawForRegularCommands(t *testing.T) {
	for _, flag := range []string{"-v", "-r", "--verbose", "--recurse"} {
		pa, err := parseArgs([]string{"docker", "build", flag})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", flag, err)
		}
		if !reflect.DeepEqual(pa.flags, []string{flag}) {
			t.Fatalf("%s: flags = %+v, want [%s]", flag, pa.flags, flag)
		}
	}
}

// TestParseArgsVerboseStillWorksForList проверяет, что -v работает для list
// (listRequest.verbose = true).
func TestParseArgsVerboseStillWorksForList(t *testing.T) {
	_, err := parseArgs([]string{"docker", "list", "-v"})
	var listReq *listRequest
	if !errors.As(err, &listReq) {
		t.Fatalf("expected listRequest, got: %v", err)
	}
	if !listReq.verbose {
		t.Fatal("expected verbose=true for list")
	}
}

// --- #15: cache refresh --verbose shows module details ---

func TestParseArgsStrictOrder(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantNS    string
		wantCmd   string
		wantFlags []string
	}{
		{"слово после флага остаётся во флагах", []string{"k8s", "--follow", "logs"}, "k8s", "", []string{"--follow", "logs"}},
		{"короткий флаг", []string{"docker", "build", "-v"}, "docker", "build", []string{"-v"}},
		{"одиночный дефис — слово пути", []string{"a", "-", "b"}, "a.-", "b", nil},
		{"-- сразу после пути", []string{"docker", "build", "--", "--x"}, "docker", "build", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pa, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pa.namespace != tt.wantNS || pa.command != tt.wantCmd {
				t.Fatalf("path = (%q, %q), want (%q, %q)", pa.namespace, pa.command, tt.wantNS, tt.wantCmd)
			}
			if len(pa.flags) != len(tt.wantFlags) || (len(tt.wantFlags) > 0 && !reflect.DeepEqual(pa.flags, tt.wantFlags)) {
				t.Fatalf("flags = %+v, want %+v", pa.flags, tt.wantFlags)
			}
		})
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

func TestParseArgsListFlags(t *testing.T) {
	_, err := parseArgs([]string{"list", "-r", "-v"})
	var req *listRequest
	if !errors.As(err, &req) || !req.recurse || !req.verbose {
		t.Fatalf("err = %v, req = %+v, want listRequest recurse+verbose", err, req)
	}

	_, err = parseArgs([]string{"list", "--foo"})
	want := `list: unknown flag "--foo" (supported: -v, --verbose, -r, --recurse)`
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	var lr *listRequest
	if errors.As(err, &lr) {
		t.Fatal("unknown flag must not yield listRequest")
	}

	if _, err = parseArgs([]string{"docker", "list", "-h"}); !errors.Is(err, errHelp) {
		t.Fatalf("list -h err = %v, want errHelp", err)
	}
}

func TestParseArgsHelpRequestIgnoresFlags(t *testing.T) {
	_, err := parseArgs([]string{"docker", "help", "--x"})
	var req *helpRequest
	if !errors.As(err, &req) || !reflect.DeepEqual(req.positional, []string{"docker"}) {
		t.Fatalf("err = %v, want helpRequest{docker}", err)
	}
}

// validate больше не системное слово: -h после него — обычный флаг справки
// по пути (а не errHelp общего usage).
func TestParseArgsValidateHelpFlagIsOrdinary(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		pa, err := parseArgs([]string{"validate", flag})
		if err != nil || pa.namespace != "validate" || len(pa.flags) != 1 || pa.flags[0] != flag {
			t.Fatalf("validate %s: pa=%+v err=%v, want обычный путь с флагом", flag, pa, err)
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
