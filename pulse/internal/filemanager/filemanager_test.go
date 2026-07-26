package filemanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestListRoot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "server.properties", "server-port=25565\n")
	writeFile(t, dir, "plugins/Example.jar", "fake jar")
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}

	entries, err := List(dir, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(entries), entries)
	}
	// Directories-first, then alphabetical.
	if !entries[0].IsDir || !entries[1].IsDir {
		t.Fatalf("expected the two directories first, got %+v", entries)
	}
	if entries[0].Name != "logs" || entries[1].Name != "plugins" {
		t.Fatalf("unexpected directory order: %+v", entries)
	}
	if entries[2].Name != "server.properties" || entries[2].Path != "server.properties" {
		t.Fatalf("unexpected file entry: %+v", entries[2])
	}
}

func TestListNested(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "plugins/config/settings.yml", "a: b\n")

	entries, err := List(dir, "plugins")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "config" || entries[0].Path != "plugins/config" {
		t.Fatalf("unexpected nested listing: %+v", entries)
	}
}

func TestListEmptyDir(t *testing.T) {
	dir := t.TempDir()
	entries, err := List(dir, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty listing, got %+v", entries)
	}
}

func TestListNonExistentDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := List(dir, "does-not-exist"); err == nil {
		t.Fatal("expected an error listing a nonexistent directory")
	}
}

func TestDeleteFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "plugins/Example.jar", "fake jar")

	if err := Delete(dir, "plugins/Example.jar"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "plugins/Example.jar")); !os.IsNotExist(err) {
		t.Fatalf("expected file to be gone, stat err=%v", err)
	}
}

func TestDeleteDirectoryRecursive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "mods/a.jar", "a")
	writeFile(t, dir, "mods/nested/b.jar", "b")

	if err := Delete(dir, "mods"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "mods")); !os.IsNotExist(err) {
		t.Fatalf("expected mods dir to be gone, stat err=%v", err)
	}
}

func TestDeleteIdempotentOnMissing(t *testing.T) {
	dir := t.TempDir()
	if err := Delete(dir, "does-not-exist"); err != nil {
		t.Fatalf("expected deleting an already-missing path to be a no-op, got: %v", err)
	}
}

func TestDeleteRejectsWorkingDirItself(t *testing.T) {
	dir := t.TempDir()
	cases := []string{"", ".", "sub/..", "sub/../.."}
	for _, relPath := range cases {
		t.Run(relPath, func(t *testing.T) {
			if err := Delete(dir, relPath); err == nil {
				t.Fatalf("expected Delete(%q) to be rejected", relPath)
			}
			if _, statErr := os.Stat(dir); statErr != nil {
				t.Fatalf("working dir should still exist: %v", statErr)
			}
		})
	}
}

