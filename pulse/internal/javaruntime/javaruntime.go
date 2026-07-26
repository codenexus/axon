// Package javaruntime detects and (on Linux only) installs a Java runtime
// matching a required major version, for provisioning new Java-edition
// Minecraft servers. Auto-install requires the Pulse service user to have a
// scoped passwordless-sudo rule for package installation — see CLAUDE.md's
// "Deploying Pulse to a real host" for the analogous existing sudo setup
// this project already documents. On any other platform, or if no supported
// package manager is found, or the required major has no known package
// mapping, or the install itself fails, callers get a clear error rather
// than Pulse attempting anything unsafe.
package javaruntime

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

var versionPattern = regexp.MustCompile(`version "([^"]+)"`)

// DetectMajor runs "<javaBin> -version" and parses its reported major
// version. java -version writes to stderr, and the version string format
// differs across two eras: "1.8.0_392" (Java 8 and earlier — major is the
// second dot component) vs "17.0.9" / "21.0.1" (Java 9+ — major is the
// first).
func DetectMajor(javaBin string) (int, error) {
	cmd := exec.Command(javaBin, "-version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("run %s -version: %w", javaBin, err)
	}
	return parseMajor(out.String())
}

func parseMajor(output string) (int, error) {
	m := versionPattern.FindStringSubmatch(output)
	if m == nil {
		return 0, fmt.Errorf("no version string found in %q", output)
	}
	return majorFromVersionString(m[1])
}

func majorFromVersionString(version string) (int, error) {
	parts := strings.SplitN(version, ".", 3)
	first, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("unparseable version string %q: %w", version, err)
	}
	if first == 1 && len(parts) > 1 {
		// Legacy "1.8.0_392" scheme — the real major is the second component.
		second, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, fmt.Errorf("unparseable legacy version string %q: %w", version, err)
		}
		return second, nil
	}
	return first, nil
}

// FindInstalled looks for an already-installed Java matching major, first
// on PATH, then in well-known distro install-path globs. Returns the path
// to the java binary and true if a match was found.
func FindInstalled(major int) (path string, ok bool) {
	if p, err := exec.LookPath("java"); err == nil {
		if detected, err := DetectMajor(p); err == nil && detected == major {
			return p, true
		}
	}

	globs := []string{
		fmt.Sprintf("/usr/lib/jvm/java-%d-openjdk*/bin/java", major), // Debian/Ubuntu
		fmt.Sprintf("/usr/lib/jvm/java-%d-openjdk/bin/java", major),  // RHEL/Fedora
	}
	for _, pattern := range globs {
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			if detected, err := DetectMajor(m); err == nil && detected == major {
				return m, true
			}
		}
	}
	return "", false
}

// packageNames maps a Java major version to its package name per package
// manager, covering only the majors the latest ~3 Minecraft versions
// currently need — update by hand as Mojang's requirements move (same
// hand-mirrored-and-updated-by-hand philosophy as
// pulse/internal/protocol/types.go's wire types).
var packageNames = map[int]map[string]string{
	17: {"apt": "openjdk-17-jre-headless", "dnf": "java-17-openjdk-headless"},
	21: {"apt": "openjdk-21-jre-headless", "dnf": "java-21-openjdk-headless"},
	25: {"apt": "openjdk-25-jre-headless", "dnf": "java-25-openjdk-headless"},
}

// EnsureInstalled returns a Java binary matching major, installing one via
// the host's package manager (Linux only) if none is already present.
func EnsureInstalled(major int) (string, error) {
	if path, ok := FindInstalled(major); ok {
		return path, nil
	}

	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("java %d not found and auto-install is not supported on %s; install it manually and retry", major, runtime.GOOS)
	}

	bin, key, ok := detectPackageManager()
	if !ok {
		return "", fmt.Errorf("java %d not found and no supported package manager (apt/dnf/yum) was detected; install it manually and retry", major)
	}

	pkg, ok := packageNames[major][key]
	if !ok {
		return "", fmt.Errorf("java %d not found and no known %s package for it; install it manually and retry", major, key)
	}

	if err := installPackage(bin, pkg); err != nil {
		return "", fmt.Errorf("install %s via %s (requires passwordless sudo for the Pulse service user): %w", pkg, bin, err)
	}

	path, ok := FindInstalled(major)
	if !ok {
		return "", fmt.Errorf("installed %s but still could not find a working java %d afterward", pkg, major)
	}
	return path, nil
}

// detectPackageManager returns the binary to invoke and the packageNames
// lookup key for it. yum and dnf share the same "install -y <pkg>" syntax
// and package naming on RHEL-family distros, so both key off "dnf".
func detectPackageManager() (bin, key string, ok bool) {
	if _, err := exec.LookPath("apt-get"); err == nil {
		return "apt-get", "apt", true
	}
	if _, err := exec.LookPath("dnf"); err == nil {
		return "dnf", "dnf", true
	}
	if _, err := exec.LookPath("yum"); err == nil {
		return "yum", "dnf", true
	}
	return "", "", false
}

func installPackage(bin, pkg string) error {
	if bin == "apt-get" {
		if out, err := exec.Command("sudo", "apt-get", "update").CombinedOutput(); err != nil {
			return fmt.Errorf("apt-get update: %w: %s", err, out)
		}
	}
	out, err := exec.Command("sudo", bin, "install", "-y", pkg).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s install: %w: %s", bin, err, out)
	}
	return nil
}
