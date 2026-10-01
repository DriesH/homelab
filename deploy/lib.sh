# Shared helpers for the installers. Source it, don't run it.
# shellcheck shell=bash

TEMPLATE_STORAGE="local"

log() { printf '\033[1;32m==>\033[0m %s\n' "$*"; }

die() {
    printf '\033[1;31merror:\033[0m %s\n' "$*" >&2
    exit 1
}

# ask VAR "Question" [default] [secret]. Reads from the terminal, so it also
# works when the script itself comes from a pipe.
ask() {
    local var="$1" question="$2" default="${3:-}" secret="${4:-}" answer
    local prompt="$question"
    [[ -n "$default" ]] && prompt+=" [$default]"

    if [[ -n "$secret" ]]; then
        read -r -s -p "$prompt: " answer </dev/tty
        echo
    else
        read -r -p "$prompt: " answer </dev/tty
    fi

    printf -v "$var" '%s' "${answer:-$default}"
}

require_proxmox() {
    [[ $EUID -eq 0 ]] || die "run this as root on the Proxmox host"
    command -v pveversion >/dev/null || die "pveversion not found, this is not a Proxmox VE host"

    local major
    major="$(pveversion | sed -E 's#^pve-manager/([0-9]+).*#\1#')"
    ((major >= 9)) || die "Proxmox VE 9 or newer is required, found: $(pveversion)"

    [[ "$(dpkg --print-architecture)" == "amd64" ]] || die "only amd64 hosts are supported for now"
}

container_exists() {
    pct list | awk 'NR > 1 { print $NF }' | grep -qx "$1"
}

# Sets TEMPLATE to the newest Debian 13 template and downloads it if needed.
download_template() {
    log "Downloading Debian 13 template"
    pveam update >/dev/null

    TEMPLATE="$(pveam available --section system | awk '{ print $2 }' | grep -E '^debian-13-standard_.*_amd64\.tar\.zst$' | sort -V | tail -n1)"
    [[ -n "$TEMPLATE" ]] || die "no Debian 13 template found"

    if ! pveam list "$TEMPLATE_STORAGE" | grep -q "$TEMPLATE"; then
        pveam download "$TEMPLATE_STORAGE" "$TEMPLATE"
    fi
}

wait_for_network() {
    local ctid="$1"

    log "Waiting for network in container $ctid"
    for _ in $(seq 1 30); do
        pct exec "$ctid" -- getent hosts deb.debian.org >/dev/null 2>&1 && return
        sleep 2
    done
    die "container $ctid has no network"
}

container_ip() {
    pct exec "$1" -- hostname -I | awk '{ print $1 }'
}

# The media on the host: a NAS share, or a folder on a disk of the host. The
# media stack and Jellyfin both see it as /data/media.
MEDIA_MOUNT="/mnt/homelab/media"
MEDIA_MOUNT_UNIT="mnt-homelab-media.mount"
# Unprivileged LXCs shift ids by 100000, and the apps of the media stack run as uid 1000.
HOST_CONTAINER_UID=101000
SYSTEM_FOLDERS=" bin boot dev etc lib lib32 lib64 libx32 proc root run sbin sys tmp usr var "

# ask_media_source asks where the media is: on a NAS, or in a folder on this
# host. It asks nothing when the media is already mounted.
ask_media_source() {
    NAS_SERVER="" NAS_EXPORT="" MEDIA_FOLDER=""
    if systemctl is-active --quiet "$MEDIA_MOUNT_UNIT"; then
        echo "Using the media at $MEDIA_MOUNT ($(media_source))."
        return 0
    fi

    local where
    ask where "Are your movies and series on a NAS or in a folder on this host? (nas/folder)" "nas"
    case "$where" in
        nas)
            ask NAS_SERVER "NAS address (IP or hostname)"
            ask NAS_EXPORT "NFS export path on the NAS (UGOS shows it, e.g. /volume1/media)"
            ;;
        folder)
            echo "For a new disk, make a folder on it first: Proxmox > Disks > Directory (for example /mnt/pve/media)."
            ask MEDIA_FOLDER "Full path of the folder on this host"
            ;;
        *) die "answer nas or folder" ;;
    esac
}

# media_source is the NAS share or the folder that is mounted at MEDIA_MOUNT.
media_source() {
    systemctl show -p What --value "$MEDIA_MOUNT_UNIT"
}

