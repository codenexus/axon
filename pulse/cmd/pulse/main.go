// Command pulse is the Axon Pulse agent: it manages Minecraft server
// processes on this host and reports state to Axon Panel via a pull-based
// HTTP heartbeat + command-poll loop (Pulse always initiates contact).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/codenexus/axon/pulse/internal/backup"
	"github.com/codenexus/axon/pulse/internal/credential"
	"github.com/codenexus/axon/pulse/internal/diagnostics"
	"github.com/codenexus/axon/pulse/internal/filemanager"
	"github.com/codenexus/axon/pulse/internal/inventory"
	"github.com/codenexus/axon/pulse/internal/mcserver"
	"github.com/codenexus/axon/pulse/internal/protocol"
	"github.com/codenexus/axon/pulse/internal/updater"
)

// version is a var, not a const, so `-ldflags -X main.version=...` linker
// injection works at release-build time (see scripts/publish tooling, once
// self-update lands).
var version = "dev"

func main() {
	serverURL := flag.String("server-url", "", "Axon Panel base URL (required on first run)")
	enrollToken := flag.String("enroll-token", "", "enrollment token from Panel (required on first run)")
	configPath := flag.String("config", "pulse.instances.json", "path to the local instance config file")
	interval := flag.Duration("interval", 30*time.Second, "heartbeat/poll interval")
	flag.Parse()

	cred, err := credential.Load()
	if err != nil {
		if !credential.IsNotEnrolled(err) {
			log.Fatalf("load credential: %v", err)
		}
		if *serverURL == "" || *enrollToken == "" {
			log.Fatal("no credential found; --server-url and --enroll-token are required on first run")
		}
		cred, err = enroll(*serverURL, *enrollToken)
		if err != nil {
			log.Fatalf("enroll: %v", err)
		}
		log.Printf("enrolled as device %s", cred.DeviceID)
	} else if *serverURL != "" && *serverURL != cred.ServerURL {
		cred.ServerURL = *serverURL
	}

	instanceConfigs, backupsDir, err := mcserver.LoadConfig(*configPath)
	if err != nil {
		log.Printf("no instance config loaded (%v); starting with zero configured instances", err)
	}
	manager := mcserver.NewManager(instanceConfigs)
	manager.Reconcile()

	if backupsDir == "" {
		backupsDir = filepath.Join(credential.Dir(), "backups")
	}
	backupEngine := backup.NewEngine(backupsDir)

	// exePath is empty for `go run`/test binaries (os.Executable resolves
	// them to a transient path updater.Start already treats as a dev
	// build); self-update is simply disabled in that case rather than
	// failing startup.
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("could not resolve executable path, self-update disabled: %v", err)
		exePath = ""
	}
	updater.Start(exePath, credential.Dir())

	client := protocol.NewClient(cred.ServerURL)
	runLoop(client, cred, manager, backupEngine, *configPath, backupsDir, exePath, *interval)
}

func enroll(serverURL, token string) (*credential.Credential, error) {
	client := protocol.NewClient(serverURL)
	hostname, _ := os.Hostname()

	resp, err := client.Enroll(token, protocol.EnrollRequest{
		Hostname:     hostname,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		PulseVersion: version,
	})
	if err != nil {
		return nil, err
	}

	cred := &credential.Credential{
		DeviceID:         resp.DeviceID,
		DeviceCredential: resp.DeviceCredential,
		ServerURL:        serverURL,
	}
	if err := credential.Save(cred); err != nil {
		return nil, err
	}
	return cred, nil
}

