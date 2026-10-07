package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tivjnb/futi/internal/executor"
)

const (
	maxDownloadBytes = 100 << 20
	downloadTimeout  = 5 * time.Minute
)

// downloadLimit — действующий лимит размера скачивания (переменная — для тестов).
var downloadLimit int64 = maxDownloadBytes

var errDownloadTooLarge = errors.New("exceeds 100 MiB limit")

// limitWriter пишет в w не больше left байт, дальше возвращает errDownloadTooLarge
// (curl --max-filesize не срабатывает на ответ без Content-Length).
type limitWriter struct {
	w        io.Writer
	left     int64
	exceeded bool // exec.Cmd.Wait отбрасывает ошибку копирования при ненулевом выходе curl
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.left {
		l.exceeded = true
		return 0, errDownloadTooLarge
	}
	l.left -= int64(len(p))
	return l.w.Write(p)
}

// checkTransportSource отвергает опасные значения source/ref до запуска
// git/curl: ведущий "-" (читался бы как опция) и незащищённые http://, git://.
func checkTransportSource(src string, kind sourceKind, ref string) error {
	if strings.HasPrefix(src, "-") {
		return fmt.Errorf("source %q must not start with \"-\"", src)
	}
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("ref %q must not start with \"-\"", ref)
	}
	if kind == kindGit || kind == kindArchive {
		lower := strings.ToLower(src)
		for _, scheme := range []string{"http://", "git://"} {
			if strings.HasPrefix(lower, scheme) {
				return fmt.Errorf("insecure %s source %q: only https:// is supported; download it manually and install from a local path", scheme, src)
			}
		}
	}
	return nil
}

// fetchGit клонирует репозиторий (--depth=1) в tempBase/repo, удаляет .git
// и возвращает каталог модуля. commit — full SHA HEAD (до удаления .git).
func fetchGit(ctx context.Context, url, ref, tempBase string) (string, string, error) {
	dest := filepath.Join(tempBase, "repo")

	args := []string{"clone", "--depth=1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, "--", url, dest)
	if err := executor.RunWithConfig(ctx, executor.NewConfig(), "git", args...); err != nil {
		return "", "", fmt.Errorf("git clone: %w", err)
	}

	// full SHA до удаления .git.
	var buf bytes.Buffer
	cfg := executor.NewConfig().WithDir(dest).WithStdout(&buf)
	if err := executor.RunWithConfig(ctx, cfg, "git", "rev-parse", "HEAD"); err != nil {
		return "", "", fmt.Errorf("git rev-parse: %w", err)
	}
	commit := buf.String()
	if i := strings.IndexByte(commit, '\n'); i >= 0 {
		commit = commit[:i]
	}

	if err := cleanupExcludedDirs(dest); err != nil {
		return "", "", err
	}
	dir, err := resolveModuleDir(dest)
	if err != nil {
		return "", "", err
	}
	return dir, commit, nil
}

// fetchArchive скачивает архив (curl -fsSL) и распаковывает в
// tempBase/extracted. Возвращает каталог модуля.
func fetchArchive(ctx context.Context, url, tempBase string) (string, error) {
	if _, err := exec.LookPath("curl"); err != nil {
		return "", fmt.Errorf("curl not found; install curl to install from web archives")
	}

	archivePath := filepath.Join(tempBase, "archive"+archiveExt(url))
	f, err := os.Create(archivePath)
	if err != nil {
		return "", err
	}
	lw := &limitWriter{w: f, left: downloadLimit}
	cfg := executor.NewConfig().WithStdout(lw)
	err = executor.RunWithConfig(ctx, cfg, "curl",
		"-fsSL", "--proto", "=https", "--proto-redir", "=https",
		"--max-filesize", strconv.FormatInt(maxDownloadBytes, 10),
		"--max-time", strconv.Itoa(int(downloadTimeout/time.Second)),
		"--", url)
	f.Close()
	if lw.exceeded {
		// Без обёртки executor: сообщение должно быть коротким и понятным.
		return "", fmt.Errorf("download archive: %w", errDownloadTooLarge)
	}
	if err != nil {
		return "", fmt.Errorf("download archive: %w", err)
	}

	extracted := filepath.Join(tempBase, "extracted")
	if err := os.MkdirAll(extracted, 0o755); err != nil {
		return "", err
	}
	if err := extractArchive(archivePath, extracted); err != nil {
		return "", err
	}
	return resolveModuleDir(extracted)
}
