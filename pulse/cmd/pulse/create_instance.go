package main

import (
	"encoding/json"
	"sync"

	"github.com/codenexus/axon/pulse/internal/javaruntime"
	"github.com/codenexus/axon/pulse/internal/mcserver"
	"github.com/codenexus/axon/pulse/internal/protocol"
	"github.com/codenexus/axon/pulse/internal/provision"
)

// creationJob tracks one in-flight create_instance command across multiple
// heartbeat cycles — the only command type that can't complete within a
// single cycle (installing Java, downloading a server jar/zip, can take
// minutes). Every other command type is a single blocking call inside
// execute(); this one runs in its own goroutine instead, reporting a coarse
// phase via HeartbeatRequest.InProgressCommands until it finishes, at which
// point runLoop folds its result into the normal pending_command_results
// batch on the next heartbeat.
type creationJob struct {
	mu     sync.Mutex
	phase  string
	done   bool
	result protocol.CommandResult
}

func (j *creationJob) setPhase(phase string) {
	j.mu.Lock()
	j.phase = phase
	j.mu.Unlock()
}

func (j *creationJob) finish(result protocol.CommandResult) {
	j.mu.Lock()
	j.done = true
	j.result = result
	j.mu.Unlock()
}

// snapshot returns done/phase/result in one lock acquisition — runLoop
// calls this once per active job, per heartbeat cycle.
func (j *creationJob) snapshot() (done bool, phase string, result protocol.CommandResult) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.done, j.phase, j.result
}

// startCreateInstanceJob kicks off provisioning in a goroutine and returns
// immediately — runLoop tracks the returned job in activeJobs until done.
func startCreateInstanceJob(manager *mcserver.Manager, configPath, backupsDir string, cmd protocol.Command) *creationJob {
	job := &creationJob{phase: "preparing"}
	go runCreateInstanceJob(job, manager, configPath, backupsDir, cmd)
	return job
}

func runCreateInstanceJob(job *creationJob, manager *mcserver.Manager, configPath, backupsDir string, cmd protocol.Command) {
	var payload protocol.CreateInstanceCommandPayload
	if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
		job.finish(protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "invalid payload: " + err.Error()})
		return
	}

	// Java resolution/download URL/required major are all fully resolved by
	// Panel before this command was ever queued (see
	// panel/src/lib/server/versionCatalog.ts) — Pulse does zero
	// version-catalog lookups of its own here, it just acts on what it's
	// told.
	var javaBin string
	if payload.GamePlatform == "java" {
		job.setPhase("installing_java")
		bin, err := javaruntime.EnsureInstalled(payload.JavaMajorVersion)
		if err != nil {
			job.finish(protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()})
			return
		}
		javaBin = bin
	}

	job.setPhase("downloading")
	if _, err := provision.Download(payload.DownloadURL, payload.WorkingDir, payload.GamePlatform, payload.SoftwareType); err != nil {
		job.finish(protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()})
		return
	}

	// Fabric/Forge downloaded an *installer*, not a runnable server —
	// run it now to produce the actual launch target (fabric-server-
	// launch.jar, or Forge's run.sh/run.bat) before Configure() below
	// builds the launch command around it.
	if payload.SoftwareType == "fabric" || payload.SoftwareType == "forge" {
		job.setPhase("installing_loader")
		if err := provision.RunInstaller(payload.SoftwareType, payload.WorkingDir, javaBin, payload.Version, payload.LoaderVersion); err != nil {
			job.finish(protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()})
			return
		}
	}

	job.setPhase("configuring")
	command, env, err := provision.Configure(payload, javaBin)
	if err != nil {
		job.finish(protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()})
		return
	}

	job.setPhase("registering")
	cfg := mcserver.InstanceConfig{
		ID:           cmd.InstanceID,
		Name:         payload.Name,
		GamePlatform: payload.GamePlatform,
		Version:      payload.Version,
		SoftwareType: payload.SoftwareType,
		Command:      command,
		WorkingDir:   payload.WorkingDir,
		Env:          env,
		Port:         payload.Port,
	}
	if err := manager.AddInstance(cfg, configPath, backupsDir); err != nil {
		job.finish(protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()})
		return
	}

	job.finish(protocol.CommandResult{CommandID: cmd.ID, Success: true})
}
