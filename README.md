# Homelab

A web app to manage a Proxmox VE server from `https://homelab.local`.

## How it works

- **Manager**: a Go binary in its own LXC container. It serves the React app and talks to the Proxmox API with a limited API token.
- **Host agent**: a small Go service on the Proxmox host. It does what the API can't do, like `apt` upgrades. It only listens on a Unix socket that is shared with the manager container.
- **HTTPS**: the manager runs its own certificate authority. That CA can only sign certificates for `homelab.local`.
- **Login**: one admin account with a password and an authenticator app code (TOTP).

## Install

You need Proxmox VE 9 or newer on an amd64 host.

1. Build the bundle: `make bundle`.
2. Copy `dist/homelab-<version>-linux-amd64.tar.gz` to the Proxmox host.
3. Extract it and run `./install.sh` as root.
4. Follow the prompts to create your admin account.
5. Download `http://homelab.local/ca.crt` and install it as a trusted root certificate on each device.

## Media stack (optional)

Prowlarr, Radarr, Sonarr, Bazarr, qBittorrent, Seerr, Recyclarr and FlareSolverr in one LXC with Docker. qBittorrent, Prowlarr and FlareSolverr go through ProtonVPN via Gluetun.

Before you start:

1. On the UGREEN NAS, turn on NFS: Control Panel > File Services > NFS.
2. Add an NFS permission rule to the media share for the Proxmox host. Map all users to one NAS user with read/write access.
3. Create a ProtonVPN WireGuard key with "NAT-PMP (Port Forwarding)" on.
4. Optional: create a Jellyfin API key (Dashboard > API Keys).

Then run `stacks/arr/install.sh` from the bundle as root on the Proxmox host. The installer:

- mounts the NAS share on the host and shares it with the new LXC and with Jellyfin as `/data/media`;
- keeps downloads on a local disk (`/data/downloads`);
- connects all apps: logins, root folders, qBittorrent, Prowlarr sync, FlareSolverr, subtitles and Jellyfin libraries.

After the install, finish the Seerr setup and add your indexers in Prowlarr.

## Development

```sh
make test       # Go tests, lint and typecheck
make dev-api    # API on 127.0.0.1:8080 (needs Proxmox env vars, see Makefile)
make dev-web    # Vite dev server, proxies /api to the API
```
