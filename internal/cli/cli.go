package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"strings"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/executor"
)

// Main runs futi with the given argv (args[0] is the program name) and streams
// and returns the process exit code.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	// --dry-run снимается первым, до всех быстрых путей.
	var dryRun bool
	args, dryRun = stripDryRun(args)
	if dryRun && dryRunRejected(args) {
		fmt.Fprintln(stderr, errDryRunScope)
		return 1
	}

	if len(args) < 2 {
		printUsage(stderr)
		return 1
	}

	// Быстрый путь: -h/--help не требует config/source/cache.
	if args[1] == "-h" || args[1] == "--help" {
		printUsage(stdout)
		return 0
	}

	// Быстрый путь: --version не требует config/source/cache.
	if args[1] == "--version" {
		if len(args) > 2 {
			fmt.Fprintln(stderr, "futi: --version takes no arguments")
			return 1
		}
		info, ok := debug.ReadBuildInfo()
		fmt.Fprintln(stdout, versionString(version, info, ok))
		return 0
	}

	// Быстрый путь: futi modules init не требует config/source/cache.
	if args[1] == "modules" && len(args) > 2 && args[2] == "init" {
		if err := runModulesInit(stdout, stderr, args[3:]); err != nil {
			fmt.Fprintf(stderr, "modules init: %v\n", err)
			return 1
		}
		return 0
	}

	// Быстрый путь: futi completion <shell> — только генерация скрипта,
	// без config/source/cache.
	if args[1] == "completion" {
		if len(args) < 3 {
			fmt.Fprintf(stderr, "futi: completion requires a shell (valid: %s)\n", strings.Join(validCompletionShells, ", "))
			return 1
		}
		if err := runCompletion(stdout, args[2]); err != nil {
			fmt.Fprintf(stderr, "completion: %v\n", err)
			return 1
		}
		return 0
	}

	// Runtime: config и глобальный source строятся сразу (ошибка конфига — до любых
	// команд), кэш и локальный source — лениво, при первом обращении.
	opts, err := core.DefaultOptions()
	if err != nil {
		fmt.Fprintf(stderr, "source: %v\n", err)
		return 1
	}
	opts.Warn = func(msg string) { fmt.Fprintln(stderr, msg) }
	rt, err := core.NewRuntime(opts)
	if err != nil {
		fmt.Fprintf(stderr, "source: %v\n", err)
		return 1
	}

	// Быстрый путь: futi __complete <args...> — hidden-обработчик completion.
	// ДО cache.Start: __complete сам решает, нужно ли прогревать кэш
	// (самообновление при холодном/устаревшем), и не запускает Refresh по политике
	// на каждый TAB.
	if args[1] == "__complete" {
		return runCompleteMain(stdout, stderr, rt, args[2:])
	}

	// Быстрый путь: `futi modules <subcommand>` — до cache.Start, чтобы не
	// гонять Refresh по политике: мутирующие подкоманды сами инвалидируют
	// кэш, read-only обходятся загрузкой по требованию.
	if args[1] == "modules" {
		// Cache() prints cache warnings (Warn) before any subcommand output,
		// read-only subcommands included.
		_ = rt.Cache()
		if _, err := rt.Source(); err != nil {
			fmt.Fprintf(stderr, "source: %v\n", err)
			return 1
		}
		if err := runModules(stdout, stderr, stdin, rt, args[2:]); err != nil {
			fmt.Fprintf(stderr, "modules: %v\n", err)
			// Прерванный хук — futi гибнет тем же сигналом; код хука, git
			// или curl наружу не пробрасывается: прочие ошибки дают 1.
			if _, ok := executor.InterruptSignal(err); ok {
				raiseInterrupt(err)
				return core.ExitCode(err)
			}
			return 1
		}
		return 0
	}

	// `futi cache <subcommand>` — fast path ДО cache.Start, чтобы избежать
	// двойного Refresh (Start с AlwaysPolicy + runCacheRefresh). Loading from disk
	// (Load) happens inside core.RefreshCache / core.CacheStatusOf; runCacheRefresh
	// is then the only Refresh, with a correct mtime+size comparison.
	if subcmd, ok := cacheSubcommand(args[1:]); ok {
		cache := rt.Cache()
		cachePath := rt.CachePath()
		switch subcmd {
		case "refresh":
			// Парсим --verbose/-v из оставшихся аргументов.
			verbose := false
			for _, a := range args[3:] {
				if a == "-v" || a == "--verbose" {
					verbose = true
				}
			}
			if err := runCacheRefresh(stdout, cache, rt.GlobalSource(), verbose); err != nil && !printPartialWarnings(stderr, err) {
				fmt.Fprintf(stderr, "%v\n", err)
				return 1
			}
			return 0
		case "status":
			if err := runCacheStatus(stdout, cache, cachePath); err != nil {
				fmt.Fprintf(stderr, "%v\n", err)
				return 1
			}
			return 0
		case "clear":
			if err := runCacheClear(stdout, cachePath); err != nil {
				fmt.Fprintf(stderr, "%v\n", err)
				return 1
			}
			return 0
		case "":
			fmt.Fprintln(stderr, `futi: cache requires a subcommand (try "cache refresh", "cache status", or "cache clear")`)
			return 1
		default:
			fmt.Fprintf(stderr, "futi: unknown cache subcommand %q (try \"cache refresh\", \"cache status\", or \"cache clear\")\n", subcmd)
			return 1
		}
	}

	// Создаём и запускаем кэш. Ошибки кэша не фатальны — CLI работает и без него.
	cache := rt.Cache()
	if err := rt.Start(); err != nil && !printPartialWarnings(stderr, err) {
		fmt.Fprintf(stderr, "cache: %v (continuing without cache)\n", err)
	}

	pa, err := parseArgs(args[1:])
	if err != nil {
		var listReq *listRequest
		var helpReq *helpRequest
		switch {
		case dryRun && (errors.As(err, &listReq) || errors.Is(err, errHelp)):
			fmt.Fprintln(stderr, errDryRunScope)
			return 1
		case errors.As(err, &listReq):
			source, err := rt.Source()
			if err != nil {
				fmt.Fprintf(stderr, "source: %v\n", err)
				return 1
			}
			if listErr := runList(stdout, source, listReq.namespace, listReq.verbose, listReq.recurse, cache); listErr != nil && !printPartialWarnings(stderr, listErr) {
				fmt.Fprintln(stderr, listErrorText(listErr, listReq.namespace))
				return 1
			}
			return 0
		case errors.As(err, &helpReq):
			if _, err := rt.Source(); err != nil {
				fmt.Fprintf(stderr, "source: %v\n", err)
				return 1
			}
			helpReq.dryRun = dryRun
			code, runErr := runHelp(context.Background(), stdin, stdout, stderr, rt, helpReq)
			if runErr != nil {
				raiseInterrupt(runErr)
				return core.ExitCode(runErr)
			}
			return code
		case errors.Is(err, errHelp):
			printUsage(stdout)
			return 0
		default:
			fmt.Fprintf(stderr, "parse: %v\n", err)
			return 1
		}
	}

	if _, err := rt.Source(); err != nil {
		fmt.Fprintf(stderr, "source: %v\n", err)
		return 1
	}
	pa.dryRun = dryRun
	code, runErr := runParsed(context.Background(), stdin, stdout, stderr, rt, pa)
	if runErr != nil {
		raiseInterrupt(runErr)
		return core.ExitCode(runErr)
	}
	return code
}
