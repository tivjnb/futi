package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tivjnb/futi/internal/core"
	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/modulerunner"
	"github.com/tivjnb/futi/internal/termtext"

	"github.com/olekukonko/tablewriter"
)

// runList выводит содержимое namespace или команды модуля.
// Если namespace резолвится как модуль — показывает команды (-r игнорируется).
// Если namespace содержит путь к команде с subcommands — показывает subcommands.
// Иначе — показывает непосредственных детей namespace, или всё поддерево при recurse.
// При verbose=true добавляет колонку PATH.
// loader используется для загрузки модулей (с кэшем или без).
func runList(w io.Writer, source discovery.Source, namespace string, verbose, recurse bool, loader modulerunner.ModuleLoader) error {
	res, err := core.ListIn(source, loader, namespace, recurse)
	// Частичный листинг: результат валиден, выводим его и возвращаем ошибку,
	// чтобы вызывающий показал предупреждения.
	var partial *discovery.PartialError
	if err != nil && !errors.As(err, &partial) {
		return err
	}
	if res.Kind == core.ListNamespace {
		if len(res.Children) == 0 {
			fmt.Fprintln(w, "No modules found.")
			return err
		}
		if rerr := renderChildren(w, res.Children, verbose); rerr != nil {
			return rerr
		}
		return err
	}
	return renderCommandTable(w, res, verbose)
}

func printCommands(w io.Writer, manifestPath, moduleRoot string, verbose bool, loader modulerunner.ModuleLoader) error {
	res, err := core.ModuleCommands(loader, manifestPath, moduleRoot)
	if err != nil {
		return err
	}
	return renderCommandTable(w, res, verbose)
}

// renderCommandTable prints commands as a table, used for a module's
// top-level commands and for subcommands.
// res.HasDefault adds a (default) row first in the table (the module's
// default cmd or the subcommand's fallback cmd).
func renderCommandTable(w io.Writer, res core.ListResult, verbose bool) error {
	if len(res.Commands) == 0 && !res.HasDefault {
		fmt.Fprintln(w, "No commands found.")
		return nil
	}

	var rows [][]string
	for _, c := range res.Commands {
		if verbose {
			rows = append(rows, []string{termtext.Line(c.Name), termtext.Line(c.Description), termtext.Line(res.Manifest)})
		} else {
			rows = append(rows, []string{termtext.Line(c.Name), termtext.Line(c.Description)})
		}
	}

	// Sort by the sanitized name.
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i][0] < rows[j][0]
	})

	if res.HasDefault {
		var defaultRow []string
		if verbose {
			defaultRow = []string{"(default)", "", termtext.Line(res.Manifest)}
		} else {
			defaultRow = []string{"(default)", ""}
		}
		rows = append([][]string{defaultRow}, rows...)
	}

	tw := tablewriter.NewTable(w)
	if verbose {
		tw.Header("COMMAND", "DESCRIPTION", "PATH")
	} else {
		tw.Header("COMMAND", "DESCRIPTION")
	}
	if err := tw.Bulk(rows); err != nil {
		return fmt.Errorf("list: render: %w", err)
	}
	if err := tw.Render(); err != nil {
		return fmt.Errorf("list: render: %w", err)
	}
	return nil
}

// renderChildren prints the children of a namespace as a table.
func renderChildren(w io.Writer, children []core.ChildEntry, verbose bool) error {
	var rows [][]string
	for _, c := range children {
		typ := "namespace"
		var desc, path string
		if c.IsModule {
			typ = "module"
			path = termtext.Line(c.Path)
			if c.Conflict && len(c.ConflictSources) > 0 {
				// Модуль с одним именем в нескольких sources.
				desc = "conflict: module in several sources: " + termtext.Line(strings.Join(c.ConflictSources, ", "))
			} else if c.Conflict {
				// ManifestPath конфликтной записи — от x.yaml; c.Name в `list -r` —
				// полный путь (ns.x), поэтому имена файлов берём из пути.
				base := filepath.Base(c.Path)
				desc = termtext.Line("conflict: " + base + " and " + strings.TrimSuffix(base, ".yaml") + "/config.yaml")
			} else if c.LoadErr != nil {
				// Битый модуль не должен быть неотличим от модуля без описания.
				// Полную причину (с путём) не печатаем: колонка раздувается, путь
				// есть в PATH при -v, причину покажет validate.
				desc = "(error: cannot load module; run 'futi modules validate <namespace>')"
			} else {
				desc = termtext.Line(c.Description)
			}
		}
		if verbose {
			rows = append(rows, []string{typ, termtext.Line(c.Name), desc, path})
		} else {
			rows = append(rows, []string{typ, termtext.Line(c.Name), desc})
		}
	}

	// По имени, при равных именах (модуль и namespace x) — по типу.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i][1] != rows[j][1] {
			return rows[i][1] < rows[j][1]
		}
		return rows[i][0] < rows[j][0]
	})

	tw := tablewriter.NewTable(w)
	if verbose {
		tw.Header("TYPE", "NAME", "DESCRIPTION", "PATH")
	} else {
		tw.Header("TYPE", "NAME", "DESCRIPTION")
	}
	if err := tw.Bulk(rows); err != nil {
		return fmt.Errorf("list: render: %w", err)
	}
	if err := tw.Render(); err != nil {
		return fmt.Errorf("list: render: %w", err)
	}
	return nil
}

// listErrorText — текст итоговой ошибки list: короткий, если причина уже
// напечатана предупреждением, иначе полный.
func listErrorText(err error, namespace string) string {
	if coveredByWarning(err) {
		return coveredReadMessage("list", namespace)
	}
	return err.Error()
}
