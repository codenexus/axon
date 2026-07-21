// Package mcserver manages the lifecycle of locally-configured Minecraft
// server processes (layer 1 of the spec's three command layers — OS-level
// process management, no Minecraft protocol involved). Console commands via
// RCON (layer 2) are not implemented yet; graceful in-game shutdown
// ("save-all" + "stop") will replace the plain terminate signal used here
// once RCON lands.
package mcserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
}

type fileConfig struct {
	Instances []InstanceConfig `json:"instances"`
}

func LoadConfig(path string) ([]InstanceConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read instance config: %w", err)
	}
	var cfg fileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse instance config: %w", err)
	}
	return cfg.Instances, nil
}

type instance struct {
	cfg InstanceConfig

	mu        sync.Mutex
	cmd       *exec.Cmd
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
	setProcAttrs(cmd)

	if err := cmd.Start(); err != nil {
		logFile.Close()
		inst.mu.Unlock()
		return fmt.Errorf("start instance %q: %w", id, err)
	}

	inst.cmd = cmd
	inst.state = protocol.StateStarting
	inst.startedAt = time.Now()
	inst.mu.Unlock()

	go func() {
		err := cmd.Wait()
		logFile.Close()

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
	defer inst.mu.Unlock()

	if inst.state != protocol.StateRunning && inst.state != protocol.StateStarting {
		return nil
	}
	if inst.cmd == nil || inst.cmd.Process == nil {
		return nil
	}

	inst.state = protocol.StateStopping
	return terminate(inst.cmd)
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
		})
		inst.mu.Unlock()
	}
	return out
}
