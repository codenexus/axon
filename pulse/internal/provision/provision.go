// Package provision acquires and lays out a brand-new Minecraft server's
// software — downloading the right binary/jar and writing the handful of
// config files it needs before its first launch. Distinct from
// pulse/internal/backup (archive/restore of an *existing* instance's data)
// and pulse/internal/mcserver (process lifecycle of an already-configured
// instance) — this package only ever runs once, before either of those has
// anything to work with yet.
package provision

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/codenexus/axon/pulse/internal/mcserver"
	"github.com/codenexus/axon/pulse/internal/protocol"
)

// downloadHTTPClient has no timeout — a several-hundred-MB server jar/zip
// download must not be killed by a blanket timeout, mirroring
// protocol.Client's uploadHTTPClient (which has the identical reasoning for
// backup uploads).
var downloadHTTPClient = &http.Client{}

// Download fetches url into workingDir. For "java" it's saved directly as
// server.jar. For "bedrock" it's downloaded to a temp file and extracted in
// place as a zip (the official Bedrock Dedicated Server distribution
// shape), with the bedrock_server binary's executable bit set explicitly
// afterward — zip entries don't reliably carry it across platforms.
func Download(url, workingDir, gamePlatform string) (destPath string, err error) {
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		return "", fmt.Errorf("create working dir: %w", err)
	}

	resp, err := downloadHTTPClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("download %s returned status %d", url, resp.StatusCode)
	}

	switch gamePlatform {
	case "java":
		dest := filepath.Join(workingDir, "server.jar")
		f, err := os.Create(dest)
		if err != nil {
			return "", fmt.Errorf("create server.jar: %w", err)
		}
		if _, err := io.Copy(f, resp.Body); err != nil {
			f.Close()
			return "", fmt.Errorf("write server.jar: %w", err)
		}
		if err := f.Close(); err != nil {
			return "", fmt.Errorf("close server.jar: %w", err)
		}
		return dest, nil

	case "bedrock":
		tmp, err := os.CreateTemp("", "axon-bedrock-*.zip")
		if err != nil {
			return "", fmt.Errorf("create temp download file: %w", err)
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)

		if _, err := io.Copy(tmp, resp.Body); err != nil {
			tmp.Close()
			return "", fmt.Errorf("write temp download: %w", err)
		}
		if err := tmp.Close(); err != nil {
			return "", fmt.Errorf("close temp download: %w", err)
		}

		if err := extractZip(tmpPath, workingDir); err != nil {
			return "", fmt.Errorf("extract bedrock server: %w", err)
		}

		binPath := filepath.Join(workingDir, "bedrock_server")
		if err := os.Chmod(binPath, 0o755); err != nil {
			return "", fmt.Errorf("make bedrock_server executable: %w", err)
		}
		return binPath, nil

	default:
		return "", fmt.Errorf("unsupported game_platform %q", gamePlatform)
	}
}

// extractZip unpacks a zip archive into destDir, rejecting any entry whose
// name would resolve outside destDir (path traversal / "zip-slip"). This
// archive comes from a remote, third-party server — unlike
// backup/engine.go's identical-in-spirit check on its own tar archives
// (defensive-in-depth against corruption), this one guards against
// genuinely untrusted content.
func extractZip(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("resolve destination: %w", err)
	}

	for _, entry := range r.File {
		target := filepath.Join(absDestDir, filepath.FromSlash(entry.Name))
		if target != absDestDir && !withinRoot(target, absDestDir) {
			return fmt.Errorf("archive entry %q escapes destination", entry.Name)
		}

		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create dir %q: %w", entry.Name, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("create parent dir for %q: %w", entry.Name, err)
		}

		if err := extractZipEntry(entry, target); err != nil {
			return err
		}
	}
	return nil
}

func extractZipEntry(entry *zip.File, target string) error {
	rc, err := entry.Open()
	if err != nil {
		return fmt.Errorf("open entry %q: %w", entry.Name, err)
	}
	defer rc.Close()

	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create file %q: %w", entry.Name, err)
	}
	_, copyErr := io.Copy(out, rc)
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("write file %q: %w", entry.Name, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close file %q: %w", entry.Name, closeErr)
	}
	return nil
}

func withinRoot(absPath, root string) bool {
	return absPath == root || strings.HasPrefix(absPath, root+string(filepath.Separator))
}

// DefaultJavaHeapMB is the fixed -Xmx applied to every newly-provisioned
// Java instance in v1 — no per-host RAM-based sizing or UI control yet.
// The admin can hand-edit the instance's launch command afterward for a
// specific server that needs something different.
const DefaultJavaHeapMB = 2048

// Configure writes whatever config a freshly-downloaded server needs before
// its first launch (eula.txt for Java — the server refuses to start
// without it; server-port for both) and returns the launch command and any
// extra environment variables mcserver.Manager should run it with.
func Configure(payload protocol.CreateInstanceCommandPayload, javaBin string) (command []string, env []string, err error) {
	propsPath := filepath.Join(payload.WorkingDir, "server.properties")
	if err := mcserver.WriteProperty(propsPath, "server-port", strconv.Itoa(payload.Port)); err != nil {
		return nil, nil, fmt.Errorf("write server.properties: %w", err)
	}

	switch payload.GamePlatform {
	case "java":
		eulaPath := filepath.Join(payload.WorkingDir, "eula.txt")
		if err := os.WriteFile(eulaPath, []byte("eula=true\n"), 0o644); err != nil {
			return nil, nil, fmt.Errorf("write eula.txt: %w", err)
		}
		command := []string{javaBin, fmt.Sprintf("-Xmx%dM", DefaultJavaHeapMB), "-jar", "server.jar", "nogui"}
		return command, nil, nil

	case "bedrock":
		// bedrock_server needs to find its bundled .so files, which aren't
		// on the system library path.
		return []string{"./bedrock_server"}, []string{"LD_LIBRARY_PATH=."}, nil

	default:
		return nil, nil, fmt.Errorf("unsupported game_platform %q", payload.GamePlatform)
	}
}
