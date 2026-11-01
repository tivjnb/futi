package execmodule

import (
	"errors"
	"strings"
	"testing"
)

func TestResolveCommandSimple(t *testing.T) {
	mod := &Module{
		Name: "testmod",
		Command: Command{Commands: map[string]*Command{
			"status": {Cmd: cmdText("echo status")},
		}},
	}

	cmd, err := mod.ResolveCommand([]string{"status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Cmd.Text != "echo status" {
		t.Fatalf("unexpected cmd: %+v", cmd.Cmd)
	}
}

func TestResolveCommandWithSubcommand(t *testing.T) {
	mod := &Module{
		Name: "db",
		Command: Command{Commands: map[string]*Command{
			"migrate": {
				Description: "Database migrations",
				Commands: map[string]*Command{
					"up":   {Cmd: cmdText("echo up")},
					"down": {Cmd: cmdText("echo down")},
				},
			},
		}},
	}

	cmd, err := mod.ResolveCommand([]string{"migrate", "up"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Cmd.Text != "echo up" {
		t.Fatalf("unexpected cmd: %+v", cmd.Cmd)
	}
}

func TestResolveCommandNestedSubcommands(t *testing.T) {
	mod := &Module{
		Name: "db",
		Command: Command{Commands: map[string]*Command{
			"migrate": {
				Commands: map[string]*Command{
					"up": {
						Commands: map[string]*Command{
							"force": {Cmd: cmdText("echo force")},
							"safe":  {Cmd: cmdText("echo safe")},
						},
					},
				},
			},
		}},
	}

	cmd, err := mod.ResolveCommand([]string{"migrate", "up", "force"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Cmd.Text != "echo force" {
		t.Fatalf("unexpected cmd: %+v", cmd.Cmd)
	}
}

func TestResolveCommandBranchWithoutLeaf(t *testing.T) {
	mod := &Module{
		Name: "db",
		Command: Command{Commands: map[string]*Command{
			"migrate": {
				Commands: map[string]*Command{
					"up": {Cmd: cmdText("echo up")},
				},
			},
		}},
	}

	_, err := mod.ResolveCommand([]string{"migrate"})
	if err == nil {
		t.Fatal("expected error: 'migrate' has commands but no cmd")
	}
	if !strings.Contains(err.Error(), "migrate") {
		t.Fatalf("error should mention command name, got: %v", err)
	}
	if !errors.Is(err, ErrRequiresSubcommand) {
		t.Fatalf("err = %v, want ErrRequiresSubcommand", err)
	}
}

func TestResolveCommandNotFound(t *testing.T) {
	mod := &Module{
		Name: "db",
		Command: Command{Commands: map[string]*Command{
			"status": {Cmd: cmdText("echo status")},
		}},
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
		Command: Command{Commands: map[string]*Command{
			"migrate": {
				Commands: map[string]*Command{
					"up": {Cmd: cmdText("echo up")},
				},
			},
		}},
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
		Command: Command{Commands: map[string]*Command{
			"status": {Cmd: cmdText("echo status")},
		}},
	}

	_, err := mod.ResolveCommand([]string{"status", "up"})
	if err == nil {
		t.Fatal("expected error: 'status' has no subcommands")
	}
}

func TestResolveCommandEmptyPath(t *testing.T) {
	mod := &Module{
		Name: "db",
		Command: Command{Commands: map[string]*Command{
			"status": {Cmd: cmdText("echo status")},
		}},
	}

	_, err := mod.ResolveCommand([]string{})
	if err == nil {
		t.Fatal("expected error for empty command path")
	}
}
