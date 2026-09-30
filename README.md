# Homelab

A web app to manage a Proxmox VE server from `https://homelab.local`.

## How it works

- **Manager**: a Go binary in its own LXC container. It serves the React app and talks to the Proxmox API with a limited API token.
- **Host agent**: a small Go service on the Proxmox host. It does what the API can't do, like `apt` upgrades. It only listens on a Unix socket that is shared with the manager container.
- **HTTPS**: the manager runs its own certificate authority. That CA can only sign certificates for `homelab.local`.
- **Login**: one admin account with a password and an authenticator app code (TOTP).

## Updates

The Updates page checks the Proxmox host and all running containers for updates. It uses apt, the `update` command of community-scripts.org containers, and Docker Compose images.

- **Containers**: once a week (Sunday 04:00 by default), each container with auto-update on gets a snapshot and then its updates. If the update fails, or the container stops, the manager rolls it back to the snapshot. The last 3 snapshots are kept.
- **Proxmox host**: the schedule only checks it, because the host has no snapshots. Install host updates from the dashboard. The page tells you when a new kernel needs a reboot.
- **The manager itself** is never updated by the schedule, and a failed update of it is not rolled back automatically.
- **Telegram**: optional. Create a bot with @BotFather, send it a message, and get your chat ID from @userinfobot. Then enter both on the Updates page.

## Jellyfin

Connect Jellyfin on the Jellyfin page with its URL and an API key (Jellyfin: Dashboard > API Keys). The page shows library counts, what is playing (and whether it transcodes), and recently added items.

The page can also turn on a Netflix-style theme for the Jellyfin web client. The theme is written for the Modern layout of Jellyfin 12 and lives in `internal/jellyfin/netflix.css`. The manager adds it to the custom CSS of Jellyfin between two marker comments, so your own custom CSS stays. The TV and phone apps do not use custom CSS.

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
