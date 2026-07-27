package diagnostics

import (
	"strings"
	"testing"
)

// withTestAllowlistEntry temporarily adds name to the package-level
// allowlist for the duration of the test, restoring it afterward — this
// package test intentionally avoids depending on any real diagnostic
// (uptime/df/free) being present in whatever environment runs `go test`,
// using always-present Unix binaries (echo, head) instead.
func withTestAllowlistEntry(t *testing.T, name string, base []string) {
	t.Helper()
	allowlist[name] = base
	t.Cleanup(func() { delete(allowlist, name) })
}

func TestRunUnknownDiagnosticFails(t *testing.T) {
	_, err := Run("not-a-real-diagnostic", "")
	if err == nil {
		t.Fatal("expected an error for an unknown diagnostic name")
	}
}

func TestRunExecutesBaseCommand(t *testing.T) {
	withTestAllowlistEntry(t, "test_echo", []string{"echo", "hello"})

	output, err := Run("test_echo", "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(output) != "hello" {
		t.Fatalf("output = %q, want %q", output, "hello")
	}
}

func TestRunAppendsExtraArguments(t *testing.T) {
	withTestAllowlistEntry(t, "test_echo_base", []string{"echo", "base"})

	output, err := Run("test_echo_base", "extra1 extra2")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(output) != "base extra1 extra2" {
		t.Fatalf("output = %q, want %q", output, "base extra1 extra2")
	}
}

func TestRunNeverErrorsOnNonzeroExit(t *testing.T) {
	// `false` always exits 1 — Run's contract is that a recognized
	// diagnostic name never errors just because the command itself
	// failed; whatever it printed (nothing, here) still comes back.
	withTestAllowlistEntry(t, "test_false", []string{"false"})

	output, err := Run("test_false", "")
	if err != nil {
		t.Fatalf("Run should not error on a nonzero exit code, got: %v", err)
	}
	if output != "" {
		t.Fatalf("expected empty output from `false`, got %q", output)
	}
}

func TestRunTruncatesLargeOutput(t *testing.T) {
	// A single-command way to produce deterministic large output without a
	// shell pipe: `head -c N /dev/zero`.
	withTestAllowlistEntry(t, "test_big", []string{"head", "-c", "200000", "/dev/zero"})

	output, err := Run("test_big", "")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(output) > maxOutputBytes+64 { // small slack for the "... (truncated)" suffix
		t.Fatalf("output not truncated: got %d bytes, want <= ~%d", len(output), maxOutputBytes)
	}
	if !strings.Contains(output, "(truncated)") {
		t.Fatal("expected truncated output to say so")
	}
}

func TestNamesReturnsAllowlistKeys(t *testing.T) {
	withTestAllowlistEntry(t, "test_names_marker", []string{"echo", "marker"})

	names := Names()
	found := false
	for _, n := range names {
		if n == "test_names_marker" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Names() to include the test entry, got %v", names)
	}
}
