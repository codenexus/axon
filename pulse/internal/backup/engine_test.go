package backup

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/codenexus/axon/pulse/internal/mcserver"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// listArchive returns the set of entry names (files only) in a tar.gz.
func listArchive(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	out := make(map[string]string)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			body, _ := io.ReadAll(tr)
			out[hdr.Name] = string(body)
		}
	}
	return out
}

func TestCreateArchivesWorkingDirExcludingOperationalFiles(t *testing.T) {
	workDir := t.TempDir()
	backupsRoot := t.TempDir()

	writeFile(t, filepath.Join(workDir, "server.properties"), "enable-rcon=false\n")
	writeFile(t, filepath.Join(workDir, "world", "level.dat"), "world-data")
	writeFile(t, filepath.Join(workDir, "world", "session.lock"), "lock")
	writeFile(t, filepath.Join(workDir, "logs", "latest.log"), "log line")
	writeFile(t, filepath.Join(workDir, "pulse.log"), "pulse operational log")

	cfg := mcserver.InstanceConfig{ID: "survival", WorkingDir: workDir}
	e := NewEngine(backupsRoot)

	result, err := e.Create(cfg, "bkp_test1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if result.SizeBytes == 0 {
		t.Fatal("expected non-zero archive size")
	}
	if result.ChecksumSHA256 == "" {
		t.Fatal("expected a checksum")
	}
	wantPath := e.PathFor("survival", "bkp_test1")
	if result.Path != wantPath {
		t.Fatalf("Path = %q, want %q", result.Path, wantPath)
	}

	entries := listArchive(t, result.Path)
	if got, want := entries["server.properties"], "enable-rcon=false\n"; got != want {
		t.Errorf("server.properties = %q, want %q", got, want)
	}
	if got, want := entries["world/level.dat"], "world-data"; got != want {
		t.Errorf("world/level.dat = %q, want %q", got, want)
	}
	for _, excluded := range []string{"world/session.lock", "logs/latest.log", "pulse.log"} {
		if _, ok := entries[excluded]; ok {
			t.Errorf("expected %q to be excluded from archive, found it", excluded)
		}
	}
}

func TestCreateWithBackupsRootNestedInsideWorkingDir(t *testing.T) {
	workDir := t.TempDir()
	backupsRoot := filepath.Join(workDir, "backups")

	writeFile(t, filepath.Join(workDir, "server.properties"), "enable-rcon=false\n")

	cfg := mcserver.InstanceConfig{ID: "survival", WorkingDir: workDir}
	e := NewEngine(backupsRoot)

	result, err := e.Create(cfg, "bkp_first")
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}

	// A second backup must not choke on / include the first backup's own
	// archive file living under workDir/backups.
	result2, err := e.Create(cfg, "bkp_second")
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}

	entries := listArchive(t, result2.Path)
	for name := range entries {
		if filepath.Dir(name) == "backups" || name == "backups" {
			t.Errorf("expected backups dir to be excluded from archive, found entry %q", name)
		}
	}
	if result.ChecksumSHA256 == result2.ChecksumSHA256 {
		// Not strictly required to differ, but both should at least be valid/non-empty.
		t.Logf("checksums matched (expected, identical content): %s", result.ChecksumSHA256)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	workDir := t.TempDir()
	backupsRoot := t.TempDir()
	writeFile(t, filepath.Join(workDir, "server.properties"), "enable-rcon=false\n")

	cfg := mcserver.InstanceConfig{ID: "survival", WorkingDir: workDir}
	e := NewEngine(backupsRoot)

	result, err := e.Create(cfg, "bkp_del")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := os.Stat(result.Path); err != nil {
		t.Fatalf("expected archive to exist: %v", err)
	}

	if err := e.Delete("survival", "bkp_del"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(result.Path); !os.IsNotExist(err) {
		t.Fatalf("expected archive to be removed, stat err = %v", err)
	}

	// Deleting again should not error.
	if err := e.Delete("survival", "bkp_del"); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
}

func TestCreateUnknownWorkingDirFails(t *testing.T) {
	backupsRoot := t.TempDir()
	cfg := mcserver.InstanceConfig{ID: "ghost", WorkingDir: filepath.Join(backupsRoot, "does-not-exist")}
	e := NewEngine(backupsRoot)

	if _, err := e.Create(cfg, "bkp_x"); err == nil {
		t.Fatal("expected error archiving a nonexistent working dir")
	}
	if _, err := os.Stat(e.PathFor("ghost", "bkp_x")); !os.IsNotExist(err) {
		t.Fatalf("expected no partial archive left behind, stat err = %v", err)
	}
}

func TestRestoreIsAnExactRollbackNotAMerge(t *testing.T) {
	workDir := t.TempDir()
	backupsRoot := t.TempDir()
	cfg := mcserver.InstanceConfig{ID: "survival", WorkingDir: workDir}
	e := NewEngine(backupsRoot)

	writeFile(t, filepath.Join(workDir, "server.properties"), "version=1\n")
	writeFile(t, filepath.Join(workDir, "world", "level.dat"), "original-world-data")
	if _, err := e.Create(cfg, "bkp_before"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Simulate time passing: the world changes, and a new file appears that
	// didn't exist at backup time.
	writeFile(t, filepath.Join(workDir, "world", "level.dat"), "modified-world-data")
	writeFile(t, filepath.Join(workDir, "world", "new-chunk.dat"), "should not survive restore")

	if err := e.Restore(cfg, "bkp_before"); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(workDir, "world", "level.dat"))
	if err != nil {
		t.Fatalf("read restored level.dat: %v", err)
	}
	if string(got) != "original-world-data" {
		t.Fatalf("level.dat = %q, want original-world-data (restore should have rolled it back)", got)
	}

	if _, err := os.Stat(filepath.Join(workDir, "world", "new-chunk.dat")); !os.IsNotExist(err) {
		t.Fatalf("expected new-chunk.dat (absent from the backup) to be gone after restore, stat err = %v", err)
	}

	props, err := os.ReadFile(filepath.Join(workDir, "server.properties"))
	if err != nil || string(props) != "version=1\n" {
		t.Fatalf("server.properties not restored correctly: content=%q err=%v", props, err)
	}
}

func TestRestoreUnknownBackupFailsWithoutTouchingWorkingDir(t *testing.T) {
	workDir := t.TempDir()
	backupsRoot := t.TempDir()
	cfg := mcserver.InstanceConfig{ID: "survival", WorkingDir: workDir}
	e := NewEngine(backupsRoot)

	writeFile(t, filepath.Join(workDir, "server.properties"), "version=1\n")

	if err := e.Restore(cfg, "bkp_does_not_exist"); err == nil {
		t.Fatal("expected error restoring a nonexistent backup")
	}

	// The whole point of checking archive existence before wiping anything
	// is that a bad restore request must never destroy a live world.
	got, err := os.ReadFile(filepath.Join(workDir, "server.properties"))
	if err != nil || string(got) != "version=1\n" {
		t.Fatalf("expected working dir untouched after a failed restore, content=%q err=%v", got, err)
	}
}
