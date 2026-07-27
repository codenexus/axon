package updater

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
