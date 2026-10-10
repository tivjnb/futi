package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tarEntry — запись для buildTarGz.
type tarEntry struct {
	name string
	mode int64
	body string
}

func buildTarGz(t *testing.T, entries []tarEntry) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: e.mode, Size: int64(len(e.body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// zipEntry — запись для buildZip; mode задаётся через SetMode.
type zipEntry struct {
	name string
	mode os.FileMode
	body string
}

func buildZip(t *testing.T, entries []zipEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		hdr.SetMode(e.mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// assertSafePerm проверяет независимо от umask: нет setuid/setgid/sticky и
// владелец может читать и писать. Точное маскирование — в TestFileMode.
func assertSafePerm(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	m := fi.Mode()
	if m&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		t.Fatalf("mode of %s = %v, special bits must be dropped", filepath.Base(path), m)
	}
	if m.Perm()&0o600 != 0o600 {
		t.Fatalf("mode of %s = %v, owner must have rw", filepath.Base(path), m)
	}
}

func TestFileMode(t *testing.T) {
	cases := []struct {
		in   os.FileMode
		want os.FileMode
	}{
		{os.ModeSetuid | 0o755, 0o755},
		{os.ModeSetgid | 0o755, 0o755},
		{os.ModeSticky | 0o777, 0o777},
		{0, 0o600},
		{0o400, 0o600},
	}
	for _, tc := range cases {
		if got := fileMode(tc.in); got != tc.want {
			t.Errorf("fileMode(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestExtractZipDropsSetuidBits(t *testing.T) {
	archive := buildZip(t, []zipEntry{{"x", os.ModeSetuid | 0o755, "x"}})
	dest := t.TempDir()
	if err := extractArchive(archive, dest); err != nil {
		t.Fatal(err)
	}
	assertSafePerm(t, filepath.Join(dest, "x"))
}

func TestExtractZeroModeBecomesOwnerReadable(t *testing.T) {
	t.Run("zip", func(t *testing.T) {
		archive := buildZip(t, []zipEntry{{"x", 0, "x"}})
		dest := t.TempDir()
		if err := extractArchive(archive, dest); err != nil {
			t.Fatal(err)
		}
		assertSafePerm(t, filepath.Join(dest, "x"))
	})
	t.Run("tar", func(t *testing.T) {
		archive := buildTarGz(t, []tarEntry{{"x", 0, "x"}})
		dest := t.TempDir()
		if err := extractArchive(archive, dest); err != nil {
			t.Fatal(err)
		}
		assertSafePerm(t, filepath.Join(dest, "x"))
	})
}

func TestExtractTarDropsSetuidBits(t *testing.T) {
	archive := buildTarGz(t, []tarEntry{{"x", 0o4755, "x"}})
	dest := t.TempDir()
	if err := extractArchive(archive, dest); err != nil {
		t.Fatal(err)
	}
	assertSafePerm(t, filepath.Join(dest, "x"))
}

func TestExtractZipSkipsSymlinkEntry(t *testing.T) {
	archive := buildZip(t, []zipEntry{
		{"link", os.ModeSymlink | 0o777, "/etc/passwd"},
		{"ok.txt", 0o644, "ok"},
	})
	dest := t.TempDir()
	if err := extractArchive(archive, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dest, "link")); !os.IsNotExist(err) {
		t.Fatalf("symlink entry must be skipped, Lstat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "ok.txt")); err != nil {
		t.Fatalf("regular entry must be extracted: %v", err)
	}
}

func setExtractLimits(t *testing.T, bytes int64, files int) {
	t.Helper()
	oldB, oldF := extractBytesLimit, extractFilesLimit
	extractBytesLimit, extractFilesLimit = bytes, files
	t.Cleanup(func() { extractBytesLimit, extractFilesLimit = oldB, oldF })
}

func TestExtractLimits(t *testing.T) {
	archives := map[string]func(t *testing.T, bodies []string) string{
		"tar": func(t *testing.T, bodies []string) string {
			var es []tarEntry
			for i, b := range bodies {
				es = append(es, tarEntry{string(rune('a'+i)) + ".txt", 0o644, b})
			}
			return buildTarGz(t, es)
		},
		"zip": func(t *testing.T, bodies []string) string {
			var es []zipEntry
			for i, b := range bodies {
				es = append(es, zipEntry{string(rune('a'+i)) + ".txt", 0o644, b})
			}
			return buildZip(t, es)
		},
	}
	cases := []struct {
		name     string
		bodies   []string
		maxBytes int64
		maxFiles int
		wantErr  error
	}{
		{"total bytes over limit", []string{"123456", "123456"}, 10, 100, errExtractTooManyBytes},
		{"total bytes at limit", []string{"12345", "12345"}, 10, 100, nil},
		{"files over limit", []string{"1", "2", "3"}, 1000, 2, errExtractTooManyFiles},
		{"files at limit", []string{"1", "2"}, 1000, 2, nil},
	}
	for format, build := range archives {
		for _, tc := range cases {
			t.Run(format+"/"+tc.name, func(t *testing.T) {
				setExtractLimits(t, tc.maxBytes, tc.maxFiles)
				err := extractArchive(build(t, tc.bodies), t.TempDir())
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("extractArchive err = %v, want %v", err, tc.wantErr)
				}
			})
		}
	}
}

func TestExtractLimitsCountDirectories(t *testing.T) {
	setExtractLimits(t, 1000, 2)
	t.Run("tar", func(t *testing.T) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for _, n := range []string{"a/", "b/", "c/"} {
			if err := tw.WriteHeader(&tar.Header{Name: n, Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
				t.Fatal(err)
			}
		}
		tw.Close()
		gz.Close()
		path := filepath.Join(t.TempDir(), "d.tar.gz")
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := extractArchive(path, t.TempDir()); !errors.Is(err, errExtractTooManyFiles) {
			t.Fatalf("err = %v, want errExtractTooManyFiles", err)
		}
	})
	t.Run("zip", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "d.zip")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(f)
		for _, n := range []string{"a/", "b/", "c/"} {
			if _, err := zw.Create(n); err != nil {
				t.Fatal(err)
			}
		}
		zw.Close()
		f.Close()
		if err := extractArchive(path, t.TempDir()); !errors.Is(err, errExtractTooManyFiles) {
			t.Fatalf("err = %v, want errExtractTooManyFiles", err)
		}
	})
}

// TestExtractLimitsCountIntermediateDirs: промежуточные каталоги пути входят в лимит
// записей (audit F50): a/b/c/d/f = 4 каталога + файл = 5 записей.
func TestExtractLimitsCountIntermediateDirs(t *testing.T) {
	builders := map[string]func(t *testing.T) string{
		"tar": func(t *testing.T) string { return buildTarGz(t, []tarEntry{{"a/b/c/d/f", 0o644, "x"}}) },
		"zip": func(t *testing.T) string { return buildZip(t, []zipEntry{{"a/b/c/d/f", 0o644, "x"}}) },
	}
	cases := []struct {
		name     string
		maxFiles int
		wantErr  error
	}{
		{"over limit", 4, errExtractTooManyFiles},
		{"at limit", 5, nil},
	}
	for format, build := range builders {
		for _, tc := range cases {
			t.Run(format+"/"+tc.name, func(t *testing.T) {
				setExtractLimits(t, 1000, tc.maxFiles)
				err := extractArchive(build(t), t.TempDir())
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("extractArchive err = %v, want %v", err, tc.wantErr)
				}
			})
		}
	}
}

func TestExtractArchiveByContent(t *testing.T) {
	// Формат определяется по содержимому: имя без расширения.
	copyAs := func(t *testing.T, src string) string {
		t.Helper()
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(t.TempDir(), "archive")
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	t.Run("zip", func(t *testing.T) {
		p := copyAs(t, buildZip(t, []zipEntry{{"a.txt", 0o644, "zip-body"}}))
		dest := t.TempDir()
		if err := extractArchive(p, dest); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(b) != "zip-body" {
			t.Errorf("a.txt = %q, want zip-body", b)
		}
	})
	t.Run("tar.gz", func(t *testing.T) {
		p := copyAs(t, buildTarGz(t, []tarEntry{{"a.txt", 0o644, "tar-body"}}))
		dest := t.TempDir()
		if err := extractArchive(p, dest); err != nil {
			t.Fatal(err)
		}
		if b, _ := os.ReadFile(filepath.Join(dest, "a.txt")); string(b) != "tar-body" {
			t.Errorf("a.txt = %q, want tar-body", b)
		}
	})
	t.Run("zip под именем .tar.gz", func(t *testing.T) {
		data, _ := os.ReadFile(buildZip(t, []zipEntry{{"a.txt", 0o644, "x"}}))
		p := filepath.Join(t.TempDir(), "m.tar.gz")
		os.WriteFile(p, data, 0o600)
		if err := extractArchive(p, t.TempDir()); err != nil {
			t.Fatalf("err = %v, want nil (формат по содержимому)", err)
		}
	})
	t.Run("мусор", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "archive")
		os.WriteFile(p, []byte("not an archive at all"), 0o600)
		err := extractArchive(p, t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "unsupported archive format") {
			t.Fatalf("err = %v, want unsupported archive format", err)
		}
	})
}
