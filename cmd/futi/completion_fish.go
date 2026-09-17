package main

// fishCompletionScript — статичный fish-скрипт автодополнения для futi.
// Адаптирован из cobra (BSD-3-Clause). fish всегда отключает файловое
// дополнение (-f), что соответствует NoFileComp по умолчанию для futi.
//
// REV-003: бинарь вызывается напрямую с массивом аргументов, без eval.
// В fish переменные в позиции команды раскрываются в отдельные argv без
// повторного парсинга как кода, поэтому токены с $(...), ';', '>' из
// набранной строки не исполняются при TAB, а пробельные токены не
// распадаются (в отличие от eval, который парсит склеенную строку как код).
const fishCompletionScript = `# fish completion for futi
# Adapted from cobra (BSD-3-Clause).

function __futi_perform_completion
    set -l args (commandline -opc)
    set -l cursor (commandline -ct)

    if test -z "$cursor"
        # Cursor at end of line: append an empty toComplete argument.
        set args $args ""
    end

    set -l results ($args[1] __complete $args[2..-1] 2>/dev/null)

    set -l directive 0
    if set -q results[-1]
        set -l last $results[-1]
        if string match -q ':*' -- $last
            set directive (string sub -s 2 -- $last)
            set -e results[-1]
        end
    end

    # Directive Default (0): futi разрешает файловое дополнение —
    # значения флагов (пути). fish глобально запретил файлы (-f),
    # поэтому генерируем кандидатов сами.
    if test "$directive" = 0
        __fish_complete_path "$cursor" 2>/dev/null
        return
    end

    # Print candidates, stripping any description after a tab.
    for r in $results
        set -l parts (string split \t -- $r)
        printf '%s\n' $parts[1]
    end
end

complete -c futi -f
complete -c futi -a '(__futi_perform_completion)'
`
