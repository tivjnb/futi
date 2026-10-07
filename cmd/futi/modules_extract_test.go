package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
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

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode(); got != want {
		t.Fatalf("mode of %s = %v, want %v", filepath.Base(path), got, want)
	}
}

func TestExtractZipDropsSetuidBits(t *testing.T) {
	archive := buildZip(t, []zipEntry{{"x", os.ModeSetuid | 0o755, "x"}})
	dest := t.TempDir()
	if err := extractArchive(archive, dest); err != nil {
		t.Fatal(err)
	}
	assertPerm(t, filepath.Join(dest, "x"), 0o755)
}

func TestExtractZeroModeBecomesOwnerReadable(t *testing.T) {
	t.Run("zip", func(t *testing.T) {
		archive := buildZip(t, []zipEntry{{"x", 0, "x"}})
		dest := t.TempDir()
		if err := extractArchive(archive, dest); err != nil {
			t.Fatal(err)
		}
		assertPerm(t, filepath.Join(dest, "x"), 0o600)
	})
	t.Run("tar", func(t *testing.T) {
		archive := buildTarGz(t, []tarEntry{{"x", 0, "x"}})
		dest := t.TempDir()
		if err := extractArchive(archive, dest); err != nil {
			t.Fatal(err)
		}
		assertPerm(t, filepath.Join(dest, "x"), 0o600)
	})
}

func TestExtractTarDropsSetuidBits(t *testing.T) {
	archive := buildTarGz(t, []tarEntry{{"x", 0o4755, "x"}})
	dest := t.TempDir()
	if err := extractArchive(archive, dest); err != nil {
		t.Fatal(err)
	}
	assertPerm(t, filepath.Join(dest, "x"), 0o755)
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
