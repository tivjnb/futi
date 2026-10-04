package executor

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// cancelWaitDelay — сколько ждать завершения ребёнка после SIGTERM при отмене
// контекста; затем Go посылает SIGKILL.
const cancelWaitDelay = 5 * time.Second

// Config controls how a script is executed.
type Config struct {
	Dir string
	Env []string
	// Stdin — stdin процесса; nil = наследуется os.Stdin futi.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// NewConfig returns an empty execution config.
func NewConfig() *Config {
	return &Config{}
}

// WithDir adds Dir to cfg.
func (cfg *Config) WithDir(dir string) *Config {
	cfg.Dir = dir
	return cfg
}

// WithEnv adds Env to cfg.
func (cfg *Config) WithEnv(env []string) *Config {
	cfg.Env = env
	return cfg
}

// WithStdout adds Stdout to cfg.
func (cfg *Config) WithStdout(stdout io.Writer) *Config {
	cfg.Stdout = stdout
	return cfg
}

// WithStdin adds Stdin to cfg.
func (cfg *Config) WithStdin(stdin io.Reader) *Config {
	cfg.Stdin = stdin
	return cfg
}

// WithStderr adds Stderr to cfg.
func (cfg *Config) WithStderr(stderr io.Writer) *Config {
	cfg.Stderr = stderr
	return cfg
}

// Run executes a script at scriptPath with the provided arguments.
func Run(ctx context.Context, scriptPath string, args ...string) error {
	return RunWithConfig(ctx, &Config{}, scriptPath, args...)
}

// RunWithConfig executes a script at scriptPath with the provided arguments and execution settings.
//
// nil-поля cfg (и nil cfg) означают наследование os.Stdin/os.Stdout/os.Stderr.
// Пока ребёнок работает, SIGINT и SIGQUIT перехватываются без действия (терминал
// сам доставляет их всей foreground-группе, включая ребёнка), а SIGTERM и SIGHUP
// пересылаются ребёнку; результат ребёнка возвращается после его завершения.
// Отмена ctx посылает ребёнку SIGTERM, через cancelWaitDelay — SIGKILL.
// Ошибка ненулевого/сигнального завершения оборачивает *exec.ExitError.
func RunWithConfig(ctx context.Context, cfg *Config, executable string, args ...string) error {
	if ctx == nil {
		return fmt.Errorf("executor: context is required")
	}

	executablePath, err := exec.LookPath(executable)
	if err != nil {
		return fmt.Errorf("failed to resolve path %q: %w", executable, err)
	}

	command := exec.CommandContext(ctx, executablePath, args...)
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	command.WaitDelay = cancelWaitDelay
	command.Stdin = os.Stdin
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

	// Подписка до Start: сигнал, пришедший сразу после старта, не потеряется.
	// signal.Notify (а не Ignore): игнорирование наследуется через exec, и
	// ребёнок перестал бы реагировать на Ctrl+C.
	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	if err := command.Start(); err != nil {
		return fmt.Errorf("executor: run %q: %w", executable, err)
	}

	stopForward := make(chan struct{})
	forwardDone := make(chan struct{})
	go func() {
		defer close(forwardDone)
		for {
			select {
			case sig := <-sigCh:
				// SIGINT/SIGQUIT терминал доставил ребёнку сам — не дублируем.
				if sig == syscall.SIGTERM || sig == syscall.SIGHUP {
					_ = command.Process.Signal(sig)
				}
			case <-stopForward:
				return
			}
		}
	}()

	waitErr := command.Wait()
	close(stopForward)
	<-forwardDone

	if waitErr != nil {
		return fmt.Errorf("executor: run %q: %w", executable, waitErr)
	}
	return nil
}