func runLoop(client *protocol.Client, cred *credential.Credential, manager *mcserver.Manager, backupEngine *backup.Engine, configPath, backupsDir, exePath string, interval time.Duration) {
	var pendingResults []protocol.CommandResult
	// activeJobs tracks create_instance commands still provisioning across
	// multiple heartbeat cycles — see create_instance.go's package doc.
	// Every other command type is synchronous within one execute() call and
	// never appears here.
	activeJobs := make(map[string]*creationJob)

	for {
		var inProgress []protocol.CommandProgress
		for id, job := range activeJobs {
			done, phase, result := job.snapshot()
			if done {
				pendingResults = append(pendingResults, result)
				delete(activeJobs, id)
			} else {
				inProgress = append(inProgress, protocol.CommandProgress{CommandID: id, Phase: phase})
			}
		}

		req := protocol.HeartbeatRequest{
			DeviceID:              cred.DeviceID,
			Timestamp:             time.Now().Unix(),
			PulseVersion:          version,
			Host:                  inventory.Collect(),
			Instances:             manager.Statuses(),
			IntervalSeconds:       int(interval.Seconds()),
			PendingCommandResults: pendingResults,
			InProgressCommands:    inProgress,
		}

		resp, err := client.Heartbeat(cred.DeviceCredential, req)
		if err != nil {
			log.Printf("heartbeat failed: %v", err)
			time.Sleep(interval)
			continue
		}
		pendingResults = nil
		updater.NotifyCheckIn()

		for _, cmd := range resp.Commands {
			if cmd.Type == "create_instance" {
				activeJobs[cmd.ID] = startCreateInstanceJob(manager, configPath, backupsDir, cmd)
				continue
			}
			if cmd.Type == "delete_instance" {
				// Needs configPath/backupsDir, which execute()'s switch
				// doesn't receive — intercepted here for the same reason
				// create_instance is, rather than threading two more params
				// through execute()'s signature for one command type.
				pendingResults = append(pendingResults, executeDeleteInstance(manager, configPath, backupsDir, cmd))
				continue
			}
			pendingResults = append(pendingResults, execute(client, cred, manager, backupEngine, cmd))
		}

		// An update landing mid multi-minute create_instance provisioning
		// would otherwise silently kill that goroutine on re-exec — defer
		// until activeJobs drains. Panel keeps offering it on every
		// subsequent heartbeat until versions match, so nothing is lost by
		// waiting.
		if resp.Update != nil && len(activeJobs) == 0 {
			if exePath == "" {
				log.Printf("update to %s available but self-update is disabled (no resolved executable path)", resp.Update.Version)
			} else if err := updater.ApplyUpdate(exePath, credential.Dir(), *resp.Update); err != nil {
				log.Printf("update to %s failed: %v", resp.Update.Version, err)
			}
			// On success, ApplyUpdate does not return (process image
			// replaced or, on Windows, a new process spawned and this one
			// exits) — nothing after this point in the loop ever runs.
		}

		time.Sleep(interval)
	}
}

// execute runs a command synchronously and returns its terminal result
// within this heartbeat cycle. create_instance is the one exception to that
// contract (it can take minutes) — runLoop intercepts it before it ever
// reaches this function; see create_instance.go.
func execute(client *protocol.Client, cred *credential.Credential, manager *mcserver.Manager, backupEngine *backup.Engine, cmd protocol.Command) protocol.CommandResult {
	switch cmd.Type {
	case "start_instance":
		if err := manager.Start(cmd.InstanceID); err != nil {
			log.Printf("command %s (%s) failed: %v", cmd.ID, cmd.Type, err)
			return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()}
		}
		return protocol.CommandResult{CommandID: cmd.ID, Success: true}

	case "stop_instance":
		if err := manager.Stop(cmd.InstanceID); err != nil {
			log.Printf("command %s (%s) failed: %v", cmd.ID, cmd.Type, err)
			return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()}
		}
		return protocol.CommandResult{CommandID: cmd.ID, Success: true}

	case "restart_instance":
		return executeRestart(manager, cmd)

	case "backup_instance":
		return executeBackup(manager, backupEngine, cmd)

	case "delete_backup":
		var payload protocol.BackupCommandPayload
		if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
			return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "invalid payload: " + err.Error()}
		}
		if err := backupEngine.Delete(cmd.InstanceID, payload.BackupID); err != nil {
			log.Printf("command %s (%s) failed: %v", cmd.ID, cmd.Type, err)
			return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()}
		}
		return protocol.CommandResult{CommandID: cmd.ID, Success: true}

	case "push_backup":
		return executePushBackup(client, cred, backupEngine, cmd)

	case "restore_backup":
		return executeRestore(manager, backupEngine, cmd)

	case "console_command":
		return executeConsoleCommand(manager, cmd)

	case "read_properties":
		return executeReadProperties(manager, cmd)

	case "write_properties":
		return executeWriteProperties(manager, cmd)

	case "list_files":
		return executeListFiles(manager, cmd)

	case "upload_file":
		return executeUploadFile(client, cred, manager, cmd)

	case "delete_file":
		return executeDeleteFile(manager, cmd)

	case "run_diagnostic":
		return executeRunDiagnostic(cmd)

	default:
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "unknown command type: " + cmd.Type}
	}
}

// stopWaitTimeout bounds how long a command that needs an instance fully
// stopped first (backup_instance, restart_instance) waits for that before
// giving up entirely.
const stopWaitTimeout = 30 * time.Second

