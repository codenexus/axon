# Axon Pulse

Headless Go agent. Manages Minecraft server processes on this host and talks
to Axon Panel over a pull-based HTTP heartbeat + command-poll loop — Pulse
always initiates contact, so no inbound ports are required.

## Dev usage

```
go build -o pulse ./cmd/pulse
cp pulse.instances.example.json pulse.instances.json   # edit paths/command for a real server
./pulse --server-url http://localhost:5173 --enroll-token <token-from-panel>
```

On first run, Pulse enrolls with Panel using the token and persists a device
credential (`~/.config/axon/credential.json` in dev, `/etc/axon/credential.json`
when run as root on Linux — see `internal/credential.Dir()`). Subsequent runs
reuse the saved credential and `--enroll-token` is no longer needed.

## Status

Vertical slice: enrollment, heartbeat with host metrics, and `start_instance`
/ `stop_instance` commands against a locally-configured process. Not yet
implemented: RCON console commands, backups, self-update, file management,
mDNS discovery.
