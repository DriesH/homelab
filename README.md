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

## Health

The Health page shows the SMART status of each disk, the ZFS pools, the Proxmox storage and the network shares (like the NAS media share). You can also add services to watch, by URL or by TCP port.

The manager sends a Telegram alert when:

- a disk fails SMART or is 90% worn out;
- a ZFS pool is not `ONLINE`;
- a storage or network share is 90% full;
- a network share is not mounted or does not answer;
- a service fails two checks in a row (services are checked every minute).

When the problem is fixed, you get a second message. Disks and shares are checked every 10 minutes, because each disk check runs SMART.

The host agent finds network shares in the systemd `.mount` units and `/etc/fstab` of the Proxmox host. It can't read the SMART status of the disks inside the NAS. Use the disk warnings of the NAS itself for that. Alerts use the Telegram settings from the Updates page.

## Install

You need Proxmox VE 9 or newer on an amd64 host.

1. Download `homelab-<version>-linux-amd64.tar.gz` from the latest GitHub release, or build it with `make bundle`.
2. Copy it to the Proxmox host.
3. Extract it and run `./install.sh` as root.
4. Follow the prompts to create your admin account.
5. Download `http://homelab.local/ca.crt` and install it as a trusted root certificate on each device.

## Updating Homelab

The manager checks the GitHub releases every 6 hours. When there is a new version, the header shows "Update available" and you get a Telegram message. Install it from the Updates page. You can also turn on automatic installs there.

The host agent only installs a bundle that is signed with the release key. It keeps the previous version, and brings it back when the new version does not start within a minute.

For a private repo, create a fine-grained token with read-only access to Contents of this repo only, and enter it on the Updates page.

### Making a release

1. One time: create the signing key. The private key goes straight into a GitHub secret. Commit the public key.

   ```sh
   go run ./cmd/homelab-release keygen -public internal/release/signing.pub | gh secret set HOMELAB_SIGNING_KEY
   ```

2. Tag a version and push the tag. GitHub Actions builds, tests, signs and publishes the release.

   ```sh
   git tag v0.2.0 && git push origin v0.2.0
   ```

A build without the key in `signing.pub` can't install updates. If you lose the private key, make a new one and install the next version by hand once.

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

## License

MIT, see [LICENSE](LICENSE).