// executeRestart stops a running instance and starts it back up. If the
// instance was already stopped, it's equivalent to a plain start — safe to
// route every "Restart" click through this one command type regardless of
// current state, rather than the dashboard needing to know which of
// start/restart applies.
func executeRestart(manager *mcserver.Manager, cmd protocol.Command) protocol.CommandResult {
	if manager.IsRunning(cmd.InstanceID) {
		if err := manager.Stop(cmd.InstanceID); err != nil {
			return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "stop before restart: " + err.Error()}
		}
		if err := manager.WaitStopped(cmd.InstanceID, stopWaitTimeout); err != nil {
			return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "waiting for stop before restart: " + err.Error()}
		}
	}
	if err := manager.Start(cmd.InstanceID); err != nil {
		log.Printf("command %s (%s) failed: %v", cmd.ID, cmd.Type, err)
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()}
	}
	return protocol.CommandResult{CommandID: cmd.ID, Success: true}
}

// executeDeleteInstance is the inverse of create_instance: stop (if
// running) -> remove from Pulse's tracking (RemoveInstance) -> delete
// working_dir. Backup archives already taken for this instance are NOT
// touched here — they live in the shared backups_dir, not under
// working_dir — only Panel's metadata about them is cleaned up, on the
// Panel side, once this command reports success.
func executeDeleteInstance(manager *mcserver.Manager, configPath, backupsDir string, cmd protocol.Command) protocol.CommandResult {
	cfg, ok := manager.InstanceConfig(cmd.InstanceID)
	if !ok {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "unknown instance " + cmd.InstanceID}
	}

	if manager.IsRunning(cmd.InstanceID) {
		if err := manager.Stop(cmd.InstanceID); err != nil {
			return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "stop before delete: " + err.Error()}
		}
		if err := manager.WaitStopped(cmd.InstanceID, stopWaitTimeout); err != nil {
			return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "waiting for stop before delete: " + err.Error()}
		}
	}

	if err := manager.RemoveInstance(cmd.InstanceID, configPath, backupsDir); err != nil {
		log.Printf("command %s (%s) failed: %v", cmd.ID, cmd.Type, err)
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()}
	}

	if err := os.RemoveAll(cfg.WorkingDir); err != nil {
		// Tracking is already gone (the part Panel needs to hear "success"
		// for) — report the leftover directory so the admin knows to clean
		// it up by hand, without contradicting the overall success.
		msg := "instance removed but failed to delete working_dir: " + err.Error()
		log.Printf("command %s (%s): %s", cmd.ID, cmd.Type, msg)
		return protocol.CommandResult{CommandID: cmd.ID, Success: true, Message: msg}
	}

	return protocol.CommandResult{CommandID: cmd.ID, Success: true}
}

// executeBackup stops a running instance before archiving it (RCON's
// save-off/save-all pause-writes convention is Java-only and unsupported on
// Bedrock, so stopping first is the only edition-agnostic way to guarantee
// a consistent archive — see the backup package doc), then restarts it
// afterward if and only if it was running beforehand. An already-stopped
// instance is archived directly and left stopped.
func executeBackup(manager *mcserver.Manager, backupEngine *backup.Engine, cmd protocol.Command) protocol.CommandResult {
	var payload protocol.BackupCommandPayload
	if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "invalid payload: " + err.Error()}
	}

	cfg, ok := manager.InstanceConfig(cmd.InstanceID)
	if !ok {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "unknown instance " + cmd.InstanceID}
	}

	wasRunning := manager.IsRunning(cmd.InstanceID)
	if wasRunning {
		if err := manager.Stop(cmd.InstanceID); err != nil {
			return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "stop before backup: " + err.Error()}
		}
		if err := manager.WaitStopped(cmd.InstanceID, stopWaitTimeout); err != nil {
			return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "waiting for stop before backup: " + err.Error()}
		}
	}

	result, err := backupEngine.Create(cfg, payload.BackupID)

	var restartErr error
	if wasRunning {
		restartErr = manager.Start(cmd.InstanceID)
	}

	if err != nil {
		msg := err.Error()
		if restartErr != nil {
			msg += fmt.Sprintf("; also failed to restart instance after backup: %v", restartErr)
		}
		log.Printf("command %s (%s) failed: %s", cmd.ID, cmd.Type, msg)
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: msg}
	}

	message := ""
	if restartErr != nil {
		message = fmt.Sprintf("backup succeeded but failed to restart instance: %v", restartErr)
		log.Printf("command %s (%s): %s", cmd.ID, cmd.Type, message)
	}
	return protocol.CommandResult{
		CommandID: cmd.ID,
		Success:   true,
		Message:   message,
		SizeBytes: result.SizeBytes,
		Checksum:  result.ChecksumSHA256,
	}
}

