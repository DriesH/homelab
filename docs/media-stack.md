# Media stack

Prowlarr, Radarr, Sonarr, Bazarr, qBittorrent, Seerr, Recyclarr and FlareSolverr in one LXC with Docker. qBittorrent, Prowlarr and FlareSolverr go through ProtonVPN via Gluetun.

## Before you start

1. On the UGREEN NAS, turn on NFS: Control Panel > File Services > NFS.
2. Add an NFS permission rule to the media share for the Proxmox host. Map all users to one NAS user with read/write access.
3. Create a ProtonVPN WireGuard key with "NAT-PMP (Port Forwarding)" on.
4. Optional: create a Jellyfin API key (Dashboard > API Keys).

## Install

Install it on the Apps page. The host agent runs the installer from the release bundle, in its own container, and the page shows the log. If the install fails, it removes the container it made, so you can try again. Telegram tells you when it is done.

You can also run `stacks/arr/install.sh` from the bundle as root on the Proxmox host. It asks the same questions.

## Folders

Movies and series each get their own folder in the NAS share. The form asks for both names:

| Folder  | Default  | On the NAS                   | In the apps and Jellyfin |
| ------- | -------- | ---------------------------- | ------------------------ |
| Movies  | `movies` | `/volume1/media/movies`      | `/data/media/movies`     |
| Series  | `series` | `/volume1/media/series`      | `/data/media/series`     |

Radarr puts movies in the first folder and Sonarr puts series in the second. With a Jellyfin API key, the installer adds a "Movies" and a "Series" library for them. If a folder already has files, Jellyfin shows them. To add them to Radarr or Sonarr, use Library Import in those apps.

Media stacks that were installed before you could choose the folders use `movies` and `tv`.

## What the installer does

- mounts the NAS share on the host and shares it with the new LXC and with Jellyfin as `/data/media`;
- keeps downloads on a local disk (`/data/downloads`);
- connects all apps: logins, root folders, qBittorrent, Prowlarr sync, FlareSolverr, subtitles and Jellyfin libraries.

After the install, the Apps page shows links to each app. Finish the Seerr setup first, then add your indexers in Prowlarr.

The agent checks every answer before it writes them to a file that only root can read. The installer deletes that file when it has read it.
