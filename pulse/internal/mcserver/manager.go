// Package mcserver manages the lifecycle of locally-configured Minecraft
// server processes (layer 1 of the spec's three command layers — OS-level
// process management, no Minecraft protocol involved). Graceful stop
// (gracefulStop in rcon_stop.go) issues RCON "save-all" + "stop" when the
// instance's server.properties has RCON enabled and configured, falling
// back to the plain terminate() signal otherwise. A raw RCON console
// (layer 2) is not implemented yet.
package mcserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/codenexus/axon/pulse/internal/protocol"
)

type InstanceConfig struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	GamePlatform string   `json:"game_platform"` // "java" | "bedrock"
	Version      string   `json:"version"`
	SoftwareType string   `json:"software_type"` // "vanilla" for v1
	Command      []string `json:"command"`
	WorkingDir   string   `json:"working_dir"`
	// Env holds extra "KEY=VALUE" entries appended to the spawned process's
	// environment (on top of Pulse's own inherited environment) — e.g.
	// bedrock_server needs LD_LIBRARY_PATH=. to find its bundled .so files.
	// Empty/omitted for every hand-configured instance and for Java.
	Env []string `json:"env,omitempty"`
	// Port is only set for instances Pulse itself provisioned via
	// create_instance — see protocol.InstanceStatus.Port's doc comment.
	Port int `json:"port,omitempty"`
}

type fileConfig struct {
	Instances []InstanceConfig `json:"instances"`
	// BackupsDir overrides where backup archives are stored (default:
	// <credential.Dir()>/backups, chosen by the caller when this is empty).
	// Top-level rather than per-instance since disk space for potentially
	// many whole-world tarballs across multiple instances is the kind of
	// thing an admin wants redirected to a bigger/separate disk as a single
	// setting, not configured per instance.
	BackupsDir string `json:"backups_dir"`
}

// LoadConfig reads the instance list and optional backups_dir override from
// the Pulse instance config file.
func LoadConfig(path string) (instances []InstanceConfig, backupsDir string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read instance config: %w", err)
	}
	var cfg fileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, "", fmt.Errorf("parse instance config: %w", err)
	}
	return cfg.Instances, cfg.BackupsDir, nil
}

type instance struct {
	cfg InstanceConfig

	mu sync.Mutex
	// cmd is non-nil only for a process this Manager spawned itself this
	// run (so its Wait() goroutine can reap it). pid is set whenever the
	// instance is Running/Starting/Stopping regardless of whether we
	// spawned it or adopted it via Reconcile — Stop() and PID-file
	// bookkeeping key off pid, not cmd, so both paths behave identically.
	cmd       *exec.Cmd
	pid       int
	state     protocol.RunningState
	startedAt time.Time
}

type Manager struct {
	mu        sync.RWMutex
	instances map[string]*instance
}

func NewManager(configs []InstanceConfig) *Manager {
	m := &Manager{instances: make(map[string]*instance, len(configs))}
	for _, c := range configs {
		m.instances[c.ID] = &instance{cfg: c, state: protocol.StateStopped}
	}
	return m
}

// AddInstance registers a newly-provisioned instance (see
// pulse/internal/provision and pulse/cmd/pulse/create_instance.go) and
// persists the full instance list to configPath in the same critical
// section as the in-memory insert — so two create_instance commands landing
// in the same heartbeat batch can't race writing the config file, and a
// failed disk write can never leave memory and disk holding different
// instance lists (the in-memory insert is rolled back if SaveConfig fails).
// Rejects a duplicate ID (defensive — Panel-generated ids shouldn't collide
// in practice).
func (m *Manager) AddInstance(cfg InstanceConfig, configPath, backupsDir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.instances[cfg.ID]; exists {
		return fmt.Errorf("instance %q already exists", cfg.ID)
	}
	m.instances[cfg.ID] = &instance{cfg: cfg, state: protocol.StateStopped}

	configs := make([]InstanceConfig, 0, len(m.instances))
	for _, inst := range m.instances {
		inst.mu.Lock()
		configs = append(configs, inst.cfg)
		inst.mu.Unlock()
	}
	sort.Slice(configs, func(i, j int) bool { return configs[i].ID < configs[j].ID })

	if err := SaveConfig(configPath, configs, backupsDir); err != nil {
		delete(m.instances, cfg.ID)
		return fmt.Errorf("persist instance config: %w", err)
	}
	return nil
}

