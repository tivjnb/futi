package main

import (
	"errors"
	"testing"
)

func TestParseArgsNamespaceAndCommand(t *testing.T) {
	args := []string{"docker", "build", "--tag", "myapp"}
	ns, cmd, params, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ns != "docker" {
		t.Fatalf("unexpected namespace: %q", ns)
	}
	if cmd != "build" {
		t.Fatalf("unexpected command: %q", cmd)
	}
	if params["tag"] != "myapp" {
		t.Fatalf("unexpected params: %+v", params)
	}
}

func TestParseArgsNestedNamespace(t *testing.T) {
	args := []string{"dev", "deploy", "staging"}
	ns, cmd, params, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ns != "dev.deploy" {
		t.Fatalf("unexpected namespace: %q", ns)
	}
	if cmd != "staging" {
		t.Fatalf("unexpected command: %q", cmd)
	}
	if len(params) != 0 {
		t.Fatalf("unexpected params: %+v", params)
	}
}

func TestParseArgsEqualSyntax(t *testing.T) {
	args := []string{"docker", "build", "--tag=myapp"}
	ns, cmd, params, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ns != "docker" {
		t.Fatalf("unexpected namespace: %q", ns)
	}
	if cmd != "build" {
		t.Fatalf("unexpected command: %q", cmd)
	}
	if params["tag"] != "myapp" {
		t.Fatalf("unexpected params: %+v", params)
	}
}

func TestParseArgsBoolFlag(t *testing.T) {
	// NOTE: Brief had args {"k8s", "logs", "mypod", "--follow"} expecting
	// ns="k8s", cmd="logs" — but that's 3 positionals, which by the rule
	// (last = command, rest = namespace) yields ns="k8s.logs", cmd="mypod".
	// "mypod" removed to align with the rule and TestParseArgsNestedNamespace.
	// The test's purpose (bool flag --follow -> "true") is preserved.
	args := []string{"k8s", "logs", "--follow"}
	ns, cmd, params, err := parseArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ns != "k8s" {
		t.Fatalf("unexpected namespace: %q", ns)
	}
	if cmd != "logs" {
		t.Fatalf("unexpected command: %q", cmd)
	}
	if params["follow"] != "true" {
		t.Fatalf("unexpected follow value: %q", params["follow"])
	}
}

func TestParseArgsTooFewArgs(t *testing.T) {
	_, _, _, err := parseArgs([]string{"docker"})
	if err == nil {
		t.Fatal("expected error for too few args")
	}
}

func TestParseArgsListReserved(t *testing.T) {
	args := []string{"list"}
	_, _, _, err := parseArgs(args)
	if !errors.Is(err, errListCommand) {
		t.Fatalf("expected errListCommand, got: %v", err)
	}
}

func TestParseArgsListWithNamespace(t *testing.T) {
	args := []string{"images", "list"}
	_, _, _, err := parseArgs(args)
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
	_, _, _, err := parseArgs(args)
	var req *listRequest
	if !errors.As(err, &req) {
		t.Fatalf("expected *listRequest, got: %v", err)
	}
	if req.namespace != "dev.deploy" {
		t.Fatalf("unexpected namespace: %q", req.namespace)
	}
}
