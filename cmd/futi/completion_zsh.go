package main

// zshCompletionScript — статичный zsh-скрипт автодополнения для futi.
// Адаптирован из cobra (BSD-3-Clause). Поддерживает описания через tab-формат:
// "text\tdescription" парсится в _describe.
const zshCompletionScript = `#compdef futi
# zsh completion for futi
# Adapted from cobra (BSD-3-Clause).

_futi() {
    local -a completions
    local -a completions_with_descriptions
    local -a response
    (( ! $+commands[futi] )) && return 1

    # Слова от первого после futi до текущего включительно: слова после
    # курсора (TAB в середине строки) не передаются.
    local -a args
    args=("${(@)words[2,CURRENT]}")

    response=("${(@f)$(futi __complete "${args[@]}" 2>/dev/null)}")

    # Директива — только последняя строка вида :N; иначе это кандидат.
    local directive=0
    if [[ "${response[-1]}" =~ '^:[0-9]+$' ]]; then
        directive="${response[-1]#:}"
        response=("${(@)response[1,-2]}")
    fi

    local comp ctext cdesc
    for comp in "${response[@]}"; do
        [[ -z "$comp" ]] && continue
        # _describe использует ':' как разделитель text:description, поэтому
        # '\' и ':' экранируются (сначала '\') в тексте и в описании.
        if [[ "$comp" == *$'\t'* ]]; then
            ctext="${comp%%$'\t'*}"
            cdesc="${comp#*$'\t'}"
            ctext="${ctext//\\/\\\\}"
            ctext="${ctext//:/\\:}"
            cdesc="${cdesc//\\/\\\\}"
            cdesc="${cdesc//:/\\:}"
            completions_with_descriptions+=("${ctext}:${cdesc}")
        else
            ctext="${comp//\\/\\\\}"
            completions+=("${ctext//:/\\:}")
        fi
    done

    if (( ${#completions[@]} )); then
        _describe 'values' completions
    fi
    if (( ${#completions_with_descriptions[@]} )); then
        _describe -d 'values' completions_with_descriptions
    fi

    # Директива — битовая маска: 1 — ошибка, 2 — без пробела, 4 — без файлов.
    if (( directive & 1 )); then
        _message 'error occurred during completion'
    fi
    if (( directive & 2 )); then
        _message 'no space left'
    fi
    if (( directive & 4 )); then
        _message 'no file completion'
    fi
    # Default: futi разрешает файловое дополнение (значения флагов —
    # пути). Функция завершения полностью контролирует кандидатов,
    # поэтому генерируем файловые сами.
    if (( directive == 0 )); then
        _files
    fi

    return 0
}

if [ "$funcstack[1]" = "_futi" ]; then
    _futi "$@"
fi
`