func (m *Manager) Start(id string) error {
	m.mu.RLock()
	inst, ok := m.instances[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown instance %q", id)
	}

	inst.mu.Lock()
	if inst.state == protocol.StateRunning || inst.state == protocol.StateStarting {
		inst.mu.Unlock()
		return nil
	}
	if len(inst.cfg.Command) == 0 {
		inst.mu.Unlock()
		return fmt.Errorf("instance %q has no command configured", id)
	}

	logPath := inst.cfg.WorkingDir
	if logPath == "" {
		logPath = "."
	}
	logFile, err := os.OpenFile(logPath+"/pulse.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	cmd := exec.Command(inst.cfg.Command[0], inst.cfg.Command[1:]...)
	cmd.Dir = inst.cfg.WorkingDir
	cmd.Stdout = bufio.NewWriter(logFile)
	cmd.Stderr = cmd.Stdout
	if len(inst.cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), inst.cfg.Env...)
	}
	setProcAttrs(cmd)

	if err := cmd.Start(); err != nil {
		logFile.Close()
		inst.mu.Unlock()
		return fmt.Errorf("start instance %q: %w", id, err)
	}

	inst.cmd = cmd
	inst.pid = cmd.Process.Pid
	inst.state = protocol.StateStarting
	inst.startedAt = time.Now()
	inst.mu.Unlock()

	writePIDFile(inst.cfg.WorkingDir, cmd.Process.Pid, inst.cfg.Command)

	go func() {
		err := cmd.Wait()
		logFile.Close()
		removePIDFile(inst.cfg.WorkingDir)

		inst.mu.Lock()
		defer inst.mu.Unlock()
		if inst.state == protocol.StateStopping {
			inst.state = protocol.StateStopped
		} else if err != nil {
			inst.state = protocol.StateCrashed
		} else {
			inst.state = protocol.StateStopped
		}
		inst.cmd = nil
		inst.pid = 0
	}()

	// Give the process a moment to fail fast (bad command, missing jar)
	// before reporting "starting" back as truth.
	time.Sleep(300 * time.Millisecond)
	inst.mu.Lock()
	if inst.state == protocol.StateStarting {
		inst.state = protocol.StateRunning
	}
	inst.mu.Unlock()

	return nil
}

func (m *Manager) Stop(id string) error {
	m.mu.RLock()
	inst, ok := m.instances[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown instance %q", id)
	}

	inst.mu.Lock()
	if inst.state != protocol.StateRunning && inst.state != protocol.StateStarting {
		inst.mu.Unlock()
		return nil
	}
	if inst.pid == 0 {
		inst.mu.Unlock()
		return nil
	}

	inst.state = protocol.StateStopping
	pid := inst.pid
	workingDir := inst.cfg.WorkingDir
	inst.mu.Unlock()

	// Released the lock before this network round-trip so a slow/hanging
	// RCON exchange for one instance can't stall Statuses() reads during
	// the heartbeat loop.
	return gracefulStop(workingDir, pid)
}

