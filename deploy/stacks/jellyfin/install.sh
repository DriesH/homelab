#!/usr/bin/env bash
# Installs Jellyfin in a new LXC on this Proxmox host, with the official
# Jellyfin repository, the media (on a NAS or a disk of this host) and the GPU of the host.
#
#   ./install.sh [--storage local-lvm] [--bridge vmbr0] [--ctid 140] [--answers file]
#
# With --answers, it asks nothing and reads the answers from a KEY=value file.
# The host agent uses this for the Apps page.
set -euo pipefail

STACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=deploy/lib.sh
source "$STACK_DIR/../../lib.sh"

STORAGE="local-lvm"
BRIDGE="vmbr0"
CT_HOSTNAME="jellyfin"
CT_ID=""
ANSWERS=""
NAS_SERVER=""
NAS_EXPORT=""
MEDIA_FOLDER=""
THEME="y"
GPU=""
GPU_DEVICE="/dev/dri/renderD128"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --storage) STORAGE="$2"; shift 2 ;;
        --bridge) BRIDGE="$2"; shift 2 ;;
        --ctid) CT_ID="$2"; shift 2 ;;
        --answers) ANSWERS="$2"; shift 2 ;;
        *) die "unknown option: $1" ;;
    esac
done

preflight() {
    require_proxmox
    [[ -x "$STACK_DIR/homelab-jellyfin" ]] || die "missing homelab-jellyfin next to install.sh"
    container_exists "$CT_HOSTNAME" && die "a container named '$CT_HOSTNAME' already exists"

    if [[ -e "$GPU_DEVICE" ]]; then
        GPU=1
    fi
}

# load_answers reads KEY=value lines. Values are only stored, never run.
load_answers() {
    local line key value
    while IFS= read -r line || [[ -n "$line" ]]; do
        [[ -z "$line" ]] && continue
        key="${line%%=*}" value="${line#*=}"
        case "$key" in
            NAS_SERVER | NAS_EXPORT | MEDIA_FOLDER | MOVIES_FOLDER | SERIES_FOLDER | JELLYFIN_ADMIN_USERNAME | \
                JELLYFIN_ADMIN_PASSWORD | THEME | STORAGE)
                printf -v "$key" '%s' "$value"
                ;;
            *) die "unknown answer: $key" ;;
        esac
    done <"$ANSWERS"
    # It holds the admin password.
    rm -f "$ANSWERS"
}

ask_settings() {
    if [[ -n "$ANSWERS" ]]; then
        load_answers
    else
        log "A few questions first"
        ask_media_source
        ask MOVIES_FOLDER "Folder for movies in it" "movies"
        ask SERIES_FOLDER "Folder for series in it" "series"
        echo "The admin account of Jellyfin:"
        ask JELLYFIN_ADMIN_USERNAME "Username" "admin"
        ask JELLYFIN_ADMIN_PASSWORD "Password" "" secret
        ask THEME "Turn on the Netflix theme? (y/n)" "y"
    fi

    check_folders
    [[ -n "${JELLYFIN_ADMIN_USERNAME:-}" && -n "${JELLYFIN_ADMIN_PASSWORD:-}" ]] || die "the Jellyfin admin needs a username and a password"
}

create_container() {
    CT_ID="${CT_ID:-$(pvesh get /cluster/nextid)}"
    log "Creating container $CT_ID ($CT_HOSTNAME)"

    pct create "$CT_ID" "$TEMPLATE_STORAGE:vztmpl/$TEMPLATE" \
        --hostname "$CT_HOSTNAME" \
        --unprivileged 1 \
        --features nesting=1 \
        --cores 2 --memory 2048 --swap 512 \
        --rootfs "$STORAGE:16" \
        --net0 "name=eth0,bridge=$BRIDGE,ip=dhcp" \
        --onboot 1 \
        --mp0 "$MEDIA_MOUNT,mp=/data/media,ro=1" \
        --tags "homelab;jellyfin" \
        --description "Jellyfin, installed by Homelab"

    CREATED_CT="$CT_ID"
    pct start "$CT_ID"
    wait_for_network "$CT_ID"
}

