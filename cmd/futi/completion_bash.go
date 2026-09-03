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

__futi_get_completion_results() {
    local out directive line
    out="$("futi" __complete "${COMP_WORDS[@]:1}" 2>/dev/null)"

    __futi_debug "futi __complete output:"
    __futi_debug "$out"

    directive=0
    COMPREPLY=()
    while IFS= read -r line; do
        if [[ "$line" == :* ]]; then
            directive="${line#:}"
        elif [[ -n "$line" ]]; then
            # Description (if any) follows a tab — strip it for bash.
            COMPREPLY+=("${line%%$'\t'*}")
        fi
    done <<< "$out"
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