// Reconcile checks each configured instance's PID file (written by Start,
// possibly from an earlier Pulse process) and adopts any process that's
// still alive and plausibly ours, rather than assuming every instance is
// freshly Stopped. This matters because Manager keeps instance state purely
// in memory: without it, any Pulse restart (binary upgrade, crash, host
// reboot) would forget that a real Minecraft server process is still
// running underneath it — risking Start() spawning a duplicate competing
// process, and causing backup_instance to skip its safe
// stop-before/restart-after sequencing simply because Pulse didn't know a
// stop was needed. Call once at startup, after NewManager and before the
// heartbeat loop begins.
func (m *Manager) Reconcile() {
	m.mu.RLock()
	instances := make([]*instance, 0, len(m.instances))
	for _, inst := range m.instances {
		instances = append(instances, inst)
	}
	m.mu.RUnlock()

	for _, inst := range instances {
		inst.mu.Lock()
		workingDir := inst.cfg.WorkingDir
		id := inst.cfg.ID
		inst.mu.Unlock()

		pf, ok := readPIDFile(workingDir)
		if !ok {
			continue
		}
		if !processMatches(pf.PID, pf.Command) {
			removePIDFile(workingDir)
			continue
		}

		inst.mu.Lock()
		inst.cmd = nil // not our child process — Wait() would fail (ECHILD); watchReattached polls instead
		inst.pid = pf.PID
		inst.state = protocol.StateRunning
		inst.startedAt = time.Now() // true start time unknown; uptime approximates from reconciliation instead
		inst.mu.Unlock()

		log.Printf("reconciled instance %q with already-running pid %d", id, pf.PID)
		go m.watchReattached(inst, pf.PID)
	}
}

// watchReattached polls a re-attached process's liveness periodically,
// since only a process's actual parent can reap it via wait(2) — a
// reattached process is a grandchild of init after the earlier Pulse
// process that spawned it already exited, so cmd.Wait() isn't available
// for it the way it is for a process Start() spawned this run.
func (m *Manager) watchReattached(inst *instance, pid int) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		inst.mu.Lock()
		samePID := inst.pid == pid
		state := inst.state
		inst.mu.Unlock()
		if !samePID || (state != protocol.StateRunning && state != protocol.StateStopping) {
			return // Stop()/a fresh Start() already took over, or already transitioned
		}
		if processAlive(pid) {
			continue
		}

		inst.mu.Lock()
		if inst.pid == pid {
			inst.state = protocol.StateStopped
			inst.pid = 0
		}
		workingDir := inst.cfg.WorkingDir
		inst.mu.Unlock()
		removePIDFile(workingDir)
		return
	}
}

// InstanceConfig returns the configuration for a known instance, so callers
// outside this package (the backup engine) can read WorkingDir etc. without
// reaching into Manager internals.
func (m *Manager) InstanceConfig(id string) (InstanceConfig, bool) {
	m.mu.RLock()
	inst, ok := m.instances[id]
	m.mu.RUnlock()
	if !ok {
		return InstanceConfig{}, false
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.cfg, true
}

// IsRunning reports whether an instance is currently running or starting.
func (m *Manager) IsRunning(id string) bool {
	m.mu.RLock()
	inst, ok := m.instances[id]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.state == protocol.StateRunning || inst.state == protocol.StateStarting
}

// WaitStopped polls Statuses() until the instance reports Stopped (or
// Crashed) or timeout elapses. Same poll-until-stopped pattern used by
// manager_test.go's TestStartStopLifecycle, promoted here for restore_backup
// (which must not extract over a still-running server).
func (m *Manager) WaitStopped(id string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		m.mu.RLock()
		inst, ok := m.instances[id]
		m.mu.RUnlock()
		if !ok {
			return fmt.Errorf("unknown instance %q", id)
		}
		inst.mu.Lock()
		state := inst.state
		inst.mu.Unlock()
		if state == protocol.StateStopped || state == protocol.StateCrashed {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("instance %q did not stop within %s", id, timeout)
}

func (m *Manager) Statuses() []protocol.InstanceStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]protocol.InstanceStatus, 0, len(m.instances))
	for _, inst := range m.instances {
		inst.mu.Lock()
		var uptime int64
		if inst.state == protocol.StateRunning && !inst.startedAt.IsZero() {
			uptime = int64(time.Since(inst.startedAt).Seconds())
		}
		out = append(out, protocol.InstanceStatus{
			ID:            inst.cfg.ID,
			Name:          inst.cfg.Name,
			GamePlatform:  inst.cfg.GamePlatform,
			Version:       inst.cfg.Version,
			SoftwareType:  inst.cfg.SoftwareType,
			RunningState:  inst.state,
			PlayerCount:   0,
			Players:       []string{},
			UptimeSeconds: uptime,
			Port:          inst.cfg.Port,
		})
		inst.mu.Unlock()
	}
	return out
}
