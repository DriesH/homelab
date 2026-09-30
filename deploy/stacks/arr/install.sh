#!/usr/bin/env bash
# Installs the media stack (Prowlarr, Radarr, Sonarr, Bazarr, qBittorrent,
# Seerr, Recyclarr, FlareSolverr) in a new LXC on this Proxmox host.
# qBittorrent, Prowlarr and FlareSolverr go through ProtonVPN via Gluetun.
#
#   ./install.sh [--storage local-lvm] [--bridge vmbr0] [--hostname media] [--ctid 130]
#                [--downloads-size 200] [--jellyfin-ctid 110] [--answers file]
# With --answers, it asks nothing and reads the answers from a KEY=value file.
# The host agent uses this for the Apps page.
set -euo pipefail

STACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=deploy/lib.sh
source "$STACK_DIR/../../lib.sh"

STORAGE="local-lvm"
BRIDGE="vmbr0"
CT_HOSTNAME="media"
CT_ID=""
DOWNLOADS_SIZE=200
JELLYFIN_CTID=""
ANSWERS=""
RESTART_JELLYFIN="y"

# Media share on the host. Both this LXC and Jellyfin get it as /data/media.
MEDIA_MOUNT="/mnt/homelab/media"
MEDIA_MOUNT_UNIT="mnt-homelab-media.mount"
# Unprivileged LXCs shift ids by 100000, and the containers run as uid 1000.
HOST_CONTAINER_UID=101000

while [[ $# -gt 0 ]]; do
    case "$1" in
        --storage) STORAGE="$2"; shift 2 ;;
        --bridge) BRIDGE="$2"; shift 2 ;;
        --hostname) CT_HOSTNAME="$2"; shift 2 ;;
        --ctid) CT_ID="$2"; shift 2 ;;
        --downloads-size) DOWNLOADS_SIZE="$2"; shift 2 ;;
        --jellyfin-ctid) JELLYFIN_CTID="$2"; shift 2 ;;
        --answers) ANSWERS="$2"; shift 2 ;;
        *) die "unknown option: $1" ;;
    esac
done

preflight() {
    require_proxmox

    # Older lxc-pve AppArmor profiles break Docker in unprivileged LXCs (Proxmox bug 7006).
    local lxc_version
    lxc_version="$(dpkg-query -W -f='${Version}' lxc-pve)"
    dpkg --compare-versions "$lxc_version" ge 6.0.5-2 ||
        die "lxc-pve $lxc_version is too old for Docker in LXC, update the host first (apt full-upgrade)"

    for file in compose.yaml recyclarr.yml homelab-arr; do
        [[ -f "$STACK_DIR/$file" ]] || die "missing $file next to install.sh"
    done

    container_exists "$CT_HOSTNAME" && die "a container named '$CT_HOSTNAME' already exists"

    if [[ -z "$JELLYFIN_CTID" ]]; then
        JELLYFIN_CTID="$(pct list | awk 'NR > 1 && $NF == "jellyfin" { print $1 }')"
    fi
}

