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
- [Управление модулями](#управление-модулями)
- [Кэш модулей](#кэш-модулей)
- [Автодополнение](#автодополнение)
- [Безопасность](#безопасность)
- [Примеры](#примеры)
- [ROADMAP](#roadmap)
- [Лицензия](#лицензия)

---

## Установка

Сборка из исходников:

```bash
git clone https://github.com/tivjnb/futi futi
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
mkdir -p ~/.config/futi
cp -r examples/modules ~/.config/futi/
```

Или через `futi modules init` (создаёт модуль с шаблоном):

```bash
futi modules init hello --desc "Demo module" --command greet
```

Вызовите (модуль, созданный вручную, принимает `--name`):

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

Модуль из `futi modules init` создаёт команды-заглушки без параметров — параметры
дописываются в YAML вручную (см. [Структура модуля](#структура-модуля)):

```bash
$ futi hello greet
greet
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
| `params` | list | Схема команды по умолчанию: параметры (опционально) |
| `positional` | block | Схема команды по умолчанию: позиционные аргументы (опционально) |
| `commands` | map | Команды модуля |
| `commands.<name>.description` | string | Описание команды (опционально) |
| `commands.<name>.cmd` | string | Шаблон команды (если есть `commands` — fallback) |
| `commands.<name>.params` | list | Параметры команды (опционально) |
| `commands.<name>.positional` | block | Требования к позиционным аргументам (опционально) |
| `commands.<name>.commands` | map | Вложенные команды (опционально) |
| `params[].name` | string | Имя параметра (обязательно): буквы, цифры, `_` и `-`, не начинается с цифры или `-`. Имена `args`, `args_list`, `env`, `module_name`, `module_root` зарезервированы — ошибка схемы |
| `params[].type` | string | Тип: `string` (по умолчанию), `bool` или `int` |
| `params[].required` | bool | Обязательный параметр (по умолчанию `false`) |
| `params[].default` | string | Значение по умолчанию |
| `params[].description` | string | Описание параметра (опционально, показывается в help) |
| `positional.min` | int | Обязательный минимум позиционных аргументов после `--` (по умолчанию `0`) |

Имя с дефисом, например `dry-run`: в CLI — `--dry-run`, в шаблоне — `{{index . "dry-run"}}` (точка-нотация `{{.dry-run}}` не работает).

Верхнеуровневые `params` и `positional` описывают команду по умолчанию (`cmd`):
валидация, defaults и help работают так же, как у обычных команд.

```yaml
# meta.yaml — модуль как одна команда со схемой
name: meta
cmd: "python scripts/meta.py --files {{quote .args}}{{if .json}} --json{{end}}"
params:
  - name: json
    type: bool
    default: "false"
positional:
  min: 1
```

```bash
futi meta --json -- photo.jpg
```

Неизвестный ключ манифеста (например, опечатка `requird` в параметре) — ошибка схемы с номером строки: `line 9: unknown key "requird" in commands.build.params[0]`. Ключи с префиксом `x-` разрешены на любом уровне структуры и игнорируются — под них удобно заводить YAML-якоря (`x-common: &c {…}` и `<<: *c`). Исключение: ключ внутри `commands` — это имя команды, и `x-build` там обычная команда.

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

**Именование записей:** имя файла (без `.yaml`) и имя каталога — это сегмент namespace, поэтому оно не должно быть пустым, содержать точку или равняться `config`. Записи вроде `my.tool.yaml`, `.yaml`, `my.dir/`, `.git/` и `config/` (вместе с содержимым) не листятся и не резолвятся: `my.tool` резолвился бы в `my/tool.yaml`.

**Конфликт имён:** если для одного namespace существуют и `.yaml`, и `/config.yaml` — ошибка. Уберите одно из двух. `futi list` такую пару не скрывает: показывает одну запись `x` с пометкой `конфликт: x.yaml и x/config.yaml`, а `futi x` завершается ошибкой конфликта.

Модуль `x.yaml` и namespace `x/` без `config.yaml` — не конфликт: в листинге видны обе записи, `futi x <command>` вызывает модуль, а `futi x <имя> <command>` — модули внутри namespace.

### Симлинки

Симлинки в каталоге модулей поддерживаются и обрабатываются так же, как
реальные файлы и директории: симлинк на `.yaml`-файл, на пакет-директорию
(с `config.yaml`) и на namespace — везде листятся, резолвятся и исполняются.

Это удобно для dotfiles-менеджеров (stow, chezmoi) и для разработки:
модуль можно держать в git-репо и подключить симлинком:

```bash
ln -s ~/projects/my-modules/deploy ~/.config/futi/modules/deploy
futi deploy   # пакет-симлинк листится и исполняется как обычный
```

Нюансы:

- Циклы симлинков (`a → b → a`) безопасны: циклы отсекаются по предкам —
  каталог (по реальному пути), уже лежащий на текущем пути обхода, не
  раскрывается повторно. Два симлинка на один каталог
  (`a/x → real`, `b/y → real`) раскрываются оба.
- Битый симлинк с именем `*.yaml` (цель недоступна) не листится и не
  резолвится, как и FIFO или устройство с таким именем: модулем считается
  только обычный файл. Если цель временно недоступна (например, не
  примонтирован volume), запись кэша такого модуля удаляется при обновлении
  кэша и восстанавливается, когда цель снова доступна.
- Запись, которую нельзя проверить из-за прав (например, каталог без права
  поиска `x`), тоже не листится, но молча не пропадает: `futi list` выводит
  `warning: …` с путём и причиной, а кэш при таком частичном листинге не
  очищается.
- `futi modules init` при записи через симлинк-директорию создаёт файл в целевом
  каталоге симлинка и показывает **реальный** (резолвнутый) путь в выводе.

### Локальные модули проекта

Помимо глобального каталога, futi автоматически подхватывает модули из текущей
директории проекта:

- `./.futi/` — каталог модулей с той же структурой (yaml-файлы, пакеты,
  namespaces);
- `./futi.yaml` — одиночный модуль проекта (имя берётся из поля `name`).

При наличии обоих — `./.futi/` имеет приоритет. Локальные модули **перекрывают
глобальные** с тем же именем: удобно переопределять поведение под конкретный
проект (свои `deploy`, `test`, `build` в репо — без правки глобальных модулей).

```
myproject/
├── futi.yaml          # модуль проекта целиком
├── .futi/             # или каталог локальных модулей
│   ├── deploy.yaml
│   └── test.yaml
└── src/
```

```bash
cd myproject
futi list              # видны и локальные, и глобальные модули
futi deploy            # вызван локальный .futi/deploy.yaml
```

Локальные модули живут только in-memory (не попадают в persistent кэш) —
каждый запуск читает их заново, поэтому изменения подхватываются сразу.
Невалидный `futi.yaml` — ошибка при старте (ранняя диагностика).

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
- `path` (обязательный) — каталог с модулями; `~/…` — путь от домашнего каталога (сам домашний каталог — только в кавычках: `"~"`, голый `~` в YAML — это null), относительный путь считается от каталога `config.yaml`
- `mountpoint` — внутрь какого namespace монтировать содержимое (по умолчанию: корень)
- `mount_as_namespace` — обернуть содержимое в дополнительный namespace с этим именем

Правила `mountpoint` и `mount_as_namespace`: внешние точки обрезаются (`.dev.` → `dev`); значение, состоящее только из точек (`.`, `..`), пустой сегмент (`a..b`), а также `/`, `\`, пробельные и управляющие символы в имени — ошибка при загрузке конфига.

Неизвестный ключ в `config.yaml` (например, опечатка `mountpont`) — ошибка с номером строки: `line 3: unknown key "mountpont" in sources[0]`. Ключи с префиксом `x-` разрешены на любом уровне и игнорируются — под них удобно заводить YAML-якоря (`x-common: &common {…}`).

Несколько источников с одинаковым mountpoint **объединяются** — модули из всех источников видны в одном namespace. Если два источника дают модуль с одинаковым именем — это конфликт: `futi list` показывает модуль с пометкой `конфликт: модуль в нескольких sources: <каталоги sources>`, а `futi x` завершается ошибкой конфликта.

Несуществующий или нечитаемый каталог source не ломает остальные: модули остальных sources работают, а в stderr выводится `warning: …` с путём и причиной (при политике кэша `every_time` — при каждом запуске). Автодополнение в таком случае молчит, а кэш при частичном листинге не очищается. Без `config.yaml` отсутствующий каталог модулей (свежая установка) — не ошибка и предупреждений не даёт.

Пустые namespace создаются неявно: если источник монтируется на `ns1.ns2`, то `ns1` и `ns2` существуют как namespaces, даже без собственных модулей.

Если конфиг-файл отсутствует — futi использует один каталог `~/.config/futi/modules/` (или `FUTI_MODULES_DIR`).

---

## Параметры и шаблонизация

### Передача параметров

```bash
futi docker build --tag myapp --file Dockerfile.dev
futi docker logs --follow
futi docker logs --follow=false
futi docker logs --follow -- mycontainer
```

Форматы:
- `--param value` — пробел (для string/int-параметров)
- `--param=value` — через `=` (обязательно для значений, начинающихся с `--`)
- `--bool-flag` — без значения (=`true`)
- `--bool-flag=false` — явный false

> **Важно:** порядок строгий — `futi <путь…> [флаги] [-- args]`. Путь
> (namespace и команда) заканчивается на первом токене, начинающемся с `-`
> (одиночный `-` — обычное слово). После этого слово допустимо только как
> значение предыдущего string/int-флага; bool-флаг слово никогда не съедает
> (`--force false` — ошибка, пишите `--force=false`), а лишнее слово —
> ошибка `unexpected argument`. Позиционные аргументы команды передаются
> после `--`. Флаги до пути (`futi --tag x docker build`) не поддерживаются.
>
> Значение `string`/`int` может начинаться с одного `-` (`--n -5`,
> `--msg -h`); значение, начинающееся с `--`, — только через `=`
> (`--msg=--x`). Короткие флаги (`-v`, `-f` и т. п.) у обычных команд не
> принимаются — параметры передаются как `--name`. `-v`/`-r` — только флаги
> `list`; `--verbose`/`--recurse` у обычной команды — обычные параметры.
> `-h`/`--help` после пути показывает справку по этому пути.

### Шаблонизация

futi использует Go `text/template`:

| Синтаксис | Описание |
|---|---|
| `{{.name}}` | Подстановка значения параметра |
| `{{quote .name}}` | Безопасная вставка значения как **одного** аргумента (экранирует кавычки/пробелы/метасимволы) |
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

Bool-параметры принимают `true`/`false`, `1`/`0`, `yes`/`no`, `on`/`off` (без учёта регистра и пробелов по краям): falsy-значения приводятся к пустой строке, truthy — к `true`. Любое другое значение, включая пустое (`--flag=`), — ошибка. Не переданный bool без `default` — false.

### Валидация параметров

- `required: true` и параметр не передан → ошибка; пустое значение string/int (`--tag=`) тоже ошибка (для bool явный `false` допустим)
- `required: true` вместе с непустым `default` → ошибка схемы (`futi modules validate <ns>`)
- `default` int-параметра должен быть целым числом, `default` bool — одним из значений выше, иначе ошибка схемы
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

### Важно: пробелы и кавычки в значениях

Если значение параметра содержит пробелы, используйте `{{quote}}`:

```yaml
cmd: "echo {{quote .msg}}"   # --msg "hello world" → один аргумент: hello world
```

`{{quote}}` экранирует значение так, что оно становится ровно **одним**
аргументом команды, даже если содержит пробелы, кавычки или метасимволы.

Паттерн с ручными кавычками (`cmd: "echo \"{{.msg}}\""`) тоже работает,
но небезопасен: значение проходит повторный разбор `shlex`, поэтому
пользователь может «пробить» кавычки и добавить произвольные флаги:

```bash
futi mod --msg 'x" --force "y'
# cmd: "echo \"{{.msg}}\""  → echo "x" --force "y"  → --force стал отдельным аргументом!
# cmd: "echo {{quote .msg}}" → echo 'x" --force "y' → --force остался частью значения
```

Для значений в опасных позициях (`git commit -m`, `docker run`, любые
`--flag {{.param}}`) всегда используйте `{{quote}}`.

### Переменные окружения в шаблонах

Доступ к переменным окружения через `{{.env.FOO}}` — полезно для секретов (токены, API keys), которые небезопасно передавать через `--param` (видны в `ps`):

```yaml
commands:
  deploy:
    params:
      - name: target
        type: string
        required: true
    cmd: "deploy --token {{.env.DEPLOY_TOKEN}} --env {{quote .target}}"
```

```bash
export DEPLOY_TOKEN=secret123
futi deploy --target staging
# → deploy --token secret123 --env staging
```

Обращение к несуществующей переменной вызывает ошибку (fail-fast). Для опциональной проверки используйте `index`:

```yaml
cmd: "deploy {{if index .env \"VERBOSE\"}}-v{{end}}"
```

---

## Модуль-пакет со скриптами

Модуль может содержать собственные скрипты. futi не задаёт `WorkDir` для
команды — она исполняется в cwd вызова (`./photo.jpg` разрешается оттуда).
Ссылайтесь на файлы модуля явно через шаблон `{{quote .module_root}}`:

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
      - name: target
        type: string
        required: true
    cmd: "{{quote .module_root}}/scripts/deploy.sh {{quote .target}}"
```

```bash
futi mytool deploy --target staging
# выполнит ~/.config/futi/modules/mytool/scripts/deploy.sh staging
```

Скрипты вызываются с `WorkDir` = cwd вызова futi. Первый токен cmd
подчиняется обычной Unix-семантике: относительный путь резолвится от cwd,
bare-имя — через `PATH`. Свои скрипты пакета — явно через
`{{quote .module_root}}/scripts/...` (кавычки экранируют пробелы в пути,
если модуль лежит в каталоге с пробелами).

### `{{.module_root}}` и `{{.module_name}}`

В шаблоне cmd доступны две новые переменные — путь и имя модуля. Они
используются для обращения к файлам модуля из cmd и хуков:

```yaml
cmd: "{{quote .module_root}}/scripts/{{.name}}.sh"
```

```yaml
# Модуль с собственным venv — путь к интерпретатору от корня модуля:
# ~/.config/futi/modules/images/qr/
# ├── config.yaml
# ├── scripts/make.py
# └── .venv/            # python3 -m venv .venv && .venv/bin/pip install qrcode
commands:
  make:
    cmd: "{{quote .module_root}}/.venv/bin/python {{quote .module_root}}/scripts/make.py --text {{quote .text}}"
```

Переменные прокинуты также как `FUTI_MODULE_ROOT` и `FUTI_MODULE_NAME` в
окружении дочернего процесса — скрипты на любом языке могут использовать их
напрямую:

```python
# scripts/run.py — путь к модулю из env
import os
mod_root = os.environ["FUTI_MODULE_ROOT"]
config = os.path.join(mod_root, "config.local.yaml")
```

Обращение к `{{.module_root}}`/`{{.module_name}}` при пустом `ModuleRef` даёт
missingkey-ошибку (`fail-fast`, как `{{.env.X}}` при отсутствии env var).

### Cwd вызова и пути пользователя

Относительные пути, переданные пользователем (`--out result.png`, файлы
после `--`), резолвятся ОС от cwd вызова `futi` — естественная Unix-семантика.
Скрипту, которому нужен cwd, достаточно `getcwd()` (он = cwd вызова futi):

```python
import os
out = os.path.join(os.getcwd(), "result.png")
```

> **Migration note (pre-release):** до переработки рабочего каталога futi вызывал команды с
> `WorkDir` = корень модуля и передавал `FUTI_CWD` в env. Это поведение
> удалено. Правки модулей:
>
> - `cmd: "./scripts/x.sh ..."` → `cmd: "{{quote .module_root}}/scripts/x.sh ..."`
> - `cmd: ".venv/bin/python scripts/x.py ..."` → `cmd: "{{quote .module_root}}/.venv/bin/python {{quote .module_root}}/scripts/x.py ..."`
> - `base = os.environ.get("FUTI_CWD") or os.getcwd()` → `base = os.getcwd()`
>   (cwd вызова futi = cwd скрипта, `getcwd()` достаточно)
> - `os.environ["FUTI_CWD"]` для путей модуля → `os.environ["FUTI_MODULE_ROOT"]`

---

## Команды CLI

```bash
futi <namespace...> <command> [--param val] [-- args]   # вызов команды модуля
futi <namespace...> [<command>] -h          # справка по пути (модуль, ветка или команда)
futi <namespace>                            # команда по умолчанию (если есть cmd на уровне модуля)
futi list                                   # модули и namespaces в корне
futi <namespace...> list                    # содержимое namespace или команды модуля
futi <namespace...> list -v                 # то же, с колонкой PATH
futi <namespace...> list -r                 # рекурсивный обход поддерева
futi modules validate <namespace>           # валидация схемы модуля
futi <namespace...> help                    # справка по модулю/команде
futi modules init <name> [flags]            # создать модуль с шаблоном
futi modules install <source> [flags]       # установить модуль из source (local/git/archive)
futi modules uninstall <name> [flags]       # detach + удаление (с подтверждением)
futi modules update [name] [flags]          # re-fetch из lockfile-источника + re-attach
futi modules list                           # установленные модули
futi modules info <name>                    # детали установки (lockfile/about)
futi modules path <name>                    # каталог установленного модуля
futi modules add_command <mod> <cmd> [flags]   # добавить команду в манифест
futi modules remove_command <mod> <cmd>      # удалить команду из манифеста
futi cache refresh                          # явное обновление кэша модулей
futi cache status                           # статистика кэша
futi cache clear                            # удалить persistent кэш с диска
futi completion <bash|zsh|fish>             # сгенерировать скрипт автодополнения
futi __complete <args...>                   # hidden: подсказки для completion (внутр.)
futi -h | --help                            # справка
```

Удалить единственную команду модуля без `cmd` нельзя — получился бы пустой модуль; `futi modules remove_command` откажет и оставит манифест без изменений.

Парсинг: путь — слова до первого флага (токена с `-`); последнее слово пути — команда, всё до него — namespace (сегменты через `.`); затем флаги, затем `--`. Аргументы после `--` передаются в шаблон команды как `{{.args}}` (строка) и `{{.args_list}}` (массив).

**Зарезервированные имена**: `list`, `cache`, `modules`, `completion`, `__complete` (перехватываются CLI до резолва модулей) нельзя использовать ни в одном сегменте пути модуля — ни в namespace, ни в имени. Проверка выполняется при `futi modules init` и `futi modules install` (для `--namespace` и итогового имени); уже установленные модули не затрагиваются. Команда модуля с именем `list` — ошибка схемы (см. раздел ошибок валидации). `help` — исключение: если в модуле определена своя команда `help`, вызывается она.

```bash
futi docker build --tag myapp        # namespace=docker, command=build
futi dev deploy staging              # namespace=dev.deploy, command=staging
futi k8s logs --follow -- mypod      # namespace=k8s, command=logs, follow=true, args="mypod"
futi echo_mod echo -- hello world    # namespace=echo_mod, command=echo, args="hello world"
futi echo_mod -- hello world         # default cmd модуля echo_mod, args="hello world"
```

**Stdin, сигналы и код выхода.** Команда модуля наследует stdin futi: пайпы (`echo data | futi m c`) и интерактивные утилиты (`ssh`, `psql`, `docker exec -it`) работают. Ctrl+C (SIGINT) и Ctrl+\ (SIGQUIT) терминал доставляет сразу команде — когда futi запущен на переднем плане терминала, он ждёт её завершения и ничего не делает сам. Без терминала или не на переднем плане (CI, фон, нет управляющего терминала) SIGINT/SIGQUIT, присланные futi (`kill -INT <pid>`, IDE), пересылаются команде. SIGTERM и SIGHUP, полученные futi, пересылаются команде всегда. При сигнале всей группе процессов (`timeout`, `timeout -s INT`, systemd `KillMode=control-group`, `kill -TERM -<pgid>`) команда получит SIGTERM (а без терминала и SIGINT/SIGQUIT) дважды: напрямую и от futi. Если futi переслал команде SIGTERM, SIGHUP или SIGINT, после её завершения он завершается тем же сигналом, даже если команда перехватила сигнал и вышла с 0; после пересланного SIGQUIT код выхода — `128+3`. Код выхода futi для команды модуля равен коду команды; если команда убита сигналом N — `128+N` (например, 143 для SIGTERM); при гибели команды от SIGINT/SIGTERM/SIGHUP futi сам завершается тем же сигналом, поэтому шелл прерывает циклы по Ctrl+C. То же относится к `futi modules *`: если хук (`attach`/`detach`) убит прерывающим сигналом, futi завершается этим сигналом; прочие ошибки `modules`-команд дают код 1. SIGINT и SIGHUP, проигнорированные при запуске futi (`nohup`, фоновая команда шелла), остаются игнорируемыми и для команды (для SIGQUIT и SIGTERM рантайм Go ставит свой обработчик, наследования нет).

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

Логика: если namespace резолвится как модуль — `list` показывает его команды. Если как namespace-директория — показывает непосредственных детей (вложенные namespaces и модули). `list` зарезервирован — команда с именем `list` (на любом уровне вложенности) в модуле — ошибка схемы.

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

### Справка через `help`

`help` — контекстная справка на любом уровне, без выхода в README:

```bash
futi demo help                 # справка по модулю
futi demo build help           # справка по команде
futi demo migrate help         # справка по ветке команд
```

Модульная справка — описание, default cmd и список команд:

```bash
$ futi demo help
demo — Demo module
default: echo default
commands:
  build    Build the project
  migrate  Database migrations
```

Командная справка — USAGE с параметрами (тип, required, default, описание),
позиционные требования и итоговый шаблон команды:

```bash
$ futi demo build help
demo build — Build the project

USAGE
  futi demo build [--tag value] [--file value] [--push] [-- args]

PARAMETERS
  --tag   (string, required)  Image tag
  --file  (string, default: Dockerfile)  Dockerfile path
  --push  (bool, default: false)

CMD
  docker build -t {{.tag}} -f {{.file}} .
```

`help` подсказывается автодополнением на каждом уровне (модуль/команда/
subcommand). Если в модуле определена собственная команда `help` — вызывается
она, системная справка не перехватывает user-defined имена.

---

## Валидация

`futi modules validate <ns>` проверяет схему модуля без вызова команды — полезно при разработке модуля, в CI или перед публикацией.

```bash
$ futi modules validate docker
OK
```

Невалидный модуль выводит отчёт со всеми ошибками и предупреждениями за один проход:

```bash
$ futi modules validate bad
ERROR: command "empty": must have cmd or commands
ERROR: command "build": param "tag" has invalid type "float" (valid: string, bool, int)
ERROR: command "build": duplicate param "tag"
modules: validate: module "bad" has 3 error(s)
$ echo $?
1
```

Предупреждения не блокируют (exit code 0):

```bash
$ futi modules validate db
WARNING: command "migrate": has subcommands but no cmd (calling without subcommand will fail)
$ echo $?
0
```

### Что проверяется

**Ошибки** (блокируют загрузку модуля, `futi <ns> <cmd>` упадёт с понятным сообщением):

- неизвестный ключ манифеста (с номером строки; такие ошибки выводятся первыми)
- модуль без `cmd` и без `commands`
- команда без `cmd` и без `commands` (бесполезная команда); `cmd` из одних пробелов считается пустым
- `params` или `positional.min` без `cmd` — на любом уровне (модуль, команда); параметры не наследуются подкомандами
- синтаксическая ошибка шаблона в `cmd`, `attach`, `detach` (проверяется только разбор, имена переменных — нет)
- недопустимое имя команды в `commands`: пустое, начинается с `-`, содержит `.`, пробельные или управляющие символы
- команда с именем `list` (на любом уровне) — недостижима, `list` перехватывается как системный запрос
- дубликаты имён `params` в команде
- `type` параметра не из `{string, bool, int}` (пустой = `string`)
- имя параметра зарезервировано (`args`, `args_list`, `env`, `module_name`, `module_root`) — иначе оно перекрылось бы служебной переменной шаблона
- имя параметра не соответствует `^[A-Za-z_][A-Za-z0-9_-]*$` (в т.ч. пустое)

**Предупреждения** (не блокируют, показываются только в `validate`):

- команда с `commands`, но без `cmd` — вызов без subcommand падает в runtime

Валидация выполняется рекурсивно для всех вложенных `commands`. Порядок ошибок детерминирован: неизвестные ключи, затем `attach`/`detach` и пустой модуль, затем `(default)`, затем команды по алфавиту (родитель раньше вложенных). При обычном вызове (`futi <ns> <cmd>`) валидация запускается автоматически при загрузке модуля — `futi modules validate <ns>` нужен для просмотра **всех** ошибок за один проход и для warnings (которые в runtime не показываются).

---

## Создание модулей

`futi modules init` создаёт YAML-модуль с шаблоном — не нужно копировать из README вручную. Модуль сразу доступен через auto-discovery. Сегменты пути не содержат точку (она — разделитель namespace); если уже есть `<path>/config.yaml`, init отказывает (иначе `.yaml` и `/config.yaml` дали бы конфликт).

```bash
futi modules init mytool                          # ./.futi/mytool.yaml
futi modules init dev/mytool                      # ./.futi/dev/mytool.yaml (namespace dev)
futi modules init mytool --global                 # ~/.config/futi/modules/mytool.yaml
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
$ futi modules init mytool
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
$ futi modules init dev/apitool --desc "API utilities" --default-cmd "echo api" --command fetch --command build
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

## Управление модулями

Установка модулей из любого источника — локальный путь, git-репозиторий, web-архив:

```bash
# из локального каталога
futi modules install ~/dev/my-modules/qr

# из git (branch/tag через --ref)
futi modules install https://github.com/foo/qr-mod.git --ref v1.2.0

# из архива (.zip / .tar.gz)
futi modules install https://example.com/qr-mod.tar.gz

# в namespace и локально в проект
futi modules install ~/dev/qr --namespace images
futi modules install ~/dev/qr --local
```

Одиночный `.yaml` при установке оборачивается в пакет: создаётся каталог `<module>/config.yaml`; имя без `.yaml`/`.yml` берётся из `name:` манифеста или из имени источника, оно не должно содержать точку, а рядом не должно лежать одноимённого `<module>.yaml`. Флаги install: `--namespace <ns>`, `--ref <git-ref>`, `--local` (в `./.futi/` вместо глобального каталога), `--skip-attach`, `--yes` / `-y` (запустить attach-хук без вопроса), `--from-local` / `--from-git` / `--from-archive` (переопределить эвристику определения типа источника).

Сетевые источники — только `https://`: `http://`- и `git://`-адреса отвергаются, редирект с https на http запрещён (и для архива, и для git); для git-репозитория скачайте модуль вручную и установите из локального пути, для архива — скачайте, распакуйте и установите каталог (локальный `.tar.gz`/`.zip` install не принимает). Скачивание архива ограничено 100 MiB и 5 минутами. Распаковка ограничена 500 MiB суммарно и 10000 записями (файлы и каталоги); symlink и прочие не-regular записи в `.zip` и `.tar.gz` пропускаются, а права файлов приводятся к `rwx` (без setuid/setgid/sticky, владелец всегда может читать и писать). Источник и `--ref` не должны начинаться с `-`.

Модуль может объявлять setup/teardown-хуки — `futi` выполнит их при install/update и uninstall:

```yaml
attach: ".venv/bin/python -m pip install -r requirements.txt"
detach: "rm -rf .venv"
```

Хуки выполняются в каталоге модуля; доступны `{{.module_name}}`, `{{.module_root}}`, `{{.env.FOO}}`, `{{quote ...}}` и переменные окружения FUTI_MODULE_NAME / FUTI_MODULE_ROOT. Хуки наследуют stdin futi и выполняются без таймаута: зависший хук прерывается Ctrl+C. Без управляющего терминала (CI, cron, ssh без tty) хук запускается в своей группе процессов: сигналы SIGINT, SIGQUIT, SIGTERM и SIGHUP, полученные futi, пересылаются всем процессам хука, включая порождённые составной командой (`sh -c`). При управляющем терминале хук остаётся в группе futi (интерактивные хуки работают) — в том числе когда futi запущен в фоне (`&`) или с перенаправленным вводом (`</dev/null`): перенаправление и фон изоляцию не включают; при адресном `kill -TERM <futi>` процессы, порождённые хуком, могут остаться. Изолированный хук (без управляющего терминала) не погибает от сигнала, посланного группе futi (`kill -9 -<pgid>`, `timeout -s KILL futi …`): его группа отдельная. Провал `attach` при install откатывает установку (модуль удаляется); при update — только предупреждение, новое дерево сохраняется. Провал `detach` при uninstall — предупреждение (или отказ с `--strict-detach`).

`attach` из скачанного манифеста — чужой код, поэтому install и update перед его запуском спрашивают подтверждение (в stderr, шаблон показывается как есть, без подстановки значений):

```
Module qr runs an attach hook (in /home/user/.config/futi/modules/qr):
  .venv/bin/python -m pip install -r requirements.txt
Run it? [y/N]
```

Вопрос задаётся до изменений на диске: при отказе install не устанавливает модуль, update оставляет старую версию (`update` без имени спрашивает про каждый модуль; отказ — ошибка этого модуля, остальные обновляются). Если stdin не терминал (CI, скрипты, пайп), futi не спрашивает и завершается ошибкой; если ответа нет (`</dev/null`, `docker run` без `-i`, systemd `StandardInput=null`) — тоже ошибка. В обоих случаях укажите `--yes` (`-y`), чтобы запустить хук, или `--skip-attach`, чтобы пропустить; `--skip-attach` важнее `--yes`. Модуль без `attach` и update без изменений источника подтверждения не требуют.

Каждая установка пишет sidecar `__futi_meta.yaml` рядом с config.yaml (источник, тип, ref/commit, hash дерева, даты) — по нему работают:

```bash
futi modules list              # установленные модули
futi modules info qr           # детали установки
futi modules update            # re-fetch + re-attach (все модули)
futi modules update qr         # один модуль
futi modules uninstall qr      # detach + удаление (с подтверждением)
futi modules path qr           # каталог модуля (для скриптов)
```

`update` сравнивает hash дерева источника с lockfile: без изменений — no-op (обновляется только `last_check`), иначе — атомарная замена дерева с сохранением `installed_at` и `about`. При сбое переноса или записи lockfile update восстанавливает старое дерево. Если от прерванного update остался `<module>.futi-backup`, update отказывает и просит разобрать backup вручную; `list` и `update` такой каталог модулем не считают (update без имени лишь предупреждает о нём в stderr). Флаги update: `--skip-attach`, `--yes` / `-y`.

Удаление требует подтверждения (размер и число файлов); без sidecar (модуль создан вручную или через `modules init`) — только с `--force`. Флаги uninstall: `--yes`, `--force`, `--skip-detach`, `--strict-detach`.

Управление командами модуля без редактора:

```bash
futi modules add_command qr scan --desc "Scan a QR code"
futi modules remove_command qr scan
```

Удалить единственную команду модуля без `cmd` нельзя — получился бы пустой модуль; `remove_command` откажет и оставит манифест без изменений.

**Известное ограничение:** если процесс futi упадёт (краш) посреди install/update, может остаться частично скопированное дерево без sidecar. Откат работает при ошибках attach и при провале валидации, но не при краше процесса — такой каталог можно удалить вручную или через `futi modules uninstall <name> --force`.

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

### `futi cache status` / `futi cache clear`

```bash
$ futi cache status
Policy: every_time
Last refresh: 2026-09-14 17:24:54
Modules: 5
Cache file: /home/user/.cache/futi/modulecache.gob

$ futi cache clear
Cache cleared: /home/user/.cache/futi/modulecache.gob
```

`status` показывает активную политику, время последнего обновления, число
модулей в кэше и путь к файлу. `clear` удаляет persistent кэш — при следующем
запуске он будет перестроен с нуля (полезно при странном поведении или после
смены версии futi).

### Целостность кэша

Кэш хранит SHA-256 чексумму манифеста каждого модуля. При **исполнении**
модуля чексумма сверяется с файлом на диске: если манифест изменился или
запись в кэше не совпадает с файлом — модуль перечитывается с диска, а не
берётся из кэша. Это защищает от двух сценариев: устаревший кэш (файл
изменился после Refresh) и повреждённый/подменённый gob-файл. Быстрые
операции (`list`, completion, help) работают из кэша без сверки — скорость
важнее.

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

При повреждённом gob-файле — кэш автоматически перестраивается с пустого состояния. При несовпадении версии схемы (несовместимый формат файла кэша) — аналогично. Кэш также перестраивается при каждом обновлении или пересборке бинаря futi: файл кэша привязан к идентичности бинаря (mtime и размер), чужой кэш игнорируется без сообщений. При политике `force_only` после обновления futi персистентный кэш пуст до `futi cache refresh` (модули при этом читаются с диска напрямую).

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
- **Все параметры на команде** — `futi docker build <TAB>` сразу показывает все флаги с пометками: `--tag (required)`, `--force (default: false)` (метки видны в zsh, bash/fish отрезают)
- **После значения флага** — `futi images qr make --out qr.png <TAB>` подсказывает оставшиеся флаги (`--text`, `--scale`); значение флага не ломает подсказки. Путь (namespace и команда) всегда идёт до первого флага, поэтому после флагов слова пути и `help` не предлагаются
- **Пути в значениях флагов** — при вводе значения не-bool флага (`--out ~/<TAB>`, `--out=~/<TAB>`) shell предлагает файлы/директории; в будущем — только для параметров типа `path` (ROADMAP)
- **Параметры default-команды** — модуль-однокоманда (`cmd` + `params`, без `commands`) подсказывает свои флаги: `futi meta <TAB>` → `--json`
- **После bool-флага** — `futi docker build --force <TAB>` подсказывает оставшиеся (ещё не введённые) флаги (`--tag`); значения `true`/`false` не предлагаются: bool-флаг значения не берёт, `false` задаётся как `--force=false`
- **Builtins** — `futi cache <TAB>` → `refresh`/`status`/`clear`; `futi completion <TAB>` → `bash`/`zsh`/`fish`
- **Валидация модуля** — `futi modules validate <TAB>` подсказывает полные имена модулей (`dev.deploy`), после первого аргумента подсказок нет
- **Приоритет builtins** — на корне `cache`, `completion`, `list`, `modules` подсказываются как builtins даже при одноимённом модуле (CLI исполнит builtin); `futi cache <TAB>` не показывает команды такого модуля
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

## Безопасность

### Модель доверия

**Модуль — это код.** Кто может писать в каталог модулей (или в конфиг с
источниками), тот и определяет, какие команды исполняются — это осознанный
дизайн, как у make, npm scripts или shell-алиасов. futi не пытается
сандбоксить модули от их владельца. Следствия:

- Симлинки в каталоге модулей разрешены (dotfiles-менеджеры, dev-симлинки) —
  контроль каталога и так даёт полный контроль.
- `futi modules init` пишет только внутри каталога модулей и показывает реальный
  (резолвнутый) путь записи.
- `futi modules install` и `futi modules update` показывают `attach`-хук
  устанавливаемого модуля и запускают его только после подтверждения (или с
  `--yes`); без терминала на stdin и без `--yes` — отказ.
- Публичный registry скачанных модулей (см. ROADMAP) потребует пересмотра
  этой модели — доверие к чужим модулям нельзя будет неявно унаследовать.

### Исполнение без shell

Команды исполняются через `exec` с готовым argv — **без** оболочки.
Метасимволы `;`, `$()`, `` ` `` из параметров не выполняются как shell-код
(проходят как обычный текст в аргументах). Нужен shell — пишите его явно
(`sh -c '...'`), передавая пользовательский ввод позиционным аргументом
с `{{quote}}` (см. примеры).

### `{{quote}}` для пользовательских значений

Значения параметров проходят повторный разбор (shlex) при сборке argv.
Вставка `{{.param}}` в кавычках (`"{{.msg}}"`) может быть «пробита» значением
с кавычкой — оно выйдет за кавычки и станет отдельными аргументами
(flag-инъекция). Для значений в опасных позициях используйте `{{quote .param}}` —
гарантия ровно одного аргумента (см. [пробелы и кавычки](#важно-пробелы-и-кавычки-в-значениях)).

### Секреты — через окружение

Токены и ключи передавайте через `{{.env.NAME}}`, а не через `--param`:
параметры видны в `ps` и истории shell, окружение процесса — нет.

### Целостность кэша

Persistent кэш сверяет SHA-256 чексумму манифеста при исполнении модуля —
устаревший или подменённый кэш не приведёт к исполнению чужой команды
(см. [Кэш модулей](#кэш-модулей)).

---

## Примеры

### Echo-модуль (args + default cmd)

```yaml
# ~/.config/futi/modules/echo_mod.yaml
name: echo_mod
description: Echo utilities
cmd: "echo {{.args}}"           # команда по умолчанию (без shell)
commands:
  upper:
    description: Uppercase echo
    # futi исполняет argv БЕЗ shell: pipes, $() и циклы не работают напрямую.
    # Нужен shell — пишите его явно (sh -c). Пользовательский ввод передавайте
    # позиционным аргументом sh ($1) с {{quote}}, чтобы кавычки в значении
    # не ломали скрипт и не инжектили код.
    cmd: "sh -c 'echo \"$1\" | tr a-z A-Z' sh {{quote .args}}"
  repeat:
    description: Repeat first arg N times
    params:
      - name: count
        type: int
        required: true
    cmd: "sh -c 'for i in $(seq 1 {{.count}}); do echo \"$1\"; done' sh {{quote (index .args_list 0)}}"
```

```bash
futi echo_mod -- hello world         # echo hello world (default cmd)
futi echo_mod upper -- hello world   # HELLO WORLD (через sh -c | tr)
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
    cmd: "sh -c 'git fetch origin && git reset --hard origin/\"$1\"' sh {{quote .branch}}"

  squash:
    description: Squash last N commits
    params:
      - name: count
        type: string
        required: true
      - name: message
        type: string
        required: true
    cmd: "sh -c 'git reset --soft HEAD~\"$1\" && git commit -m \"$2\"' sh {{quote .count}} {{quote .message}}"
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
    cmd: "sh -c 'docker container prune -f && docker image prune -f'"

  shell:
    description: Shell into running container
    params:
      - name: container
        type: string
        required: true
    cmd: "docker exec -it {{.container}} sh"

  build-all:
    description: Build all images in directory
    cmd: "sh -c 'for d in */; do docker build -t \"${d%/}\" \"$d\"; done'"
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
      - name: target
        type: string
        required: true
    cmd: "./scripts/rollback.sh {{quote .target}}"
```

```bash
futi deploy staging --version v1.2.3
futi deploy production --version v1.2.3
futi deploy rollback --target production
```

---

## ROADMAP

futi находится на стадии PRE PRE MVP. Ниже — направления развития.

### Ближайшие планы

- **Типы параметров** — расширение за пределы `int`: `file`, `json`, `secret` (безопасная передача через env, не args; базовый доступ к env vars уже есть через `{{.env.FOO}}`). Для `file`/`path`-типов автокомплит будет предлагать пути только им (сейчас — всем не-bool флагам)
- **Гибридный args-парсинг** — позиционные args для листовых команд без явного `--`
- ~~**Shell completion** — автодополнение для bash/zsh/fish~~ ✅ реализовано (см. раздел «Автодополнение»)
- ~~**Help-система** — модульная/командная справка через `help`~~ ✅ реализовано (`futi <ns> help`, `futi <ns> <cmd> help`; user-defined `help` выигрывает)
- **Descriptions в bash/fish** — zsh выводит описания уже сейчас; bash/fish — roadmap
- **PowerShell** — 4-й shell после стабилизации bash/zsh/fish
- **Модульный completion** — модули сами предоставляют подсказки для автодополнения (динамические кандидаты: имена контейнеров, веток и т.п.); тогда же имя `completion` может быть отдано модулям через smart-роутинг (`futi completion bash|zsh|fish` остаётся builtin-генерацией скрипта, остальные вызовы — резолв модуля). Сейчас `completion`/`__complete` зарезервированы (см. `futi modules init`)

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

- **MCP** — интеграция с AI-агентами (Claude, GPT и т.п.): команды модулей становятся tools,
  схема параметров — их input schema. Основной сценарий — **удалённый**: futi развёрнут на
  машине как сервер, удалённый агент подключается по сети (MCP over streamable HTTP) и
  запускает команды на этой машине. Локально — `futi mcp` по stdio
- **HTTP API** — REST для CI/CD и автоматизации, на том же сервере, что и MCP
- **Безопасность сервера** — по умолчанию слушает только localhost; токены, права по
  namespace (allowlist), лимиты вывода и времени, аудит-лог
- **Стрим вывода** (SSE/WebSocket) — позже

```bash
futi serve --addr 127.0.0.1:8080            # форма команды предварительная
curl -X POST http://localhost:8080/api/v1/exec -H "Authorization: Bearer $TOKEN" \
  -d '{"namespace": "docker", "command": "build", "params": {"tag": "myapp"}}'
```

### Менеджер модулей

Базовый жизненный цикл реализован — `futi modules install/uninstall/update` из
локальных путей, git и архивов (см. [Управление модулями](#управление-модулями)).
Осталось — registry:

```bash
futi modules install docker        # из registry (по короткому имени)
futi modules check                 # проверить обновления
```

### Дополнительно

- **Module versioning** — версии модулей, совместимость
- **Project overrides** — переопределение модулей на уровне проекта (приоритет над глобальными)
- **Config linting** — расширение валидации схем (базовая валидация через `futi modules validate` уже есть): подсказки по best practices, deprecated-поля, стилистика
- **TUI** — интерактивный просмотр и вызов модулей
- **Module registry** — публичный реестр шаримых модулей

---

## Лицензия

MIT
