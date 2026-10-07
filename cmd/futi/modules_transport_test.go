package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tivjnb/futi/internal/discovery"
	"github.com/tivjnb/futi/internal/metayaml"
)

// fakeBin кладёт в начало PATH исполняемые sh-скрипты name→тело. Скрипт
// дополнительно пишет свои argv (по одному в строке) в <name>.argv.
// Возвращает каталог с файлами argv.
func fakeBin(t *testing.T, scripts map[string]string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake sh executables are not supported on Windows")
	}
	bin := t.TempDir()
	for name, body := range scripts {
		script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >> \"" +
			filepath.Join(bin, name+".argv") + "\"; done\n" + body + "\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return bin
}

func readArgv(t *testing.T, bin, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(bin, name+".argv"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func TestFetchGitPassesDoubleDashBeforeURL(t *testing.T) {
	bin := fakeBin(t, map[string]string{"git": "exit 1"})
	_, _, _ = fetchGit(context.Background(), "https://example.com/r.git", "v1", t.TempDir())
	argv := readArgv(t, bin, "git")
	dd := indexOf(argv, "--")
	url := indexOf(argv, "https://example.com/r.git")
	if dd < 0 || url != dd+1 {
		t.Fatalf("git argv = %q, want \"--\" right before URL", argv)
	}
	if b := indexOf(argv, "--branch"); b < 0 || b > dd {
		t.Fatalf("git argv = %q, want --branch before \"--\"", argv)
	}
}

// TestFetchGitForbidsHTTPProtocol: git не должен идти по редиректу на http:// (audit F50).
func TestFetchGitForbidsHTTPProtocol(t *testing.T) {
	bin := fakeBin(t, map[string]string{"git": "exit 1"})
	_, _, _ = fetchGit(context.Background(), "https://example.com/r.git", "", t.TempDir())
	argv := readArgv(t, bin, "git")
	c := indexOf(argv, "-c")
	clone := indexOf(argv, "clone")
	if c != 0 || clone < 0 || argv[c+1] != "protocol.http.allow=never" || c > clone {
		t.Fatalf("git argv = %q, want -c protocol.http.allow=never before clone", argv)
	}
}

func TestFetchArchiveCurlHardeningArgs(t *testing.T) {
	bin := fakeBin(t, map[string]string{"curl": "exit 1"})
	_, _ = fetchArchive(context.Background(), "https://example.com/m.tar.gz", t.TempDir())
	argv := readArgv(t, bin, "curl")
	want := []string{"-fsSL", "--proto", "=https", "--proto-redir", "=https",
		"--max-filesize", "104857600", "--max-time", "300", "--", "https://example.com/m.tar.gz"}
	if strings.Join(argv, "\n") != strings.Join(want, "\n") {
		t.Fatalf("curl argv = %q, want %q", argv, want)
	}
}

func TestFetchSourceRejectsLeadingDash(t *testing.T) {
	bin := fakeBin(t, map[string]string{"git": "exit 0", "curl": "exit 0"})
	for _, kind := range []sourceKind{kindLocal, kindGit, kindArchive} {
		_, _, err := fetchSource(context.Background(), "-x", kind, "", t.TempDir())
		if err == nil || !strings.Contains(err.Error(), `must not start with "-"`) {
			t.Errorf("kind %v: err = %v, want leading-dash error", kind, err)
		}
	}
	_, _, err := fetchSource(context.Background(), "https://example.com/r.git", kindGit, "-x", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), `ref "-x" must not start with "-"`) {
		t.Errorf("ref: err = %v, want leading-dash error", err)
	}
	if a := readArgv(t, bin, "git"); a != nil {
		t.Errorf("git must not run, argv = %q", a)
	}
	if a := readArgv(t, bin, "curl"); a != nil {
		t.Errorf("curl must not run, argv = %q", a)
	}
}

func TestFetchSourceRejectsHTTP(t *testing.T) {
	bin := fakeBin(t, map[string]string{"git": "exit 0", "curl": "exit 0"})
	cases := []struct {
		src  string
		kind sourceKind
	}{
		{"http://e.com/m.tar.gz", kindArchive},
		{"http://e.com/r.git", kindGit},
		{"HTTP://e.com/r.git", kindGit},
		{"git://e.com/r.git", kindGit},
		{"GIT://e.com/r.git", kindGit},
	}
	for _, tc := range cases {
		_, _, err := fetchSource(context.Background(), tc.src, tc.kind, "", t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "only https:// is supported") {
			t.Errorf("%s: err = %v, want https error", tc.src, err)
		}
	}
	if a := readArgv(t, bin, "git"); a != nil {
		t.Errorf("git must not run, argv = %q", a)
	}
	if a := readArgv(t, bin, "curl"); a != nil {
		t.Errorf("curl must not run, argv = %q", a)
	}
}

// Локальный архив install не принимает, поэтому для архива подсказка — распаковать
// и установить каталог; для git — прежняя (audit F50).
func TestCheckTransportSourceHint(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		kind     sourceKind
		contains string
		absent   string
	}{
		{"archive", "http://e.com/m.tar.gz", kindArchive, "download and unpack it, then install the directory", "install from a local path"},
		{"git", "git://e.com/r.git", kindGit, "install from a local path", "unpack"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkTransportSource(tc.src, tc.kind, "")
			if err == nil {
				t.Fatal("want error")
			}
			if !strings.Contains(err.Error(), tc.contains) || strings.Contains(err.Error(), tc.absent) {
				t.Fatalf("err = %q, want containing %q and not %q", err, tc.contains, tc.absent)
			}
		})
	}
}

