# Media stack

Prowlarr, Radarr, Sonarr, Bazarr, qBittorrent, Seerr, Recyclarr and FlareSolverr in one LXC with Docker. qBittorrent, Prowlarr and FlareSolverr go through ProtonVPN via Gluetun.

## Before you start

1. On the UGREEN NAS, turn on NFS: Control Panel > File Services > NFS.
2. Add an NFS permission rule to the media share for the IP of the Proxmox host: Read/Write, and squash "Map all users to admin".
3. Make sure that the movies and series folders allow writing over NFS. The installer checks this and names the folder that fails. NFS only looks at the folder permissions on the NAS disk, not at the share permissions for SMB users.
4. Create a ProtonVPN WireGuard key with "NAT-PMP (Port Forwarding)" on.
5. Optional: create a Jellyfin API key (Dashboard > API Keys).
6. Optional: make a free account on [opensubtitles.com](https://www.opensubtitles.com). Not opensubtitles.org: that is the old site, and Bazarr can't use it without a paid VIP account.

## Install

Install it on the Apps page. The host agent runs the installer from the release bundle, in its own container, and the page shows the log. Telegram tells you when it is done.

If the install fails, it removes the container and the NAS mount it made. Then:

- **Try again** runs the install again with the same answers.
- **Change answers** opens the form with your answers. Leave the WireGuard key, the password and the Jellyfin API key empty to keep them.

The answers stay on the Proxmox host in a file that only root can read, for 24 hours or until the install works. The keys and passwords never go back to the browser. "Remove them now" deletes them at once.

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

After the install, the Apps page shows links to each app. Then add your indexers in Prowlarr.

## Subtitles

Bazarr adds subtitles in your languages to every new movie and episode. The installer turns on these sources:

- **Embedded Subtitles**: subtitles that are already inside the video file;
- **Podnapisi**: free, no account;
- **OpenSubtitles.com**: only when you give its username and password in the form. It has by far the most subtitles. The free account has a daily download limit, which is enough for normal use.

The OpenSubtitles.com password is a secret like the other passwords: it goes to the container on stdin and is never written to a file there. Bazarr keeps it in its own settings.

To add OpenSubtitles.com later: in Bazarr, open **Settings > Providers**, add **OpenSubtitles.com** (not .org), and enter your username, not your email address. Then **Wanted > Search All** searches at once.

## Seerr

Give a Jellyfin admin username and password in the form, and the installer does the setup of Seerr:

- that Jellyfin account becomes the owner of Seerr, so you log in to Seerr with it;
- the movie and series libraries of Jellyfin are turned on;
- Radarr and Sonarr are added, with the quality profile from Recyclarr and your movies and series folders.

Without a Jellyfin admin, open Seerr after the install and do its setup yourself. Until then, anyone on your network can open it. If Seerr is already set up, the installer leaves it alone.

The Jellyfin admin password is a secret like the other passwords: it goes to the container on stdin and is never written to a file there. Seerr then makes its own Jellyfin API key.

The agent checks every answer before it writes them to a file that only root can read. The installer deletes that file when it has read it.
