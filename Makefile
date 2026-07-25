.PHONY: help build-pulse build-pulse-linux build-pulse-windows build-pulse-darwin \
	pulse-test panel-install panel-dev panel-check panel-build-node panel-build-cloudflare \
	migrate-local migrate-remote seed-dev

help:
	@echo "Axon monorepo tasks:"
	@echo "  build-pulse            build pulse for the host OS/arch"
	@echo "  build-pulse-linux      cross-compile pulse for linux/amd64"
	@echo "  build-pulse-windows    cross-compile pulse for windows/amd64"
	@echo "  build-pulse-darwin     cross-compile pulse for darwin/arm64"
	@echo "  pulse-test             go vet + go test ./... for pulse"
	@echo "  panel-install          pnpm install for panel"
	@echo "  panel-dev              run panel dev server (adapter-node)"
	@echo "  panel-check            svelte-check for panel"
	@echo "  panel-build-node       build panel with adapter-node"
	@echo "  panel-build-cloudflare build panel with adapter-cloudflare"
	@echo "  migrate-local          apply D1 migrations to local wrangler dev DB"
	@echo "  migrate-remote         apply D1 migrations to remote D1 DB"
	@echo "  seed-dev               seed a local admin + enrollment token for dev testing"

# Short commit (+ "-dirty" if the working tree has uncommitted changes) —
# no release tags exist yet, so this is the only thing that reliably tells
# you which exact source state a deployed binary came from. Shows up in
# Panel's dashboard as "Pulse v<this>" via the heartbeat's pulse_version
# field, otherwise every build looks identical ("dev").
PULSE_VERSION := $(shell git describe --always --dirty 2>/dev/null || echo dev)

build-pulse:
	cd pulse && go build -ldflags="-X main.version=$(PULSE_VERSION)" -o dist/pulse ./cmd/pulse

build-pulse-linux:
	cd pulse && GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.version=$(PULSE_VERSION)" -o dist/pulse-linux-amd64 ./cmd/pulse

build-pulse-windows:
	cd pulse && GOOS=windows GOARCH=amd64 go build -ldflags="-s -w -X main.version=$(PULSE_VERSION)" -o dist/pulse-windows-amd64.exe ./cmd/pulse

build-pulse-darwin:
	cd pulse && GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w -X main.version=$(PULSE_VERSION)" -o dist/pulse-darwin-arm64 ./cmd/pulse

pulse-test:
	cd pulse && go vet ./... && go test ./...

panel-install:
	cd panel && pnpm install

panel-dev:
	cd panel && ADAPTER=node pnpm run dev

panel-check:
	cd panel && pnpm run check

panel-build-node:
	cd panel && ADAPTER=node pnpm run build

panel-build-cloudflare:
	cd panel && ADAPTER=cloudflare pnpm run build

migrate-local:
	cd panel && pnpm exec wrangler d1 migrations apply axon --local

migrate-remote:
	cd panel && pnpm exec wrangler d1 migrations apply axon --remote

seed-dev:
	node scripts/seed-dev.mjs
