# Homelab

A web app to manage a Proxmox VE server from `https://homelab.local`: monitoring, updates, backups, logs, consoles, Tailscale and an app catalog.

## Install

You need Proxmox VE 9 or newer on an amd64 host.

On the Proxmox host, as root. Use the newest version from [Releases](https://github.com/DriesH/homelab/releases):

```sh
curl -LO https://github.com/DriesH/homelab/releases/download/v0.8.0/homelab-v0.8.0-linux-amd64.tar.gz
tar xzf homelab-v0.8.0-linux-amd64.tar.gz
cd homelab-v0.8.0-linux-amd64
./install.sh
```

1. Create your admin account when the installer asks. Add the secret it shows to an authenticator app.
2. Download `http://homelab.local/ca.crt` and install it as a trusted root certificate on each device.
3. Open `https://homelab.local`.

Options: `./install.sh --storage local-lvm --bridge vmbr0 --hostname homelab --ctid 120`.

## Update

Open the Updates page and click "Install v…". Homelab checks GitHub every 6 hours and only installs signed releases. If a new version does not start, the old one comes back.

## Uninstall

```sh
homelab-uninstall
```

It asks before it deletes the manager container, the update snapshots or the backup job. See [docs/uninstall.md](docs/uninstall.md).

## Run it locally

You need Go and Node.js 22.

```sh
make dev        # fake Proxmox and host agent, API and web app
make dev-code   # the login code, in a second terminal
```

Open http://localhost:5173 and log in with `admin` / `homelab-dev-password`. See [docs/development.md](docs/development.md) for tests, a real Proxmox and releases.

## Docs

- [How it works](docs/architecture.md)
- [Updates and Telegram](docs/updates.md)
- [Health and alerts](docs/health.md)
- [Backups and restoring the manager](docs/backups.md)
- [Logs and console](docs/logs-and-console.md)
- [Jellyfin: install and the Jellyfin page](docs/jellyfin.md)
- [Tailscale](docs/tailscale.md)
- [App names (seerr.homelab.local)](docs/app-names.md)
- [Media stack](docs/media-stack.md)
- [Media folder: on a NAS or on the host](docs/media-folder.md)
- [Settings file (homelab.yaml)](docs/settings-file.md)
- [Uninstall](docs/uninstall.md)
- [Development](docs/development.md)

## License

MIT, see [LICENSE](LICENSE).
