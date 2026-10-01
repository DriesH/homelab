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

# The media share on the host. The media stack and Jellyfin both see it as /data/media.
MEDIA_MOUNT="/mnt/homelab/media"
MEDIA_MOUNT_UNIT="mnt-homelab-media.mount"

# mount_media_share mounts NAS_SERVER:NAS_EXPORT at MEDIA_MOUNT. When the share
# is already mounted from there, it keeps that mount, so the apps that use it
# notice nothing. It sets CREATED_MOUNT when it wrote the mount unit.
mount_media_share() {
    local what="$NAS_SERVER:$NAS_EXPORT" current
    if systemctl is-active --quiet "$MEDIA_MOUNT_UNIT"; then
        current="$(systemctl show -p What --value "$MEDIA_MOUNT_UNIT")"
        if [[ -z "$NAS_SERVER" || "$current" == "$what" ]]; then
            log "Using the NAS share that is already mounted at $MEDIA_MOUNT ($current)"
            return 0
        fi
        die "the media share is already mounted from $current. Use the same NAS address and export, or leave them empty"
    fi
    [[ -n "$NAS_SERVER" && -n "$NAS_EXPORT" ]] || die "give the NAS address and the NFS export of the media share"

    log "Mounting $what at $MEDIA_MOUNT"
    install -d -m 0755 "$MEDIA_MOUNT"
    cat >"/etc/systemd/system/$MEDIA_MOUNT_UNIT" <<EOF
[Unit]
Description=Homelab media share on the NAS
After=network-online.target
Wants=network-online.target
# Containers bind-mount this folder, so mount it before they start.
Before=pve-guests.service

[Mount]
What=$what
Where=$MEDIA_MOUNT
Type=nfs
Options=_netdev,hard,noatime

[Install]
WantedBy=remote-fs.target
EOF
    # shellcheck disable=SC2034 # the installers read it to clean up
    CREATED_MOUNT=1
    systemctl daemon-reload
    systemctl enable "$MEDIA_MOUNT_UNIT"
    # Restart, not start: a mount from an earlier try keeps old NFS settings and cached answers.
    systemctl restart "$MEDIA_MOUNT_UNIT" || die "could not mount the NAS share, check that NFS is on in UGOS (Control Panel > File Services > NFS) and that the share has an NFS permission rule for this host"
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
