package main

// bashCompletionScript — статичный bash-скрипт автодополнения для futi.
// Адаптирован из cobra (BSD-3-Clause), упрощён для протокола __complete.
const bashCompletionScript = `# bash completion for futi
# Adapted from cobra (BSD-3-Clause), simplified for the __complete protocol.

__futi_debug() {
    if [[ -n ${BASH_COMP_DEBUG_FILE:-} ]]; then
        echo "$*" >> "${BASH_COMP_DEBUG_FILE}"
    fi
}

# Слова от первого после futi до текущего включительно; "=" и ":" не
# разбивают слово (COMP_WORDBREAKS). Результат — в массиве __futi_args
# (объявлен в вызывающей функции, dynamic scoping) и __futi_cur.
__futi_collect_words() {
    local -a words
    local cur cword i w join=0
    __futi_args=()
    if declare -F _get_comp_words_by_ref >/dev/null 2>&1; then
        _get_comp_words_by_ref -n =: cur words cword
        __futi_cur="$cur"
        __futi_args=("${words[@]:1:cword}")
        return
    fi
    # Без bash-completion: склеиваем слова, состоящие только из "=" и ":",
    # с предыдущим и следующим непустым словом.
    cword=$COMP_CWORD
    for (( i = 1; i <= cword; i++ )); do
        w="${COMP_WORDS[i]}"
        if [[ "$w" =~ ^[=:]+$ ]]; then
            if (( ${#__futi_args[@]} )); then
                __futi_args[-1]+="$w"
            else
                __futi_args+=("$w")
            fi
            join=1
        elif (( join )) && [[ -n "$w" ]]; then
            __futi_args[-1]+="$w"
            join=0
        else
            __futi_args+=("$w")
            join=0
        fi
    done
    __futi_cur="${__futi_args[-1]:-}"
}

__futi_get_completion_results() {
    # directive и __futi_args/__futi_cur НЕ local здесь: _futi объявляет
    # directive local и читает после вызова — bash dynamic scoping мутирует
    # переменную вызывающего.
    local out line cur colon_prefix i
    local -a lines
    local -a __futi_args
    __futi_collect_words
    cur="$__futi_cur"
    out="$("futi" __complete "${__futi_args[@]}" 2>/dev/null)"

    __futi_debug "futi __complete output:"
    __futi_debug "$out"

    directive=0
    COMPREPLY=()
    mapfile -t lines <<< "$out"
    # Директива — только последняя строка вида :N; иначе это кандидат.
    if (( ${#lines[@]} )) && [[ "${lines[-1]}" =~ ^:[0-9]+$ ]]; then
        directive="${lines[-1]#:}"
        unset 'lines[-1]'
    fi
    for line in "${lines[@]}"; do
        if [[ -n "$line" ]]; then
            # Description (if any) follows a tab — strip it for bash.
            COMPREPLY+=("${line%%$'\t'*}")
        fi
    done

    # ":" входит в COMP_WORDBREAKS: bash заменяет только часть слова после
    # последнего ":", поэтому отрезаем префикс у кандидатов.
    if [[ "$cur" == *:* ]]; then
        colon_prefix="${cur%"${cur##*:}"}"
        for i in "${!COMPREPLY[@]}"; do
            COMPREPLY[i]="${COMPREPLY[i]#"$colon_prefix"}"
        done
    fi
}

_futi() {
    local directive
    __futi_get_completion_results

    # ShellCompDirectiveNoSpace (1<<1 = 2): don't append a trailing space.
    if (( directive & 2 )); then
        compopt -o nospace 2>/dev/null || true
    fi

    # ShellCompDirectiveNoFileComp (1<<2 = 4): disable default filename completion.
    if (( directive & 4 )); then
        compopt +o default 2>/dev/null || true
    fi

    return 0
}

if [[ -n ${BASH_VERSION:-} ]]; then
    complete -o default -F _futi futi
fi
`
