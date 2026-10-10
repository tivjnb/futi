package metayaml

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)

	ts := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	m := &Meta{
		Lockfile: Lockfile{
			Source: "https://example.com/qr.git", Type: "git",
			Commit: "abc123", Hash: "sha256:aa",
			InstalledAt: ts, LastCheck: ts,
		},
		About: &About{Author: "foo@example.com", Tags: []string{"qr"}},
	}
	if err := Write(path, m); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Lockfile != m.Lockfile {
		t.Fatalf("lockfile round-trip: got %+v, want %+v", got.Lockfile, m.Lockfile)
	}
	if got.About == nil || got.About.Author != "foo@example.com" {
		t.Fatalf("about round-trip: got %+v", got.About)
	}
}

func TestWriteOverwritesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := Write(path, &Meta{Lockfile: Lockfile{Source: "a"}}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := Write(path, &Meta{Lockfile: Lockfile{Source: "b"}}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, _ := Read(path)
	if got.Lockfile.Source != "b" {
		t.Fatalf("expected overwrite, got %q", got.Lockfile.Source)
	}
	// После двух атомарных записей не осталось temp-файлов.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file after writes, got %d", len(entries))
	}
}

func TestMergeAboutPrefersSource(t *testing.T) {
	dest := &Meta{Lockfile: Lockfile{Hash: "sha256:old"}, About: &About{Author: "old@example.com"}}
	source := &Meta{Lockfile: Lockfile{Hash: "sha256:new"}, About: &About{Author: "new@example.com"}}

	merged := MergeAbout(dest, source)
	if merged.Lockfile.Hash != "sha256:old" {
		t.Fatalf("lockfile must come from dest, got %q", merged.Lockfile.Hash)
	}
	if merged.About.Author != "new@example.com" {
		t.Fatalf("about must come from source, got %q", merged.About.Author)
	}
}

func TestMergeAboutKeepsDestWhenSourceEmpty(t *testing.T) {
	dest := &Meta{Lockfile: Lockfile{Hash: "sha256:x"}, About: &About{Author: "keep@example.com"}}
	merged := MergeAbout(dest, &Meta{})
	if merged.About == nil || merged.About.Author != "keep@example.com" {
		t.Fatalf("about must be kept, got %+v", merged.About)
	}
}

func TestComputeTreeHashStable(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "config.yaml"), "name: a\n")
	write(t, filepath.Join(dir, "scripts", "run.sh"), "echo hi\n")

	h1, err := ComputeTreeHash(dir)
	if err != nil {
		t.Fatalf("ComputeTreeHash: %v", err)
	}
	h2, err := ComputeTreeHash(dir)
	if err != nil {
		t.Fatalf("ComputeTreeHash: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("hash not stable: %q vs %q", h1, h2)
	}
	if len(h1) != len("sha256:")+64 {
		t.Fatalf("unexpected hash format: %q", h1)
	}
}

func TestComputeTreeHashIgnoresExcludes(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "config.yaml"), "name: a\n")

	h1, err := ComputeTreeHash(dir)
	if err != nil {
		t.Fatalf("ComputeTreeHash: %v", err)
	}

	write(t, filepath.Join(dir, ".venv", "bin", "python"), "binary\n")
	write(t, filepath.Join(dir, "__pycache__", "x.pyc"), "junk\n")
	write(t, filepath.Join(dir, FileName), "lockfile: {}\n")

	h2, err := ComputeTreeHash(dir)
	if err != nil {
		t.Fatalf("ComputeTreeHash: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("excludes must not affect hash: %q vs %q", h1, h2)
	}
}

func TestComputeTreeHashDetectsChange(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "config.yaml"), "name: a\n")
	h1, err := ComputeTreeHash(dir)
	if err != nil {
		t.Fatalf("ComputeTreeHash: %v", err)
	}

	write(t, filepath.Join(dir, "config.yaml"), "name: b\n")
	h2, err := ComputeTreeHash(dir)
	if err != nil {
		t.Fatalf("ComputeTreeHash: %v", err)
	}
	if h1 == h2 {
		t.Fatal("hash must change when content changes")
	}
}

func TestWriteFileMode0644(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("права POSIX на Windows не проверяются")
	}
	dir := t.TempDir()
	m := &Meta{Lockfile: Lockfile{Source: "s", Type: "local", Hash: "sha256:aa"}}

	fresh := filepath.Join(dir, "fresh", FileName)
	if err := os.MkdirAll(filepath.Dir(fresh), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Write(fresh, m); err != nil {
		t.Fatalf("Write fresh: %v", err)
	}
	existing := filepath.Join(dir, FileName)
	if err := os.WriteFile(existing, []byte("old: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(existing, m); err != nil {
		t.Fatalf("Write existing: %v", err)
	}
	for _, p := range []string{fresh, existing} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != 0o644 {
			t.Errorf("%s: mode = %v, want 0644", p, got)
		}
	}
}