# on_exit removes the container and the mount that a failed install from the
# Apps page made, so the next try can start clean.
on_exit() {
    local code=$?
    if ((code == 0)) || [[ -z "$ANSWERS" ]]; then
        return
    fi
    if [[ -n "${CREATED_CT:-}" ]]; then
        log "The install failed, removing container $CREATED_CT"
        pct stop "$CREATED_CT" >/dev/null 2>&1 || true
        pct destroy "$CREATED_CT" --purge 1 || true
    fi
    if [[ -n "${CREATED_MOUNT:-}" ]]; then
        log "Removing the media mount"
        systemctl disable --now "$MEDIA_MOUNT_UNIT" >/dev/null 2>&1 || true
        rm -f "/etc/systemd/system/$MEDIA_MOUNT_UNIT"
        systemctl daemon-reload || true
    fi
}

install_jellyfin() {
    log "Installing Jellyfin from repo.jellyfin.org"
    # shellcheck disable=SC2016 # expands inside the container
    pct exec "$CT_ID" -- bash -euc '
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -q
        apt-get install -y -q ca-certificates curl gnupg
        install -d -m 0755 /etc/apt/keyrings
        curl -fsSL https://repo.jellyfin.org/jellyfin_team.gpg.key | gpg --dearmor --yes -o /etc/apt/keyrings/jellyfin.gpg
        cat >/etc/apt/sources.list.d/jellyfin.sources <<EOF
Types: deb
URIs: https://repo.jellyfin.org/debian
Suites: $(. /etc/os-release && echo "$VERSION_CODENAME")
Components: main
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/jellyfin.gpg
EOF
        apt-get update -q
        apt-get install -y -q jellyfin
    '
}

# setup_gpu passes the GPU of the host to the container, and checks that
# Jellyfin can use it. Without a working GPU, Jellyfin uses the CPU.
setup_gpu() {
    [[ -n "$GPU" ]] || return 0

    log "Passing the GPU ($GPU_DEVICE) to the container"
    local gid
    gid="$(pct exec "$CT_ID" -- bash -euc '
        getent group render >/dev/null || groupadd --system render
        usermod -aG render,video jellyfin
        getent group render | cut -d: -f3
    ')"
    pct set "$CT_ID" --dev0 "$GPU_DEVICE,gid=$gid"
    pct reboot "$CT_ID"
    wait_for_network "$CT_ID"

    if ! pct exec "$CT_ID" -- runuser -u jellyfin -- /usr/lib/jellyfin-ffmpeg/vainfo --display drm --device "$GPU_DEVICE" >/dev/null 2>&1; then
        log "Jellyfin can't use the GPU, it transcodes on the CPU"
        GPU=""
    fi
}

configure_jellyfin() {
    log "Setting up Jellyfin"
    local flags=()
    [[ -n "$GPU" ]] && flags+=(--gpu)
    [[ "$THEME" == "y" ]] && flags+=(--theme)

    # The password goes on stdin, so it is not in the process list.
    printf '%s\n' "$JELLYFIN_ADMIN_PASSWORD" | "$STACK_DIR/homelab-jellyfin" setup \
        --url "http://$(container_ip "$CT_ID"):8096" \
        --admin "$JELLYFIN_ADMIN_USERNAME" \
        --movies "/data/media/$MOVIES_FOLDER" \
        --series "/data/media/$SERIES_FOLDER" \
        "${flags[@]}"
}

main() {
    trap on_exit EXIT
    preflight
    ask_settings
    mount_media_share
    download_template
    create_container
    install_jellyfin
    setup_gpu
    configure_jellyfin

    local ip
    ip="$(container_ip "$CT_ID")"

    log "Done"
    # For the host agent, which shows the link on the Apps page.
    echo "HOMELAB app jellyfin $CT_ID $ip"
    cat <<EOF

  Jellyfin:  http://$ip:8096   <- log in as '$JELLYFIN_ADMIN_USERNAME'
  Hardware transcoding: $([[ -n "$GPU" ]] && echo "on (VA-API, $GPU_DEVICE)" || echo "off, the CPU transcodes")

EOF
}

main