func TestDetectSourceTypeHTTPArchiveStillArchive(t *testing.T) {
	kind, err := detectSourceType("http://e.com/m.tar.gz", false, false, false)
	if err != nil || kind != kindArchive {
		t.Fatalf("got (%v, %v), want (archive, nil)", kind, err)
	}
}

func TestParseInstallArgsRejectsRefWithLeadingDash(t *testing.T) {
	for _, args := range [][]string{
		{"https://e.com/r.git", "--ref", "-x"},
		{"https://e.com/r.git", "--ref=-x"},
	} {
		_, err := parseInstallArgs(args)
		if err == nil || !strings.Contains(err.Error(), `must not start with "-"`) {
			t.Errorf("args %q: err = %v, want leading-dash error", args, err)
		}
	}
}

func TestFetchArchiveEnforcesSizeLimit(t *testing.T) {
	fakeBin(t, map[string]string{"curl": "head -c 1000000 /dev/zero"})
	old := downloadLimit
	downloadLimit = 10
	t.Cleanup(func() { downloadLimit = old })

	_, err := fetchArchive(context.Background(), "https://example.com/m.tar.gz", t.TempDir())
	if err == nil || err.Error() != "download archive: exceeds 100 MiB limit" {
		t.Fatalf("err = %v, want \"download archive: exceeds 100 MiB limit\"", err)
	}
}

func TestRunModulesUpdateRejectsHTTPSource(t *testing.T) {
	fakeBin(t, map[string]string{"curl": "exit 0"})
	global := t.TempDir()
	t.Setenv("FUTI_MODULES_DIR", global)
	t.Chdir(t.TempDir())

	src := makeSourceModule(t, t.TempDir(), "qr", "")
	cache := newTestCache()
	if err := runModulesInstall(&bytes.Buffer{}, nil, cache, []string{src}); err != nil {
		t.Fatalf("install: %v", err)
	}
	metaPath := filepath.Join(global, "qr", metayaml.FileName)
	meta, err := metayaml.Read(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	meta.Lockfile.Type = "archive"
	meta.Lockfile.Source = "http://example.com/qr.tar.gz"
	if err := metayaml.Write(metaPath, meta); err != nil {
		t.Fatal(err)
	}

	err = runModulesUpdate(&bytes.Buffer{}, nil, discovery.NewGlobalSource(global), cache, []string{"qr"})
	if err == nil || !strings.Contains(err.Error(), "only https:// is supported") {
		t.Fatalf("err = %v, want https error", err)
	}
}
