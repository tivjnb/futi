package executor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"futi/internal/testutil"
)

func TestRunWithConfigExecutesScriptWithArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts are not supported in this test on Windows")
	}

	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "script.sh")

	script := []byte("#!/bin/sh\nprintf '%s %s\\n' \"$1\" \"$2\"\n")
	if err := os.WriteFile(scriptPath, script, 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	var stdout bytes.Buffer
	if err := RunWithConfig(context.Background(), &Config{Stdout: &stdout}, scriptPath, "alpha", "beta"); err != nil {
		t.Fatalf("run script: %v", err)
	}

	if got := stdout.String(); got != "alpha beta\n" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestConfigBuilders(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cfg := NewConfig().
		WithDir("/tmp").
		WithEnv([]string{"A=B"}).
		WithStdout(&stdout).
		WithStderr(&stderr)

	if cfg.Dir != "/tmp" {
		t.Fatalf("unexpected dir: %q", cfg.Dir)
	}
	if len(cfg.Env) != 1 || cfg.Env[0] != "A=B" {
		t.Fatalf("unexpected env: %#v", cfg.Env)
	}
	if cfg.Stdout != &stdout {
		t.Fatal("unexpected stdout writer")
	}
	if cfg.Stderr != &stderr {
		t.Fatal("unexpected stderr writer")
	}
}

func TestRunWithConfigRejectsNilContext(t *testing.T) {
	if err := RunWithConfig(context.TODO(), &Config{}, "script.sh"); err == nil {
		t.Fatal("expected an error for nil context")
	}
}

func TestRunWithConfigInheritsStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cat недоступен на Windows")
	}
	testutil.ReplaceStdin(t, "hello")

	var buf bytes.Buffer
	if err := RunWithConfig(context.Background(), &Config{Stdout: &buf}, "cat"); err != nil {
		t.Fatalf("RunWithConfig: %v", err)
	}
	if got := buf.String(); got != "hello" {
		t.Fatalf("stdout = %q, want %q", got, "hello")
	}
}

func TestRunWithConfigExplicitStdinWins(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cat недоступен на Windows")
	}
	testutil.ReplaceStdin(t, "hello")

	var buf bytes.Buffer
	cfg := &Config{Stdin: strings.NewReader("x"), Stdout: &buf}
	if err := RunWithConfig(context.Background(), cfg, "cat"); err != nil {
		t.Fatalf("RunWithConfig: %v", err)
	}
	if got := buf.String(); got != "x" {
		t.Fatalf("stdout = %q, want %q", got, "x")
	}
}

// signalChildEnv возвращает окружение с путями маркеров MARK и READY.
func signalChildEnv(t *testing.T) (env []string, mark, ready string) {
	t.Helper()
	dir := t.TempDir()
	mark = filepath.Join(dir, "mark")
	ready = filepath.Join(dir, "ready")
	env = append(os.Environ(), "MARK="+mark, "READY="+ready)
	return env, mark, ready
}

// setForegroundTTY задаёт ответ foregroundTTY до конца теста: go test,
// запущенный из терминала, сам находится в foreground-группе.
func setForegroundTTY(t *testing.T, foreground bool) {
	t.Helper()
	orig := foregroundTTY
	foregroundTTY = func() bool { return foreground }
	t.Cleanup(func() { foregroundTTY = orig })
}

// setControllingTTY задаёт ответ controllingTTY до конца теста: изоляция
// команды не должна зависеть от того, запущен ли go test из терминала.
func setControllingTTY(t *testing.T, controlling bool) {
	t.Helper()
	orig := controllingTTY
	controllingTTY = func() bool { return controlling }
	t.Cleanup(func() { controllingTTY = orig })
}

// runUntilSignalled запускает script с cfg, ждёт READY, шлёт sig тестовому
// процессу и возвращает ошибку RunWithConfig.
func runUntilSignalled(t *testing.T, cfg *Config, ready, script string, sig syscall.Signal) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- RunWithConfig(context.Background(), cfg, "sh", "-c", script)
	}()
	testutil.WaitForFile(t, ready, 5*time.Second)
	if err := syscall.Kill(os.Getpid(), sig); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("RunWithConfig не завершился")
		return nil
	}
}

