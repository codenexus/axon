package provision

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/codenexus/axon/pulse/internal/protocol"
)

func TestDownloadJava(t *testing.T) {
	const fakeJar = "fake jar bytes"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(fakeJar))
	}))
	defer server.Close()

	dir := t.TempDir()
	dest, err := Download(server.URL, dir, "java")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if dest != filepath.Join(dir, "server.jar") {
		t.Fatalf("unexpected dest %q", dest)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != fakeJar {
		t.Fatalf("got %q, want %q", data, fakeJar)
	}
}

func buildTestZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %q: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buf.Bytes()
}

func TestDownloadBedrockExtractsAndSetsExecBit(t *testing.T) {
	zipData := buildTestZip(t, map[string]string{
		"bedrock_server":      "fake binary",
		"server.properties":   "server-port=19132\n",
		"definitions/foo.txt": "nested file",
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipData)
	}))
	defer server.Close()

	dir := t.TempDir()
	dest, err := Download(server.URL, dir, "bedrock")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if dest != filepath.Join(dir, "bedrock_server") {
		t.Fatalf("unexpected dest %q", dest)
	}

	for _, rel := range []string{"bedrock_server", "server.properties", "definitions/foo.txt"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("expected %q to exist: %v", rel, err)
		}
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(dest)
		if err != nil {
			t.Fatalf("stat bedrock_server: %v", err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Fatalf("bedrock_server is not executable: mode=%v", info.Mode())
		}
	}
}

func TestExtractZipRejectsPathTraversal(t *testing.T) {
	zipData := buildTestZip(t, map[string]string{
		"../../etc/passwd": "malicious",
	})
	dir := t.TempDir()
	zipPath := filepath.Join(t.TempDir(), "evil.zip")
	if err := os.WriteFile(zipPath, zipData, 0o644); err != nil {
		t.Fatalf("write test zip: %v", err)
	}

	if err := extractZip(zipPath, dir); err == nil {
		t.Fatal("expected an error rejecting the path-traversal entry, got nil")
	}
}

func TestConfigureJava(t *testing.T) {
	dir := t.TempDir()
	payload := protocol.CreateInstanceCommandPayload{
		GamePlatform: "java",
		Port:         25566,
		WorkingDir:   dir,
	}
	command, env, err := Configure(payload, "/usr/bin/java")
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if len(command) == 0 || command[0] != "/usr/bin/java" {
		t.Fatalf("unexpected command: %v", command)
	}
	if env != nil {
		t.Fatalf("expected no extra env for java, got %v", env)
	}

	eula, err := os.ReadFile(filepath.Join(dir, "eula.txt"))
	if err != nil || string(eula) != "eula=true\n" {
		t.Fatalf("eula.txt = %q, %v", eula, err)
	}
	props, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil || string(props) != "server-port=25566\n" {
		t.Fatalf("server.properties = %q, %v", props, err)
	}
}

func TestConfigureBedrockSetsLibraryPath(t *testing.T) {
	dir := t.TempDir()
	payload := protocol.CreateInstanceCommandPayload{
		GamePlatform: "bedrock",
		Port:         19133,
		WorkingDir:   dir,
	}
	command, env, err := Configure(payload, "")
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if len(command) != 1 || command[0] != "./bedrock_server" {
		t.Fatalf("unexpected command: %v", command)
	}
	if len(env) != 1 || env[0] != "LD_LIBRARY_PATH=." {
		t.Fatalf("unexpected env: %v", env)
	}
	if _, err := os.Stat(filepath.Join(dir, "eula.txt")); err == nil {
		t.Fatal("bedrock should not get an eula.txt")
	}
}
