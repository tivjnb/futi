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
	if err := runModules(&b, nil, nil, nil, []string{"init", "mymod"}); err != nil {
		t.Fatalf("runModules: %v", err)
	}
	if _, err := os.Stat(filepath.Join(".futi", "mymod.yaml")); err != nil {
		t.Fatalf("module not created: %v", err)
	}
}

func TestRunModulesNoArgsUsage(t *testing.T) {
	err := runModules(io.Discard, nil, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestRunModulesUnknownSubcommand(t *testing.T) {
	err := runModules(io.Discard, nil, nil, nil, []string{"frobnicate"})
	if err == nil || !strings.Contains(err.Error(), "unknown modules subcommand") {
		t.Fatalf("expected unknown subcommand error, got %v", err)
	}
}
