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

futi хранит модули в `~/.config/futi/modules/`. Переопределить каталог:

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

Вызовите:

```bash
$ futi hello greet --name world
hello world

$ futi list
hello                /home/user/.config/futi/modules/hello.yaml
```

---

## Структура модуля

Модуль — это YAML-файл с описанием команд.

```yaml
name: docker                    # опционально, по умолчанию = имя файла
description: Docker management  # опционально

commands:
  # Простая команда без параметров
  ps:
    cmd: "docker ps"

  # Команда с параметрами
  build:
    description: Build Docker image
    params:
      - name: tag
        type: string
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
```

### Поля

| Поле | Тип | Описание |
|---|---|---|
| `name` | string | Имя модуля (опционально, по умолчанию = имя файла/директории) |
| `description` | string | Описание модуля (опционально) |
| `commands` | map | Команды модуля |
| `commands.<name>.description` | string | Описание команды (опционально) |
| `commands.<name>.cmd` | string | Шаблон команды (обязательно) |
| `commands.<name>.params` | list | Параметры команды (опционально) |
| `params[].name` | string | Имя параметра (обязательно) |
| `params[].type` | string | Тип: `string` (по умолчанию) или `bool` |
| `params[].required` | bool | Обязательный параметр (по умолчанию `false`) |
| `params[].default` | string | Значение по умолчанию |

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
| `{{if .follow}}...{{end}}` | Условный блок (для bool) |

Bool-параметры нормализуются: `false`, `0`, `no`, `""` → falsy; `true`, `1`, `yes` → truthy.

### Валидация

- `required: true` и параметр не передан → ошибка
- `default` применяется, если параметр не передан
- Неизвестный параметр (нет в схеме) → ошибка

### Важно: пробелы в значениях

Если значение параметра содержит пробелы, квотируйте в шаблоне:

```yaml
cmd: "echo \"{{.msg}}\""   # --msg "hello world" → echo "hello world"
```

Без кавычек `shlex` разобьёт строку на отдельные аргументы.

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
futi <namespace...> <command> [--param val]    # вызов команды модуля
futi list                                       # модули и namespaces в корне
futi <namespace...> list                        # содержимое namespace или команды модуля
futi <namespace...> list -v                     # то же, с колонкой PATH
futi <namespace...> list -r                     # рекурсивный обход поддерева
futi -h | --help                                # справка
```

Парсинг: последний позиционный аргумент (без `--`) — команда, всё до него — namespace (сегменты через `.`).

```bash
futi docker build --tag myapp        # namespace=docker, command=build
futi dev deploy staging              # namespace=dev.deploy, command=staging
futi k8s logs mypod --follow         # namespace=k8s, command=logs, follow=true
```

### Навигация через `list`

`list` показывает содержимое текущего уровня иерархии:

```bash
$ futi list                          # корень: модули и namespaces
TYPE         NAME                 DESCRIPTION
module       docker               Docker management
namespace    images

$ futi images list                   # namespace: вложенные namespaces и модули
TYPE         NAME                 DESCRIPTION
namespace    converter
module       png                  PNG utilities

$ futi images converter list         # вложенный namespace
TYPE         NAME                 DESCRIPTION
module       to_png               Convert images to PNG

$ futi images png list               # модуль: команды
COMMAND              DESCRIPTION
optimize             Optimize PNG file
resize

$ futi docker list                   # модуль в корне: команды
COMMAND              DESCRIPTION
ps
build                Build image
```

Логика: если namespace резолвится как модуль — `list` показывает его команды. Если как namespace-директория — показывает непосредственных детей (вложенные namespaces и модули). `list` зарезервирован — нельзя создать команду с именем `list` в модуле.

Флаг `-v`/`--verbose` добавляет колонку `PATH` — путь к манифесту модуля. Полезно при нескольких источниках модулей или отладке:

```bash
$ futi list -v
TYPE         NAME                 DESCRIPTION                    PATH
module       docker               Docker management              /home/user/.config/futi/modules/docker.yaml
namespace    images
```

Флаг `-r`/`--recurse` рекурсивно обходит всё поддерево namespace, показывая полный namespace-путь от контекста:

```bash
$ futi list -r
TYPE         NAME                     DESCRIPTION
module       docker                   Docker management
namespace    images
namespace    images.converter
module       images.converter.to_png   Convert images to PNG
module       images.png               PNG utilities

$ futi images list -r                # от контекста images
TYPE         NAME                     DESCRIPTION
namespace    converter
module       converter.to_png         Convert images to PNG
module       png                      PNG utilities
```

`-r` игнорируется для модулей (показывает команды как обычно). Можно комбинировать: `futi list -r -v`.

---

## Примеры

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

- **Локальная загрузка модулей** — project-local модули (аналог `Makefile`), переопределение глобальных
- **Типы параметров** — `int`, `file`, `json`, `env`, `secret` (безопасная передача через env, не args)
- **Subcommands** — вложенные команды внутри модуля (`futi db migrate up`)
- **Сохранение exit code** — проброс кода завершения вызываемой команды
- **Shell completion** — автодополнение для bash/zsh/fish

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
- **Project overrides** — `./futi.yaml` для проектных переопределений
- **Config linting** — валидация схем модулей, подсказки
- **TUI** — интерактивный просмотр и вызов модулей
- **Module registry** — публичный реестр шаримых модулей

---

## Лицензия

MIT
