package provision

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	dest, err := Download(server.URL, dir, "java", "vanilla")
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
	dest, err := Download(server.URL, dir, "bedrock", "vanilla")
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
		SoftwareType: "vanilla",
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

func TestConfigurePaperMatchesVanillaShape(t *testing.T) {
	dir := t.TempDir()
	payload := protocol.CreateInstanceCommandPayload{
		GamePlatform: "java",
		SoftwareType: "paper",
		Port:         25567,
		WorkingDir:   dir,
	}
	command, env, err := Configure(payload, "/usr/bin/java")
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	want := []string{"/usr/bin/java", "-Xmx2048M", "-jar", "server.jar", "nogui"}
	if len(command) != len(want) {
		t.Fatalf("command = %v, want %v", command, want)
	}
	for i := range want {
		if command[i] != want[i] {
			t.Fatalf("command = %v, want %v", command, want)
		}
	}
	if env != nil {
		t.Fatalf("expected no extra env for paper, got %v", env)
	}
}

func TestConfigureFabricLaunchesGeneratedJar(t *testing.T) {
	dir := t.TempDir()
	payload := protocol.CreateInstanceCommandPayload{
		GamePlatform: "java",
		SoftwareType: "fabric",
		Port:         25568,
		WorkingDir:   dir,
	}
	command, _, err := Configure(payload, "/usr/bin/java")
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	want := []string{"/usr/bin/java", "-Xmx2048M", "-jar", "fabric-server-launch.jar", "nogui"}
	if len(command) != len(want) {
		t.Fatalf("command = %v, want %v", command, want)
	}
	for i := range want {
		if command[i] != want[i] {
			t.Fatalf("command = %v, want %v", command, want)
		}
	}
}

func TestConfigureForgeInvokesGeneratedRunScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this assertion targets the non-Windows run.sh path")
	}
	dir := t.TempDir()
	payload := protocol.CreateInstanceCommandPayload{
		GamePlatform: "java",
		SoftwareType: "forge",
		Port:         25569,
		WorkingDir:   dir,
	}
	command, _, err := Configure(payload, "/usr/bin/java")
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	want := []string{"sh", "run.sh", "nogui"}
	if len(command) != len(want) {
		t.Fatalf("command = %v, want %v", command, want)
	}
	for i := range want {
		if command[i] != want[i] {
			t.Fatalf("command = %v, want %v", command, want)
		}
	}
}

func TestConfigureUnsupportedSoftwareTypeFails(t *testing.T) {
	dir := t.TempDir()
	payload := protocol.CreateInstanceCommandPayload{
		GamePlatform: "java",
		SoftwareType: "bogus-loader",
		Port:         25570,
		WorkingDir:   dir,
	}
	if _, _, err := Configure(payload, "/usr/bin/java"); err == nil {
		t.Fatal("expected an error for an unsupported software_type")
	}
}

func TestInstallerArgsFabric(t *testing.T) {
	args, err := installerArgs("fabric", "/work/dir", "1.21.1", "0.19.3")
	if err != nil {
		t.Fatalf("installerArgs: %v", err)
	}
	want := []string{"server", "-mcversion", "1.21.1", "-loader", "0.19.3", "-downloadMinecraft", "-dir", "/work/dir"}
	if len(args) != len(want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %v, want %v", args, want)
		}
	}
}

func TestInstallerArgsForge(t *testing.T) {
	args, err := installerArgs("forge", "/work/dir", "1.21", "")
	if err != nil {
		t.Fatalf("installerArgs: %v", err)
	}
	if len(args) != 1 || args[0] != "--installServer" {
		t.Fatalf("args = %v, want [--installServer]", args)
	}
}

func TestInstallerArgsRejectsNonInstallerSoftwareType(t *testing.T) {
	if _, err := installerArgs("vanilla", "/work/dir", "1.21.1", ""); err == nil {
		t.Fatal("expected an error for a software type that doesn't use an installer")
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

func TestConfigureJavaCustomHeapSize(t *testing.T) {
	dir := t.TempDir()
	payload := protocol.CreateInstanceCommandPayload{
		GamePlatform: "java",
		SoftwareType: "vanilla",
		Port:         25570,
		WorkingDir:   dir,
		JavaHeapMB:   4096,
	}
	command, _, err := Configure(payload, "/usr/bin/java")
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}
	want := []string{"/usr/bin/java", "-Xmx4096M", "-jar", "server.jar", "nogui"}
	if len(command) != len(want) {
		t.Fatalf("command = %v, want %v", command, want)
	}
	for i := range want {
		if command[i] != want[i] {
			t.Fatalf("command = %v, want %v", command, want)
		}
	}
}

func TestConfigureJavaPropertyOverrides(t *testing.T) {
	dir := t.TempDir()
	payload := protocol.CreateInstanceCommandPayload{
		GamePlatform: "java",
		SoftwareType: "vanilla",
		Port:         25571,
		WorkingDir:   dir,
		Gamemode:     "creative",
		Difficulty:   "hard",
		MaxPlayers:   5,
		Motd:         "My Server",
	}
	if _, _, err := Configure(payload, "/usr/bin/java"); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	props, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil {
		t.Fatalf("read server.properties: %v", err)
	}
	for _, want := range []string{"server-port=25571", "gamemode=creative", "difficulty=hard", "max-players=5", "motd=My Server"} {
		if !strings.Contains(string(props), want) {
			t.Fatalf("server.properties = %q, missing %q", props, want)
		}
	}
}

// Bedrock has no "motd" key -- its equivalent is "server-name". A payload
// built for Bedrock must never write a "motd" line, or the value is
// silently ignored by the real server.
func TestConfigureBedrockMotdMapsToServerName(t *testing.T) {
	dir := t.TempDir()
	payload := protocol.CreateInstanceCommandPayload{
		GamePlatform: "bedrock",
		Port:         19134,
		WorkingDir:   dir,
		Motd:         "My Bedrock Server",
	}
	if _, _, err := Configure(payload, ""); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	props, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil {
		t.Fatalf("read server.properties: %v", err)
	}
	if !strings.Contains(string(props), "server-name=My Bedrock Server") {
		t.Fatalf("server.properties = %q, missing server-name", props)
	}
	if strings.Contains(string(props), "motd=") {
		t.Fatalf("server.properties = %q, should not contain a motd key for bedrock", props)
	}
}
