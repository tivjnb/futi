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

# Words from the first one after futi up to the current one, inclusive; "=" and ":" do not
# split a word (COMP_WORDBREAKS). The result goes into the __futi_args array
# (declared in the calling function, dynamic scoping) and __futi_cur.
__futi_collect_words() {
    local -a words
    local cur cword i n w head join=0
    __futi_args=()
    if declare -F _get_comp_words_by_ref >/dev/null 2>&1; then
        _get_comp_words_by_ref -n =: cur words cword
        __futi_cur="$cur"
        __futi_args=("${words[@]:1:cword}")
        # cur is the part of the word before the cursor; it replaces the last argument.
        if (( cword >= 1 )); then
            __futi_args[${#__futi_args[@]}-1]="$cur"
        fi
        return
    fi
    # Without bash-completion: join words consisting only of "=" and ":"
    # with the previous and next non-empty word.
    cword=$COMP_CWORD
    for (( i = 1; i <= cword; i++ )); do
        w="${COMP_WORDS[i]}"
        if [[ "$w" =~ ^[=:]+$ ]]; then
            n=${#__futi_args[@]}
            if (( n )); then
                __futi_args[n-1]="${__futi_args[n-1]}$w"
            else
                __futi_args+=("$w")
            fi
            join=1
        elif (( join )) && { [[ -n "$w" ]] || [[ "${COMP_LINE:0:COMP_POINT}" != *[[:space:]] ]]; }; then
            # An empty word after "=" is joined only if the cursor is
            # right next to it (no trailing space in the line before the cursor).
            n=${#__futi_args[@]}
            __futi_args[n-1]="${__futi_args[n-1]}$w"
            join=0
        else
            __futi_args+=("$w")
            join=0
        fi
    done
    n=${#__futi_args[@]}
    __futi_cur=""
    if (( n )); then
        # The cursor may be inside a word: take the part before the cursor.
        head="${COMP_LINE:0:COMP_POINT}"
        __futi_args[n-1]="${head##*[[:space:]]}"
        __futi_cur="${__futi_args[n-1]}"
    fi
}

__futi_get_completion_results() {
    # directive is NOT local here: _futi declares it local and reads it after
    # the call; bash dynamic scoping mutates the caller variable.
    # __futi_args/__futi_cur are declared local here and filled in
    # __futi_collect_words (also dynamic scoping); they do not leak into the shell.
    local out line cur colon_prefix i n __futi_cur
    local -a lines
    local -a __futi_args
    __futi_collect_words
    cur="$__futi_cur"
    out="$("futi" __complete "${__futi_args[@]}" 2>/dev/null)"

    __futi_debug "futi __complete output:"
    __futi_debug "$out"

    directive=0
    COMPREPLY=()
    lines=()
    while IFS= read -r line; do
        lines+=("$line")
    done <<< "$out"
    # The directive is only a last line of the form :N; otherwise it is a candidate.
    n=${#lines[@]}
    if (( n )) && [[ "${lines[n-1]}" =~ ^:[0-9]+$ ]]; then
        directive="${lines[n-1]#:}"
        lines=("${lines[@]:0:n-1}")
    fi
    for line in "${lines[@]}"; do
        if [[ -n "$line" ]]; then
            # Description (if any) follows a tab — strip it for bash.
            COMPREPLY+=("${line%%$'\t'*}")
        fi
    done

    # ":" is in COMP_WORDBREAKS: bash replaces only the part of the word after
    # the last ":", so strip the prefix from candidates.
    # Only if ":" is really in COMP_WORDBREAKS.
    if [[ "$COMP_WORDBREAKS" == *:* && "$cur" == *:* ]]; then
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
