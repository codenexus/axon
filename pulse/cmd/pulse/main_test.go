package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codenexus/axon/pulse/internal/backup"
	"github.com/codenexus/axon/pulse/internal/mcserver"
	"github.com/codenexus/axon/pulse/internal/protocol"
)

func backupCommand(t *testing.T, instanceID, backupID string) protocol.Command {
	t.Helper()
	payload, err := json.Marshal(protocol.BackupCommandPayload{BackupID: backupID})
	if err != nil {
		t.Fatal(err)
	}
	return protocol.Command{ID: "cmd_1", Type: "backup_instance", InstanceID: instanceID, Payload: payload}
}

func restoreCommand(t *testing.T, instanceID, backupID, safetyBackupID string) protocol.Command {
	t.Helper()
	payload, err := json.Marshal(protocol.RestoreCommandPayload{BackupID: backupID, SafetyBackupID: safetyBackupID})
	if err != nil {
		t.Fatal(err)
	}
	return protocol.Command{ID: "cmd_restore", Type: "restore_backup", InstanceID: instanceID, Payload: payload}
}

func TestExecuteBackupStopsAndRestartsRunningInstance(t *testing.T) {
	workDir := t.TempDir()
	backupsRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "world.dat"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	manager := mcserver.NewManager([]mcserver.InstanceConfig{{
		ID:         "survival",
		Command:    []string{"sh", "-c", "sleep 5"},
		WorkingDir: workDir,
	}})
	if err := manager.Start("survival"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !manager.IsRunning("survival") {
		t.Fatal("expected instance to be running before backup")
	}

	engine := backup.NewEngine(backupsRoot)
	result := executeBackup(manager, engine, backupCommand(t, "survival", "bkp_1"))
	if !result.Success {
		t.Fatalf("executeBackup failed: %s", result.Message)
	}
	if result.SizeBytes == 0 || result.Checksum == "" {
		t.Fatalf("expected size/checksum in result, got %+v", result)
	}

	if !manager.IsRunning("survival") {
		t.Fatal("expected instance to be running again after backup, since it was running before")
	}

	if _, err := os.Stat(engine.PathFor("survival", "bkp_1")); err != nil {
		t.Fatalf("expected archive on disk: %v", err)
	}

	manager.Stop("survival")
}

func TestExecuteBackupLeavesStoppedInstanceStopped(t *testing.T) {
	workDir := t.TempDir()
	backupsRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "world.dat"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	manager := mcserver.NewManager([]mcserver.InstanceConfig{{
		ID:         "survival",
		Command:    []string{"sh", "-c", "sleep 5"},
		WorkingDir: workDir,
	}})
	if manager.IsRunning("survival") {
		t.Fatal("expected instance to start out stopped")
	}

	engine := backup.NewEngine(backupsRoot)
	result := executeBackup(manager, engine, backupCommand(t, "survival", "bkp_2"))
	if !result.Success {
		t.Fatalf("executeBackup failed: %s", result.Message)
	}

	if manager.IsRunning("survival") {
		t.Fatal("expected instance to remain stopped after backing up an already-stopped instance")
	}
	if _, err := os.Stat(engine.PathFor("survival", "bkp_2")); err != nil {
		t.Fatalf("expected archive on disk: %v", err)
	}
}

func TestExecuteRestartOnRunningInstanceEndsRunning(t *testing.T) {
	workDir := t.TempDir()
	manager := mcserver.NewManager([]mcserver.InstanceConfig{{
		ID:         "survival",
		Command:    []string{"sh", "-c", "sleep 5"},
		WorkingDir: workDir,
	}})
	if err := manager.Start("survival"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	result := executeRestart(manager, protocol.Command{ID: "cmd_1", Type: "restart_instance", InstanceID: "survival"})
	if !result.Success {
		t.Fatalf("executeRestart failed: %s", result.Message)
	}
	if !manager.IsRunning("survival") {
		t.Fatal("expected instance to be running after restart")
	}

	manager.Stop("survival")
}

func TestExecuteRestartOnStoppedInstanceStartsIt(t *testing.T) {
	workDir := t.TempDir()
	manager := mcserver.NewManager([]mcserver.InstanceConfig{{
		ID:         "survival",
		Command:    []string{"sh", "-c", "sleep 5"},
		WorkingDir: workDir,
	}})
	if manager.IsRunning("survival") {
		t.Fatal("expected instance to start out stopped")
	}

	result := executeRestart(manager, protocol.Command{ID: "cmd_1", Type: "restart_instance", InstanceID: "survival"})
	if !result.Success {
		t.Fatalf("executeRestart failed: %s", result.Message)
	}
	if !manager.IsRunning("survival") {
		t.Fatal("expected restart on a stopped instance to behave like a plain start")
	}

	manager.Stop("survival")
}

func TestExecuteRestoreStoppedInstanceRollsBackAndStaysStopped(t *testing.T) {
	workDir := t.TempDir()
	backupsRoot := t.TempDir()
	worldPath := filepath.Join(workDir, "world.dat")
	if err := os.WriteFile(worldPath, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	manager := mcserver.NewManager([]mcserver.InstanceConfig{{
		ID:         "survival",
		Command:    []string{"sh", "-c", "sleep 5"},
		WorkingDir: workDir,
	}})
	engine := backup.NewEngine(backupsRoot)

	if result := executeBackup(manager, engine, backupCommand(t, "survival", "bkp_v1")); !result.Success {
		t.Fatalf("seed backup failed: %s", result.Message)
	}

	if err := os.WriteFile(worldPath, []byte("v2-corrupted"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := executeRestore(manager, engine, restoreCommand(t, "survival", "bkp_v1", "bkp_safety"))
	if !result.Success {
		t.Fatalf("executeRestore failed: %s", result.Message)
	}
	if result.SizeBytes == 0 || result.Checksum == "" {
		t.Fatalf("expected safety backup size/checksum in result, got %+v", result)
	}

	got, err := os.ReadFile(worldPath)
	if err != nil || string(got) != "v1" {
		t.Fatalf("world.dat = %q err=%v, want \"v1\" (restore should have rolled back the corruption)", got, err)
	}

	if manager.IsRunning("survival") {
		t.Fatal("expected instance to remain stopped after restore, per the leave-stopped design decision")
	}

	if _, err := os.Stat(engine.PathFor("survival", "bkp_safety")); err != nil {
		t.Fatalf("expected pre-restore safety backup on disk: %v", err)
	}
}

func TestExecuteRestoreRunningInstanceEndsStoppedNotRestarted(t *testing.T) {
	workDir := t.TempDir()
	backupsRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "world.dat"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	manager := mcserver.NewManager([]mcserver.InstanceConfig{{
		ID:         "survival",
		Command:    []string{"sh", "-c", "sleep 5"},
		WorkingDir: workDir,
	}})
	engine := backup.NewEngine(backupsRoot)

	if result := executeBackup(manager, engine, backupCommand(t, "survival", "bkp_v1")); !result.Success {
		t.Fatalf("seed backup failed: %s", result.Message)
	}
	if err := manager.Start("survival"); err != nil {
		t.Fatalf("Start: %v", err)
	}

	result := executeRestore(manager, engine, restoreCommand(t, "survival", "bkp_v1", "bkp_safety2"))
	if !result.Success {
		t.Fatalf("executeRestore failed: %s", result.Message)
	}

	// Unlike executeBackup (which restarts if it was running), restore is
	// deliberately left stopped regardless of prior state — the admin
	// should look at the restored world before bringing it back up.
	if manager.IsRunning("survival") {
		t.Fatal("expected restore to leave the instance stopped even though it was running beforehand")
	}
}

func TestExecuteRestoreUnknownBackupStillTakesSafetyBackupButLeavesWorldUntouched(t *testing.T) {
	workDir := t.TempDir()
	backupsRoot := t.TempDir()
	worldPath := filepath.Join(workDir, "world.dat")
	if err := os.WriteFile(worldPath, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}

	manager := mcserver.NewManager([]mcserver.InstanceConfig{{
		ID:         "survival",
		Command:    []string{"sh", "-c", "sleep 5"},
		WorkingDir: workDir,
	}})
	engine := backup.NewEngine(backupsRoot)

	result := executeRestore(manager, engine, restoreCommand(t, "survival", "bkp_does_not_exist", "bkp_safety3"))
	if result.Success {
		t.Fatal("expected failure restoring a nonexistent backup")
	}
	if result.SizeBytes == 0 || result.Checksum == "" {
		t.Fatalf("expected the safety backup to still be reported even though the restore itself failed, got %+v", result)
	}
	if _, err := os.Stat(engine.PathFor("survival", "bkp_safety3")); err != nil {
		t.Fatalf("expected pre-restore safety backup on disk despite the restore failing: %v", err)
	}

	got, err := os.ReadFile(worldPath)
	if err != nil || string(got) != "current" {
		t.Fatalf("world.dat = %q err=%v, want unchanged \"current\" (a failed restore must never destroy the live world)", got, err)
	}
}