// executePushBackup streams an already-created backup archive to Panel on
// request, for the admin to download — Pulse's disk is the only durable
// copy (see the backup package doc), Panel only ever holds this file
// transiently.
func executePushBackup(client *protocol.Client, cred *credential.Credential, backupEngine *backup.Engine, cmd protocol.Command) protocol.CommandResult {
	var payload protocol.BackupCommandPayload
	if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "invalid payload: " + err.Error()}
	}

	path := backupEngine.PathFor(cmd.InstanceID, payload.BackupID)
	f, err := os.Open(path)
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "open backup file: " + err.Error()}
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "stat backup file: " + err.Error()}
	}

	if err := client.PushBackup(cred.DeviceCredential, payload.BackupID, cmd.InstanceID, f, info.Size()); err != nil {
		log.Printf("command %s (%s) failed: %v", cmd.ID, cmd.Type, err)
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()}
	}
	return protocol.CommandResult{CommandID: cmd.ID, Success: true}
}

// executeRestore is the highest-blast-radius command in the system — it
// overwrites a live world with an older one. Sequence: stop (if running) ->
// take a pre-restore safety backup -> wipe+extract the target backup ->
// leave stopped (the admin restarts explicitly once satisfied, rather than
// this silently handing control back to a world they haven't looked at
// yet). The safety backup's SizeBytes/Checksum are reported whenever that
// step itself succeeded, independent of the overall Success flag — so
// Panel can record a real, usable safety backup even in the case where the
// safety backup worked but the extract step afterward failed.
func executeRestore(manager *mcserver.Manager, backupEngine *backup.Engine, cmd protocol.Command) protocol.CommandResult {
	var payload protocol.RestoreCommandPayload
	if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "invalid payload: " + err.Error()}
	}

	cfg, ok := manager.InstanceConfig(cmd.InstanceID)
	if !ok {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "unknown instance " + cmd.InstanceID}
	}

	if manager.IsRunning(cmd.InstanceID) {
		if err := manager.Stop(cmd.InstanceID); err != nil {
			return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "stop before restore: " + err.Error()}
		}
		if err := manager.WaitStopped(cmd.InstanceID, stopWaitTimeout); err != nil {
			return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "waiting for stop before restore: " + err.Error()}
		}
	}

	safetyResult, err := backupEngine.Create(cfg, payload.SafetyBackupID)
	if err != nil {
		log.Printf("command %s (%s) failed: pre-restore safety backup: %v", cmd.ID, cmd.Type, err)
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "pre-restore safety backup failed: " + err.Error()}
	}

	result := protocol.CommandResult{
		CommandID: cmd.ID,
		SizeBytes: safetyResult.SizeBytes,
		Checksum:  safetyResult.ChecksumSHA256,
	}

	if err := backupEngine.Restore(cfg, payload.BackupID); err != nil {
		log.Printf("command %s (%s) failed: restore: %v", cmd.ID, cmd.Type, err)
		result.Success = false
		result.Message = "restore failed (pre-restore safety backup was taken successfully first): " + err.Error()
		return result
	}

	result.Success = true
	return result
}

// executeConsoleCommand sends an arbitrary admin command to the instance's
// RCON port. A normal synchronous command like every other type here —
// RCON's dial+auth+execute is bounded at a few seconds, unlike
// create_instance's multi-minute provisioning, so this doesn't need the
// async job pattern in create_instance.go.
func executeConsoleCommand(manager *mcserver.Manager, cmd protocol.Command) protocol.CommandResult {
	var payload protocol.ConsoleCommandPayload
	if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "invalid payload: " + err.Error()}
	}

	output, err := manager.RunConsoleCommand(cmd.InstanceID, payload.Command)
	if err != nil {
		log.Printf("command %s (%s) failed: %v", cmd.ID, cmd.Type, err)
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()}
	}
	return protocol.CommandResult{CommandID: cmd.ID, Success: true, Output: output}
}

// executeReadProperties returns an instance's raw server.properties
// contents for Panel's raw-text editor — see mcserver.ReadPropertiesFile's
// doc comment for why this is unparsed text, not structured fields.
func executeReadProperties(manager *mcserver.Manager, cmd protocol.Command) protocol.CommandResult {
	cfg, ok := manager.InstanceConfig(cmd.InstanceID)
	if !ok {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "unknown instance " + cmd.InstanceID}
	}
	content, err := mcserver.ReadPropertiesFile(cfg.WorkingDir)
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()}
	}
	return protocol.CommandResult{CommandID: cmd.ID, Success: true, Output: content}
}