// assertMark проверяет, что ребёнок записал want в MARK.
func assertMark(t *testing.T, mark, want string) {
	t.Helper()
	data, err := os.ReadFile(mark)
	if err != nil {
		t.Fatalf("ребёнок не получил сигнал: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != want {
		t.Fatalf("mark = %q, want %q", got, want)
	}
}

// assertInterrupted проверяет, что err — *InterruptedError с сигналом want
// и без ошибки Wait (ребёнок вышел с 0).
func assertInterrupted(t *testing.T, err error, want syscall.Signal) {
	t.Helper()
	var intErr *InterruptedError
	if !errors.As(err, &intErr) {
		t.Fatalf("err = %v, want *InterruptedError", err)
	}
	if intErr.Signal != want || intErr.Err != nil {
		t.Fatalf("InterruptedError{Signal: %v, Err: %v}, want {Signal: %v, Err: nil}", intErr.Signal, intErr.Err, want)
	}
}

func TestRunWithConfigForwardsSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	env, mark, ready := signalChildEnv(t)
	script := `trap 'echo term > "$MARK"; exit 0' TERM; echo ready > "$READY"; while :; do sleep 0.05; done`

	err := runUntilSignalled(t, &Config{Env: env}, ready, script, syscall.SIGTERM)
	assertMark(t, mark, "term")
	assertInterrupted(t, err, syscall.SIGTERM)
}

func TestRunWithConfigForwardsSIGHUP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	env, mark, ready := signalChildEnv(t)
	script := `trap 'echo hup > "$MARK"; exit 0' HUP; echo ready > "$READY"; while :; do sleep 0.05; done`

	err := runUntilSignalled(t, &Config{Env: env}, ready, script, syscall.SIGHUP)
	assertMark(t, mark, "hup")
	assertInterrupted(t, err, syscall.SIGHUP)
}

// Без терминала SIGINT, присланный futi (kill -INT, IDE, CI), адресный —
// терминал его ребёнку не доставит, futi пересылает сам.
func TestRunWithConfigForwardsSIGINTWithoutTTY(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	setForegroundTTY(t, false)
	env, mark, ready := signalChildEnv(t)
	script := `trap 'echo int > "$MARK"; exit 0' INT; echo ready > "$READY"; while :; do sleep 0.05; done`

	err := runUntilSignalled(t, &Config{Env: env}, ready, script, syscall.SIGINT)
	assertMark(t, mark, "int")
	assertInterrupted(t, err, syscall.SIGINT)
}

func TestRunWithConfigDoesNotForwardSIGINT(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	setForegroundTTY(t, true)
	env, mark, ready := signalChildEnv(t)
	// SIGINT шлём только тестовому процессу (не группе): в foreground-режиме
	// его доставляет терминал, futi не пересылает. Ребёнок живёт, пока тест
	// не создаст SENT (сигнал получен процессом); дедлайн ~10 с.
	script := `trap 'echo int > "$MARK"' INT; echo ready > "$READY"; i=0; while [ ! -e "$SENT" ] && [ $i -lt 200 ]; do sleep 0.05; i=$((i+1)); done; exit 0`
	sent := filepath.Join(filepath.Dir(ready), "sent")
	env = append(env, "SENT="+sent)

	// Своя подписка теста: SENT создаётся только после того, как рантайм
	// доставил SIGINT подписчикам, а не после угаданной паузы.
	gotInt := make(chan os.Signal, 1)
	signal.Notify(gotInt, syscall.SIGINT)
	t.Cleanup(func() { signal.Stop(gotInt) })

	done := make(chan error, 1)
	go func() {
		done <- RunWithConfig(context.Background(), &Config{Env: env}, "sh", "-c", script)
	}()
	testutil.WaitForFile(t, ready, 5*time.Second)
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gotInt:
	case <-time.After(5 * time.Second):
		t.Fatal("SIGINT не доставлен тестовому процессу")
	}
	if err := os.WriteFile(sent, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunWithConfig: %v, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunWithConfig не завершился")
	}
	if _, err := os.Stat(mark); err == nil {
		t.Fatal("SIGINT переслан ребёнку, а не должен")
	}
}

func TestRunWithConfigCancelSendsSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	env, mark, ready := signalChildEnv(t)
	script := `trap 'echo term > "$MARK"; exit 0' TERM; echo ready > "$READY"; while :; do sleep 0.05; done`

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunWithConfig(ctx, &Config{Env: env}, "sh", "-c", script)
	}()
	testutil.WaitForFile(t, ready, 5*time.Second)
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("RunWithConfig не завершился")
	}
	data, err := os.ReadFile(mark)
	if err != nil {
		t.Fatalf("ребёнок не получил SIGTERM при отмене: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "term" {
		t.Fatalf("mark = %q, want %q", got, "term")
	}
}

// Ребёнок, игнорирующий SIGTERM при отмене ctx, добивается SIGKILL через
// cancelWaitDelay.
func TestRunWithConfigCancelKillsChildIgnoringSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	origDelay := cancelWaitDelay
	cancelWaitDelay = 200 * time.Millisecond
	t.Cleanup(func() { cancelWaitDelay = origDelay })
	env, _, ready := signalChildEnv(t)
	script := `trap '' TERM; echo ready > "$READY"; while :; do sleep 0.05; done`

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunWithConfig(ctx, &Config{Env: env}, "sh", "-c", script)
	}()
	testutil.WaitForFile(t, ready, 5*time.Second)
	start := time.Now()
	cancel()
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("RunWithConfig не завершился")
	}
	if elapsed := time.Since(start); elapsed >= 3*time.Second {
		t.Fatalf("ребёнок убит через %v, want < 3s", elapsed)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %v, want *exec.ExitError", err)
	}
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("ребёнок завершён не SIGKILL: %v", err)
	}
}

// isolatedGrandchildEnv готовит составную команду (как хук через sh -c):
// внук в фоне ловит SIGTERM и пишет gc в MARK, внешний sh ждёт его. Внук
// живёт ограниченное время, чтобы не остаться навсегда при провале теста;
// stderr внука заглушён — sh сообщил бы «Terminated» о своём sleep, убитом
// групповым SIGTERM.
func isolatedGrandchildEnv(t *testing.T) (env []string, mark, ready, script string) {
	t.Helper()
	env, mark, ready = signalChildEnv(t)
	inner := `exec 2>/dev/null; trap 'echo gc > "$MARK"; exit 0' TERM; echo ready > "$READY"; i=0; while [ $i -lt 200 ]; do sleep 0.05; i=$((i+1)); done`
	return append(env, "INNER="+inner), mark, ready, `sh -c "$INNER" & wait`
}

// Без управляющего терминала изолированная команда — своя группа: адресный SIGTERM futi
// получает и внук составной команды, а не только её sh.
func TestRunWithConfigIsolatedGroupGetsSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	setControllingTTY(t, false)
	env, mark, ready, script := isolatedGrandchildEnv(t)

	err := runUntilSignalled(t, &Config{Env: env, IsolateWithoutTTY: true}, ready, script, syscall.SIGTERM)
	testutil.WaitForFile(t, mark, 5*time.Second)
	assertMark(t, mark, "gc")
	var intErr *InterruptedError
	if !errors.As(err, &intErr) || intErr.Signal != syscall.SIGTERM {
		t.Fatalf("err = %v, want *InterruptedError{Signal: SIGTERM}", err)
	}
}