media_is_local() {
    [[ "$(media_source)" == /* ]]
}

# mount_media_share mounts the NAS share (NAS_SERVER:NAS_EXPORT) or the folder
# (MEDIA_FOLDER) at MEDIA_MOUNT. When the same media is already mounted, it
# keeps that mount, so the apps that use it notice nothing. It sets
# CREATED_MOUNT when it wrote the mount unit.
mount_media_share() {
    local what="${NAS_SERVER:-}:${NAS_EXPORT:-}" current
    [[ -n "${MEDIA_FOLDER:-}" ]] && what="$MEDIA_FOLDER"

    if systemctl is-active --quiet "$MEDIA_MOUNT_UNIT"; then
        current="$(media_source)"
        if [[ "$what" == ":" || "$current" == "$what" ]]; then
            log "Using the media that is already mounted at $MEDIA_MOUNT ($current)"
            make_media_folders
            return 0
        fi
        die "the media is already mounted from $current. Use the same NAS share or folder, or leave them empty"
    fi

    if [[ -n "${MEDIA_FOLDER:-}" ]]; then
        mount_media_folder
    else
        mount_nas_share
    fi
    make_media_folders
}

mount_nas_share() {
    [[ -n "${NAS_SERVER:-}" && -n "${NAS_EXPORT:-}" ]] || die "give the NAS address and the NFS export of the media share, or a folder on this host"

    log "Mounting $NAS_SERVER:$NAS_EXPORT at $MEDIA_MOUNT"
    install -d -m 0755 "$MEDIA_MOUNT"
    cat >"/etc/systemd/system/$MEDIA_MOUNT_UNIT" <<EOF
[Unit]
Description=Homelab media share on the NAS
After=network-online.target
Wants=network-online.target
# Containers bind-mount this folder, so mount it before they start.
Before=pve-guests.service

[Mount]
What=$NAS_SERVER:$NAS_EXPORT
Where=$MEDIA_MOUNT
Type=nfs
Options=_netdev,hard,noatime

[Install]
WantedBy=remote-fs.target
EOF
    start_media_mount "could not mount the NAS share, check that NFS is on in UGOS (Control Panel > File Services > NFS) and that the share has an NFS permission rule for this host"
}

# mount_media_folder bind-mounts a folder on a disk of this host at
# MEDIA_MOUNT, so the apps use it like a NAS share.
mount_media_folder() {
    check_media_folder

    log "Mounting $MEDIA_FOLDER at $MEDIA_MOUNT"
    install -d -m 0755 "$MEDIA_MOUNT"
    cat >"/etc/systemd/system/$MEDIA_MOUNT_UNIT" <<EOF
[Unit]
Description=Homelab media folder on this host
# Mount after the disk of the folder, never the empty folder under it.
RequiresMountsFor=$MEDIA_FOLDER
# Containers bind-mount this folder, so mount it before they start.
Before=pve-guests.service

[Mount]
What=$MEDIA_FOLDER
Where=$MEDIA_MOUNT
Type=none
Options=bind

[Install]
WantedBy=local-fs.target
EOF
    start_media_mount "could not mount $MEDIA_FOLDER"
}

start_media_mount() {
    # shellcheck disable=SC2034 # the installers read it to clean up
    CREATED_MOUNT=1
    systemctl daemon-reload
    systemctl enable "$MEDIA_MOUNT_UNIT"
    # Restart, not start: a mount from an earlier try keeps old NFS settings and cached answers.
    systemctl restart "$MEDIA_MOUNT_UNIT" || die "$1"
}

# check_media_folder allows a full path to a folder, outside the folders of
# the system and of Homelab. The host agent checks the same.
check_media_folder() {
    local folder="$MEDIA_FOLDER" first
    if [[ ! "$folder" =~ ^(/[A-Za-z0-9._-]+)+$ || ${#folder} -gt 255 || "$(realpath -ms -- "$folder")" != "$folder" ]]; then
        die "'$folder' is not a full path, use something like /mnt/pve/media"
    fi
    first="${folder#/}"
    first="${first%%/*}"
    if [[ "$SYSTEM_FOLDERS" == *" $first "* || "$MEDIA_MOUNT/" == "$folder/"* || "$folder" == /mnt/homelab/* ]]; then
        die "$folder can't hold the media, use a folder outside the system folders, like /mnt/pve/media"
    fi
    [[ -d "$folder" ]] || die "$folder is not a folder on this host. For a new disk, make it with Proxmox > Disks > Directory"
}

# make_media_folders makes the movies and series folders that are missing in
# a folder on this host, owned by the user of the containers. It never changes
# folders that exist. On a NAS, the NAS decides who owns what.
make_media_folders() {
    media_is_local || return 0

    local folder
    for folder in "$MOVIES_FOLDER" "$SERIES_FOLDER"; do
        if [[ ! -e "$MEDIA_MOUNT/$folder" ]]; then
            log "Making the folder $(media_source)/$folder"
            install -d -m 0755 -o "$HOST_CONTAINER_UID" -g "$HOST_CONTAINER_UID" "$MEDIA_MOUNT/$folder"
        fi
    done
}

# check_folders allows two different folder names in the share, and no paths.
check_folders() {
    local folder pattern='^[A-Za-z0-9]([A-Za-z0-9 ._-]{0,62}[A-Za-z0-9_-])?$'
    for folder in "${MOVIES_FOLDER:-}" "${SERIES_FOLDER:-}"; do
        if [[ ! "$folder" =~ $pattern || "$folder" == *..* ]]; then
            die "'$folder' is not a folder name, use something like movies or series"
        fi
    done
    [[ "${MOVIES_FOLDER,,}" != "${SERIES_FOLDER,,}" ]] || die "movies and series need different folders"
}
