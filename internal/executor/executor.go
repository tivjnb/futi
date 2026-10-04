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
// контекста; затем Go посылает SIGKILL. Переменная — для тестов.
var cancelWaitDelay = 5 * time.Second

// foregroundTTY сообщает, находится ли futi в foreground-группе управляющего
// терминала: тогда Ctrl+C и Ctrl+\ терминал доставляет всей группе, включая
// ребёнка. Переменная — тесты задают состояние явно (go test, запущенный из
// терминала, сам находится в foreground-группе).
var foregroundTTY = isForegroundTTY

// InterruptedError — futi во время работы команды получил прерывающий сигнал
// и переслал его команде (либо сигнал пришёл сразу после её завершения).
// Err — ошибка ожидания команды, nil при выходе с 0; *exec.ExitError из неё
// достаётся через errors.As.
type InterruptedError struct {
	Signal syscall.Signal
	Err    error
}

func (e *InterruptedError) Error() string {
	msg := "interrupted by " + signalName(e.Signal)
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *InterruptedError) Unwrap() error { return e.Err }

// signalName возвращает имя перехватываемого сигнала (SIGTERM), для прочих —
// описание из syscall.
func signalName(sig syscall.Signal) string {
	switch sig {
	case syscall.SIGINT:
		return "SIGINT"
	case syscall.SIGQUIT:
		return "SIGQUIT"
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGHUP:
		return "SIGHUP"
	}
	return sig.String()
}

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
// Пока ребёнок работает, futi перехватывает SIGINT, SIGQUIT, SIGTERM и SIGHUP
// (кроме проигнорированных при старте) и ждёт завершения ребёнка:
//   - SIGTERM и SIGHUP пересылаются ребёнку;
//   - SIGINT (Ctrl+C) и SIGQUIT (Ctrl+\) в foreground-группе терминала не
//     пересылаются — терминал сам доставил их всей группе, включая ребёнка;
//     без терминала (CI, фон, перенаправленный ввод/вывод) они адресные
//     (kill -INT) и пересылаются. Терминал проверяется в момент сигнала.
//
// Если сигнал переслан (или пришёл между завершением ребёнка и снятием
// подписки), возвращается ошибка с *InterruptedError, даже если ребёнок
// вышел с 0. Отмена ctx посылает ребёнку SIGTERM, через cancelWaitDelay —
// SIGKILL. Ошибка ненулевого/сигнального завершения оборачивает *exec.ExitError.
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
	// ребёнок перестал бы реагировать на Ctrl+C. Сигналы, проигнорированные
	// при старте futi (nohup, фоновая команда шелла), не перехватываем: Notify
	// снял бы SIG_IGN, а при exec обработчик сброшен в SIG_DFL — ребёнок потерял
	// бы игнорирование.
	// Наследование игнорирования работает для SIGINT и SIGHUP; для SIGQUIT и
	// SIGTERM рантайм Go ставит свой обработчик, и Ignored для них не срабатывает.
	sigCh := make(chan os.Signal, 4)
	var watched []os.Signal
	for _, sig := range []os.Signal{syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP} {
		if !signal.Ignored(sig) {
			watched = append(watched, sig)
		}
	}
	if len(watched) > 0 { // Notify без сигналов подписал бы на все
		signal.Notify(sigCh, watched...)
		defer signal.Stop(sigCh)
	}

	if err := command.Start(); err != nil {
		return fmt.Errorf("executor: run %q: %w", executable, err)
	}

	// signalChild — единственное место доставки пересылаемого сигнала.
	signalChild := func(sig syscall.Signal) { _ = command.Process.Signal(sig) }
	// interruptedBy — первый пересланный сигнал; 0 — пересылки не было.
	// Пишет только горутина пересылки; читается после forwardDone.
	var interruptedBy syscall.Signal
	stopForward := make(chan struct{})
	forwardDone := make(chan struct{})
	go func() {
		defer close(forwardDone)
		for {
			select {
			case s := <-sigCh:
				sig, ok := s.(syscall.Signal)
				if !ok || !forwardsSignal(sig) {
					continue
				}
				signalChild(sig)
				if interruptedBy == 0 {
					interruptedBy = sig
				}
			case <-stopForward:
				return
			}
		}
	}()

	waitErr := command.Wait()
	close(stopForward)
	<-forwardDone
	// Сигнал, пришедший между Wait и снятием подписки, ребёнку уже не нужен,
	// но прерывание futi терять нельзя.
	signal.Stop(sigCh)
	if sig, ok := drainSignals(sigCh, forwardsSignal); ok && interruptedBy == 0 {
		interruptedBy = sig
	}

	runErr := waitErr
	if interruptedBy != 0 {
		runErr = &InterruptedError{Signal: interruptedBy, Err: waitErr}
	}
	if runErr != nil {
		return fmt.Errorf("executor: run %q: %w", executable, runErr)
	}
	return nil
}

// forwardsSignal решает, пересылать ли ребёнку сигнал, полученный futi.
// SIGTERM и SIGHUP терминал ребёнку не доставляет — пересылаются всегда.
// SIGINT и SIGQUIT в foreground-группе терминала ребёнок уже получил сам;
// без терминала они адресные — пересылаются. Терминал проверяется в момент
// сигнала: futi могли перевести в фон или на передний план (bg/fg).
func forwardsSignal(sig syscall.Signal) bool {
	switch sig {
	case syscall.SIGTERM, syscall.SIGHUP:
		return true
	case syscall.SIGINT, syscall.SIGQUIT:
		return !foregroundTTY()
	}
	return false
}

// drainSignals неблокирующе вычитывает ch до конца и возвращает первый
// сигнал, для которого forwards истинна.
func drainSignals(ch <-chan os.Signal, forwards func(syscall.Signal) bool) (syscall.Signal, bool) {
	var found syscall.Signal
	for {
		select {
		case s := <-ch:
			if sig, ok := s.(syscall.Signal); ok && found == 0 && forwards(sig) {
				found = sig
			}
		default:
			return found, found != 0
		}
	}
}
