package mcserver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadWritePropertiesFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	const content = "server-port=25566\ndifficulty=hard\nmotd=Test Server\n"

	if err := WritePropertiesFile(dir, content); err != nil {
		t.Fatalf("WritePropertiesFile: %v", err)
	}

	got, err := ReadPropertiesFile(dir)
	if err != nil {
		t.Fatalf("ReadPropertiesFile: %v", err)
	}
	if got != content {
		t.Fatalf("got %q, want %q", got, content)
	}
}

func TestWritePropertiesFileOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	if err := WritePropertiesFile(dir, "server-port=25565\n"); err != nil {
		t.Fatalf("initial write: %v", err)
	}
	if err := WritePropertiesFile(dir, "server-port=25999\ndifficulty=peaceful\n"); err != nil {
		t.Fatalf("overwrite: %v", err)
	}

	got, err := ReadPropertiesFile(dir)
	if err != nil {
		t.Fatalf("ReadPropertiesFile: %v", err)
	}
	want := "server-port=25999\ndifficulty=peaceful\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWritePropertiesFileLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	if err := WritePropertiesFile(dir, "server-port=25565\n"); err != nil {
		t.Fatalf("WritePropertiesFile: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "server.properties" {
		t.Fatalf("expected exactly one file named server.properties, got %v", entries)
	}
}

func TestReadPropertiesFileMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadPropertiesFile(dir); err == nil {
		t.Fatal("expected an error reading a nonexistent server.properties")
	}
}

func TestWritePropertiesFileBadWorkingDir(t *testing.T) {
	badDir := filepath.Join(t.TempDir(), "does-not-exist")
	if err := WritePropertiesFile(badDir, "server-port=25565\n"); err == nil {
		t.Fatal("expected an error writing into a nonexistent directory")
	}
}
