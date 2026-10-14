package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/metayaml"
	"github.com/tivjnb/futi/internal/termtext"
)

// runModulesPath выводит каталог модуля (для скриптов).
func runModulesPath(w io.Writer, source discovery.Source, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: futi modules path <name>")
	}
	if err := validateResolveName(args[0]); err != nil {
		return err
	}
	manifestPath, _, err := source.Resolve(args[0])
	if err != nil {
		return fmt.Errorf("modules path: %w", err)
	}
	fmt.Fprintln(w, filepath.Dir(manifestPath))
	return nil
}

// runModulesInfo выводит детали модуля: путь + содержимое __futi_meta.yaml.
// Модуль без sidecar — не ошибка: выводим путь и примечание.
func runModulesInfo(w io.Writer, source discovery.Source, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: futi modules info <name>")
	}
	if err := validateResolveName(args[0]); err != nil {
		return err
	}
	manifestPath, moduleRoot, err := source.Resolve(args[0])
	if err != nil {
		return fmt.Errorf("modules info: %w", err)
	}

	fmt.Fprintf(w, "Path: %s\n", termtext.Line(moduleRoot))

	metaPath := filepath.Join(filepath.Dir(manifestPath), metayaml.FileName)
	if _, err := os.Stat(metaPath); err != nil {
		fmt.Fprintln(w, "Installed: no (no __futi_meta.yaml — hand-authored or created via modules init)")
		return nil
	}

	meta, err := metayaml.Read(metaPath)
	if err != nil {
		return fmt.Errorf("modules info: %w", err)
	}

	lf := meta.Lockfile
	fmt.Fprintf(w, "Source: %s\n", termtext.Line(lf.Source))
	fmt.Fprintf(w, "Type: %s\n", termtext.Line(lf.Type))
	if lf.Type == "git" && lf.Ref != "" {
		fmt.Fprintf(w, "Ref: %s\n", termtext.Line(lf.Ref))
	}
	if lf.Commit != "" {
		fmt.Fprintf(w, "Commit: %s\n", termtext.Line(lf.Commit))
	}
	if lf.Hash != "" {
		fmt.Fprintf(w, "Hash: %s\n", termtext.Line(lf.Hash))
	}
	if !lf.InstalledAt.IsZero() {
		fmt.Fprintf(w, "Installed at: %s\n", lf.InstalledAt.Format("2006-01-02 15:04:05"))
	}
	if !lf.LastCheck.IsZero() {
		fmt.Fprintf(w, "Last check: %s\n", lf.LastCheck.Format("2006-01-02 15:04:05"))
	}
	if meta.About != nil {
		if meta.About.Author != "" {
			fmt.Fprintf(w, "Author: %s\n", termtext.Line(meta.About.Author))
		}
		if meta.About.License != "" {
			fmt.Fprintf(w, "License: %s\n", termtext.Line(meta.About.License))
		}
		if meta.About.Homepage != "" {
			fmt.Fprintf(w, "Homepage: %s\n", termtext.Line(meta.About.Homepage))
		}
		if len(meta.About.Tags) > 0 {
			fmt.Fprintf(w, "Tags: %s\n", termtext.Line(strings.Join(meta.About.Tags, ", ")))
		}
	}
	return nil
}
