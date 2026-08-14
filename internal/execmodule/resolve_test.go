package execmodule

import (
	"strings"
	"testing"
)

func TestResolveCommandSimple(t *testing.T) {
	mod := &Module{
		Name: "testmod",
		Commands: map[string]*Command{
			"status": {Cmd: "echo status"},
		},
	}

	cmd, err := mod.ResolveCommand([]string{"status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Cmd != "echo status" {
		t.Fatalf("unexpected cmd: %q", cmd.Cmd)
	}
}

func TestResolveCommandWithSubcommand(t *testing.T) {
	mod := &Module{
		Name: "db",
		Commands: map[string]*Command{
			"migrate": {
				Description: "Database migrations",
				Commands: map[string]*Command{
					"up":   {Cmd: "echo up"},
					"down": {Cmd: "echo down"},
				},
			},
		},
	}

	cmd, err := mod.ResolveCommand([]string{"migrate", "up"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Cmd != "echo up" {
		t.Fatalf("unexpected cmd: %q", cmd.Cmd)
	}
}

func TestResolveCommandNestedSubcommands(t *testing.T) {
	mod := &Module{
		Name: "db",
		Commands: map[string]*Command{
			"migrate": {
				Commands: map[string]*Command{
					"up": {
						Commands: map[string]*Command{
							"force": {Cmd: "echo force"},
							"safe":  {Cmd: "echo safe"},
						},
					},
				},
			},
		},
	}

	cmd, err := mod.ResolveCommand([]string{"migrate", "up", "force"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Cmd != "echo force" {
		t.Fatalf("unexpected cmd: %q", cmd.Cmd)
	}
}

func TestResolveCommandBranchWithoutLeaf(t *testing.T) {
	mod := &Module{
		Name: "db",
		Commands: map[string]*Command{
			"migrate": {
				Commands: map[string]*Command{
					"up": {Cmd: "echo up"},
				},
			},
		},
	}

	_, err := mod.ResolveCommand([]string{"migrate"})
	if err == nil {
		t.Fatal("expected error: 'migrate' has commands but no cmd")
	}
	if !strings.Contains(err.Error(), "migrate") {
		t.Fatalf("error should mention command name, got: %v", err)
	}
}

func TestResolveCommandNotFound(t *testing.T) {
	mod := &Module{
		Name: "db",
		Commands: map[string]*Command{
			"status": {Cmd: "echo status"},
		},
	}

	_, err := mod.ResolveCommand([]string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for non-existent command")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("error should mention command name, got: %v", err)
	}
}

func TestResolveCommandSubcommandNotFound(t *testing.T) {
	mod := &Module{
		Name: "db",
		Commands: map[string]*Command{
			"migrate": {
				Commands: map[string]*Command{
					"up": {Cmd: "echo up"},
				},
			},
		},
	}

	_, err := mod.ResolveCommand([]string{"migrate", "nonexistent"})
	if err == nil {
		t.Fatal("expected error for non-existent subcommand")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("error should mention subcommand name, got: %v", err)
	}
}

func TestResolveCommandNoSubcommandsButPathContinues(t *testing.T) {
	mod := &Module{
		Name: "db",
		Commands: map[string]*Command{
			"status": {Cmd: "echo status"},
		},
	}

	_, err := mod.ResolveCommand([]string{"status", "up"})
	if err == nil {
		t.Fatal("expected error: 'status' has no subcommands")
	}
}

func TestResolveCommandEmptyPath(t *testing.T) {
	mod := &Module{
		Name: "db",
		Commands: map[string]*Command{
			"status": {Cmd: "echo status"},
		},
	}

	_, err := mod.ResolveCommand([]string{})
	if err == nil {
		t.Fatal("expected error for empty command path")
	}
}
