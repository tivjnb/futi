# futi

**futi** — глобальный менеджер консольных утилит. Объединяет разрозненные команды, скрипты и инструменты в модули с единым CLI-интерфейсом.

Вместо десятков скриптов с разным синтаксисом — один YAML-файл на модуль и единый способ вызова:

```bash
futi docker build --tag myapp
futi git commit --message "fix"
futi deploy staging
```

---

## Содержание

- [Установка](#установка)
- [Быстрый старт](#быстрый-старт)
- [Структура модуля](#структура-модуля)
- [Discovery и namespaces](#discovery-и-namespaces)
- [Параметры и шаблонизация](#параметры-и-шаблонизация)
- [Модуль-пакет со скриптами](#модуль-пакет-со-скриптами)
- [Команды CLI](#команды-cli)
- [Валидация](#валидация)
- [Создание модулей](#создание-модулей)
- [Кэш модулей](#кэш-модулей)
- [Автодополнение](#автодополнение)
- [Примеры](#примеры)
- [ROADMAP](#roadmap)
- [Лицензия](#лицензия)

---

## Установка

```bash
go install ./cmd/futi
```

Или собрать из исходников:

```bash
git clone <repo-url> futi
cd futi
go build -o futi ./cmd/futi
```

Проверка:

```bash
futi list
# No modules found.
```

futi хранит модули в `~/.config/futi/modules/` (при наличии `config.yaml` — в путях, указанных в конфиге). Переопределить каталог для fallback без конфига:

```bash
export FUTI_MODULES_DIR=/path/to/modules
```

---

## Быстрый старт

Создайте первый модуль:

```bash
mkdir -p ~/.config/futi/modules
cat > ~/.config/futi/modules/hello.yaml << 'EOF'
name: hello
description: Demo module
commands:
  greet:
    params:
      - name: name
        type: string
        required: true
    cmd: "echo hello {{.name}}"
EOF
```

Или через `futi init` (создаёт модуль с шаблоном):

```bash
futi init hello --desc "Demo module" --command greet
```

Вызовите:

```bash
$ futi hello greet --name world
hello world

$ futi list
┌────────┬───────┬─────────────┐
│  TYPE  │ NAME  │ DESCRIPTION │
├────────┼───────┼─────────────┤
│ module │ hello │ Demo module │
└────────┴───────┴─────────────┘
```

---

## Структура модуля

Модуль — это YAML-файл с описанием команд.

```yaml
name: docker                    # опционально, по умолчанию = имя файла
description: Docker management  # опционально
cmd: "docker ps"                # опционально: команда по умолчанию (без явной команды)

commands:
  # Простая команда без параметров
  ps:
    cmd: "docker ps"

  # Команда с параметрами
  build:
    description: Build Docker image
    params:
      - name: tag
        type: string            # string (по умолчанию), bool или int
        required: true
      - name: file
        type: string
        default: Dockerfile
    cmd: "docker build -t {{.tag}} -f {{.file}} ."

  # Команда с bool-параметром
  logs:
    params:
      - name: container
        type: string
        required: true
      - name: follow
        type: bool
        default: false
    cmd: "docker logs {{.container}} {{if .follow}}-f{{end}}"

  # Вложенные команды: команда может иметь commands вместе с cmd или вместо него.
  # Если указан cmd + commands, cmd вызывается как fallback при отсутствии subcommand.
  migrate:
    description: Database migrations
    cmd: "migrate"              # fallback если не указан subcommand
    commands:
      up:
        cmd: "migrate up"
      down:
        cmd: "migrate down"
```

### Поля

| Поле | Тип | Описание |
|---|---|---|
| `name` | string | Имя модуля (опционально, по умолчанию = имя файла/директории) |
| `description` | string | Описание модуля (опционально) |
| `cmd` | string | Команда по умолчанию на уровне модуля (опционально) |
| `commands` | map | Команды модуля |
| `commands.<name>.description` | string | Описание команды (опционально) |
| `commands.<name>.cmd` | string | Шаблон команды (если есть `commands` — fallback) |
| `commands.<name>.params` | list | Параметры команды (опционально) |
| `commands.<name>.positional` | block | Требования к позиционным аргументам (опционально) |
| `commands.<name>.commands` | map | Вложенные команды (опционально) |
| `params[].name` | string | Имя параметра (обязательно) |
| `params[].type` | string | Тип: `string` (по умолчанию), `bool` или `int` |
| `params[].required` | bool | Обязательный параметр (по умолчанию `false`) |
| `params[].default` | string | Значение по умолчанию |
| `params[].description` | string | Описание параметра (опционально, показывается в help) |
| `positional.min` | int | Обязательный минимум позиционных аргументов после `--` (по умолчанию `0`) |

---

## Discovery и namespaces

futi использует SaltStack-модель discovery. Модуль может быть:

**Одиночным YAML-файлом:**

```
~/.config/futi/modules/docker.yaml      → futi docker <command>
~/.config/futi/modules/git.yaml         → futi git <command>
```

**Пакетом — директорией с `config.yaml` и скриптами:**

```
~/.config/futi/modules/deploy/
├── config.yaml
└── scripts/
    └── deploy.sh
```

→ `futi deploy <command>`

**Вложенные namespaces:**

```
~/.config/futi/modules/dev/deploy.yaml          → futi dev deploy <command>
~/.config/futi/modules/dev/deploy/config.yaml   → futi dev deploy <command>
```

namespace формируется из пути: `dev/deploy.yaml` → `dev.deploy`, вызывается как `futi dev deploy <command>` (последний аргумент — команда, остальное — namespace).

**Конфликт имён:** если для одного namespace существуют и `.yaml`, и `/config.yaml` — ошибка. Уберите одно из двух.

### Конфиг-файл: несколько источников

Файл `~/.config/futi/config.yaml` задаёт источники модулей с mountpoint'ами:

```yaml
sources:
  - path: ~/.config/futi/modules         # корень (по умолчанию)
  - path: /path/to/project-modules       # содержимое монтируется в ns1.ns2
    mountpoint: ns1.ns2
  - path: /path/to/another-source        # дополнение ns1.ns2 из другого пути
    mountpoint: ns1.ns2
  - path: /path/to/tools                 # обёрнуто как namespace "tools" внутри "dev"
    mountpoint: dev
    mount_as_namespace: tools
```

Параметры:
- `path` (обязательный) — каталог с модулями
- `mountpoint` — внутрь какого namespace монтировать содержимое (по умолчанию: корень)
- `mount_as_namespace` — обернуть содержимое в дополнительный namespace с этим именем

Несколько источников с одинаковым mountpoint **объединяются** — модули из всех источников видны в одном namespace. Если два источника дают модуль с одинаковым именем — ошибка конфликта.

Пустые namespace создаются неявно: если источник монтируется на `ns1.ns2`, то `ns1` и `ns2` существуют как namespaces, даже без собственных модулей.

Если конфиг-файл отсутствует — futi использует один каталог `~/.config/futi/modules/` (или `FUTI_MODULES_DIR`).

---

## Параметры и шаблонизация

### Передача параметров

```bash
futi docker build --tag myapp --file Dockerfile.dev
futi docker logs mycontainer --follow
futi docker logs mycontainer --follow=false
```

Форматы:
- `--param value` — пробел
- `--param=value` — через `=`
- `--bool-flag` — без значения (=`true`)
- `--bool-flag=false` — явный false

### Шаблонизация

futi использует Go `text/template`:

| Синтаксис | Описание |
|---|---|
| `{{.name}}` | Подстановка значения параметра |
| `{{.args}}` | Все аргументы после `--` (строка, пробел-joined) |
| `{{index .args_list 0}}` | N-й аргумент после `--` (доступ к массиву) |
| `{{if .follow}}...{{end}}` | Условный блок (для bool) |
| `{{.env.FOO}}` | Переменная окружения `FOO` (error если не задана) |
| `{{if index .env "FOO"}}...{{end}}` | Условный блок по наличию env var |

Аргументы после `--` передаются в шаблон:

```bash
futi echo_mod echo -- hello world
# → cmd: "echo {{.args}}" → echo hello world

futi echo_mod echo -- hello world
# → cmd: "echo {{index .args_list 0}}" → echo hello
```

Команда по умолчанию (на уровне модуля) вызывается без явной команды:

```bash
futi echo_mod -- hello world
# → module.cmd: "echo {{.args}}" → echo hello world
```

> **Примечание:** разделитель `--` для args может быть заменён гибридным режимом в будущих версиях (позиционные args для листовых команд без явного `--`).

Bool-параметры нормализуются: `false`, `0`, `no`, `""` → falsy; `true`, `1`, `yes` → truthy.

### Валидация параметров

- `required: true` и параметр не передан → ошибка
- `default` применяется, если параметр не передан
- Неизвестный параметр (нет в схеме) → ошибка
- `positional.min` и аргументов после `--` меньше минимума → ошибка

Пример `positional`:

```yaml
commands:
  greet:
    cmd: "echo {{index .args_list 0}}"
    positional:
      min: 1       # требует минимум 1 позиционный аргумент после --
```

### Важно: пробелы в значениях

Если значение параметра содержит пробелы, квотируйте в шаблоне:

```yaml
cmd: "echo \"{{.msg}}\""   # --msg "hello world" → echo "hello world"
```

Без кавычек `shlex` разобьёт строку на отдельные аргументы.

### Переменные окружения в шаблонах

Доступ к переменным окружения через `{{.env.FOO}}` — полезно для секретов (токены, API keys), которые небезопасно передавать через `--param` (видны в `ps`):

```yaml
commands:
  deploy:
    params:
      - name: env
        type: string
        required: true
    cmd: "deploy --token {{.env.DEPLOY_TOKEN}} --env {{.env}}"
```

```bash
export DEPLOY_TOKEN=secret123
futi deploy --env staging
# → deploy --token secret123 --env staging
```

Обращение к несуществующей переменной вызывает ошибку (fail-fast). Для опциональной проверки используйте `index`:

```yaml
cmd: "deploy {{if index .env \"VERBOSE\"}}-v{{end}}"
```

---

## Модуль-пакет со скриптами

Модуль может содержать собственные скрипты. Ссылайтесь на них относительным путём от корня модуля:

```
~/.config/futi/modules/mytool/
├── config.yaml
└── scripts/
    └── deploy.sh
```

```yaml
# config.yaml
name: mytool
commands:
  deploy:
    params:
      - name: env
        type: string
        required: true
    cmd: "./scripts/deploy.sh {{.env}}"
```

```bash
futi mytool deploy --env staging
# выполнит ~/.config/futi/modules/mytool/scripts/deploy.sh staging
```

Скрипты вызываются с `WorkDir` = корень модуля. Указывайте `./` для явного вызова локального скрипта (bare-имя без `/` ищется в `PATH`).

---

## Команды CLI

```bash
futi <namespace...> <command> [--param val] [-- args]   # вызов команды модуля
futi <namespace>                            # команда по умолчанию (если есть cmd на уровне модуля)
futi list                                   # модули и namespaces в корне
futi <namespace...> list                    # содержимое namespace или команды модуля
futi <namespace...> list -v                 # то же, с колонкой PATH
futi <namespace...> list -r                 # рекурсивный обход поддерева
futi <namespace...> validate                # валидация схемы модуля
futi init <name> [flags]                    # создать модуль с шаблоном
futi cache refresh                          # явное обновление кэша модулей
futi completion <bash|zsh|fish>             # сгенерировать скрипт автодополнения
futi __complete <args...>                   # hidden: подсказки для completion (внутр.)
futi -h | --help                            # справка
```

Парсинг: последний позиционный аргумент (до `--`) — команда, всё до него — namespace (сегменты через `.`). Аргументы после `--` передаются в шаблон команды как `{{.args}}` (строка) и `{{.args_list}}` (массив).

`list` и `validate` — зарезервированные слова, их нельзя использовать как имена команд в модуле.

```bash
futi docker build --tag myapp        # namespace=docker, command=build
futi dev deploy staging              # namespace=dev.deploy, command=staging
futi k8s logs mypod --follow         # namespace=k8s, command=logs, follow=true
futi echo_mod echo -- hello world    # namespace=echo_mod, command=echo, args="hello world"
futi echo_mod -- hello world         # default cmd модуля echo_mod, args="hello world"
```

### Навигация через `list`

`list` показывает содержимое текущего уровня иерархии:

```bash
$ futi list                          # корень: модули и namespaces
┌───────────┬────────┬───────────────────┐
│   TYPE    │  NAME  │    DESCRIPTION    │
├───────────┼────────┼───────────────────┤
│ module    │ docker │ Docker management │
│ namespace │ images │                   │
└───────────┴────────┴───────────────────┘

$ futi images list                   # namespace: вложенные namespaces и модули
┌───────────┬───────────┬───────────────┐
│   TYPE    │   NAME    │  DESCRIPTION  │
├───────────┼───────────┼───────────────┤
│ namespace │ converter │               │
│ module    │ png       │ PNG utilities │
└───────────┴───────────┴───────────────┘

$ futi images converter list         # вложенный namespace
┌────────┬────────┬──────────────────────┐
│  TYPE  │  NAME  │     DESCRIPTION      │
├────────┼────────┼──────────────────────┤
│ module │ to_png │ Convert images to PNG │
└────────┴────────┴──────────────────────┘

$ futi images png list               # модуль: команды
┌─────────┬─────────────┐
│ COMMAND │ DESCRIPTION │
├─────────┼─────────────┤
│ optimize│ Optimize PNG│
│ resize  │             │
└─────────┴─────────────┘

$ futi docker list                   # модуль в корне: команды
┌─────────┬─────────────┐
│ COMMAND │ DESCRIPTION │
├─────────┼─────────────┤
│ ps      │             │
│ build   │ Build image │
└─────────┴─────────────┘
```

Если у модуля есть команда по умолчанию (`cmd` на уровне модуля), она отображается первой с маркером `(default)`:

```bash
$ futi echo_mod list                 # модуль с default cmd
┌───────────┬─────────────┐
│ COMMAND   │ DESCRIPTION │
├───────────┼─────────────┤
│ (default) │             │
│ upper     │             │
└───────────┴─────────────┘
```

То же касается subcommands: если команда имеет `cmd` + `commands`, `cmd` показывается как `(default)` (fallback при вызове без subcommand).

Логика: если namespace резолвится как модуль — `list` показывает его команды. Если как namespace-директория — показывает непосредственных детей (вложенные namespaces и модули). `list` зарезервирован — нельзя создать команду с именем `list` в модуле.

Флаг `-v`/`--verbose` добавляет колонку `PATH` — путь к манифесту модуля. Полезно при нескольких источниках модулей или отладке:

```bash
$ futi list -v
┌───────────┬────────┬───────────────────┬──────────────────────────────────────────┐
│   TYPE    │  NAME  │    DESCRIPTION    │                   PATH                    │
├───────────┼────────┼───────────────────┼──────────────────────────────────────────┤
│ module    │ docker │ Docker management │ /home/user/.config/futi/modules/docker.yaml│
│ namespace │ images │                   │                                          │
└───────────┴────────┴───────────────────┴──────────────────────────────────────────┘
```

Флаг `-r`/`--recurse` рекурсивно обходит всё поддерево namespace, показывая полный namespace-путь от контекста:

```bash
$ futi list -r
┌───────────┬──────────────────────┬──────────────────────┐
│   TYPE    │         NAME         │     DESCRIPTION      │
├───────────┼──────────────────────┼──────────────────────┤
│ module    │ docker               │ Docker management    │
│ namespace │ images               │                      │
│ namespace │ images.converter     │                      │
│ module    │ images.converter.to_png│ Convert images to PNG│
│ module    │ images.png           │ PNG utilities        │
└───────────┴──────────────────────┴──────────────────────┘

$ futi images list -r                # от контекста images
┌───────────┬──────────────────┬──────────────────────┐
│   TYPE    │       NAME       │     DESCRIPTION      │
├───────────┼──────────────────┼──────────────────────┤
│ namespace │ converter        │                      │
│ module    │ converter.to_png │ Convert images to PNG│
│ module    │ png              │ PNG utilities        │
└───────────┴──────────────────┴──────────────────────┘
```

`-r` игнорируется для модулей (показывает команды как обычно). Можно комбинировать: `futi list -r -v`.

---

## Валидация

`futi validate` проверяет схему модуля без вызова команды — полезно при разработке модуля, в CI или перед публикацией.

```bash
$ futi docker validate
OK
```

Невалидный модуль выводит отчёт со всеми ошибками и предупреждениями за один проход:

```bash
$ futi bad validate
ERROR: command "empty": must have cmd or commands
ERROR: command "build": param "tag" has invalid type "float" (valid: string, bool, int)
ERROR: command "build": duplicate param "tag"
validate: module "bad" has 3 error(s)
$ echo $?
1
```

Предупреждения не блокируют (exit code 0):

```bash
$ futi db validate
WARNING: command "migrate": has subcommands but no cmd (calling without subcommand will fail)
$ echo $?
0
```

### Что проверяется

**Ошибки** (блокируют загрузку модуля, `futi <ns> <cmd>` упадёт с понятным сообщением):

- команда без `cmd` и без `commands` (бесполезная команда)
- дубликаты имён `params` в команде
- `type` параметра не из `{string, bool, int}` (пустой = `string`)

**Предупреждения** (не блокируют, показываются только в `validate`):

- команда с `commands`, но без `cmd` — вызов без subcommand падает в runtime

Валидация выполняется рекурсивно для всех вложенных `commands`. При обычном вызове (`futi <ns> <cmd>`) валидация запускается автоматически при загрузке модуля — `futi validate` нужен для просмотра **всех** ошибок за один проход и для warnings (которые в runtime не показываются).

`validate` зарезервирован как `list` — нельзя создать команду с именем `validate` в модуле.

---

## Создание модулей

`futi init` создаёт YAML-модуль с шаблоном — не нужно копировать из README вручную. Модуль сразу доступен через auto-discovery.

```bash
futi init mytool                          # ./.futi/mytool.yaml
futi init dev/mytool                      # ./.futi/dev/mytool.yaml (namespace dev)
futi init mytool --global                 # ~/.config/futi/modules/mytool.yaml
```

Флаги:

| Флаг | Описание |
|---|---|
| `--desc <text>` | Описание модуля (по умолчанию `My tool`) |
| `--default-cmd <cmd>` | Команда по умолчанию на уровне модуля (`cmd` поле) |
| `--command <name>` | Добавить команду (можно несколько раз) |
| `--global` | Создать в `~/.config/futi/modules/` вместо `./.futi/` |

Минимальный модуль (без флагов):

```bash
$ futi init mytool
Created: .futi/mytool.yaml

$ futi mytool hello
hello
```

```yaml
# .futi/mytool.yaml
name: mytool
description: My tool
commands:
  hello:
    cmd: "echo hello"
```

С флагами:

```bash
$ futi init dev/apitool --desc "API utilities" --default-cmd "echo api" --command fetch --command build
Created: .futi/dev/apitool.yaml

$ futi dev apitool           # default cmd
api

$ futi dev apitool fetch     # команда
fetch
```

```yaml
# .futi/dev/apitool.yaml
name: apitool
description: API utilities
cmd: "echo api"
commands:
  fetch:
    cmd: "echo fetch"
  build:
    cmd: "echo build"
```

При существующем файле возвращается ошибка (без перезаписи). Созданный модуль проходит валидацию схемы.

---

## Кэш модулей

futi кэширует загруженные модули для устранения повторной загрузки в рамках одного запуска и между запусками. Persistent кэш хранится в `~/.cache/futi/modulecache.gob` (gob-формат), in-memory кэш — в рамках процесса.

### Поведение

- При старте CLI загружает persistent кэш с диска (глобальные модули).
- In-memory кэш хранит ВСЕ модули (глобальные + локальные из `./.futi/` или `./futi.yaml`).
- Локальные модули не сохраняются на диск — только глобальные.
- При Refresh: проверка `mtime + size` — перечитываются только изменившиеся файлы.
- Удалённые из source модули очищаются из кэша при следующем Refresh.
- Ошибки загрузки отдельных модулей не фатальны — модуль пропускается, счётчик `skipped` инкрементируется.

### `futi cache refresh`

Явное обновление кэша: обходит source, перечитывает изменившиеся модули, сохраняет на диск.

```bash
$ futi cache refresh
Cache refreshed: 5 modules, 2 updated

# С невалидным модулем:
$ futi cache refresh
Cache refreshed: 5 modules, 2 updated, 1 skipped
```

`futi cache` без подкоманды или с неизвестной подкомандой возвращает понятную ошибку:

```bash
$ futi cache
error: cache requires a subcommand (try "cache refresh")

$ futi cache foo
error: unknown cache subcommand "foo" (try "cache refresh")
```

### Политики обновления

Стратегия автоматического обновления при старте CLI задаётся в `config.yaml`:

```yaml
cache:
  refresh: every_time   # every_time (default) | every_t_min | force_only
  time: "30m"            # только для every_t_min
```

| Политика | Поведение |
|---|---|
| `every_time` | Обновлять при каждом запуске CLI (default). |
| `every_t_min` | Обновлять раз в указанный интервал `time`. Между запусками — использовать кэш с диска. |
| `force_only` | Никогда автоматически. Только `futi cache refresh`. |

Формат `time` — расширение `time.ParseDuration` с поддержкой дней через суффикс `d`:

| Пример | Значение |
|---|---|
| `30m` | 30 минут |
| `2h` | 2 часа |
| `1d` | 1 день (24 часа) |
| `1d4h` | 1 день 4 часа (28 часов) |
| `1d2h30m` | 1 день 2 часа 30 минут |

Компоненты суммируются. Отрицательные значения не поддерживаются.

### Fallback

Если `~/.cache/` недоступен (нет `HOME` / `XDG_CACHE_HOME`) — futi fallback-ит на in-memory кэш с предупреждением в stderr. CLI работает, но persistent кэш недоступен до следующего запуска с корректным окружением.

При повреждённом gob-файле — кэш автоматически перестраивается с пустого состояния. При несовпадении версии схемы (новая версия futi) — аналогично.

---

## Автодополнение

futi поддерживает автодополнение для **bash**, **zsh** и **fish**. Имена модулей,
namespace'ов, команд, подкоманд и параметров (`--tag`, `--force`) — динамические,
поэтому completion запрашивает подсказки у futi на лету через скрытую команду
`futi __complete <args...>` (протокол, совместимый с cobra).

### Установка

```bash
# bash
futi completion bash > /etc/bash_completion.d/futi   # system-wide
source <(futi completion bash)                        # сессия

# zsh
futi completion zsh > "${fpath[1]}/_futi"
autoload -U compinit && compinit

# fish
futi completion fish > ~/.config/fish/completions/futi.fish
```

### Что дополняется

- **Модули и namespaces** — `futi <TAB>`, `futi dev <TAB>`
- **Команды и subcommands** — `futi docker <TAB>`, `futi docker migrate <TAB>`
- **Имена параметров** — `futi docker build --<TAB>` → `--tag`, `--force`
- **Bool-значения** — `futi docker build --force <TAB>` → `true`/`false`
- **Обязательные параметры** — на листовой команде показываются сразу (без ввода `--`): `futi docker build <TAB>` → `--tag` (с меткой `(required)` в zsh)
- **Builtins** — `futi cache <TAB>` → `refresh`/`status`/`clear`; `futi completion <TAB>` → `bash`/`zsh`/`fish`
- **Подсказка `help`** — `help` предлагается на каждом уровне (модуль/команда/subcommand): `futi demo <TAB>`, `futi demo db <TAB>`, `futi demo greet <TAB>`. Если в модуле уже определена команда `help` (user-defined), она и предлагается — системная не дублируется.

Значения строковых/числовых параметров (обычно user-supplied, например `--tag myapp`)
не угадываются и не дополняются. После `--` (args команды) futi разрешает
дополнение файлов.

zsh выводит описания команд/модулей рядом с подсказками.

### Кэш и completion

`__complete` использует persistent кэш модулей (как обычный CLI), поэтому работает
быстро — без обхода source на каждый TAB. Чтобы подсказки не были пустыми на
холодном кэше (первый запуск) или устаревшими, `__complete` **самообновляет** кэш,
если он пуст, никогда не обновлялся или обновлялся более часа назад. В остальных
случаях берёт данные из кэша без Refresh.

---

## Примеры

### Echo-модуль (args + default cmd)

```yaml
# ~/.config/futi/modules/echo_mod.yaml
name: echo_mod
description: Echo utilities
cmd: "echo {{.args}}"           # команда по умолчанию
commands:
  upper:
    description: Uppercase echo
    cmd: "echo {{.args}} | tr a-z A-Z"
  repeat:
    description: Repeat first arg N times
    params:
      - name: count
        type: int
        required: true
    cmd: "for i in $(seq 1 {{.count}}); do echo {{index .args_list 0}}; done"
```

```bash
futi echo_mod -- hello world         # echo hello world (default cmd)
futi echo_mod upper -- hello world   # echo hello world | tr a-z A-Z
futi echo_mod repeat --count 3 -- hi # hi\nhi\nhi
```

### Git shortcuts

```yaml
# ~/.config/futi/modules/git.yaml
name: git
description: Git shortcuts
commands:
  amend:
    cmd: "git commit --amend --no-edit"

  fresh:
    description: Reset to remote branch
    params:
      - name: branch
        type: string
        default: main
    cmd: "git fetch origin && git reset --hard origin/{{.branch}}"

  squash:
    description: Squash last N commits
    params:
      - name: count
        type: string
        required: true
      - name: message
        type: string
        required: true
    cmd: "git reset --soft HEAD~{{.count}} && git commit -m \"{{.message}}\""
```

```bash
futi git amend
futi git fresh --branch develop
futi git squash --count 3 --message "feature complete"
```

### Docker wrapper

```yaml
# ~/.config/futi/modules/docker.yaml
name: docker
description: Docker utilities
commands:
  clean:
    description: Remove stopped containers and dangling images
    cmd: "docker container prune -f && docker image prune -f"

  shell:
    description: Shell into running container
    params:
      - name: container
        type: string
        required: true
    cmd: "docker exec -it {{.container}} sh"

  build-all:
    description: Build all images in directory
    cmd: "for d in */; do docker build -t \"${d%/}\" \"$d\"; done"
```

```bash
futi docker clean
futi docker shell --container myapp
futi docker build-all
```

### Проектный deploy-пакет

```
~/.config/futi/modules/deploy/
├── config.yaml
└── scripts/
    ├── deploy.sh
    └── rollback.sh
```

```yaml
# config.yaml
name: deploy
description: Deployment utilities
commands:
  staging:
    params:
      - name: version
        type: string
        required: true
    cmd: "./scripts/deploy.sh staging {{.version}}"

  production:
    params:
      - name: version
        type: string
        required: true
    cmd: "./scripts/deploy.sh production {{.version}}"

  rollback:
    params:
      - name: env
        type: string
        required: true
    cmd: "./scripts/rollback.sh {{.env}}"
```

```bash
futi deploy staging --version v1.2.3
futi deploy production --version v1.2.3
futi deploy rollback --env production
```

---

## ROADMAP

futi находится на стадии PRE PRE MVP. Ниже — направления развития.

### Ближайшие планы

- **Типы параметров** — расширение за пределы `int`: `file`, `json`, `secret` (безопасная передача через env, не args; базовый доступ к env vars уже есть через `{{.env.FOO}}`)
- **Гибридный args-парсинг** — позиционные args для листовых команд без явного `--`
- ~~**Shell completion** — автодополнение для bash/zsh/fish~~ ✅ реализовано (см. раздел «Автодополнение»)
- ~~**Help-система** — модульная/командная справка через `help`~~ ✅ реализовано (`futi <ns> help`, `futi <ns> <cmd> help`; user-defined `help` выигрывает)
- **Descriptions в bash/fish** — zsh выводит описания уже сейчас; bash/fish — roadmap
- **PowerShell** — 4-й shell после стабилизации bash/zsh/fish

### Unified I/O

Автоматическая конвертация между форматами ввода-вывода:

```yaml
inputs:
  - name: config
    type: file        # строка → временный файл → путь
    source: arg
    cleanup: true
  - name: data
    type: json        # валидация и форматирование JSON
    source: stdin
  - name: token
    type: secret      # передача через env, не args
    source: arg
```

```bash
echo '{"port": 8080}' | futi docker run --image nginx   # futi создаст временный файл
```

### Множественные интерфейсы

Одно ядро — множество способов доступа. Модуль пишется один раз, доступен из любого интерфейса:

- **HTTP API** — REST endpoint для CI/CD и автоматизации
- **MCP** — интеграция с AI-агентами (Claude, GPT, etc.)
- **WebSocket** — real-time взаимодействие

```bash
futi serve --http --port 8080
curl -X POST http://localhost:8080/api/v1/docker/build -d '{"tag": "myapp"}'
```

### Менеджер модулей

Установка, обновление, удаление модулей из registry:

```bash
futi module install docker        # из registry
futi module install ./my.yaml     # из локального файла
futi module install https://github.com/user/futi-docker
futi module update docker
futi module check                 # проверить обновления
```

### Дополнительно

- **Module versioning** — версии модулей, совместимость
- **Project overrides** — переопределение модулей на уровне проекта (приоритет над глобальными)
- **Config linting** — расширение валидации схем (базовая валидация через `futi validate` уже есть): подсказки по best practices, deprecated-поля, стилистика
- **TUI** — интерактивный просмотр и вызов модулей
- **Module registry** — публичный реестр шаримых модулей

---

## Лицензия

MIT
