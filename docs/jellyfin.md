# Jellyfin

## Install Jellyfin from Homelab

The Apps page installs Jellyfin in its own container. Install it before the media stack: the media stack then finds Jellyfin and adds its libraries and Seerr.

### Before you start

1. Choose where your movies and series are: on a NAS, or on a disk of the Proxmox host. See [media-folder.md](media-folder.md).
2. Choose a username and password for the Jellyfin admin.

If the media stack already uses a media folder, the form uses that folder and does not ask for it.

### What the installer does

1. Mounts the media folder (the NAS share or the folder on the host) at `/mnt/homelab/media`, unless it is already mounted. On the host, it makes the movies and series folders when they do not exist.
2. Makes a Debian 13 container named `jellyfin` (2 cores, 2 GB memory, 16 GB disk) with the tags `homelab` and `jellyfin`.
3. Gives the container the media folder at `/data/media`, read-only. Jellyfin keeps its own data on the disk of the container.
4. Installs Jellyfin from the official repository, `repo.jellyfin.org`. The Updates page updates it later, with the other containers.
5. Passes the GPU of the host to the container, when the host has one (see below).
6. Finishes the setup wizard with your admin account.
7. Adds the libraries **Movies** (`/data/media/<movies folder>`) and **Series** (`/data/media/<series folder>`).
8. Turns on the Netflix theme, if you chose it.
9. Makes an API key for Homelab and connects the Jellyfin page of Homelab with it. The key never goes to the browser.

The installer takes about 5 minutes. If it fails, it removes the container and the mount it made. Then click **Try again**, or **Change answers** to fix an answer. Your answers stay on the host for 24 hours. The admin password never goes back to the browser.

You can also run the installer on the Proxmox host, as root: `bash /usr/local/lib/homelab/stacks/jellyfin/install.sh`. It asks the same questions.

### Hardware transcoding

When the host has a GPU (`/dev/dri/renderD128`, Intel or AMD), the installer:

1. gives the container that device, with the `render` group of the container;
2. checks with `vainfo` of Jellyfin that Jellyfin can use it;
3. turns on VA-API for H.264 and HEVC.

If the check fails, Jellyfin transcodes on the CPU, and the log says so. For Intel, the `jellyfin-ffmpeg` package brings its own drivers, so you install nothing on the host. For AMD, the check shows whether the drivers in the container are enough.

Newer GPUs can decode more, like VP9, AV1 and 10-bit HEVC. Turn those on in Jellyfin: **Dashboard > Playback > Transcoding**. On Intel, you can also switch to **Intel QuickSync (QSV)** there, which is a bit faster.

### A Jellyfin that is already installed

A container named `jellyfin` counts as installed, also when Homelab did not make it (for example from community-scripts.org). The Apps page then shows it, and does not install a second one. Homelab can't update or remove that container from the Apps page. Update it on the Updates page.

### Update and remove

The Apps page can update and remove a Jellyfin that Homelab installed.

- **Update** makes a snapshot, and then updates Jellyfin and the other packages. Your settings, users and libraries stay. If the update fails, Homelab rolls the container back to the snapshot.
- **Remove** removes the container with its disks and snapshots, and the Jellyfin page disconnects. The backups of the container stay, so you can restore it on the Backups page. Your movies and series stay in the media folder. When no other container uses the media folder, Homelab also removes its mount on the host.

## The Jellyfin page

Connect Jellyfin on the Jellyfin page with its URL and an API key (Jellyfin: Dashboard > API Keys). The page shows library counts, what is playing (and whether it transcodes), and recently added items. A Jellyfin that Homelab installed is connected already.

The page can also turn on a Netflix-style theme for the Jellyfin web client. The theme is written for the Modern layout of Jellyfin 12 and lives in `internal/jellyfin/netflix.css`. The manager adds it to the custom CSS of Jellyfin between two marker comments, so your own custom CSS stays. The TV and phone apps do not use custom CSS.