# load_answers reads KEY=value lines. Values are only stored, never run.
load_answers() {
    local line key value
    JELLYFIN_API_KEY=""
    while IFS= read -r line || [[ -n "$line" ]]; do
        [[ -z "$line" ]] && continue
        key="${line%%=*}" value="${line#*=}"
        case "$key" in
            NAS_SERVER | NAS_EXPORT | MOVIES_FOLDER | SERIES_FOLDER | WIREGUARD_PRIVATE_KEY | VPN_COUNTRIES | \
                SUBTITLE_LANGUAGES | ARR_USERNAME | ARR_PASSWORD | JELLYFIN_API_KEY | RESTART_JELLYFIN | STORAGE | DOWNLOADS_SIZE)
                printf -v "$key" '%s' "$value"
                ;;
            *) die "unknown answer: $key" ;;
        esac
    done <"$ANSWERS"
    # It holds the VPN key and the password.
    rm -f "$ANSWERS"

    ((${#ARR_PASSWORD} >= 12)) || die "password must be at least 12 characters"
    check_folders
    JELLYFIN_URL=""
    if [[ -n "$JELLYFIN_CTID" ]]; then
        JELLYFIN_URL="http://$(container_ip "$JELLYFIN_CTID"):8096"
    else
        JELLYFIN_API_KEY=""
    fi
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

ask_settings() {
    if [[ -n "$ANSWERS" ]]; then
        load_answers
        return
    fi

    log "A few questions first"

    ask NAS_SERVER "NAS address (IP or hostname)"
    ask NAS_EXPORT "NFS export path on the NAS (UGOS shows it, e.g. /volume1/media)"
    echo "Movies and series each get a folder in this share, and a library in Jellyfin."
    ask MOVIES_FOLDER "Folder for movies" "movies"
    ask SERIES_FOLDER "Folder for series" "series"
    check_folders

    echo "Get a WireGuard key at https://account.proton.me/u/0/vpn/WireGuard"
    echo "(turn on 'NAT-PMP (Port Forwarding)' when you create it)."
    ask WIREGUARD_PRIVATE_KEY "ProtonVPN WireGuard private key" "" secret
    ask VPN_COUNTRIES "VPN server countries (comma separated)" "Netherlands"

    ask SUBTITLE_LANGUAGES "Subtitle languages (2-letter codes, comma separated)" "en"

    echo "One login for Radarr, Sonarr, Prowlarr, Bazarr and qBittorrent:"
    ask ARR_USERNAME "Username" "homelab"
    ask ARR_PASSWORD "Password (min 12 characters)" "" secret
    ((${#ARR_PASSWORD} >= 12)) || die "password must be at least 12 characters"

    JELLYFIN_URL=""
    JELLYFIN_API_KEY=""
    if [[ -n "$JELLYFIN_CTID" ]]; then
        JELLYFIN_URL="http://$(container_ip "$JELLYFIN_CTID"):8096"
        echo "Found Jellyfin in container $JELLYFIN_CTID ($JELLYFIN_URL)."
        echo "Create an API key in Jellyfin: Dashboard > API Keys > +. Leave empty to skip."
        ask JELLYFIN_API_KEY "Jellyfin API key" "" secret
    fi
}

mount_nas() {
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
    systemctl daemon-reload
    systemctl enable --now "$MEDIA_MOUNT_UNIT" || die "could not mount the NAS share, check that NFS is on in UGOS (Control Panel > File Services > NFS) and that the share has an NFS permission rule for this host"

    local test_file="$MEDIA_MOUNT/.homelab-write-test"
    if ! setpriv --reuid="$HOST_CONTAINER_UID" --regid="$HOST_CONTAINER_UID" --clear-groups touch "$test_file" 2>/dev/null; then
        die "containers can't write to the NAS share. In the share's NFS permission rule, set squash to map all users to one NAS user (all_squash) with read/write access"
    fi
    rm -f "$test_file"
}

create_container() {
    CT_ID="${CT_ID:-$(pvesh get /cluster/nextid)}"
    log "Creating container $CT_ID ($CT_HOSTNAME)"

    pct create "$CT_ID" "$TEMPLATE_STORAGE:vztmpl/$TEMPLATE" \
        --hostname "$CT_HOSTNAME" \
        --unprivileged 1 \
        --features nesting=1,keyctl=1 \
        --cores 2 --memory 4096 --swap 1024 \
        --rootfs "$STORAGE:16" \
        --net0 "name=eth0,bridge=$BRIDGE,ip=dhcp" \
        --onboot 1 \
        --dev0 /dev/net/tun \
        --mp0 "$MEDIA_MOUNT,mp=/data/media" \
        --mp1 "$STORAGE:$DOWNLOADS_SIZE,mp=/data/downloads,backup=0" \
        --tags "homelab;media" \
        --description "Homelab media stack"

    CREATED_CT="$CT_ID"
    pct start "$CT_ID"
    wait_for_network "$CT_ID"
}

# on_exit removes a half-installed container after a failed install from the
# Apps page, so the next try can start clean.
on_exit() {
    local code=$?
    if ((code != 0)) && [[ -n "$ANSWERS" && -n "${CREATED_CT:-}" ]]; then
        log "The install failed, removing container $CREATED_CT"
        pct stop "$CREATED_CT" >/dev/null 2>&1 || true
        pct destroy "$CREATED_CT" --purge 1 || true
    fi
}

install_docker() {
    log "Installing Docker"
    # shellcheck disable=SC2016 # expands inside the container
    pct exec "$CT_ID" -- bash -euc '
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -q
        apt-get install -y -q ca-certificates curl
        install -m 0755 -d /etc/apt/keyrings
        curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
        chmod a+r /etc/apt/keyrings/docker.asc
        cat >/etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/debian
Suites: $(. /etc/os-release && echo "$VERSION_CODENAME")
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF
        apt-get update -q
        apt-get install -y -q docker-ce docker-ce-cli containerd.io docker-compose-plugin
    '
}

push_stack() {
    log "Copying the stack into the container"
    pct exec "$CT_ID" -- bash -euc '
        install -d -m 0755 /opt/arr /opt/arr/config
        # Seerr and Recyclarr run as uid 1000 and do not fix their own folders.
        install -d -m 0755 -o 1000 -g 1000 /opt/arr/config/seerr /opt/arr/config/recyclarr
    '
    pct push "$CT_ID" "$STACK_DIR/compose.yaml" /opt/arr/compose.yaml --perms 0644
    pct push "$CT_ID" "$STACK_DIR/recyclarr.yml" /opt/arr/config/recyclarr/recyclarr.yml --perms 0644 --user 1000 --group 1000
    pct push "$CT_ID" "$STACK_DIR/homelab-arr" /usr/local/bin/homelab-arr --perms 0755

    local env_file
    env_file="$(mktemp)"
    trap 'rm -f "$env_file"; trap - RETURN' RETURN

    cat >"$env_file" <<EOF
TZ=$(timedatectl show -p Timezone --value)
DATA_DIR=/data
WIREGUARD_PRIVATE_KEY=$WIREGUARD_PRIVATE_KEY
VPN_COUNTRIES=$VPN_COUNTRIES
ARR_USERNAME=$ARR_USERNAME
RADARR_API_KEY=$(openssl rand -hex 16)
SONARR_API_KEY=$(openssl rand -hex 16)
PROWLARR_API_KEY=$(openssl rand -hex 16)
SUBTITLE_LANGUAGES=$SUBTITLE_LANGUAGES
MOVIES_FOLDER=$MOVIES_FOLDER
SERIES_FOLDER=$SERIES_FOLDER
JELLYFIN_URL=$JELLYFIN_URL
JELLYFIN_API_KEY=$JELLYFIN_API_KEY
EOF
    pct push "$CT_ID" "$env_file" /opt/arr/.env --perms 0600
}

start_stack() {
    log "Starting the stack (the first image download takes a few minutes)"
    pct exec "$CT_ID" -- docker compose --project-directory /opt/arr up -d --quiet-pull

    log "Waiting for the VPN"
    for _ in $(seq 1 60); do
        if [[ "$(pct exec "$CT_ID" -- docker inspect -f '{{.State.Health.Status}}' gluetun)" == "healthy" ]]; then
            return
        fi
        sleep 3
    done

    pct exec "$CT_ID" -- docker logs --tail 20 gluetun >&2
    die "the VPN did not connect, check the WireGuard key and countries in /opt/arr/.env inside container $CT_ID"
}

share_media_with_jellyfin() {
    [[ -n "$JELLYFIN_CTID" ]] || return 0

    if pct config "$JELLYFIN_CTID" | grep -q "^mp[0-9]*: $MEDIA_MOUNT,"; then
        return 0
    fi

    local index=0
    while pct config "$JELLYFIN_CTID" | grep -q "^mp$index:"; do
        index=$((index + 1))
    done

    log "Sharing the media folder with Jellyfin (container $JELLYFIN_CTID) as /data/media"
    pct set "$JELLYFIN_CTID" "--mp$index" "$MEDIA_MOUNT,mp=/data/media,ro=1"

    if [[ -z "$ANSWERS" ]]; then
        ask RESTART_JELLYFIN "Jellyfin must restart to see the folder. Restart it now? (y/n)" "y"
    fi
    if [[ "$RESTART_JELLYFIN" == "y" ]]; then
        pct reboot "$JELLYFIN_CTID"
        for _ in $(seq 1 40); do
            curl -sf "$JELLYFIN_URL/System/Info/Public" >/dev/null && return 0
            sleep 3
        done
        die "Jellyfin did not come back after the restart"
    fi

    # Libraries need the folder, so skip them until Jellyfin has restarted.
    JELLYFIN_API_KEY=""
}

configure_stack() {
    log "Connecting the apps"
    printf '%s\n' "$ARR_PASSWORD" | pct exec "$CT_ID" -- homelab-arr configure
}

main() {
    trap on_exit EXIT
    preflight
    ask_settings
    mount_nas
    download_template
    create_container
    install_docker
    share_media_with_jellyfin
    push_stack
    start_stack
    configure_stack

    local ip
    ip="$(container_ip "$CT_ID")"

    log "Done"
    # For the host agent, which shows the links on the Apps page.
    echo "HOMELAB app media $CT_ID $ip"
    cat <<EOF

  Requests (Seerr):  http://$ip:5055   <- finish its setup now, until then anyone on your network can
  Radarr (movies):   http://$ip:7878
  Sonarr (TV):       http://$ip:8989
  Prowlarr:          http://$ip:9696   <- add your indexers here, they sync to Radarr and Sonarr
  Bazarr:            http://$ip:6767
  qBittorrent:       http://$ip:8080

  Log in with '$ARR_USERNAME' and your password.
  Indexers behind Cloudflare: give them the 'flaresolverr' tag in Prowlarr.

EOF
}

main
