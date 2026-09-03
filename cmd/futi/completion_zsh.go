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

    local -a args
    args=("${words[@]:1}")

    response=("${(@f)$(futi __complete "${args[@]}" 2>/dev/null)}")

    local directive
    directive="${response[-1]#:}"
    if [[ -z "$directive" ]]; then
        directive=0
    fi

    # Drop the directive line.
    response=("${(@)response[1,-2]}")

    for comp in "${response[@]}"; do
        if [[ "$comp" == $'\t'* ]]; then
            # Tab-prefixed: description-only entry (rare).
            completions_with_descriptions+=("${comp}")
        elif [[ "$comp" == *$'\t'* ]]; then
            # "text\tdescription" — split for _describe.
            completions_with_descriptions+=("${comp%%$'\t'*}:${comp#*$'\t'}")
        else
            completions+=("${comp}")
        fi
    done

    if (( ${#completions[@]} )); then
        _describe 'values' completions
    fi
    if (( ${#completions_with_descriptions[@]} )); then
        _describe -d 'values' completions_with_descriptions
    fi

    case "$directive" in
        1) _message 'error occurred during completion' ;;
        2) _message 'no space left' ;;
        4) _message 'no file completion' ;;
        *) ;;
    esac

    return 0
}

if [ "$funcstack[1]" = "_futi" ]; then
    _futi "$@"
fi
`