func TestPathTraversalRejected(t *testing.T) {
	dir := t.TempDir()
	outsideFile := filepath.Join(filepath.Dir(dir), "outside-secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	defer os.Remove(outsideFile)

	escapes := []string{
		"../outside-secret.txt",
		"a/../../outside-secret.txt",
	}
	for _, relPath := range escapes {
		t.Run(relPath, func(t *testing.T) {
			if _, err := List(dir, relPath); err == nil {
				t.Errorf("List(%q) should have been rejected", relPath)
			}
			if err := Delete(dir, relPath); err == nil {
				t.Errorf("Delete(%q) should have been rejected", relPath)
			}
			if _, err := Save(dir, relPath, strings.NewReader("evil")); err == nil {
				t.Errorf("Save(%q) should have been rejected", relPath)
			}
		})
	}
	// Confirm the outside file was genuinely untouched by any of the above.
	data, err := os.ReadFile(outsideFile)
	if err != nil || string(data) != "secret" {
		t.Fatalf("outside file was modified or removed: data=%q err=%v", data, err)
	}
}

// TestAbsolutePathIsSafelyNested documents that an absolute-looking relPath
// does NOT escape workingDir — filepath.Join doesn't reset to root on a
// later absolute-looking segment, it cleans and nests it as a regular
// subpath. Confirms this actually is safe (not merely "didn't error"): the
// real file at that absolute path is provably untouched.
func TestAbsolutePathIsSafelyNested(t *testing.T) {
	dir := t.TempDir()
	outsideFile := filepath.Join(filepath.Dir(dir), "outside-secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	defer os.Remove(outsideFile)

	if _, err := Save(dir, outsideFile, strings.NewReader("evil")); err != nil {
		t.Fatalf("Save with an absolute-path-shaped relPath should safely nest, not error: %v", err)
	}

	data, err := os.ReadFile(outsideFile)
	if err != nil || string(data) != "secret" {
		t.Fatalf("the real outside file was modified: data=%q err=%v", data, err)
	}
}

func TestSaveWritesNewFile(t *testing.T) {
	dir := t.TempDir()
	n, err := Save(dir, "plugins/New.jar", strings.NewReader("hello world"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if n != int64(len("hello world")) {
		t.Fatalf("unexpected size: %d", n)
	}
	data, err := os.ReadFile(filepath.Join(dir, "plugins/New.jar"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("got %q", data)
	}
}

func TestSaveOverwritesAtomicallyNoTempLeftover(t *testing.T) {
	dir := t.TempDir()
	if _, err := Save(dir, "config.yml", strings.NewReader("v1")); err != nil {
		t.Fatalf("initial save: %v", err)
	}
	if _, err := Save(dir, "config.yml", strings.NewReader("v2, longer content")); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.yml"))
	if err != nil || string(data) != "v2, longer content" {
		t.Fatalf("unexpected content after overwrite: %q, err=%v", data, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.yml" {
		t.Fatalf("expected exactly one file (no leftover temp file), got %v", entries)
	}
}

func TestSaveCreatesMissingParentDirs(t *testing.T) {
	dir := t.TempDir()
	if _, err := Save(dir, "a/b/c/deep.txt", strings.NewReader("x")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a/b/c/deep.txt")); err != nil {
		t.Fatalf("expected nested file to exist: %v", err)
	}
}

func TestSaveRejectsWorkingDirItself(t *testing.T) {
	dir := t.TempDir()
	if _, err := Save(dir, "", strings.NewReader("x")); err == nil {
		t.Fatal("expected Save to reject overwriting working_dir itself")
	}
}

// errReader always fails after producing some bytes, simulating a dropped
// upload connection mid-transfer.
type errReader struct{ n int }

func (r *errReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, os.ErrClosed
	}
	c := copy(p, strings.Repeat("x", r.n))
	r.n -= c
	if r.n <= 0 {
		return c, os.ErrClosed
	}
	return c, nil
}

func TestSaveFailedMidWriteLeavesOriginalUntouched(t *testing.T) {
	dir := t.TempDir()
	if _, err := Save(dir, "config.yml", strings.NewReader("original")); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	if _, err := Save(dir, "config.yml", &errReader{n: 5}); err == nil {
		t.Fatal("expected the failed write to return an error")
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.yml"))
	if err != nil || string(data) != "original" {
		t.Fatalf("original file should be untouched: %q, err=%v", data, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected no dangling temp file after a failed write, got %v", entries)
	}
}

// TestSymlinkEscapeIsNotDetected documents the current, accepted behavior
// (not a regression to fix here): withinRoot is a purely lexical check, the
// same limitation backup/engine.go's and provision/provision.go's identical
// helpers already carry. A symlink whose *name* lexically resolves inside
// workingDir but whose target lives outside it is not specially detected —
// the OS itself follows the symlink on the actual read/write/delete syscall.
func TestSymlinkEscapeIsNotDetected(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("symlink creation can be restricted in some CI sandboxes")
	}
	dir := t.TempDir()
	outsideDir := t.TempDir()
	linkPath := filepath.Join(dir, "escape-link")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Skipf("symlink creation not supported in this environment: %v", err)
	}

	// resolve()/withinRoot lexically sees "workingDir/escape-link" as inside
	// workingDir and does not reject it — documenting that List succeeds
	// rather than asserting it should fail.
	if _, err := List(dir, "escape-link"); err != nil {
		t.Fatalf("expected List through the symlink to lexically succeed (documenting current behavior): %v", err)
	}
}
