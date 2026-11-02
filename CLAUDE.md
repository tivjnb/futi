# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**futi** — глобальный менеджер консольных утилит на Go (модуль `github.com/tivjnb/futi`, зависимости: `yaml.v3`, `tablewriter`). Пользователь описывает команды в YAML-модулях и вызывает их единым CLI: `futi docker build --tag x`. Подробная спецификация формата — в `README.md` (на русском).

## Команды

```bash
go build -o futi ./cmd/futi                   # сборка бинаря (в .gitignore как /futi)
go test ./...                                 # все тесты
go test ./internal/execmodule -run TestName   # один тест/пакет
go vet ./...
gofmt -l cmd internal                         # должен быть пуст
GOOS=windows go build -o /dev/null ./cmd/futi # Windows: только сборка (vet падает на Mkfifo в тестах)
```

Хук форматирования переписывает файл целиком — неотформатированный чужой код засоряет diff. В doc-комментариях не пиши `''` подряд — gofmt заменит на `”`; такие последовательности — в блок кода (строка `//<TAB>…`).

`FUTI_MODULES_DIR` переопределяет каталог модулей (по умолчанию `~/.config/futi/modules/`) — используй в тестах и ручных прогонах, чтобы не трогать реальные модули.

## Локальные документы

Исключены через `.git/info/exclude` и не коммитятся: `MD/` (планы: `ROADMAP.md` — фазы, `NEXT_STEPS.md` — ближайшие шаги, `MCP_HTTP_PLAN.md` — ядро/MCP/HTTP, `COMPLETED.md`, `IDEAS.md`), `docs/audits/`, `docs/plans/`, `docs/specs/`, `docs/HANDOFF.md`, `.work/`. Шаги скиллов «закоммить план/спеку» и «закоммить файл статусов аудита» в этом проекте пропускай (файл просто пишется на диск). Из публичных файлов (README, комментарии) на `MD/` не ссылайся.

## Архитектура

`cmd/futi` — только `main.go` и `version`; CLI (argv, диспетчер, help, list/validate/cache, completion-скрипты, разбор флагов `modules`) — в `internal/cli` (`cli.Main`). Остальное — в `internal/`:

- `discovery` — поиск модулей по SaltStack-модели: `dev/deploy.yaml` или `dev/deploy/config.yaml` → namespace `dev.deploy`; источники: глобальный, локальный (проект), из конфига; `composite.go` объединяет их. Конфликт `.yaml` vs `/config.yaml` — ошибка Resolve, в листинге — пометка. Частичный листинг (часть источников не читается) — `*PartialError` с валидным результатом; вызывающий различает его через `errors.As`.
- `core` — `Options`/`DefaultOptions` (единственное чтение окружения: `FUTI_MODULES_DIR`, `HOME`, каталог кэша, cwd), `Runtime`: конфиг и глобальный source сразу, `Cache()`/`Source()` лениво (`sync.Once`), предупреждения через `Options.Warn`.
- `config` — `config.yaml` futi (источники, политика кэша).
- `execmodule` — модель модуля/команд (вложенные `commands`, `args`, `flags`, `cmd` строкой или YAML-списком), резолв команды, валидация, help; старые ключи (`params`, `positional`) отклоняются с подсказкой.
- `inputs` — схема входов (`args`/`flags`: типы, `default`, `variadic`, `rest`, `allow_dash`), сканирование CLI-токенов, привязка с умолчаниями, JSON Schema входов (для будущих MCP/REST).
- `argvtmpl` — свой разбор и вычисление шаблона `cmd` (синтаксис `text/template`, подмножество) сразу в argv: статические проверки при загрузке, dash guard, проверка значений для `.bat`/`.cmd` на Windows.
- `yamlstrict` — отказ на неизвестных ключах YAML (по `yaml.Node`, с номерами строк; ключи `x-` разрешены).
- `modulerunner` — запуск команды модуля и хуки (без shell, через тот же путь шаблон → argv); `executor` — низкоуровневый exec (stdin, пересылка сигналов, код выхода 128+N, группа процессов для хука без терминала; `InterruptSignal` — прерывающий сигнал из ошибки запуска).
- `modules` — менеджер модулей: `Manager` (install/update/uninstall/info/list/path/add_command/remove_command) поверх `core.Runtime`, `Init` без Runtime; ввод/вывод — через `Streams`/`Prompter`; разбор флагов — в `internal/cli/modules_cli.go`.
- `modulecache` — кэш глобальных модулей (диск + политики обновления + проверка целостности); локальные модули в кэш не попадают.
- `metayaml` — служебные метаданные модуля (`about` сохраняется из источника).
- `completion` — логика автодополнения, вызывается скрытой командой `futi __complete`; шеллы bash/zsh/fish — в `internal/cli/completion_*.go`.
- `termtext` — фильтр управляющих символов для вывода текста из YAML/lockfile (`Line`, `Block`).
- `testutil` — общие хелперы тестов.

## Неочевидные инварианты

- Разбор CLI: `futi [--dry-run] <path…> [args и flags…] [-- args…]`. Флаги самого futi (`--dry-run`, `-h`, `--version`) — только до пути; после пути все флаги принадлежат команде, args и flags можно смешивать; после `--` всё — args (`-h` там и внутри `rest` — данные). Подкоманда распознаётся только первым словом после пути. Системные запросы — только префиксом: `futi help <path>`, `futi list <path>`, `futi <path> -h`; хвостовое `help`/`list` — обычная команда модуля.
- Исполнение **без shell**: `cmd` вычисляется `argvtmpl` прямо в argv, значение входа — всегда данные, повторного shlex-разбора нет (`quote`, `{{.args}}` удалены). Значения, начинающиеся с `-`, отклоняются dash guard, если программа прочтёт их как опцию (выход — `--opt={{.x}}`, литеральный `--` в `cmd`, `allow_dash`). Хуки — тоже без shell; составной хук пишется явно: `[sh, -c, "…"]`. Не добавляй запуск через `sh -c` в код по умолчанию.
- Зарезервированные имена входов: `env`, `module_root`, `module_name` — модуль с ними не грузится (учитывай в тестовых фикстурах).
- Модуль = код: симлинки в каталоге модулей разрешены осознанно; `modules init` пишет только внутри каталога модулей (см. раздел «Безопасность» в README).
- Публичная часть — на английском: пользовательский вывод (help, ошибки, предупреждения), сообщения коммитов, заголовок и описание PR. Комментарии в коде: новые — на английском; существующие русские переводятся отдельной задачей, попутно не переписывай.
