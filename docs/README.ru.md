[English](../README.md) | Русский

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
- [Входы и шаблонизация](#входы-и-шаблонизация)
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

Поддерживаемые платформы: Linux и macOS. Windows: собирается, но не поддерживается.

Через Go toolchain:

```bash
go install github.com/tivjnb/futi/cmd/futi@latest
```

Готовые бинари (linux и darwin, amd64 и arm64) публикуются на странице GitHub Releases вместе с файлом `checksums.txt`. Скачайте архив для своей платформы, сверьте его с `checksums.txt` и положите `futi` в каталог из `PATH`.

Сборка из исходников:

```bash
git clone https://github.com/tivjnb/futi futi
cd futi
go build -o futi ./cmd/futi
```

Проверка:

```bash
futi --version
futi list
# No modules found.
```

futi хранит модули в `~/.config/futi/modules/` (при наличии `config.yaml` модули читаются из путей, указанных в конфиге; `modules install` при этом всё равно пишет в этот каталог). Переопределить каталог для fallback без конфига:

```bash
export FUTI_MODULES_DIR=/path/to/modules
```

Автодополнение для bash, zsh и fish включается по разделу [Автодополнение](#автодополнение).

---

## Быстрый старт

Создайте шаблон модуля в текущем проекте (он попадёт в `./.futi/`):

```bash
futi modules init hello --desc "Demo module" --command greet
```

Модуль из `futi modules init` создаёт команды-заглушки без входов:

```bash
$ futi hello greet
greet
```

Входы дописываются в YAML вручную (см. [Структура модуля](#структура-модуля)). Замените содержимое `.futi/hello.yaml`:

```yaml
name: hello
description: Demo module
commands:
  greet:
    args:
      - name: name
        required: true
    cmd: "echo hello {{.name}}"
```

Вызовите и посмотрите список:

```bash
$ futi hello greet world
hello world

$ futi list
┌────────┬───────┬─────────────┐
│  TYPE  │ NAME  │ DESCRIPTION │
├────────┼───────┼─────────────┤
│ module │ hello │ Demo module │
└────────┴───────┴─────────────┘
```

Посмотреть собранную команду, не запуская её, можно через `--dry-run`:

```bash
$ futi --dry-run hello greet 'big world'
echo hello 'big world'
```

Готовые рабочие модули лежат в [`examples/`](../examples/); команды `cp` ниже работают из клона репозитория (в релизные архивы `examples/` не входит):

```bash
mkdir -p ~/.config/futi/modules
cp -r examples/git.yaml examples/docker.yaml examples/files.yaml examples/backup ~/.config/futi/modules/
futi list
```

---

## Структура модуля

Модуль — это YAML-файл с описанием команд. Команда принимает **args** (позиционные слова, `--` не нужен) и **flags** (`--name value`):

```yaml
name: docker                    # опционально, по умолчанию = имя файла
description: Docker management  # опционально
cmd: "docker ps"                # опционально: команда по умолчанию (без явной команды)

commands:
  # Простая команда без входов
  ps:
    cmd: "docker ps"

  # Команда с аргументом и флагами
  build:
    description: Build Docker image
    args:
      - name: context
        default: "."
        description: Build context directory
    flags:
      - name: tag
        short: t                # -t
        required: true
      - name: file
        default: Dockerfile
    cmd: "docker build -t {{.tag}} -f {{.file}} {{.context}}"

  # Обязательный аргумент и bool-флаг
  logs:
    args:
      - name: container
        required: true
    flags:
      - name: follow
        type: bool
        short: f
    cmd: "docker logs {{if .follow}}-f{{end}} {{.container}}"

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

```bash
futi docker build --tag myapp          # docker build -t myapp -f Dockerfile .
futi docker build ./ctx -t myapp       # args и флаги в любом порядке
futi docker logs -f web                # docker logs -f web
```

### Поля

Узел (модуль или команда) имеет такие поля:

| Поле | Тип | Описание |
|---|---|---|
| `name` | string | Только модуль. Имя модуля (опционально, по умолчанию = имя файла без `.yaml` или каталога с `config.yaml`; для `futi.yaml` — `futi`). Это значение доступно как `{{.module_name}}` и `FUTI_MODULE_NAME` в `cmd` и в хуках `attach`/`detach` |
| `description` | string | Описание (опционально, показывается в `list` и help) |
| `cmd` | string или list | Шаблон команды (см. [Шаблонизация](#шаблонизация)). У модуля — команда по умолчанию; у команды с `commands` — fallback |
| `args` | list | Позиционные входы |
| `flags` | list | Входы, передаваемые как `--name` |
| `commands` | map | Вложенные команды (узел может иметь `cmd`, `args` и `commands` одновременно) |
| `attach`, `detach` | string или list | Только модуль. Хуки install/uninstall (см. [Управление модулями](#управление-модулями)) |

`args` и `flags` узла относятся только к его собственному `cmd`: подкоманды их не наследуют. `args` и `flags` на уровне модуля описывают команду модуля по умолчанию.

Вход (элемент `args` или `flags`) — объект с `name`; сокращённой записи нет:

| Поле | Где | По умолчанию | Описание |
|---|---|---|---|
| `name` | оба | — | Обязательно. `[a-z][a-z0-9_]*`, уникально среди args и flags узла. В CLI `_` превращается в `-` (`ignore_case` — это `--ignore-case`); в шаблоне — `{{.ignore_case}}`. Имена `help`, `env`, `module_root`, `module_name` зарезервированы |
| `type` | оба | `string` | `string`, `int` или `bool`; `bool` — только у flags |
| `required` | оба, кроме bool | `false` | Вход обязателен. Вместе с любым `default` (даже пустым) — ошибка схемы. У variadic/rest — минимум одно значение |
| `default` | оба, кроме variadic/rest | — | Строка; для `int` — целое, для `bool` — одно из `true/false/1/0/yes/no/on/off` |
| `description` | оба | — | Показывается в help и completion |
| `short` | flags | — | Одна латинская буква, регистр значим (`-f` ≠ `-F`), уникальна в узле; `h` зарезервирована. Допустима у variadic-флага |
| `variadic` | оба | `false` | Args: несколько слов подряд. Flags: флаг можно повторять, значения копятся. Не для `bool` |
| `rest` | args | `false` | Только последний arg: забирает все оставшиеся токены дословно (см. [Rest](#rest-оставшиеся-токены)) |
| `allow_dash` | оба, кроме bool | `false` | Снимает [защиту от значений с ведущим `-`](#значения-с-ведущим--) |

Args команды имеют одну из трёх форм: обязательные, затем необязательные (`req* opt*`); обязательные, один `variadic`, обязательные (`req* variadic req*`); обязательные, затем один `rest` (`req* rest`). Перед `variadic`/`rest` могут стоять только обязательные args; необязательный arg может идти после обязательных и иметь `default` (`context` выше по умолчанию `.`).

```yaml
# meta.yaml — модуль как одна команда со схемой
name: meta
cmd: "python3 scripts/meta.py {{if .json}}--json{{end}} --files {{.files}}"
args:
  - name: files
    variadic: true
    required: true
flags:
  - name: json
    type: bool
```

```bash
futi meta --json photo.jpg scan.png
```

Неизвестный ключ манифеста (например, опечатка `requird` во входе) — ошибка схемы с номером строки: `line 9: unknown key "requird" in commands.build.flags[0]`. Ключи с префиксом `x-` разрешены на любом уровне структуры и игнорируются — под них удобно заводить YAML-якоря (`x-common: &c {…}` и `<<: *c`). Исключение: ключ внутри `commands` — это имя команды, и `x-build` там обычная команда.

> **Старый формат.** `params`, `positional`, `{{.args}}`, `{{.args_list}}` и `{{quote …}}` больше не существуют. Модуль, который их использует, не загружается; ошибка указывает на замену (например, `"params" was replaced by "args"/"flags" (see README)`). Параметры переносятся в `flags` (или в `args`, если они были позиционными), `{{quote .x}}` заменяется на `{{.x}}`, а `{{.args}}` — на arg с `rest` или `variadic`.

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

**Конфликт имён:** если для одного namespace существуют и `.yaml`, и `/config.yaml` — ошибка. Уберите одно из двух. `futi list` такую пару не скрывает: показывает одну запись `x` с пометкой `conflict: x.yaml and x/config.yaml`, а `futi x` завершается ошибкой конфликта.

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

Несколько источников с одинаковым mountpoint **объединяются** — модули из всех источников видны в одном namespace. Если два источника дают модуль с одинаковым именем — это конфликт: `futi list` показывает модуль с пометкой `conflict: module in several sources: <каталоги sources>`, а `futi x` завершается ошибкой конфликта.

Несуществующий или нечитаемый каталог source не ломает остальные: модули остальных sources работают, а в stderr выводится `warning: …` с путём и причиной (при политике кэша `every_time` — при каждом запуске). Автодополнение в таком случае молчит, а кэш при частичном листинге не очищается. Без `config.yaml` отсутствующий каталог модулей (свежая установка) — не ошибка и предупреждений не даёт.

Пустые namespace создаются неявно: если источник монтируется на `ns1.ns2`, то `ns1` и `ns2` существуют как namespaces, даже без собственных модулей.

Если конфиг-файл отсутствует — futi использует один каталог `~/.config/futi/modules/` (или `FUTI_MODULES_DIR`).

---

## Входы и шаблонизация

### Передача входов

```bash
futi docker build --tag myapp --file Dockerfile.dev ./ctx
futi docker build ./ctx -t myapp           # args и флаги в любом порядке
futi docker logs --follow web
futi docker logs web --follow=false
futi k8s logs -- -weird-pod-name           # после "--" всё — args
```

Командная строка: `futi [--dry-run] <путь…> [args и флаги…]`. Токены после пути читаются слева направо:

- `--name value`, `--name=value` — флаг со значением `string`/`int`; `-s value` — его короткая форма (задаётся полем `short`). Короткие флаги не склеиваются (`-fi` — ошибка с подсказкой `-f -i`) и не принимают `=` (`-f=x` — ошибка)
- `--bool-flag` — `bool`-флаг равен `true`; `--bool-flag=false` — явное значение (`true/false/1/0/yes/no/on/off`). Голый bool-флаг следующее слово никогда не съедает: `--force false` — ошибка (`bool flag --force takes no separate value; use --force=false`); чтобы передать arg `false`, поставьте его после `--`
- значение не-bool флага — следующий токен, **если он не флаг этой команды и не `--`**: `--tail -5` и `-m -x` дают значения `-5` и `-x` (если `-x` не флаг команды), а `--message --amend` при объявленном `--amend` — ошибка: `flag --message requires a value (got --amend; use --message=--amend)`. Нет значения — `flag --message requires a value`
- `-5` и `-3.2` (минус и число) — никогда не флаги: это обычные значения
- любой другой токен на `-` — неизвестный флаг: `unknown flag --x for "futi k8s logs"` (с подсказкой похожего имени)
- всё остальное — arg: args заполняются по порядку объявления
- `--` делает все следующие токены args (флаги и `-h` не распознаются). futi поглощает **не больше одного** `--`; следующие — обычные значения
- `-h` / `--help` до `--` и вне позиции значения флага показывает справку команды
- повтор не-variadic флага: побеждает последний; `variadic`-флаг копит значения по порядку
- пустое значение `""` — это значение: для необязательного string-arg или флага это пустой аргумент; для `required` и `int` — ошибка (`empty value for --tag`)

После разбора всей строки: применяются defaults, пропущенный обязательный вход — ошибка (`missing argument <pod>`, `missing required flag --tag`), `int` должен быть целым, лишнее слово — `unexpected argument "extra" (futi k8s logs takes 1 argument: <pod>)` (с подсказкой `--x=…`, если предыдущий токен — bool-флаг).

`--dry-run`, `-h/--help` и `--version` самого futi распознаются только **до** пути. После пути все флаги принадлежат команде: `futi k8s logs x --dry-run` завершится ошибкой `unknown flag --dry-run`, если у команды нет такого флага.

#### Подкоманда или arg

Если у узла есть и подкоманды, и args, первое слово после пути, совпавшее с именем подкоманды, — это **подкоманда**. Чтобы передать такое значение как arg, поставьте его после `--`: `futi db migrate -- up`. Подкоманда распознаётся только сразу после пути: если первым идёт флаг, `--` или другое слово, всё дальнейшее — входы узла (`futi db migrate --verbose up`: `up` — arg команды `migrate`). Поэтому добавление подкоманды может изменить смысл строки вроде `futi echo build` (раньше это было значение, теперь подкоманда); об узле с подкомандами и string-arg предупреждают `futi modules validate`, справка узла и `futi modules update`.

Модуль с командой по умолчанию `cmd` и командами `logs`, `ctx` по-прежнему может служить шорткатом: `futi k logs pod` запускает команду, `futi k get pods` — `kubectl get pods` из команды по умолчанию.

#### Variadic

```yaml
commands:
  copy:
    args:
      - name: src
        variadic: true
        required: true
      - name: dst
        required: true
    cmd: "cp -- {{.src}} {{.dst}}"
```

`futi files copy a.txt b.txt dir/` — `src` берёт столько слов, сколько остаётся после резервирования хвостовых обязательных (`a.txt b.txt`), `dst` — `dir/`. Variadic-флаг повторяется: `--env A=1 --env B=2`.

#### Rest: оставшиеся токены

Arg с `rest` передаёт оставшиеся токены обёрнутой утилите **дословно**, включая флаги и `-h`:

```yaml
commands:
  run:
    args:
      - name: image
        required: true
      - name: docker_args
        rest: true
    flags:
      - name: rm
        type: bool
    cmd: "docker run {{if .rm}}--rm{{end}} {{.image}} {{.docker_args}}"
```

```bash
futi docker run nginx -it --rm -p 80:80    # docker run nginx -it --rm -p 80:80
futi docker run --rm nginx -it             # --rm — флаг самого "run"; утилита получает: nginx -it
futi docker run nginx -- -it               # то же, что: futi docker run nginx -it
```

Rest начинается сразу после токена, заполнившего последний arg перед ним (а если `rest` — единственный arg, то на первом токене, который не флаг, или после `--`). Флаги команды ставятся **до** него. `--` поглощается, только если это самый первый токен rest и до него `--` не поглощался; любой следующий `--` — значение: `futi docker run nginx -- -it` даёт `-it`, `futi docker run nginx -it -- x` даёт `-it -- x`, а `futi docker run -- nginx -- -it` — `nginx -- -it`. До старта rest неизвестный флаг — ошибка, после — часть rest: поэтому добавление флага к команде никогда не меняет смысл строки, которая была валидной. Rest на ведущий `-` не проверяется.

### Шаблонизация

`cmd` — шаблон в синтаксисе Go `text/template`, но разбирает и вычисляет его сам futi (см. [`cmd` и `text/template`](#cmd-и-texttemplate)). Результат — список argv: команда запускается без shell.

| Синтаксис | Описание |
|---|---|
| `{{.name}}` | Значение arg или flag |
| `{{.env.FOO}}` | Переменная окружения `FOO` (ошибка исполнения, если не задана) |
| `{{.module_root}}`, `{{.module_name}}` | Каталог и имя модуля |
| `{{if .follow}}…{{else if .x}}…{{else}}…{{end}}` | Условный блок |
| `{{range .files}}…{{.}}…{{end}}` | Повтор для каждого значения `variadic`-входа |

Условие `if` — это `.x`, `.env.X` или `eq A B`, `and`, `or`, `not` над ними и строковыми/числовыми литералами (`{{if and .a (not .b)}}`, `{{if eq .mode "fast"}}`). `eq` сравнивает строковые формы (`int` — по десятичной записи); незаданный вход в `eq` — false; variadic/rest в `eq` — ошибка загрузки.

**Две формы `cmd`:**

- **Строка** — пробельные символы (пробел, табуляция, перевод строки) вне кавычек разделяют аргументы; многострочный `>-`/`|` безопасен и не даёт пустых аргументов. Кавычки `'…'`/`"…"` в литеральном тексте только группируют (без `$`, без экранирования, кроме `\"` и `\\` внутри `"…"`); `{{…}}` внутри кавычек — часть того же аргумента.
- **Список** — каждый элемент — ровно один аргумент; пробелы буквальные, кавычки не интерпретируются. Пустой элемент `""` — пустой аргумент, а элемент `'""'` — литерал из двух символов кавычек (не пустой аргумент).

```yaml
cmd: "docker build -t {{.tag}} ."
cmd: [docker, build, -t, "{{.tag}}", .]
```

**Значение никогда не режется и не разбирается заново.** Скалярное значение вставляется в свой аргумент как есть: пробелы, кавычки и метасимволы — данные. `--msg "hello world"` при `cmd: "echo {{.msg}}"` даёт один аргумент `hello world`. Функции `quote` и ручного квотирования нет.

**Незаданные входы.** Вход без значения и без `default` — *незаданный*:

- аргумент, который упоминает незаданный вход вне `if`, выпадает целиком (`--tail={{.tail}}` исчезает, если `tail` не передан);
- аргумент, упоминающий **два и больше** входов вне `if`, из которых хоть один может быть не задан, — ошибка загрузки: иначе вместе с ним молча пропало бы значение другого входа (`{{.user}}@{{.host}}` с необязательным `user`; пишите `{{if .user}}{{.user}}@{{end}}{{.host}}`);
- аргумент, ставший пустым только из-за пустого `if`, выпадает; ставший пустым из-за значения `""` — остаётся;
- переменная окружения с пустым значением в отдельном аргументе (`"{{.env.FOO}}"`) не даёт аргумента, как незаданный вход; для проверки используйте `{{if .env.FOO}}`.

**Variadic и rest.** Аргумент, состоящий только из variadic-входа (`{{.src}}`), превращается в N аргументов (в ноль — для пустого списка). Variadic в склейке (`--env={{.env_var}}`) — ошибка загрузки: используйте `range`.

```yaml
cmd: "docker run {{range .env_var}}-e {{.}} {{end}}{{.image}}"    # -e A=1 -e B=2 nginx
```

`range` работает только по variadic-входу, без `$i, $v` и без `{{else}}`; его тело может давать любое число аргументов. `rest` допустим только целым аргументом, не в `range` и не в склейке. `cmd`, который вычисляется в пустой список, завершается ошибкой `command rendered to empty argv`.

**Висящий флаг** — ошибка загрузки: литерал вроде `--tail`, за которым стоит аргумент, способный выпасть (необязательный вход без default), съел бы следующее слово. Пишите `{{if .tail}}--tail {{.tail}}{{end}}` или `--tail={{.tail}}`. Для variadic/rest эта проверка не действует.

#### `cmd` и `text/template`

Синтаксис — от шаблонов Go, но futi разрешает лишь его часть и отличается в двух местах:

1. **В строковой форме пробел в литеральном тексте — граница аргумента**, а значение никогда не режется: `echo {{.msg}}` — всегда ровно два аргумента, что бы ни лежало в `msg`. При связке `text/template` + разбор по пробелам значение с пробелами распалось бы на несколько слов.
2. **`if` по не-bool входу проверяет «значение задано»**, а не «не ноль»: `--tail=0` считается заданным (в Go `0` и `""` — false). Для `bool` это истинность значения; variadic истинен, когда список непуст; `.env.X` истинна, если переменная задана и не пуста (в условии незаданная переменная — false, не ошибка).

Всё остальное — ошибка загрузки с подсказкой: `with`, `$var`, `:=`, конвейеры `|`, вызовы функций (`printf`, `quote`, `index`, `len`, `html`, `js`, `call`, …), `define`/`template`/`block`, trim-маркеры `{{-` / `-}}` и комментарии внутри аргумента. Комментарий `{{/* … */}}` допустим только между пробельными символами или у края всего шаблона; у края тела `if`/`range` он отвергается.

### Значения с ведущим `-`

Аргумент, **первый символ которого получен из значения пользователя** (arg или flag, включая элементы `range`), не может начинаться с `-`: обёрнутая утилита прочла бы его как опцию. Это касается и склеек (`{{.user}}@{{.host}}`), и значений `int` (`-5`). Ошибка:

```
value "-rf" for <src> starts with "-" and would be read as an option by cp; use --opt={{.src}}, put a literal -- before it, or set allow_dash
```

(для `git` подсказка упоминает ещё `--end-of-options`). Значения `env`, `module_root` и `module_name` не проверяются. Способы обойти:

- поставить значение после опции через `=`: `--file={{.file}}` — аргумент начинается с литерала автора;
- поставить в `cmd` литерал `--` перед значением: `cp -- {{.src}} {{.dst}}`. Это снимает проверку со всех аргументов после него, **только если каждый из них состоит ровно из одной ссылки на вход** (включая тела `if` и `range`), без литеральных символов. Иначе проверка остаётся: `kubectl exec {{.pod}} -- ls {{.path}}` (литерал `ls`), `rsync -- {{.src}} {{.host}}:{{.dst}}` (литерал `:`) и `ssh {{.host}} -- tail {{.file}}` проверяются по-прежнему. Решение принимается по шаблону, а не по реальным значениям;
- `allow_dash: true` на входе снимает проверку для всех его вхождений (для рецепта с shell ниже).

`rest` не проверяется никогда. Та же проверка действует для CLI и для других фронтендов (MCP/REST), а в JSON Schema превращается в `"not": {"pattern": "^-"}` для строк и `"minimum": 0` для int.

### Переменные окружения в шаблонах

Доступ к переменным окружения через `{{.env.FOO}}` — полезно для секретов (токены, API keys), которые небезопасно передавать флагами (видны в `ps`):

```yaml
commands:
  deploy:
    args:
      - name: target
        required: true
    cmd: "deploy --token {{.env.DEPLOY_TOKEN}} --env {{.target}}"
```

```bash
export DEPLOY_TOKEN=secret123
futi deploy staging
# → deploy --token secret123 --env staging
```

Обращение к несуществующей переменной — ошибка исполнения (fail-fast). Для необязательной переменной используйте условие, где незаданная переменная — просто false:

```yaml
cmd: "deploy {{if .env.VERBOSE}}-v{{end}}"
```

### Команда, которой нужен shell

futi сам никогда не запускает `sh -c`. Если вам действительно нужны пайпы или циклы, пишите shell явно и передавайте пользовательский ввод позиционными параметрами скрипта (`$1`, `$2`), а не внутри текста скрипта:

```yaml
commands:
  upper:
    args:
      - name: text
        required: true
        allow_dash: true         # значение идёт в "$1", sh не читает его как опцию
    cmd: [sh, -c, 'printf "%s\n" "$1" | tr a-z A-Z', sh, "{{.text}}"]
```

`allow_dash` нужен, потому что элемент `"{{.text}}"` — это целое значение, и `-n` иначе был бы отвергнут. `sh` не читает `$1` как опцию (всё после скрипта он берёт как параметры), но команда внутри скрипта может: поэтому в скрипте `printf "%s\n" "$1"`, а не `echo "$1"` (`echo` принял бы `-n` или `-e` за свою опцию). Внутри скрипта всегда квотируйте `"$1"` и используйте `--` или `printf` там, где вызываемая программа это поддерживает.

---

## Модуль-пакет со скриптами

Модуль может содержать собственные скрипты. futi не задаёт `WorkDir` для
команды — она исполняется в cwd вызова (`./photo.jpg` разрешается оттуда).
Ссылайтесь на файлы модуля явно через шаблон `{{.module_root}}`:

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
    args:
      - name: target
        required: true
    cmd: "{{.module_root}}/scripts/deploy.sh {{.target}}"
```

```bash
futi mytool deploy staging
# выполнит ~/.config/futi/modules/mytool/scripts/deploy.sh staging
```

Скрипты вызываются с `WorkDir` = cwd вызова futi. Первый токен cmd
подчиняется обычной Unix-семантике: относительный путь резолвится от cwd,
bare-имя — через `PATH`. Свои скрипты пакета — явно через
`{{.module_root}}/scripts/...` (значение никогда не режется, поэтому модуль
в каталоге с пробелами работает без кавычек).

### `{{.module_root}}` и `{{.module_name}}`

В шаблоне cmd доступны две новые переменные — путь и имя модуля. Они
используются для обращения к файлам модуля из cmd и хуков:

```yaml
cmd: "{{.module_root}}/scripts/{{.name}}.sh"   # .name — вход команды
```

```yaml
# Модуль с собственным venv — путь к интерпретатору от корня модуля:
# ~/.config/futi/modules/images/qr/
# ├── config.yaml
# ├── scripts/make.py
# └── .venv/            # python3 -m venv .venv && .venv/bin/pip install qrcode
commands:
  make:
    args:
      - name: text
        required: true
    cmd: "{{.module_root}}/.venv/bin/python {{.module_root}}/scripts/make.py --text={{.text}}"
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

Относительные пути, переданные пользователем (`--out result.png`, файловые
args), резолвятся ОС от cwd вызова `futi` — естественная Unix-семантика.
Скрипту, которому нужен cwd, достаточно `getcwd()` (он = cwd вызова futi):

```python
import os
out = os.path.join(os.getcwd(), "result.png")
```

> **Migration note (pre-release):** до переработки рабочего каталога futi вызывал команды с
> `WorkDir` = корень модуля и передавал `FUTI_CWD` в env. Это поведение
> удалено. Правки модулей:
>
> - `cmd: "./scripts/x.sh ..."` → `cmd: "{{.module_root}}/scripts/x.sh ..."`
> - `cmd: ".venv/bin/python scripts/x.py ..."` → `cmd: "{{.module_root}}/.venv/bin/python {{.module_root}}/scripts/x.py ..."`
> - `base = os.environ.get("FUTI_CWD") or os.getcwd()` → `base = os.getcwd()`
>   (cwd вызова futi = cwd скрипта, `getcwd()` достаточно)
> - `os.environ["FUTI_CWD"]` для путей модуля → `os.environ["FUTI_MODULE_ROOT"]`

---

## Команды CLI

```bash
futi <namespace...> <command> [args и --flags]   # вызов команды модуля
futi <path...> -h                           # справка по пути (namespace, модуль, ветка или команда)
futi <namespace>                            # команда по умолчанию (если есть cmd на уровне модуля)
futi --dry-run <path...> <command>          # показать команду, не исполняя
futi help <path...>                         # справка по пути
futi list                                   # модули и namespaces в корне
futi list <path...>                         # содержимое namespace или команды модуля
futi list <path...> -v                      # то же, с колонкой PATH
futi list <path...> -r                      # рекурсивный обход поддерева
futi modules validate <namespace>           # валидация схемы модуля
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
futi --version                              # вывести версию
```

Удалить единственную команду модуля без `cmd` нельзя — получился бы пустой модуль; `futi modules remove_command` откажет и оставит манифест без изменений.

Парсинг: путь читается по словам по объединённому дереву всех источников — namespace-каталоги, модули и их команды. Слово продолжает путь, только если точно совпадает с именем ребёнка текущего узла; путь заканчивается на первом токене, начинающемся с `-` (длиннее одного символа), на `--`, на слове, которое не ребёнок, или в конце. Слово с точками делится на сегменты и продолжает путь, только если совпали все: `futi docker.build` = `futi docker build`, а `file.txt` после узла с ребёнком `file` — не слово пути. После пути идут входы команды: args и флаги в любом порядке, а `--` делает остальное обычными args (см. [Входы и шаблонизация](#входы-и-шаблонизация)). Флаги родительского узла подкомандами не наследуются.

Если у итогового узла нет `cmd`, вызов завершается ошибкой: `unknown command "x" for "futi docker" (available: build, run)`, если дальше есть слово, иначе `"futi db" requires a subcommand (available: migrate)`. Если имя подкоманды встречается дальше в строке (`futi db --verbose migrate up`), добавляется подсказка ` (put the full command path before flags)`.

**Коллизия путей**: путь, который называет две сущности сразу, — ошибка, например модуль `dev/deploy.yaml` (`dev deploy`) и команда `deploy` модуля `dev.yaml`: `path "dev deploy" is ambiguous: command "deploy" of module dev (<manifest>) and module dev.deploy (<manifest>)`. Проверяется по всем источникам вместе при резолве пути. Локальный модуль, перекрывающий одноимённый глобальный, — не коллизия.

**Зарезервированные имена**: `help`, `list`, `cache`, `modules`, `completion`, `__complete` — встроенные команды только первым словом, поэтому не могут быть именами модулей и namespace верхнего уровня. Глубже в пути это обычные имена: `futi git help` и `futi git list` вызывают команды `help` и `list` модуля `git`, а `tools/help.yaml` — допустимый модуль. Проверка выполняется при `futi modules init` и `futi modules install` (для `--namespace` и, без namespace, для итогового имени); уже установленные модули не затрагиваются.

futi docker build --tag myapp        # модуль docker, команда build
futi dev deploy staging              # модуль dev.deploy, команда staging
futi k8s logs mypod --follow         # модуль k8s, команда logs, pod=mypod, follow=true
futi echo_mod hello world            # cmd по умолчанию модуля echo_mod, words=[hello, world]
futi echo_mod -- upper               # "--": "upper" — слово, а не подкоманда
```
```

**Stdin, сигналы и код выхода.** Команда модуля наследует stdin futi: пайпы (`echo data | futi m c`) и интерактивные утилиты (`ssh`, `psql`, `docker exec -it`) работают. Ctrl+C (SIGINT) и Ctrl+\ (SIGQUIT) терминал доставляет сразу команде — когда futi запущен на переднем плане терминала, он ждёт её завершения и ничего не делает сам. Без терминала или не на переднем плане (CI, фон, нет управляющего терминала) SIGINT/SIGQUIT, присланные futi (`kill -INT <pid>`, IDE), пересылаются команде. SIGTERM и SIGHUP, полученные futi, пересылаются команде всегда. При сигнале всей группе процессов (`timeout`, `timeout -s INT`, systemd `KillMode=control-group`, `kill -TERM -<pgid>`) команда получит SIGTERM (а без терминала и SIGINT/SIGQUIT) дважды: напрямую и от futi. Если futi переслал команде SIGTERM, SIGHUP или SIGINT, после её завершения он завершается тем же сигналом, даже если команда перехватила сигнал и вышла с 0; после пересланного SIGQUIT код выхода — `128+3`. Код выхода futi для команды модуля равен коду команды; если команда убита сигналом N — `128+N` (например, 143 для SIGTERM); при гибели команды от SIGINT/SIGTERM/SIGHUP futi сам завершается тем же сигналом, поэтому шелл прерывает циклы по Ctrl+C. То же относится к `futi modules *`: если хук (`attach`/`detach`) убит прерывающим сигналом, futi завершается этим сигналом; прочие ошибки `modules`-команд дают код 1. SIGINT и SIGHUP, проигнорированные при запуске futi (`nohup`, фоновая команда шелла), остаются игнорируемыми и для команды (для SIGQUIT и SIGTERM рантайм Go ставит свой обработчик, наследования нет).

### Навигация через `list`

`futi list [<path...>]` показывает содержимое уровня иерархии (путь можно писать через пробел или точку: `futi list images converter` = `futi list images.converter`):

```bash
$ futi list                          # корень: модули и namespaces
┌───────────┬────────┬───────────────────┐
│   TYPE    │  NAME  │    DESCRIPTION    │
├───────────┼────────┼───────────────────┤
│ module    │ docker │ Docker management │
│ namespace │ images │                   │
└───────────┴────────┴───────────────────┘

$ futi list images                   # namespace: вложенные namespaces и модули
┌───────────┬───────────┬───────────────┐
│   TYPE    │   NAME    │  DESCRIPTION  │
├───────────┼───────────┼───────────────┤
│ namespace │ converter │               │
│ module    │ png       │ PNG utilities │
└───────────┴───────────┴───────────────┘

$ futi list images converter         # вложенный namespace
┌────────┬────────┬──────────────────────┐
│  TYPE  │  NAME  │     DESCRIPTION      │
├────────┼────────┼──────────────────────┤
│ module │ to_png │ Convert images to PNG │
└────────┴────────┴──────────────────────┘

$ futi list images png               # модуль: команды
┌─────────┬─────────────┐
│ COMMAND │ DESCRIPTION │
├─────────┼─────────────┤
│ optimize│ Optimize PNG│
│ resize  │             │
└─────────┴─────────────┘

$ futi list docker                   # модуль в корне: команды
┌─────────┬─────────────┐
│ COMMAND │ DESCRIPTION │
├─────────┼─────────────┤
│ ps      │             │
│ build   │ Build image │
└─────────┴─────────────┘
```

Если у модуля есть команда по умолчанию (`cmd` на уровне модуля), она отображается первой с маркером `(default)`:

```bash
$ futi list echo_mod                 # модуль с default cmd
┌───────────┬─────────────┐
│ COMMAND   │ DESCRIPTION │
├───────────┼─────────────┤
│ (default) │             │
│ upper     │             │
└───────────┴─────────────┘
```

То же касается subcommands: если команда имеет `cmd` + `commands`, `cmd` показывается как `(default)` (fallback при вызове без subcommand).

Логика: если namespace резолвится как модуль — `list` показывает его команды. Если как namespace-директория — показывает непосредственных детей (вложенные namespaces и модули). `list` — встроенная команда только первым словом: `futi docker list` вызывает команду `list` модуля `docker` (ошибка, если её нет).

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

$ futi list images -r                # от контекста images
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

`futi help <path...>` — контекстная справка на любом уровне, без выхода в README; `-h`/`--help` после пути делает то же:

```bash
futi help demo                 # справка по модулю
futi help demo build           # справка по команде (= futi demo build -h)
futi help demo migrate         # справка по ветке команд
futi help images               # namespace: его дети
```

Справка по модулю — описание, usage команды по умолчанию и список подкоманд:

```bash
$ futi help demo
Demo module

usage: futi demo
flags:
  -h, --help
subcommands:
  build    Build the project
  migrate  Database migrations
```

Справка по команде показывает строку usage, args (с описанием, default и `(required)`), flags (тип, короткая форма, required, default) и, если у команды есть подкоманды, их список:

```bash
$ futi help demo build
Build the project

usage: futi demo build [flags] [context]
  context  Build context directory (default ".")
flags:
  -t, --tag string   Image tag (required)
      --file string  Dockerfile path (default "Dockerfile")
      --push
  -h, --help
```

`variadic`-вход показывается с `...` (`[words...]`), `rest`-arg — как `[docker_args...]` с пометкой `(passed verbatim)`. Если у узла есть подкоманды и string-arg, в справке добавляется `words up, down are subcommands; pass such a value after --`. Шаблон в справке не показывается; собранную команду смотрите через [`--dry-run`](#--dry-run).
Завершающий `-h`/`--help` после пути игнорируется (`futi help demo -h` = `futi help demo`); `futi help --help` показывает справку самого futi, как `futi -h`. Каждое слово после `futi help` должно быть словом пути: `futi help docker build extra` — ошибка `unknown command "extra" for "futi docker build"`. `help` — встроенная команда только первым словом: `futi demo help` вызывает команду `help` модуля `demo` как обычную команду (флаги, `-- args`, её stderr и код выхода) и завершается ошибкой, если такой команды нет; `futi help demo help` показывает справку этой команды.

### `--dry-run`

`--dry-run` — глобальный флаг: печатает итоговую команду модуля и ничего не исполняет.

Для команды с шаблоном `cmd: "docker build -t {{.tag}} -f {{.file}} ."` (`file` по умолчанию `Dockerfile`):

```bash
$ futi --dry-run docker build --tag 'my app'
docker build -t 'my app' -f Dockerfile .
```

Вывод — одна строка в stdout: argv команды после вычисления шаблона, токены с пробелами и спецсимволами в одинарных кавычках (строку можно вставить в shell). Вывод печатается в синтаксисе bash/zsh (не POSIX sh/dash): форма `$'…'` используется только для токенов с управляющими или непечатаемыми символами (ESC, `\r`, bidi и т. п.), чтобы чужой модуль не мог подделать отображаемую команду. Чтобы вставить такие токены обратно в shell, нужна UTF-8 локаль: `\u…` внутри `$'…'` зависит от локали. Ошибки (неизвестная команда, пропущенный обязательный вход, ошибка шаблона) выводятся так же, как без `--dry-run`, с тем же кодом выхода.

- Флаг распознаётся только первым аргументом `futi`; после пути `--dry-run` — обычный флаг модуля, а не зарезервированное имя.
- Хуки (`attach`/`detach`) не исполняются и не показываются: печатается только команда.
- `-h` / `--help` после пути работает как обычно (справка, код 0). Команда модуля с именем `help` (`futi --dry-run demo help`) показывается как любая другая.
- `list`, `help`, `modules`, `cache`, `completion`, `--version` и `-h` без пути не команды модулей: с `--dry-run` они завершаются ошибкой `--dry-run applies only to module commands` (код 1).
- **Секреты:** шаблон подставляет значения `{{.env.X}}` — они попадут в вывод. Не публикуйте результат `--dry-run` для команд с токенами и паролями в окружении.

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
ERROR: line 10: "positional" was replaced by "args"/"flags" (see README)
ERROR: command "build": flag "tag": invalid type "float" (valid: string, int, bool)
ERROR: command "build": flag "tag": duplicate name "tag"
ERROR: command "build": cmd: 1:14: "-t" would be left without its value when "tag" is unset; use {{if .tag}}-t {{.tag}}{{end}} or -t={{.tag}}
ERROR: command "empty": must have cmd or commands
ERROR: command "old": cmd: 1:8: {{.args}} was replaced by a rest arg (see README)
modules: validate: module "bad" has 6 error(s)
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
- `args` или `flags` без `cmd` — на любом уровне (модуль, команда); входы не наследуются подкомандами
- ошибка схемы входа: `type` не из `{string, int, bool}`, `bool` с `required`/`variadic`/`allow_dash`, `required` вместе с любым `default` (даже пустым), `default` не того типа, дубликат имени среди args и flags узла, имя не по `[a-z][a-z0-9_]*`, зарезервированное имя (`help`, `env`, `module_root`, `module_name`), неверный `short` (не одна латинская буква, дубликат, `h`), `rest` не у последнего arg и недопустимая форма args (разрешены только `required… optional…`, `required… variadic required…` или `required… rest`)
- ошибка шаблона в `cmd`, `attach`, `detach`: синтаксис, недопустимая конструкция (см. [`cmd` и `text/template`](#cmd-и-texttemplate)), ссылка на необъявленный вход, bool вне условия, variadic/rest в склейке без `range`, `range` по не-variadic входу; хуки могут ссылаться только на `env`, `module_root` и `module_name`
- вход, который `cmd` нигде не упоминает (`input "port" is not used in cmd`)
- висящий флаг: литерал `--tail`, за которым стоит аргумент, способный выпасть (`"--tail" would be left without its value when "tail" is unset; use {{if .tail}}--tail {{.tail}}{{end}} or --tail={{.tail}}`); для variadic/rest не проверяется
- аргумент, упоминающий два и больше входов, из которых один может быть не задан (`{{.user}}@{{.host}}`)
- недопустимое имя команды в `commands`: пустое, начинается с `-`, содержит `.`, пробельные или управляющие символы
- старый формат: ключи `params`/`positional` (`"params" was replaced by "args"/"flags" (see README)`), функция `quote` и `{{.args}}`/`{{.args_list}}` в шаблоне (каждый случай с подсказкой)
- коллизия путей: один путь адресует две сущности объединённого дерева, например команда `deploy` модуля `dev.yaml` и модуль `dev/deploy.yaml` (`path "dev deploy" is ambiguous: …`). Проверяется по всем источникам; `futi list` печатает её строкой `Warning:` в stderr и всё равно показывает листинг

**Предупреждения** (не блокируют, показываются только в `validate`):

- команда с `commands`, но без `cmd` — вызов без subcommand падает в runtime
- узел с подкомандами и string-arg (включая `variadic` и `rest`) — слово, равное имени подкоманды, принимается за подкоманду; такое значение передавайте после `--` (arg типа `int` предупреждения не вызывает). То же предупреждение выводят справка узла и `futi modules update`
- хук с отдельным словом `&&`, `||`, `|`, `>`, `<` или `>>` — хуки исполняются без shell, поэтому оно передаётся программе аргументом; используйте `[sh, -c, "…"]`

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

Одиночный `.yaml` при установке оборачивается в пакет: создаётся каталог `<module>/config.yaml`; имя без `.yaml`/`.yml` берётся из `name:` манифеста или из имени источника, оно не должно содержать точку, а рядом не должно лежать одноимённого `<module>.yaml`. Флаги install: `--namespace <ns>`, `--ref <git-ref>` (только для git-источника, для local/archive — ошибка), `--local` (в `./.futi/` вместо глобального каталога), `--skip-attach`, `--yes` / `-y` (запустить attach-хук без вопроса), `--from-local` / `--from-git` / `--from-archive` (переопределить эвристику определения типа источника).

Сетевые источники — только `https://`: `http://`- и `git://`-адреса отвергаются, редирект с https на http запрещён (и для архива, и для git); для git-репозитория скачайте модуль вручную и установите из локального пути, для архива — скачайте, распакуйте и установите каталог (локальный `.tar.gz`/`.zip` install не принимает). Поддерживаются архивы `.zip` и `.tar.gz`/`.tgz`; формат определяется по содержимому файла, расширение в URL берётся без query и fragment, а `.tar.bz2`/`.tbz2`/`.tar.xz`/`.txz` отвергаются до скачивания. Скачивание архива ограничено 100 MiB и 5 минутами. Распаковка ограничена 500 MiB суммарно и 10000 записями (файлы и каталоги); symlink и прочие не-regular записи в `.zip` и `.tar.gz` пропускаются, а права файлов приводятся к `rwx` (без setuid/setgid/sticky, владелец всегда может читать и писать). Источник и `--ref` не должны начинаться с `-`.

Модуль может объявлять setup/teardown-хуки — `futi` выполнит их при install/update и uninstall:

```yaml
attach: ".venv/bin/python -m pip install -r requirements.txt"
detach: [rm, -rf, .venv]
```

Хук — шаблон в тех же двух формах, что и `cmd` (строка, разбираемая на слова, или список, где каждый элемент — один аргумент); он вычисляется в argv и исполняется без shell, поэтому `&&`, `|`, `>`, `;`, `$VAR` и glob не интерпретируются: они доходят до программы обычным текстом. `futi modules validate` предупреждает, если в хуке есть отдельное слово `&&`, `||`, `|`, `>`, `<` или `>>`. Составной хук пишется с явным shell:

```yaml
attach: [sh, -c, "python3 -m venv .venv && .venv/bin/pip install -r requirements.txt"]
```

Хуки выполняются в каталоге модуля; `{{.module_name}}`/`FUTI_MODULE_NAME` — то же имя, что и в `cmd` (поле `name`, а без него — имя каталога/файла модуля), при install, update и uninstall; доступны `{{.module_name}}`, `{{.module_root}}`, `{{.env.FOO}}` (входов и `quote` нет) и переменные окружения FUTI_MODULE_NAME / FUTI_MODULE_ROOT. Программа, записанная с путём (`scripts/x.sh`, `./x.sh`), ищется от каталога модуля, голое имя (`python3`) — через `PATH`. Хуки наследуют stdin futi и выполняются без таймаута: зависший хук прерывается Ctrl+C. Без управляющего терминала (CI, cron, ssh без tty) хук запускается в своей группе процессов: сигналы SIGINT, SIGQUIT, SIGTERM и SIGHUP, полученные futi, пересылаются всем процессам хука, включая порождённые составной командой (`[sh, -c, "…"]`). При управляющем терминале хук остаётся в группе futi (интерактивные хуки работают) — в том числе когда futi запущен в фоне (`&`) или с перенаправленным вводом (`</dev/null`): перенаправление и фон изоляцию не включают; при адресном `kill -TERM <futi>` процессы, порождённые хуком, могут остаться. Изолированный хук (без управляющего терминала) не погибает от сигнала, посланного группе futi (`kill -9 -<pgid>`, `timeout -s KILL futi …`): его группа отдельная. Провал `attach` при install откатывает установку (модуль удаляется); при update — только предупреждение, новое дерево сохраняется. Провал `detach` при uninstall — предупреждение (или отказ с `--strict-detach`).

`attach` из скачанного манифеста — чужой код, поэтому install и update перед его запуском спрашивают подтверждение (в stderr, хук показывается как написан, без подстановки значений; список — как `[a, "b c"]`):

```
Module qr runs an attach hook (in /home/user/.config/futi/modules/qr):
  .venv/bin/python -m pip install -r requirements.txt
Run it? [y/N]
```

Вопрос задаётся до изменений на диске: при отказе install не устанавливает модуль, update оставляет старую версию (`update` без имени спрашивает про каждый модуль; отказ — ошибка этого модуля, остальные обновляются). Если stdin не терминал (CI, скрипты, пайп), futi не спрашивает и завершается ошибкой; если ответа нет (`</dev/null`, `docker run` без `-i`, systemd `StandardInput=null`) — тоже ошибка. В обоих случаях укажите `--yes` (`-y`), чтобы запустить хук, или `--skip-attach`, чтобы пропустить; `--skip-attach` важнее `--yes`. У update `--yes` подтверждает и замену файлов (см. ниже): без терминала на stdin update с изменёнными файлами требует `--yes`, в том числе вместе с `--skip-attach`. Модуль без `attach` и update без изменений источника подтверждения attach не требуют.

Каждая установка пишет sidecar `__futi_meta.yaml` рядом с config.yaml (источник, тип, ref/commit, hash дерева, даты) — по нему работают:

```bash
futi modules list              # установленные модули
futi modules info qr           # детали установки
futi modules update            # re-fetch + re-attach (все модули)
futi modules update qr         # один модуль
futi modules uninstall qr      # detach + удаление (с подтверждением)
futi modules path qr           # каталог модуля (для скриптов)
```

`modules list` и `update` без имени видят модули всех `sources` из `config.yaml` плюс глобальный каталог и `./.futi`; `uninstall`/`update`/`info` по имени находят модуль только через discovery (в `sources` и `./.futi`) — модуль глобального каталога вне `sources` нужно добавить в `sources` или удалить вручную. Модули показываются под тем именем, под которым их находит discovery, — с mountpoint: `dev.tools.qr`; симлинки на каталоги раскрываются (циклы отсекаются, один реальный каталог показывается один раз — предпочтительно под путём без симлинка); модуль, каталог которого сам симлинк, `update` без имени пропускает (`Warning: skipped <name>: module directory is a symlink`): замена дерева превратила бы симлинк в копию; симлинк-каталог namespace выше модуля этому не мешает. Невалидный `config.yaml` — ошибка команды. Установка (`install`, `init --global`) пишет только в глобальный каталог (`FUTI_MODULES_DIR` или `~/.config/futi/modules/`) или с `--local` — в `./.futi`; если при заданных `sources` глобального каталога среди них нет, install и `init --global` предупреждают `Warning: <dir> is not listed in config.yaml sources; …` (по имени модуль не будет найден, пока каталог не добавлен в `sources`), а если он смонтирован с префиксом — печатают `Module is available as <prefix>.<name>`.

`update` сравнивает hash дерева источника с lockfile (в hash входят пути, содержимое файлов, бит исполнения и цели симлинков): без изменений — no-op (обновляется только `last_check`), локальные правки установленного модуля сохраняются. Если источник изменился, update сравнивает файлы на диске с новой версией: совпадают — переписывается только lockfile; иначе в stderr печатается список изменений (`D` — удалится, `M` — изменится, `A` — появится, в этом порядке; не больше 50 строк) и задаётся вопрос `Replace module files? [y/N]` — до вопроса про `attach`. Локальные правки при этом видны как `M`/`D` и после подтверждения теряются; при отказе модуль не меняется. Без терминала на stdin нужен `--yes` (`-y`) — в CI и скриптах укажите его. Каталоги `.venv`, `node_modules`, `.git`, `__pycache__` в hash и сравнение не входят и при замене дерева не сохраняются (старое дерево удаляется целиком, `attach` пересоздаёт их). Замена дерева атомарна, `installed_at` и `about` сохраняются. При сбое переноса или записи lockfile update восстанавливает старое дерево. Если от прерванного update остался `<module>.futi-backup`, update отказывает и просит разобрать backup вручную; `list` и `update` такой каталог модулем не считают (update без имени лишь предупреждает о нём в stderr). Если новая версия добавляет подкоманду к узлу со string-позиционным arg, update предупреждает в stderr (`Warning: module <name>: command "…": words … are subcommands; pass such a value after --`): слово, равное имени подкоманды, теперь будет принято за подкоманду. Флаги update: `--skip-attach`, `--yes` / `-y`.

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
futi: cache requires a subcommand (try "cache refresh", "cache status", or "cache clear")

$ futi cache foo
futi: unknown cache subcommand "foo" (try "cache refresh", "cache status", or "cache clear")
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
изменился после Refresh) и повреждённый/подменённый gob-файл. Чексумма
сверяется также в системной справке (`help`) и в completion. Без сверки, из
кэша, работает только `list` — скорость важнее.

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
| `every_t_min` | Обновлять раз в указанный интервал `time` (не более 365 дней; для большего используйте `force_only`). Между запусками — использовать кэш с диска. |
| `force_only` | Обновлять только по `futi cache refresh`. После обновления или пересборки futi кэш пуст до `futi cache refresh`. |

Формат `time` — расширение `time.ParseDuration` с поддержкой дней через суффикс `d`:

| Пример | Значение |
|---|---|
| `30m` | 30 минут |
| `2h` | 2 часа |
| `1d` | 1 день (24 часа) |
| `1d4h` | 1 день 4 часа (28 часов) |
| `1d2h30m` | 1 день 2 часа 30 минут |

Компоненты суммируются, итог должен быть больше нуля (`0d12h` допустимо, `0d` — нет). Отрицательные значения не поддерживаются.

### Fallback

Если `~/.cache/` недоступен (нет `HOME` / `XDG_CACHE_HOME`) — futi fallback-ит на in-memory кэш с предупреждением в stderr. CLI работает, но persistent кэш недоступен до следующего запуска с корректным окружением.

При повреждённом gob-файле — кэш автоматически перестраивается с пустого состояния. При несовпадении версии схемы (несовместимый формат файла кэша) — аналогично. Кэш также перестраивается при каждом обновлении или пересборке бинаря futi: файл кэша привязан к идентичности бинаря (mtime и размер), чужой кэш игнорируется без сообщений. При политике `force_only` после обновления futi персистентный кэш пуст до `futi cache refresh` (модули при этом читаются с диска напрямую).

---

## Автодополнение

futi поддерживает автодополнение для **bash**, **zsh** и **fish**. Имена модулей,
namespace'ов, команд, подкоманд и флагов (`--tag`, `--force`) — динамические,
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

- **Слова пути** — дети текущего узла в объединённом дереве: `futi <TAB>` (модули, namespaces, builtins), `futi dev <TAB>`, `futi docker <TAB>`, `futi docker migrate <TAB>`. Подкоманда подсказывается только сразу после пути: когда дальше идёт флаг или другое слово, остальное — входы узла
- **Флаги** — только для слова, начинающегося с `-`: `futi k8s logs -<TAB>` → `--follow`, `-f`, `--tail`, `--help`; уже использованные флаги повторно не предлагаются (кроме variadic). Пометки `(required)` и `(default: …)` видны в zsh, bash/fish их отрезают
- **Значения** — для значения не-bool флага (`futi docker build --tag <TAB>`, `--tag=<TAB>`) и для позиционного arg ничего не подсказывается: shell тоже не добавляет файлы (дополнение файлов запланировано для входов `path`/`dir`, см. ROADMAP). Bool-флаг значения не берёт, поэтому после него `true`/`false` не предлагаются
- **Rest-arg** — после старта rest-arg (`futi docker run nginx <TAB>`) или когда он первый вход узла без подкоманд (`futi run <TAB>`), токены уходят утилите как есть, и shell дополняет файлы как обычно. Всё после `--` — данные: ни флаги, ни подкоманды не подсказываются
- **`help` и `list`** — встроенные команды только первым словом; после них дополняется путь команды: `futi help do<TAB>` → `docker`, `futi list dev <TAB>`. Внутри пути `help` и `list` подсказываются, только если модуль определяет такие команды
- **Глобальные флаги** — `futi --<TAB>` → `--dry-run`, `--help`, `--version`; после ведущего `--dry-run` подсказки как для обычного вызова, но без builtins (`cache`, `completion`, `list`, `modules`): `futi --dry-run <TAB>` показывает только модули и namespaces
- **Builtins** — `futi cache <TAB>` → `refresh`/`status`/`clear`; `futi completion <TAB>` → `bash`/`zsh`/`fish`. На корне `cache`, `completion`, `list`, `modules` подсказываются как builtins даже при одноимённом модуле (CLI исполнит builtin)
- **Валидация модуля** — `futi modules validate <TAB>` подсказывает полные имена модулей (`dev.deploy`), после первого аргумента подсказок нет
- **Флаги `modules`** — `futi modules add_command <модуль> --<TAB>` → `--cmd`, `--desc`; аналогично для `init`, `install`, `uninstall`, `update`

Модуль, который не удаётся загрузить, или путь, называющий две сущности (коллизия путей), не даёт подсказок и не печатает ошибок.

zsh выводит описания команд/модулей рядом с подсказками.

### Кэш и completion

`__complete` использует persistent кэш модулей (как обычный CLI), поэтому не обходит источники на каждый TAB. Чтобы подсказки не были пустыми на холодном кэше (первый запуск) или устаревшими, `__complete` **самообновляет** кэш, если он никогда не обновлялся или обновлялся более часа назад.

Запись кэша не принимается на веру: модули на набранном пути загружаются **с проверкой свежести** — futi считает хеш манифеста на диске и сверяет его с хешем, сохранённым в кэше (та же проверка, что и перед исполнением команды). Если файл изменился, он перечитывается, поэтому устаревший кэш не скрывает флаги и подкоманды, которые вы только что добавили в модуль. Подсказки для самого пути (дети namespace) по-прежнему берутся из кэша и листинга источников.

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
- Сетевые install принимают только `https://` и ограничивают размер скачивания и распаковки (см. [Управление модулями](#управление-модулями)).
- Публичный registry скачанных модулей (см. ROADMAP) потребует пересмотра
  этой модели — доверие к чужим модулям нельзя будет неявно унаследовать.

### Исполнение без shell

Команды исполняются через `exec` с готовым argv — **без** оболочки. Метасимволы `;`, `$()`, `` ` `` из входов не выполняются как shell-код (проходят как обычный текст в аргументах). Нужен shell — пишите его явно (`[sh, -c, '…', sh, "{{.x}}"]`), передавая пользовательский ввод позиционным параметром скрипта (`$1`), а не внутри текста скрипта (см. [Команда, которой нужен shell](#команда-которой-нужен-shell) и примеры).

### Batch-файлы в Windows

В Windows, когда программа команды после поиска в `PATH` оказывается файлом `.bat` или `.cmd`, Go не умеет безопасно квотировать аргументы для `cmd.exe`. Поэтому futi отказывается запускать команду, если любой элемент argv, содержащий значение пользователя (arg, flag или `rest`), содержит `"`, `%`, `!`, `&`, `|`, `<`, `>`, `^` или перевод строки; ошибка называет вход, например `value for <x> contains "&", unsafe for batch file deploy.cmd`. Для команд, принимающих пользовательский ввод, предпочитайте `.exe`. `--dry-run` эту проверку пропускает.

### Одно значение — один аргумент

Значение arg или flag никогда не режется и не разбирается заново: оно вставляется в аргумент как есть, с пробелами, кавычками и метасимволами, поэтому не может добавить аргументы к команде. Ручного квотирования нет (функция `quote` удалена): значение — всегда ровно один аргумент.

Значение, начинающееся с `-`, всё равно могло бы быть прочитано обёрнутой программой как опция, поэтому futi отвергает такое значение, если оно стало бы первым символом аргумента, — если только шаблон не защищён литералом `--` или у входа нет `allow_dash: true` (см. [Значения с ведущим `-`](#значения-с-ведущим--)). Для значений в опасных позициях (`git commit -m`, `docker run`, `rm`) используйте `--opt={{.x}}` или литерал `--`. Токены `rest`-arg передаются дословно и не проверяются: не отдавайте `rest` программе, которая трактует свои аргументы как опасную опцию, если только это не смысл команды.

### Секреты — через окружение

Токены и ключи передавайте через `{{.env.NAME}}`, а не через флаги: аргументы видны в `ps` и истории shell, окружение процесса — нет.

### Целостность кэша

Persistent кэш сверяет SHA-256 чексумму манифеста при исполнении модуля,
в системной справке и в completion — устаревший или подменённый кэш не
приведёт к исполнению чужой команды (см. [Кэш модулей](#кэш-модулей)).

---

## Примеры

Готовые рабочие модули (`git`, `docker`, `files`, `backup`) лежат в каталоге [`examples/`](../examples/) — скопируйте их в каталог модулей или подключите через `sources`; посмотреть собранную команду без запуска можно через `futi --dry-run <путь> …`.

### Echo module (args + default cmd)

```yaml
# ~/.config/futi/modules/echo_mod.yaml
name: echo_mod
description: Echo utilities
cmd: "echo {{.words}}"           # команда по умолчанию (без shell)
args:
  - name: words
    variadic: true
commands:
  upper:
    description: Uppercase echo
    # futi исполняет argv БЕЗ shell: пайпы, $() и циклы напрямую не работают.
    # Нужен shell — пишите его явно (sh -c) и передавайте пользовательский ввод
    # позиционным параметром sh ($1): значение никогда не становится частью скрипта.
    args:
      - name: text
        required: true
        allow_dash: true       # идёт в "$1": sh не читает его как опцию
    cmd: [sh, -c, 'printf "%s\n" "$1" | tr a-z A-Z', sh, "{{.text}}"]
  repeat:
    description: Repeat the text N times
    args:
      - name: text
        required: true
        allow_dash: true
    flags:
      - name: count
        type: int
        short: n
        required: true
    cmd: [sh, -c, 'for i in $(seq 1 "$2"); do printf "%s\n" "$1"; done', sh, "{{.text}}", "{{.count}}"]
```

```bash
futi echo_mod hello world            # echo hello world (cmd по умолчанию)
futi echo_mod upper "hello world"    # HELLO WORLD (через sh -c | tr)
futi echo_mod repeat hi -n 3         # hi\nhi\nhi
futi echo_mod -- upper               # напечатает "upper": после "--" это слово, а не подкоманда
```

`futi modules validate echo_mod` здесь печатает предупреждение: команда по умолчанию принимает строковые слова, а `upper`/`repeat` — подкоманды, поэтому такое слово первым будет прочитано как подкоманда (справка узла показывает это тоже). То же предупреждение печатается для модуля со строковым позиционным arg, у которого есть дочерние модули (модуль `dev` с arg и `dev/deploy.yaml`): `futi dev deploy` уходит в дочерний модуль, а предупреждение перечисляет имена дочерних модулей.

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
    args:
      - name: branch
        default: main
    cmd: [sh, -c, 'git fetch origin && git reset --hard "origin/$1"', sh, "{{.branch}}"]

  squash:
    description: Squash last N commits
    args:
      - name: count
        type: int
        required: true
    flags:
      - name: message
        short: m
        required: true
        allow_dash: true
    cmd: [sh, -c, 'git reset --soft "HEAD~$1" && git commit -m "$2"', sh, "{{.count}}", "{{.message}}"]
```

```bash
futi git amend
futi git fresh develop
futi git squash 3 --message "feature complete"
```

### Docker wrapper

```yaml
# ~/.config/futi/modules/docker.yaml
name: docker
description: Docker utilities
commands:
  clean:
    description: Remove stopped containers and dangling images
    cmd: [sh, -c, "docker container prune -f && docker image prune -f"]

  shell:
    description: Shell into running container
    args:
      - name: container
        required: true
    cmd: "docker exec -it {{.container}} sh"

  run:
    description: Run an image; everything after the image goes to docker verbatim
    args:
      - name: image
        required: true
      - name: docker_args
        rest: true
    flags:
      - name: rm
        type: bool
    cmd: "docker run {{if .rm}}--rm{{end}} {{.image}} {{.docker_args}}"

  build-all:
    description: Build all images in directory
    cmd: [sh, -c, 'for d in */; do docker build -t "${d%/}" "$d"; done']
```

```bash
futi docker clean
futi docker shell myapp
futi docker run --rm nginx -p 8080:80
futi docker build-all
```

### Project deploy package

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
    args:
      - name: version
        required: true
    cmd: ["{{.module_root}}/scripts/deploy.sh", staging, "{{.version}}"]

  production:
    args:
      - name: version
        required: true
    cmd: ["{{.module_root}}/scripts/deploy.sh", production, "{{.version}}"]

  rollback:
    args:
      - name: target
        required: true
    cmd: ["{{.module_root}}/scripts/rollback.sh", "{{.target}}"]
```

```bash
futi deploy staging v1.2.3
futi deploy production v1.2.3
futi deploy rollback production
```

---

## ROADMAP

futi находится на ранней стадии (0.x): формат модулей может меняться между минорными версиями. Ниже — направления развития.

### Ближайшие планы

- **Типы входов** — расширение за пределы `int`: `file`, `json`, `secret` (безопасная передача через env, не args; базовый доступ к env vars уже есть через `{{.env.FOO}}`), `choices`, аннотации команд. Для `path`/`dir`-типов автокомплит будет предлагать пути только им
- **Descriptions в bash/fish** — zsh выводит описания уже сейчас; bash/fish — roadmap
- **PowerShell** — 4-й shell после стабилизации bash/zsh/fish
- **Модульный completion** — модули сами предоставляют подсказки для автодополнения (динамические кандидаты: имена контейнеров, веток и т.п.); тогда же имя `completion` может быть отдано модулям через smart-роутинг (`futi completion bash|zsh|fish` остаётся builtin-генерацией скрипта, остальные вызовы — резолв модуля). Сейчас `completion`/`__complete` зарезервированы (см. `futi modules init`)

### Unified I/O

*(планируется, пока не реализовано)* Автоматическая конвертация между форматами ввода-вывода:

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
  схема входов — их input schema. Основной сценарий — **удалённый**: futi развёрнут на
  машине как сервер, удалённый агент подключается по сети (MCP over streamable HTTP) и
  запускает команды на этой машине. Локально — `futi mcp` по stdio
- **HTTP API** — REST для CI/CD и автоматизации, на том же сервере, что и MCP
- **Безопасность сервера** — по умолчанию слушает только localhost; токены, права по
  namespace (allowlist), лимиты вывода и времени, аудит-лог
- **Стрим вывода** (SSE/WebSocket) — позже

```bash
futi serve --addr 127.0.0.1:8080            # форма команды предварительная
curl -X POST http://localhost:8080/api/v1/exec -H "Authorization: Bearer $TOKEN" \
  -d '{"namespace": "docker", "command": "build", "inputs": {"tag": "myapp"}}'
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
- **Config linting** — расширение валидации схем (базовая валидация через `futi modules validate` уже есть): подсказки по best practices, deprecated-поля, стилистика
- **TUI** — интерактивный просмотр и вызов модулей
- **Module registry** — публичный реестр шаримых модулей

---

## Лицензия

MIT — см. [LICENSE](../LICENSE).
