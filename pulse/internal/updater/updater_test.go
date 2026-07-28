package updater

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codenexus/axon/pulse/internal/protocol"
)

func TestSaveLoadStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "update-state.json")
	original := updateState{
		PendingVersion: "abc1234",
		BackupPath:     "/opt/pulse/pulse.prev",
		DeadlineUnix:   time.Now().Add(10 * time.Minute).Unix(),
	}

	if err := saveState(path, original); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	loaded, err := loadState(path)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if loaded != original {
		t.Fatalf("round-tripped state = %+v, want %+v", loaded, original)
	}
}

func TestLoadStateMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	if _, err := loadState(path); err == nil {
		t.Fatal("expected loadState to fail on a missing file")
	}
}

func TestIsDevBuild(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/tmp/go-build1234567890/b001/exe/pulse", true},
		{"/tmp/T/pulse-test-binary", true},
		{"/usr/local/bin/pulse", false},
		{"/opt/axon/pulse", false},
	}
	for _, c := range cases {
		if got := isDevBuild(c.path); got != c.want {
			t.Errorf("isDevBuild(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestNotifyCheckInDoesNotBlockWithoutAListener(t *testing.T) {
	done := make(chan struct{})
	go func() {
		NotifyCheckIn()
		NotifyCheckIn() // buffered channel already full — must not block
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("NotifyCheckIn blocked with no listener draining checkInC")
	}

	// Drain what NotifyCheckIn left behind so later tests in this package
	// don't observe a stale pending check-in.
	select {
	case <-checkInC:
	default:
	}
}

// Real production incident, reproduced here: a CI git-describe quirk made
// a built binary's own reported version never exactly equal the tag name
// published to Panel, so the "differs from what was last reported"
// self-update trigger stayed permanently true even immediately after a
// successful swap+confirm -- Pulse re-applied the "same" update every
// single heartbeat, indefinitely, on a real host. ApplyUpdate must refuse
// to re-apply a version this process already confirmed, regardless of why
// Panel is offering it again. Uses an unreachable download URL to prove
// the guard fires before any network attempt, not just before the swap.
func TestApplyUpdateRefusesAlreadyConfirmedVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, lastConfirmedFile), []byte("v0.1.3"), 0o600); err != nil {
		t.Fatalf("seed lastConfirmedFile: %v", err)
	}

	exe := filepath.Join(dir, "pulse")
	if err := os.WriteFile(exe, []byte("fake binary"), 0o755); err != nil {
		t.Fatalf("write fake exe: %v", err)
	}

	err := ApplyUpdate(exe, dir, protocol.UpdateInfo{
		Version:      "v0.1.3",
		DownloadURL:  "http://127.0.0.1:1/unreachable", // proves no download was attempted
		SignatureHex: "irrelevant",
	})
	if err != nil {
		t.Fatalf("ApplyUpdate for an already-confirmed version should return nil, got: %v", err)
	}

	// A genuinely different version must still be attempted normally (and
	// fail here only because the download URL is unreachable, not because
	// the guard incorrectly blocked it).
	err = ApplyUpdate(exe, dir, protocol.UpdateInfo{
		Version:      "v0.1.4",
		DownloadURL:  "http://127.0.0.1:1/unreachable",
		SignatureHex: "irrelevant",
	})
	if err == nil {
		t.Fatal("expected ApplyUpdate for a genuinely new version to attempt the download and fail, got nil error")
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	content := []byte("some binary content")

	if err := os.WriteFile(src, content, 0o755); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("copied content = %q, want %q", got, content)
	}
}
