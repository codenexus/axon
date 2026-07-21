// Command pulse is the Axon Pulse agent: it manages Minecraft server
// processes on this host and reports state to Axon Panel via a pull-based
// HTTP heartbeat + command-poll loop (Pulse always initiates contact).
package main

import (
	"flag"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/codenexus/axon/pulse/internal/credential"
	"github.com/codenexus/axon/pulse/internal/inventory"
	"github.com/codenexus/axon/pulse/internal/mcserver"
	"github.com/codenexus/axon/pulse/internal/protocol"
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

	instanceConfigs, err := mcserver.LoadConfig(*configPath)
	if err != nil {
		log.Printf("no instance config loaded (%v); starting with zero configured instances", err)
	}
	manager := mcserver.NewManager(instanceConfigs)

	client := protocol.NewClient(cred.ServerURL)
	runLoop(client, cred, manager, *interval)
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

func runLoop(client *protocol.Client, cred *credential.Credential, manager *mcserver.Manager, interval time.Duration) {
	var pendingResults []protocol.CommandResult

	for {
		req := protocol.HeartbeatRequest{
			DeviceID:              cred.DeviceID,
			Timestamp:             time.Now().Unix(),
			PulseVersion:          version,
			Host:                  inventory.Collect(),
			Instances:             manager.Statuses(),
			PendingCommandResults: pendingResults,
		}

		resp, err := client.Heartbeat(cred.DeviceCredential, req)
		if err != nil {
			log.Printf("heartbeat failed: %v", err)
			time.Sleep(interval)
			continue
		}
		pendingResults = nil

		for _, cmd := range resp.Commands {
			pendingResults = append(pendingResults, execute(manager, cmd))
		}

		time.Sleep(interval)
	}
}

func execute(manager *mcserver.Manager, cmd protocol.Command) protocol.CommandResult {
	var err error
	switch cmd.Type {
	case "start_instance":
		err = manager.Start(cmd.InstanceID)
	case "stop_instance":
		err = manager.Stop(cmd.InstanceID)
	default:
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: "unknown command type: " + cmd.Type}
	}

	if err != nil {
		log.Printf("command %s (%s) failed: %v", cmd.ID, cmd.Type, err)
		return protocol.CommandResult{CommandID: cmd.ID, Success: false, Message: err.Error()}
	}
	return protocol.CommandResult{CommandID: cmd.ID, Success: true}
}