// Отмена ctx изолированной команды шлёт SIGTERM всей группе.
func TestRunWithConfigIsolatedCancelSignalsGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	setControllingTTY(t, false)
	env, mark, ready, script := isolatedGrandchildEnv(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunWithConfig(ctx, &Config{Env: env, IsolateWithoutTTY: true}, "sh", "-c", script)
	}()
	testutil.WaitForFile(t, ready, 5*time.Second)
	cancel()
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("RunWithConfig не завершился")
	}
	testutil.WaitForFile(t, mark, 5*time.Second)
	assertMark(t, mark, "gc")
	// Отмена — не пересылка сигнала futi: лидер убит SIGTERM, но это не
	// прерывание futi.
	var intErr *InterruptedError
	if err == nil || errors.As(err, &intErr) {
		t.Fatalf("err = %v, want ошибку отмены без *InterruptedError", err)
	}
}

// При обычном завершении изолированной команды группу не трогают: демон,
// запущенный хуком в фоне, продолжает работать.
func TestRunWithConfigIsolatedKeepsDaemonOnNormalExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	setControllingTTY(t, false)
	pidFile := filepath.Join(t.TempDir(), "pid")
	script := `sleep 30 </dev/null >/dev/null 2>&1 & echo $! > "$PIDFILE"`
	cfg := &Config{Env: append(os.Environ(), "PIDFILE="+pidFile), IsolateWithoutTTY: true}
	if err := RunWithConfig(context.Background(), cfg, "sh", "-c", script); err != nil {
		t.Fatalf("RunWithConfig: %v", err)
	}
	pid := readPid(t, pidFile)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("демон %d убит после обычного выхода команды: %v", pid, err)
	}
}

// readPid читает pid из файла, записанного ребёнком.
func readPid(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("pid в %s: %v", path, err)
	}
	return pid
}

// waitProcessGone ждёт исчезновения процесса pid (опрос с дедлайном): осиротевший
// внук после гибели ещё мгновение висит зомби до reap новым родителем.
func waitProcessGone(t *testing.T, pid int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("процесс %d жив через %v", pid, within)
}

// Внук изолированной команды, игнорирующий SIGTERM, при отмене ctx через
// cancelWaitDelay добивается SIGKILL группе — даже если лидер (sh) уже умер
// от SIGTERM и Wait вернулся.
func TestRunWithConfigIsolatedCancelKillsGroupIgnoringSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	setControllingTTY(t, false)
	origDelay := cancelWaitDelay
	cancelWaitDelay = 200 * time.Millisecond
	t.Cleanup(func() { cancelWaitDelay = origDelay })
	env, _, ready := signalChildEnv(t)
	inner := `exec 2>/dev/null; trap '' TERM; echo $$ > "$READY.tmp"; mv "$READY.tmp" "$READY"; i=0; while [ $i -lt 400 ]; do sleep 0.05; i=$((i+1)); done`
	env = append(env, "INNER="+inner)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- RunWithConfig(ctx, &Config{Env: env, IsolateWithoutTTY: true}, "sh", "-c", `sh -c "$INNER" & wait`)
	}()
	testutil.WaitForFile(t, ready, 5*time.Second)
	pid := readPid(t, ready)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("RunWithConfig не завершился")
	}
	waitProcessGone(t, pid, 3*time.Second)
}

// Своей группе команды терминал ничего не доставляет: SIGINT пересылается
// ей независимо от ответа foregroundTTY.
func TestRunWithConfigIsolatedForwardsSIGINT(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("сигналы POSIX")
	}
	setControllingTTY(t, false)
	setForegroundTTY(t, true)
	env, mark, ready := signalChildEnv(t)
	script := `trap 'echo int > "$MARK"; exit 0' INT; echo ready > "$READY"; i=0; while [ $i -lt 200 ]; do sleep 0.05; i=$((i+1)); done`

	done := make(chan error, 1)
	go func() {
		done <- RunWithConfig(context.Background(), &Config{Env: env, IsolateWithoutTTY: true}, "sh", "-c", script)
	}()
	testutil.WaitForFile(t, ready, 5*time.Second)
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("RunWithConfig не завершился")
	}
	assertMark(t, mark, "int")
	assertInterrupted(t, err, syscall.SIGINT)
}

