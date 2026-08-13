package executor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// Config controls how a script is executed.
type Config struct {
	Dir    string
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// NewConfig returns an empty execution config.
func NewConfig() *Config {
	return &Config{}
}

// WithDir add Dir to cfg.
func (cfg *Config) WithDir(dir string) *Config {
	cfg.Dir = dir
	return cfg
}

// WithEnv add Env to cfg.
func (cfg *Config) WithEnv(env []string) *Config {
	cfg.Env = env
	return cfg
}

// WithStdout add Stdout to cfg.
func (cfg *Config) WithStdout(stdout io.Writer) *Config {
	cfg.Stdout = stdout
	return cfg
}

// WithStdin add Stdin to cfg.
func (cfg *Config) WithStdin(stdin io.Reader) *Config {
	cfg.Stdin = stdin
	return cfg
}

// WithStderr add Stderr to cfg.
func (cfg *Config) WithStderr(stderr io.Writer) *Config {
	cfg.Stderr = stderr
	return cfg
}

// Run executes a script at scriptPath the provided arguments.
func Run(ctx context.Context, scriptPath string, args ...string) error {
	return RunWithConfig(ctx, &Config{}, scriptPath, args...)
}

// RunWithConfig executes a script at scriptPath the provided arguments and execution settings.
func RunWithConfig(ctx context.Context, cfg *Config, executable string, args ...string) error {
	if ctx == nil {
		return fmt.Errorf("executor: context is required")
	}

	executablePath, err := exec.LookPath(executable)
	if err != nil {
		return fmt.Errorf("failed to resolve path %q: %w", executable, err)
	}

	command := exec.CommandContext(ctx, executablePath, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if cfg != nil {
		if cfg.Dir != "" {
			command.Dir = cfg.Dir
		}
		if len(cfg.Env) > 0 {
			command.Env = cfg.Env
		}
		if cfg.Stdout != nil {
			command.Stdout = cfg.Stdout
		}
		if cfg.Stderr != nil {
			command.Stderr = cfg.Stderr
		}
		if cfg.Stdin != nil {
			command.Stdin = cfg.Stdin
		}
	}

	if err := command.Run(); err != nil {
		return fmt.Errorf("executor: run %q: %w", executable, err)
	}
	return nil
}
