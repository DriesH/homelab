#!/usr/bin/env bash
# Runs Homelab on this computer with a fake Proxmox, a fake host agent and a
# fake tailscale CLI. Everything you change stays in .dev/. Stop with Ctrl-C.
set -euo pipefail
cd "$(dirname "$0")/.."

# Stop all the processes of this script on exit.
trap 'trap - EXIT; kill 0' INT TERM EXIT

mkdir -p .dev
[[ -d web/node_modules ]] || (cd web && npm ci)
# The manager embeds the web build, so it needs one to compile.
[[ -d web/dist ]] || (cd web && npm run build)

go build -o .dev/mock ./dev/mock
go build -o .dev/homelab ./cmd/homelab

.dev/mock &
for _ in $(seq 50); do
    [[ -S .dev/agent.sock ]] && break
    sleep 0.1
done

export PATH="$PWD/dev/bin:$PATH"
export HOMELAB_DEV=1 HOMELAB_HTTP_ADDR=127.0.0.1:8080 HOMELAB_DATA_DIR=.dev/data
export HOMELAB_AGENT_SOCKET=.dev/agent.sock HOMELAB_SELF_VMID=100
export PROXMOX_URL=http://127.0.0.1:18006 PROXMOX_TOKEN_ID='homelab@pve!dev' PROXMOX_TOKEN_SECRET=dev

# Restart the manager when it exits with an error, like systemd does. A data
# restore on the Settings page needs this.
(
    while true; do
        .dev/homelab serve && break
        echo "manager stopped, starting it again" >&2
        sleep 1
    done
) &

cd web && npm run dev
