package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunModulesInitSubcommand(t *testing.T) {
	t.Chdir(t.TempDir())
	var b bytes.Buffer
	if err := runModules(&b, nil, nil, []string{"init", "mymod"}); err != nil {
		t.Fatalf("runModules: %v", err)
	}
	if _, err := os.Stat(filepath.Join(".futi", "mymod.yaml")); err != nil {
		t.Fatalf("module not created: %v", err)
	}
}

func TestRunModulesNoArgsUsage(t *testing.T) {
	err := runModules(io.Discard, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestRunModulesUnknownSubcommand(t *testing.T) {
	err := runModules(io.Discard, nil, nil, []string{"frobnicate"})
	if err == nil || !strings.Contains(err.Error(), "unknown modules subcommand") {
		t.Fatalf("expected unknown subcommand error, got %v", err)
	}
}

func TestRunModulesInitRejectsInvalidManifest(t *testing.T) {
	tests := []struct {
		name string
		opts InitOptions
	}{
		{"command with space", InitOptions{Path: "foo", Commands: []string{"build image"}}},
		{"command starts with dash", InitOptions{Path: "foo", Commands: []string{"-x"}}},
		{"command with dot", InitOptions{Path: "foo", Commands: []string{"a.b"}}},
		{"broken default cmd template", InitOptions{Path: "foo", DefaultCmd: "echo {{"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			err := Init(io.Discard, io.Discard, tc.opts)
			if err == nil || !strings.Contains(err.Error(), "invalid module") {
				t.Fatalf("err = %v, want invalid module error", err)
			}
			if _, statErr := os.Stat(".futi"); !os.IsNotExist(statErr) {
				t.Fatalf(".futi must not be created, stat err = %v", statErr)
			}
		})
	}
}

func TestRunModulesInitValidManifestStillWritten(t *testing.T) {
	t.Chdir(t.TempDir())
	var b bytes.Buffer
	if err := Init(&b, io.Discard, InitOptions{Path: "foo", Commands: []string{"build"}, DefaultCmd: "echo hi"}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(".futi", "foo.yaml")); err != nil {
		t.Fatalf("module not created: %v", err)
	}
}