func TestMergeAboutEmptySourceKeepsDest(t *testing.T) {
	for name, src := range map[string]*About{"zero": {}, "empty tags": {Tags: []string{}}} {
		t.Run(name, func(t *testing.T) {
			dest := &Meta{About: &About{Author: "keep@example.com"}}
			merged := MergeAbout(dest, &Meta{About: src})
			if merged.About == nil || merged.About.Author != "keep@example.com" {
				t.Fatalf("empty source about must not overwrite dest, got %+v", merged.About)
			}
		})
	}
}

func TestReadEmptyFile(t *testing.T) {
	for name, content := range map[string]string{"zero bytes": "", "newline": "\n", "doc marker": "---\n", "tilde": "~\n"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			write(t, path, content)
			if m, err := Read(path); err == nil {
				t.Fatalf("Read of empty file: want error, got %+v", m)
			}
		})
	}
}

func TestReadAboutOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	write(t, path, "about: {author: x}\n")
	m, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if m.About == nil || m.About.Author != "x" {
		t.Fatalf("About = %+v, want Author x", m.About)
	}
}

// treeHash — ComputeTreeHash с провалом теста при ошибке.
func treeHash(t *testing.T, root string) string {
	t.Helper()
	h, err := ComputeTreeHash(root)
	if err != nil {
		t.Fatalf("ComputeTreeHash(%q): %v", root, err)
	}
	return h
}

func TestTreeHashExecBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("бита исполнения на Windows нет")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "run.sh")
	write(t, script, "echo hi\n")
	before := treeHash(t, dir)

	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	if after := treeHash(t, dir); after == before {
		t.Fatal("hash must change when only the exec bit changes")
	}
	// Прочие биты прав в хеш не входят: umask и checkout дают разные.
	if err := os.Chmod(script, 0o700); err != nil {
		t.Fatal(err)
	}
	execOnly := treeHash(t, dir)
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	if again := treeHash(t, dir); again != execOnly {
		t.Fatal("hash must depend on the exec bit only, not on the full mode")
	}
}

func TestTreeHashSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("симлинки на Windows требуют прав")
	}
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.sh"), "echo a\n")
	write(t, filepath.Join(dir, "b.sh"), "echo b\n")
	link := filepath.Join(dir, "current")
	if err := os.Symlink("a.sh", link); err != nil {
		t.Fatal(err)
	}
	before := treeHash(t, dir)

	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("b.sh", link); err != nil {
		t.Fatal(err)
	}
	if after := treeHash(t, dir); after == before {
		t.Fatal("hash must change when a symlink target changes")
	}
}

// Симлинк на месте корня хешируется как дерево, на которое он указывает,
// а не как пустой каталог.
func TestTreeHashRootSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("симлинки на Windows требуют прав")
	}
	real := t.TempDir()
	write(t, filepath.Join(real, "config.yaml"), "name: a\n")
	link := filepath.Join(t.TempDir(), "mod")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if got, want := treeHash(t, link), treeHash(t, real); got != want {
		t.Fatalf("hash via root symlink = %q, want %q (hash of target tree)", got, want)
	}
}

func TestDiffTrees(t *testing.T) {
	oldRoot := t.TempDir()
	newRoot := t.TempDir()
	write(t, filepath.Join(oldRoot, "config.yaml"), "name: a\n")
	write(t, filepath.Join(newRoot, "config.yaml"), "name: b\n")
	write(t, filepath.Join(oldRoot, "same.txt"), "same\n")
	write(t, filepath.Join(newRoot, "same.txt"), "same\n")
	write(t, filepath.Join(oldRoot, "local", "notes.txt"), "mine\n")
	write(t, filepath.Join(newRoot, "scripts", "new.sh"), "echo\n")
	write(t, filepath.Join(newRoot, "a.txt"), "a\n")
	// Исключения не сравниваются.
	write(t, filepath.Join(oldRoot, FileName), "lockfile: {}\n")
	write(t, filepath.Join(oldRoot, ".venv", "bin", "python"), "x\n")
	write(t, filepath.Join(newRoot, "node_modules", "m", "index.js"), "y\n")
	write(t, filepath.Join(newRoot, "sub", ".git", "HEAD"), "ref\n")

	got, err := DiffTrees(oldRoot, newRoot)
	if err != nil {
		t.Fatalf("DiffTrees: %v", err)
	}
	want := TreeDiff{
		Added:    []string{"a.txt", "scripts/new.sh"},
		Modified: []string{"config.yaml"},
		Deleted:  []string{"local/notes.txt"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiffTrees = %+v, want %+v", got, want)
	}

	same, err := DiffTrees(oldRoot, oldRoot)
	if err != nil {
		t.Fatalf("DiffTrees same: %v", err)
	}
	if !same.Empty() {
		t.Fatalf("DiffTrees(root, root) = %+v, want empty", same)
	}
}
