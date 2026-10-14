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

    # Words from the first one after futi up to the current one, inclusive: words after
    # the cursor (TAB in the middle of a line) are not passed.
    local -a args
    args=("${(@)words[2,CURRENT]}")

    response=("${(@f)$(futi __complete "${args[@]}" 2>/dev/null)}")

    # The directive is only a last line of the form :N; otherwise it is a candidate.
    local directive=0
    if [[ "${response[-1]}" =~ '^:[0-9]+$' ]]; then
        directive="${response[-1]#:}"
        response=("${(@)response[1,-2]}")
    fi

    local comp ctext cdesc
    for comp in "${response[@]}"; do
        [[ -z "$comp" ]] && continue
        # _describe uses ':' as the text:description separator, so
        # '\' and ':' are escaped (first '\') in the text and in the description.
        if [[ "$comp" == $'\t'* ]]; then
            # Tab-prefixed: description-only entry (rare).
            completions_with_descriptions+=("${comp}")
        elif [[ "$comp" == *$'\t'* ]]; then
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

    # The directive is a bitmask: 1 - error, 2 - no space, 4 - no files.
    if (( directive & 1 )); then
        _message 'error occurred during completion'
    fi
    if (( directive & 2 )); then
        _message 'no space left'
    fi
    if (( directive & 4 )); then
        _message 'no file completion'
    fi
    # Default: futi allows file completion (flag values are
    # paths). The completion function fully controls the candidates,
    # so we generate the file candidates ourselves.
    if (( directive == 0 )); then
        _files
    fi

    return 0
}

if [ "$funcstack[1]" = "_futi" ]; then
    _futi "$@"
fi
`
