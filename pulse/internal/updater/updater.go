// Package updater implements Pulse self-update: verify a Panel-proposed
// release's signature, atomically swap the running binary, and confirm or
// roll back across the resulting restart. Ported from Beacon's
// agent/internal/updater/*, adapted to Axon's architecture — Beacon polls
// a separate version-check endpoint on its own timer; Axon already has a
// periodic Pulse-initiated cycle (the heartbeat), so the update check
// rides on HeartbeatResponse.Update instead of a second poll loop. See
// ApplyUpdate's doc comment for what that simplifies away.
package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codenexus/axon/pulse/internal/protocol"
)

// gracePeriod bounds how long a freshly-swapped binary has to complete one
// successful heartbeat before this process rolls itself back — matches
// Beacon's own constant.
const gracePeriod = 10 * time.Minute

// downloadHTTPClient has no timeout — a Pulse binary is a multi-MB
// download, must not be aborted by a short default. Explicit rather than
// relying on http.Get's implicit no-timeout default, matching this
// codebase's established style (see protocol.Client's uploadHTTPClient).
var downloadHTTPClient = &http.Client{}

// checkInC is closed or sent on when Pulse successfully completes a
// heartbeat. Buffered so NotifyCheckIn never blocks.
var checkInC = make(chan struct{}, 1)

// NotifyCheckIn is called from main's runLoop after every successful
// heartbeat. If an update confirmation is pending, this clears it.
func NotifyCheckIn() {
	select {
	case checkInC <- struct{}{}:
	default:
	}
}

type updateState struct {
	PendingVersion string `json:"pending_version"`
	BackupPath     string `json:"backup_path"`
	DeadlineUnix   int64  `json:"deadline_unix"`
}

// Start checks for a leftover update-state.json from a swap that just
// happened (this process is the freshly-restarted new binary) and, if
// found, launches the confirm/rollback watch. Call once from main, early
// — before or after Manager.Reconcile() doesn't matter, they're
// independent, but both need to run on every startup.
func Start(exe, credDir string) {
	if exe == "" || isDevBuild(exe) {
		log.Printf("updater: disabled (dev build or unresolved executable path)")
		return
	}

	statePath := filepath.Join(credDir, "update-state.json")

	if state, err := loadState(statePath); err == nil {
		go awaitConfirmation(exe, state, statePath)
	}
}

// awaitConfirmation waits for the first successful heartbeat after a
// swap. If none arrives before the deadline, it rolls back.
func awaitConfirmation(exe string, state updateState, statePath string) {
	deadline := time.Unix(state.DeadlineUnix, 0)
	log.Printf("updater: awaiting confirmation of update to %s (deadline %s)", state.PendingVersion, deadline.Format(time.RFC3339))

	select {
	case <-checkInC:
		log.Printf("updater: update to %s confirmed", state.PendingVersion)
		os.Remove(statePath)
		os.Remove(state.BackupPath)
	case <-time.After(time.Until(deadline)):
		log.Printf("updater: update to %s unconfirmed — rolling back", state.PendingVersion)
		os.Remove(statePath)
		if err := rollback(exe, state.BackupPath); err != nil {
			log.Printf("updater: rollback failed: %v", err)
		}
		// On success, rollback() replaces this process's image (Unix) or
		// spawns a fresh old-binary process and exits (Windows) — this
		// goroutine's own continuation past here only matters if rollback
		// itself failed and this process is still the one running.
	}
}

// ApplyUpdate downloads, verifies, and atomically swaps in the release
// info proposes — called directly from main's runLoop when a heartbeat
// response carries Update, with the version/URL/signature already known.
// Unlike Beacon's checkAndApply (which first has to ask a separate
// endpoint "is there anything new"), there's no separate check step here:
// Panel already decided and said so in the same response Pulse just
// received. On success this function does not return — atomicSwap
// replaces the process image (or spawns a new one and exits, on Windows).
func ApplyUpdate(exe, credDir string, info protocol.UpdateInfo) error {
	statePath := filepath.Join(credDir, "update-state.json")
	newPath := exe + ".new"

	if err := downloadFile(info.DownloadURL, newPath); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("download: %w", err)
	}

	if err := VerifyBinary(newPath, info.SignatureHex); err != nil {
		os.Remove(newPath)
		return err
	}

	backupPath := exe + ".prev"
	state := updateState{
		PendingVersion: info.Version,
		BackupPath:     backupPath,
		DeadlineUnix:   time.Now().Add(gracePeriod).Unix(),
	}
	if err := saveState(statePath, state); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("save state: %w", err)
	}

	log.Printf("updater: applying %s — restarting", info.Version)

	if err := atomicSwap(exe, newPath, backupPath); err != nil {
		os.Remove(newPath)
		os.Remove(statePath)
		return fmt.Errorf("swap: %w", err)
	}
	return nil
}

func downloadFile(url, dest string) error {
	resp, err := downloadHTTPClient.Get(url) //nolint:gosec // URL comes from Panel's published release metadata, verified by signature afterward
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func loadState(path string) (updateState, error) {
	var s updateState
	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(data, &s)
}

func saveState(path string, s updateState) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// isDevBuild detects go run and similar ephemeral build paths, where a
// binary swap would be meaningless (the "binary" is a temp file that
// won't exist on the next invocation).
func isDevBuild(exe string) bool {
	return strings.Contains(exe, "go-build") || strings.Contains(exe, "/T/")
}
