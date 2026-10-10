package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxExtractBytes = 500 << 20
	maxExtractFiles = 10000
)

// Действующие лимиты распаковки (переменные — для тестов).
var (
	extractBytesLimit int64 = maxExtractBytes
	extractFilesLimit       = maxExtractFiles
)

var (
	errExtractTooManyBytes = errors.New("archive exceeds limit: more than 500 MiB unpacked")
	errExtractTooManyFiles = errors.New("archive exceeds limit: more than 10000 files and directories")
)

// extractArchive распаковывает архив path в dest. Формат определяется по
// содержимому (zip: PK\x03\x04, gzip: \x1f\x8b), а не по имени файла.
// Поддержка: zip, tar.gz. bz2/xz — не поддерживаются.
func extractArchive(path, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	var magic [4]byte
	n, _ := io.ReadFull(f, magic[:])
	f.Close()
	switch {
	case n == 4 && magic == [4]byte{'P', 'K', 3, 4}:
		return extractZip(path, dest)
	case n >= 2 && magic[0] == 0x1f && magic[1] == 0x8b:
		return extractTarGz(path, dest)
	default:
		return errUnsupportedArchive("")
	}
}

// errUnsupportedArchive — ошибка неподдерживаемого формата; ext (может быть
// пустым) — расширение из URL.
func errUnsupportedArchive(ext string) error {
	if ext != "" {
		ext = " " + ext
	}
	return fmt.Errorf("unsupported archive format%s (supported: .tar.gz, .tgz, .zip)", ext)
}

// safeJoin присоединяет name к dest с защитой от zip-slip: результирующий
// путь обязан остаться внутри dest (спека §8 #12). Entry с traversal
// ("../evil.txt") — ошибка, а не молчаливая перезапись пути.
func safeJoin(dest, name string) (string, error) {
	target := filepath.Join(dest, name)
	rel, err := filepath.Rel(dest, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry %q escapes destination directory", name)
	}
	return target, nil
}

// fileMode — права извлекаемого файла: только rwx для ugo (setuid/setgid/sticky
// отбрасываются), владелец всегда может читать и писать (mode 0 в архиве не
// должен давать нечитаемый файл).
func fileMode(m os.FileMode) os.FileMode {
	return m&0o777 | 0o600
}

// extractBudget — общий на архив остаток лимитов распаковки.
type extractBudget struct {
	bytes int64
	files int
}

func newExtractBudget() *extractBudget {
	return &extractBudget{bytes: extractBytesLimit, files: extractFilesLimit}
}

// countEntry списывает одну запись (файл или каталог) из лимита.
func (b *extractBudget) countEntry() error {
	if b.files <= 0 {
		return errExtractTooManyFiles
	}
	b.files--
	return nil
}

// mkdirAll создаёт каталог target и недостающие родители внутри dest,
// списывая из бюджета каждый реально созданный каталог. Уже существующие
// не списываются повторно.
func (b *extractBudget) mkdirAll(dest, target string) error {
	for d := target; d != dest && d != filepath.Dir(d); d = filepath.Dir(d) {
		if _, err := os.Lstat(d); err == nil {
			break
		}
		if err := b.countEntry(); err != nil {
			return err
		}
	}
	return os.MkdirAll(target, 0o755)
}

// writeFile создаёт regular-файл target с правами mode и копирует в него src,
// списывая файл и фактически записанные байты из бюджета.
func (b *extractBudget) writeFile(target string, mode os.FileMode, src io.Reader) error {
	if err := b.countEntry(); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	// +1 байт сверх остатка отличает «ровно в лимит» от превышения.
	n, err := io.Copy(out, io.LimitReader(src, b.bytes+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n > b.bytes {
		return errExtractTooManyBytes
	}
	b.bytes -= n
	return nil
}

// extractTarGz распаковывает .tar.gz/.tgz. Символические ссылки и прочие
// не-файлы пропускаются (MVP).
func extractTarGz(path, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	b := newExtractBudget()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}

		target, err := safeJoin(dest, hdr.Name)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := b.mkdirAll(dest, target); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := b.mkdirAll(dest, filepath.Dir(target)); err != nil {
				return err
			}
			if err := b.writeFile(target, fileMode(os.FileMode(hdr.Mode)), tr); err != nil {
				return err
			}
		}
	}
}

// extractZip распаковывает .zip.
func extractZip(path, dest string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("zip: %w", err)
	}
	defer zr.Close()

	b := newExtractBudget()
	for _, zf := range zr.File {
		target, err := safeJoin(dest, zf.Name)
		if err != nil {
			return err
		}
		if zf.FileInfo().IsDir() {
			if err := b.mkdirAll(dest, target); err != nil {
				return err
			}
			continue
		}
		// Не каталог и не regular-файл (symlink и т.п.) — пропуск, как в tar.
		if zf.Mode().Type() != 0 {
			continue
		}
		if err := b.mkdirAll(dest, filepath.Dir(target)); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		writeErr := b.writeFile(target, fileMode(zf.Mode()), rc)
		rc.Close()
		if writeErr != nil {
			return writeErr
		}
	}
	return nil
}
