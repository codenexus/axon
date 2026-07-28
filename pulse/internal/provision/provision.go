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
	"os/exec"
	"path/filepath"
	"runtime"
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
// server.jar — except "fabric"/"forge", where the download is actually an
// *installer* program (not a runnable server), saved as installer.jar and
// run via RunInstaller below before anything is launchable. For "bedrock"
// it's downloaded to a temp file and extracted in place as a zip (the
// official Bedrock Dedicated Server distribution shape), with the
// bedrock_server binary's executable bit set explicitly afterward — zip
// entries don't reliably carry it across platforms.
func Download(url, workingDir, gamePlatform, softwareType string) (destPath string, err error) {
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
		filename := "server.jar"
		if softwareType == "fabric" || softwareType == "forge" {
			filename = "installer.jar"
		}
		dest := filepath.Join(workingDir, filename)
		f, err := os.Create(dest)
		if err != nil {
			return "", fmt.Errorf("create %s: %w", filename, err)
		}
		if _, err := io.Copy(f, resp.Body); err != nil {
			f.Close()
			return "", fmt.Errorf("write %s: %w", filename, err)
		}
		if err := f.Close(); err != nil {
			return "", fmt.Errorf("close %s: %w", filename, err)
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

// installerArgs builds the argv RunInstaller passes to the installer jar
// (after "java", "-jar", "<path-to-installer.jar>") for a given software
// type — kept as a pure function, separate from actually invoking it, so
// this can be unit-tested without a real installer or a JVM.
//
// NOT verified against a real installer run — the environment this was
// written in has no Java runtime at all. These flags are based on each
// project's current public documentation/CLI help, not a live invocation.
// Treat this the same as self-update's Windows swap path: reasoned
// through, not execution-tested — verify on a real machine before relying
// on it against a real host.
func installerArgs(softwareType, workingDir, mcVersion, loaderVersion string) ([]string, error) {
	switch softwareType {
	case "fabric":
		// -downloadMinecraft has the installer fetch the vanilla server
		// jar itself too, so Pulse needs no separate Mojang download step
		// for Fabric. Produces a fixed, predictable fabric-server-launch.jar
		// regardless of MC version.
		return []string{
			"server",
			"-mcversion", mcVersion,
			"-loader", loaderVersion,
			"-downloadMinecraft",
			"-dir", workingDir,
		}, nil

	case "forge":
		// Produces run.sh/run.bat (+ user_jvm_args.txt + a per-version
		// libraries/.../*_args.txt) in workingDir — deliberately not
		// parsed or reconstructed here; Configure() below just invokes
		// the generated run script directly instead of trying to
		// reconstruct Forge's internal args-file path, which has varied
		// across Minecraft/Forge version eras.
		return []string{"--installServer"}, nil

	default:
		return nil, fmt.Errorf("software type %q does not use an installer", softwareType)
	}
}

// RunInstaller runs a previously-downloaded installer.jar (see Download)
// to completion, producing whatever launch target Configure() will use
// (fabric-server-launch.jar, or a run.sh/run.bat for Forge). See
// installerArgs' doc comment for the real, stated verification gap here.
func RunInstaller(softwareType, workingDir, javaBin, mcVersion, loaderVersion string) error {
	args, err := installerArgs(softwareType, workingDir, mcVersion, loaderVersion)
	if err != nil {
		return err
	}

	installerPath := filepath.Join(workingDir, "installer.jar")
	cmd := exec.Command(javaBin, append([]string{"-jar", installerPath}, args...)...)
	cmd.Dir = workingDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run %s installer: %w (output: %s)", softwareType, err, output)
	}
	return nil
}

// DefaultJavaHeapMB is the -Xmx applied when a create_instance payload
// doesn't specify JavaHeapMB (0/omitted) — no per-host RAM-based sizing,
// just a fallback for callers that don't care to set it explicitly (an
// older Panel, a hand-built payload, a test). The admin can hand-edit the
// instance's launch command afterward for a specific server that needs
// something different.
const DefaultJavaHeapMB = 2048

// Configure writes whatever config a freshly-downloaded server needs before
// its first launch (eula.txt for Java — the server refuses to start
// without it; server-port for both, plus any of gamemode/difficulty/
// max-players/motd the payload sets) and returns the launch command and
// any extra environment variables mcserver.Manager should run it with.
func Configure(payload protocol.CreateInstanceCommandPayload, javaBin string) (command []string, env []string, err error) {
	propsPath := filepath.Join(payload.WorkingDir, "server.properties")
	if err := mcserver.WriteProperty(propsPath, "server-port", strconv.Itoa(payload.Port)); err != nil {
		return nil, nil, fmt.Errorf("write server.properties: %w", err)
	}
	if err := writePropertyOverrides(propsPath, payload); err != nil {
		return nil, nil, err
	}

	switch payload.GamePlatform {
	case "java":
		eulaPath := filepath.Join(payload.WorkingDir, "eula.txt")
		if err := os.WriteFile(eulaPath, []byte("eula=true\n"), 0o644); err != nil {
			return nil, nil, fmt.Errorf("write eula.txt: %w", err)
		}
		heapMB := payload.JavaHeapMB
		if heapMB <= 0 {
			heapMB = DefaultJavaHeapMB
		}
		heapFlag := fmt.Sprintf("-Xmx%dM", heapMB)

		switch payload.SoftwareType {
		case "vanilla", "paper":
			// Paper's server jar is a drop-in replacement for vanilla's —
			// same "java -jar server.jar" launch shape, no provisioning
			// difference at all beyond which URL Download() fetched.
			return []string{javaBin, heapFlag, "-jar", "server.jar", "nogui"}, nil, nil

		case "fabric":
			// fabric-server-launch.jar is a fixed filename RunInstaller's
			// Fabric installer run always produces, regardless of MC
			// version.
			return []string{javaBin, heapFlag, "-jar", "fabric-server-launch.jar", "nogui"}, nil, nil

		case "forge":
			// Deliberately invoke Forge's own generated run script rather
			// than reconstructing its internal @user_jvm_args.txt/
			// @libraries/.../*_args.txt invocation ourselves — that
			// internal path has varied across Forge/MC version eras, the
			// run script already encodes whatever this specific version's
			// installer produced.
			if runtime.GOOS == "windows" {
				return []string{"run.bat", "nogui"}, nil, nil
			}
			return []string{"sh", "run.sh", "nogui"}, nil, nil

		default:
			return nil, nil, fmt.Errorf("unsupported software_type %q for java", payload.SoftwareType)
		}

	case "bedrock":
		// bedrock_server needs to find its bundled .so files, which aren't
		// on the system library path.
		return []string{"./bedrock_server"}, []string{"LD_LIBRARY_PATH=."}, nil

	default:
		return nil, nil, fmt.Errorf("unsupported game_platform %q", payload.GamePlatform)
	}
}

// writePropertyOverrides applies whichever of gamemode/difficulty/
// max-players/motd the payload actually set, leaving everything else for
// the server software's own first-launch defaults to fill in (same as
// server-port always has been). Gamemode/difficulty/max-players share the
// same key on both editions; motd is edition-specific — Bedrock has no
// "motd" key, its equivalent display-name field is "server-name".
func writePropertyOverrides(propsPath string, payload protocol.CreateInstanceCommandPayload) error {
	if payload.Gamemode != "" {
		if err := mcserver.WriteProperty(propsPath, "gamemode", payload.Gamemode); err != nil {
			return fmt.Errorf("write gamemode: %w", err)
		}
	}
	if payload.Difficulty != "" {
		if err := mcserver.WriteProperty(propsPath, "difficulty", payload.Difficulty); err != nil {
			return fmt.Errorf("write difficulty: %w", err)
		}
	}
	if payload.MaxPlayers > 0 {
		if err := mcserver.WriteProperty(propsPath, "max-players", strconv.Itoa(payload.MaxPlayers)); err != nil {
			return fmt.Errorf("write max-players: %w", err)
		}
	}
	if payload.Motd != "" {
		motdKey := "motd"
		if payload.GamePlatform == "bedrock" {
			motdKey = "server-name"
		}
		if err := mcserver.WriteProperty(propsPath, motdKey, payload.Motd); err != nil {
			return fmt.Errorf("write %s: %w", motdKey, err)
		}
	}
	return nil
}
