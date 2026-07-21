// Package protocol defines the wire types shared between Pulse and Panel's
// HTTP heartbeat/poll contract.
package protocol

import "encoding/json"

type EnrollRequest struct {
	Hostname     string `json:"hostname"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	PulseVersion string `json:"pulse_version"`
}

type EnrollResponse struct {
	DeviceID         string `json:"device_id"`
	DeviceCredential string `json:"device_credential"`
}

type DiskUsage struct {
	Mount      string `json:"mount"`
	TotalBytes uint64 `json:"total_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
}

type HostMetrics struct {
	CPUUsagePercent float64     `json:"cpu_usage_percent"`
	CPUCores        int         `json:"cpu_cores"`
	RAMTotalBytes   uint64      `json:"ram_total_bytes"`
	RAMUsedBytes    uint64      `json:"ram_used_bytes"`
	Disks           []DiskUsage `json:"disks"`
	OS              string      `json:"os"`
	Platform        string      `json:"platform"`
}

type RunningState string

const (
	StateStopped  RunningState = "stopped"
	StateStarting RunningState = "starting"
	StateRunning  RunningState = "running"
	StateStopping RunningState = "stopping"
	StateCrashed  RunningState = "crashed"
)

// InstanceStatus is the per-Minecraft-server-instance payload reported on
// every heartbeat. Fields beyond identity/state (TPS, players, world size)
// are best-effort and only populated once RCON/log-parsing lands; they are
// present in the wire shape now so Panel's schema doesn't need to change
// when that happens.
type InstanceStatus struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	GamePlatform   string       `json:"game_platform"` // "java" | "bedrock"
	Version        string       `json:"version"`
	SoftwareType   string       `json:"software_type"` // "vanilla" for v1
	RunningState   RunningState `json:"running_state"`
	TPS            *float64     `json:"tps,omitempty"`
	PlayerCount    int          `json:"player_count"`
	Players        []string     `json:"players"`
	WorldSizeBytes int64        `json:"world_size_bytes"`
	UptimeSeconds  int64        `json:"uptime_seconds"`
}

type HeartbeatRequest struct {
	DeviceID     string           `json:"device_id"`
	Timestamp    int64            `json:"timestamp"`
	PulseVersion string           `json:"pulse_version"`
	Host         HostMetrics      `json:"host"`
	Instances    []InstanceStatus `json:"instances"`
	// PendingCommandResults are results from previously-polled commands,
	// piggybacked onto the next heartbeat rather than pushed immediately
	// (see spec open question #3 — resolved as next-poll-cycle for v1).
	PendingCommandResults []CommandResult `json:"pending_command_results,omitempty"`
}

type CommandResult struct {
	CommandID string `json:"command_id"`
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
}

type Command struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"` // "start_instance" | "stop_instance"
	InstanceID string          `json:"instance_id"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

type HeartbeatResponse struct {
	Commands []Command `json:"commands,omitempty"`
}