// childGroup запускает sh с cfg и возвращает pid ребёнка и его группу
// процессов (поле pgrp из /proc/<pid>/stat).
func childGroup(t *testing.T, cfg *Config) (pid, pgrp int) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("нужен /proc")
	}
	var buf bytes.Buffer
	cfg.Stdout = &buf
	if err := RunWithConfig(context.Background(), cfg, "sh", "-c", `echo $$ $(cut -d" " -f5 /proc/$$/stat)`); err != nil {
		t.Fatalf("RunWithConfig: %v", err)
	}
	fields := strings.Fields(buf.String())
	if len(fields) != 2 {
		t.Fatalf("неожиданный вывод: %q", buf.String())
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatal(err)
	}
	if pgrp, err = strconv.Atoi(fields[1]); err != nil {
		t.Fatal(err)
	}
	return pid, pgrp
}

// С управляющим терминалом изоляция не включается, даже если futi не на
// переднем плане (futi … &, перенаправленный ввод): ребёнок остаётся в
// группе futi. Иначе фоновый хук, читающий tty, остановился бы SIGTTIN в
// своей группе, а fg разбудил бы только futi — оба зависли бы.
func TestRunWithConfigNotIsolatedWithTTY(t *testing.T) {
	setControllingTTY(t, true)
	setForegroundTTY(t, false)
	_, pgrp := childGroup(t, &Config{IsolateWithoutTTY: true})
	if want := syscall.Getpgrp(); pgrp != want {
		t.Fatalf("pgrp ребёнка = %d, want %d (группа теста)", pgrp, want)
	}
}

func TestRunWithConfigIsolatedOwnGroup(t *testing.T) {
	setControllingTTY(t, false)
	pid, pgrp := childGroup(t, &Config{IsolateWithoutTTY: true})
	if pgrp != pid {
		t.Fatalf("pgrp ребёнка = %d, want %d (своя группа; группа теста %d)", pgrp, pid, syscall.Getpgrp())
	}
}

// Сигнал, пришедший между Wait и signal.Stop, остаётся в канале подписки и
// должен распознаваться как прерывание, а не теряться.
func TestRunWithConfigSignalAfterWaitNotLost(t *testing.T) {
	forwardsAll := func(syscall.Signal) bool { return true }
	forwardsNone := func(syscall.Signal) bool { return false }
	cases := []struct {
		name     string
		queued   []os.Signal
		forwards func(syscall.Signal) bool
		want     syscall.Signal
		wantHit  bool
	}{
		{"SIGTERM в канале", []os.Signal{syscall.SIGTERM}, forwardsAll, syscall.SIGTERM, true},
		{"первый пересылаемый", []os.Signal{syscall.SIGINT, syscall.SIGHUP}, func(s syscall.Signal) bool { return s == syscall.SIGHUP }, syscall.SIGHUP, true},
		{"непересылаемый", []os.Signal{syscall.SIGINT}, forwardsNone, 0, false},
		{"пустой канал", nil, forwardsAll, 0, false},
		{"SIGQUIT уступает SIGTERM", []os.Signal{syscall.SIGQUIT, syscall.SIGTERM}, forwardsAll, syscall.SIGTERM, true},
		{"SIGTERM не уступает SIGQUIT", []os.Signal{syscall.SIGTERM, syscall.SIGQUIT}, forwardsAll, syscall.SIGTERM, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan os.Signal, 4)
			for _, sig := range tc.queued {
				ch <- sig
			}
			got, ok := drainSignals(ch, tc.forwards)
			if ok != tc.wantHit || got != tc.want {
				t.Fatalf("drainSignals(%v) = (%v, %v), want (%v, %v)", tc.queued, got, ok, tc.want, tc.wantHit)
			}
			if len(ch) != 0 {
				t.Fatalf("в канале осталось %d сигналов, want 0", len(ch))
			}
		})
	}
}