// executeWriteProperties overwrites an instance's server.properties with
// whatever the admin typed in Panel's editor, verbatim. Changes only take
// effect on the instance's next start, same as if the admin had edited the
// file by hand over SSH — this command doesn't restart anything itself.
func executeWriteProperties(manager *mcserver.Manager, cmd protocol.Command) protocol.CommandResult {
	var payload protocol.WritePropertiesCommandPayload
	if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "invalid payload: " + err.Error()}
	}
	cfg, ok := manager.InstanceConfig(cmd.InstanceID)
	if !ok {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "unknown instance " + cmd.InstanceID}
	}
	if err := mcserver.WritePropertiesFile(cfg.WorkingDir, payload.Content); err != nil {
		log.Printf("command %s (%s) failed: %v", cmd.ID, cmd.Type, err)
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()}
	}
	return protocol.CommandResult{CommandID: cmd.ID, Success: true}
}

// executeListFiles returns a JSON-encoded []filemanager.Entry for the
// requested directory, in CommandResult.Output — the third reuse of that
// field (after RCON output and raw server.properties content).
func executeListFiles(manager *mcserver.Manager, cmd protocol.Command) protocol.CommandResult {
	var payload protocol.ListFilesCommandPayload
	if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "invalid payload: " + err.Error()}
	}
	cfg, ok := manager.InstanceConfig(cmd.InstanceID)
	if !ok {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "unknown instance " + cmd.InstanceID}
	}
	entries, err := filemanager.List(cfg.WorkingDir, payload.Path)
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()}
	}
	out, err := json.Marshal(entries)
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "encode listing: " + err.Error()}
	}
	return protocol.CommandResult{CommandID: cmd.ID, Success: true, Output: string(out)}
}

// executeDeleteFile removes a file or directory (recursively) from the
// instance's working_dir.
func executeDeleteFile(manager *mcserver.Manager, cmd protocol.Command) protocol.CommandResult {
	var payload protocol.DeleteFileCommandPayload
	if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "invalid payload: " + err.Error()}
	}
	cfg, ok := manager.InstanceConfig(cmd.InstanceID)
	if !ok {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "unknown instance " + cmd.InstanceID}
	}
	if err := filemanager.Delete(cfg.WorkingDir, payload.Path); err != nil {
		log.Printf("command %s (%s) failed: %v", cmd.ID, cmd.Type, err)
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()}
	}
	return protocol.CommandResult{CommandID: cmd.ID, Success: true}
}

// executeRunDiagnostic runs a fixed, allowlisted host-level command —
// unlike every other command type in this file, it has no InstanceID
// target at all (it's about the Pulse host itself, not any Minecraft
// instance). See pulse/internal/diagnostics for the allowlist and the
// "Panel picks a friendly name, Pulse maps it to the real OS-appropriate
// command" split.
func executeRunDiagnostic(cmd protocol.Command) protocol.CommandResult {
	var payload protocol.RunDiagnosticCommandPayload
	if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "invalid payload: " + err.Error()}
	}
	output, err := diagnostics.Run(payload.Name, payload.Args)
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()}
	}
	return protocol.CommandResult{CommandID: cmd.ID, Success: true, Output: output}
}

// executeUploadFile pulls a file's bytes FROM Panel's transient holding
// area and saves them into working_dir — the reversed transfer direction
// of executePushBackup, still with Pulse as the only side that ever dials
// out (see protocol.Client.PullFileUpload's doc comment).
func executeUploadFile(client *protocol.Client, cred *credential.Credential, manager *mcserver.Manager, cmd protocol.Command) protocol.CommandResult {
	var payload protocol.UploadFileCommandPayload
	if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "invalid payload: " + err.Error()}
	}
	cfg, ok := manager.InstanceConfig(cmd.InstanceID)
	if !ok {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "unknown instance " + cmd.InstanceID}
	}

	body, err := client.PullFileUpload(cred.DeviceCredential, payload.HoldingID, cmd.InstanceID)
	if err != nil {
		log.Printf("command %s (%s) failed: %v", cmd.ID, cmd.Type, err)
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()}
	}
	defer body.Close()

	size, err := filemanager.Save(cfg.WorkingDir, payload.TargetPath, body)
	if err != nil {
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()}
	}
	return protocol.CommandResult{CommandID: cmd.ID, Success: true, SizeBytes: size}
}