// Прерывающий сигнал (SIGINT, SIGTERM, SIGHUP) вытесняет запомненный
// SIGQUIT: иначе команда, перехватившая второй сигнал, не прервала бы update.
func TestMergeForwarded(t *testing.T) {
	cases := []struct {
		cur, next, want syscall.Signal
	}{
		{0, syscall.SIGQUIT, syscall.SIGQUIT},
		{0, syscall.SIGTERM, syscall.SIGTERM},
		{syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGTERM},
		{syscall.SIGQUIT, syscall.SIGINT, syscall.SIGINT},
		{syscall.SIGQUIT, syscall.SIGHUP, syscall.SIGHUP},
		{syscall.SIGQUIT, syscall.SIGQUIT, syscall.SIGQUIT},
		{syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGTERM},
		{syscall.SIGTERM, syscall.SIGHUP, syscall.SIGTERM},
		{syscall.SIGINT, syscall.SIGTERM, syscall.SIGINT},
	}
	for _, tc := range cases {
		if got := mergeForwarded(tc.cur, tc.next); got != tc.want {
			t.Errorf("mergeForwarded(%v, %v) = %v, want %v", tc.cur, tc.next, got, tc.want)
		}
	}
}

func TestInterruptedErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		err  *InterruptedError
		want string
	}{
		{"без ошибки Wait", &InterruptedError{Signal: syscall.SIGTERM}, "interrupted by SIGTERM"},
		{"с ошибкой Wait", &InterruptedError{Signal: syscall.SIGQUIT, Err: errors.New("exit status 2")}, "interrupted by SIGQUIT: exit status 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Fatalf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunWithConfigWrapsExitError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh недоступен на Windows")
	}
	err := RunWithConfig(context.Background(), &Config{}, "sh", "-c", "exit 3")
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("err = %v, want *exec.ExitError с кодом 3", err)
	}
}

// ignoredSignalChildEnv включает сценарий TestRunWithConfigKeepsInheritedIgnoredSignal
// в подпроцессе теста.
const ignoredSignalChildEnv = "FUTI_TEST_IGNORED_SIGHUP_CHILD"

// Сигнал, проигнорированный при старте futi (nohup, фоновая команда шелла),
// должен остаться игнорируемым у ребёнка: Notify сбросил бы SIG_IGN при exec.
func TestRunWithConfigKeepsInheritedIgnoredSignal(t *testing.T) {
	if _, err := os.Stat("/proc/self/status"); err != nil {
		t.Skip("нужен /proc")
	}
	if os.Getenv(ignoredSignalChildEnv) != "1" {
		// Сценарий — в перезапущенном тестовом бинаре: signal.Ignored остаётся
		// true и после signal.Reset, и последующие тесты процесса (при
		// -count>1 — ForwardsSIGHUP) перестали бы перехватывать SIGHUP.
		cmd := exec.Command(os.Args[0], "-test.run=^TestRunWithConfigKeepsInheritedIgnoredSignal$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), ignoredSignalChildEnv+"=1")
		out, err := cmd.CombinedOutput()
		if err != nil || !bytes.Contains(out, []byte("--- PASS: TestRunWithConfigKeepsInheritedIgnoredSignal")) {
			t.Fatalf("подпроцесс: %v\n%s", err, out)
		}
		return
	}
	signal.Ignore(syscall.SIGHUP)

	var buf bytes.Buffer
	if err := RunWithConfig(context.Background(), &Config{Stdout: &buf}, "grep", "SigIgn", "/proc/self/status"); err != nil {
		t.Fatalf("RunWithConfig: %v", err)
	}
	fields := strings.Fields(buf.String())
	if len(fields) != 2 {
		t.Fatalf("неожиданный вывод: %q", buf.String())
	}
	mask, err := strconv.ParseUint(fields[1], 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	// SIGHUP = 1 -> бит 0.
	if mask&1 == 0 {
		t.Fatalf("SIGHUP не игнорируется у ребёнка: SigIgn = %s", fields[1])
	}
}
