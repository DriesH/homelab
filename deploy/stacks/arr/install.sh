#!/usr/bin/env bash
# Installs the media stack (Prowlarr, Radarr, Sonarr, Bazarr, qBittorrent,
# Seerr, Recyclarr, FlareSolverr) in a new LXC on this Proxmox host.
# qBittorrent, Prowlarr and FlareSolverr go through ProtonVPN via Gluetun.
#
#   ./install.sh [--storage local-lvm] [--bridge vmbr0] [--hostname media] [--ctid 130]
#                [--downloads-size 200] [--jellyfin-ctid 110] [--answers file]
#   ./install.sh --update --ctid 130
#   ./install.sh --remove --ctid 130
#   ./install.sh --vpn --ctid 130 --answers file    (VPN_COUNTRIES, and WIREGUARD_PRIVATE_KEY or empty to keep it)
# With --answers, it asks nothing and reads the answers from a KEY=value file.
# The host agent uses this for the Apps page, and also --update and --remove.
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
MODE="install"
RESTART_JELLYFIN="y"
NAS_SERVER=""
NAS_EXPORT=""
MEDIA_FOLDER=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --storage) STORAGE="$2"; shift 2 ;;
        --bridge) BRIDGE="$2"; shift 2 ;;
        --hostname) CT_HOSTNAME="$2"; shift 2 ;;
        --ctid) CT_ID="$2"; shift 2 ;;
        --downloads-size) DOWNLOADS_SIZE="$2"; shift 2 ;;
        --jellyfin-ctid) JELLYFIN_CTID="$2"; shift 2 ;;
        --answers) ANSWERS="$2"; shift 2 ;;
        --update) MODE="update"; shift ;;
        --remove) MODE="remove"; shift ;;
        --vpn) MODE="vpn"; shift ;;
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
    JELLYFIN_API_KEY="" JELLYFIN_ADMIN_USERNAME="" JELLYFIN_ADMIN_PASSWORD=""
    OPENSUBTITLES_USERNAME="" OPENSUBTITLES_PASSWORD=""
    while IFS= read -r line || [[ -n "$line" ]]; do
        [[ -z "$line" ]] && continue
        key="${line%%=*}" value="${line#*=}"
        case "$key" in
            NAS_SERVER | NAS_EXPORT | MEDIA_FOLDER | MOVIES_FOLDER | SERIES_FOLDER | WIREGUARD_PRIVATE_KEY | VPN_COUNTRIES | \
                SUBTITLE_LANGUAGES | ARR_USERNAME | ARR_PASSWORD | JELLYFIN_API_KEY | JELLYFIN_ADMIN_USERNAME | \
                JELLYFIN_ADMIN_PASSWORD | OPENSUBTITLES_USERNAME | OPENSUBTITLES_PASSWORD | RESTART_JELLYFIN | \
                STORAGE | DOWNLOADS_SIZE)
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
        JELLYFIN_API_KEY="" JELLYFIN_ADMIN_USERNAME="" JELLYFIN_ADMIN_PASSWORD=""
    fi
}

ask_settings() {
    if [[ -n "$ANSWERS" ]]; then
        load_answers
        return
    fi

    log "A few questions first"

    ask_media_source
    echo "Movies and series each get a folder in it, and a library in Jellyfin."
    ask MOVIES_FOLDER "Folder for movies" "movies"
    ask SERIES_FOLDER "Folder for series" "series"
    check_folders

    echo "Get a WireGuard key at https://account.proton.me/u/0/vpn/WireGuard"
    echo "(turn on 'NAT-PMP (Port Forwarding)' when you create it)."
    ask WIREGUARD_PRIVATE_KEY "ProtonVPN WireGuard private key" "" secret
    ask VPN_COUNTRIES "VPN server countries (comma separated)" "Netherlands"

    ask SUBTITLE_LANGUAGES "Subtitle languages (2-letter codes, comma separated)" "en"
    echo "Optional: an OpenSubtitles.com account finds many more subtitles (free at opensubtitles.com)."
    ask OPENSUBTITLES_USERNAME "OpenSubtitles.com username (empty to skip)"
    OPENSUBTITLES_PASSWORD=""
    if [[ -n "$OPENSUBTITLES_USERNAME" ]]; then
        ask OPENSUBTITLES_PASSWORD "OpenSubtitles.com password" "" secret
    fi

    echo "One login for Radarr, Sonarr, Prowlarr, Bazarr and qBittorrent:"
    ask ARR_USERNAME "Username" "homelab"
    ask ARR_PASSWORD "Password (min 12 characters)" "" secret
    ((${#ARR_PASSWORD} >= 12)) || die "password must be at least 12 characters"

    JELLYFIN_URL=""
    JELLYFIN_API_KEY="" JELLYFIN_ADMIN_USERNAME="" JELLYFIN_ADMIN_PASSWORD=""
    if [[ -n "$JELLYFIN_CTID" ]]; then
        JELLYFIN_URL="http://$(container_ip "$JELLYFIN_CTID"):8096"
        echo "Found Jellyfin in container $JELLYFIN_CTID ($JELLYFIN_URL)."
        echo "Create an API key in Jellyfin: Dashboard > API Keys > +. Leave empty to skip."
        ask JELLYFIN_API_KEY "Jellyfin API key" "" secret
        echo "With a Jellyfin admin, the installer also sets up Seerr. You then log in to Seerr with that account."
        ask JELLYFIN_ADMIN_USERNAME "Jellyfin admin username (empty to skip)"
        if [[ -n "$JELLYFIN_ADMIN_USERNAME" ]]; then
            ask JELLYFIN_ADMIN_PASSWORD "Jellyfin admin password" "" secret
        fi
    fi
}

mount_nas() {
    mount_media_share
    check_writable
}

# check_writable makes sure that the containers can write where the apps
# write: in the movies and series folders, or in the share when a folder
# still has to be made.
check_writable() {
    local folder dir
    for folder in "$MOVIES_FOLDER" "$SERIES_FOLDER"; do
        dir="$MEDIA_MOUNT/$folder"
        [[ -d "$dir" ]] || dir="$MEDIA_MOUNT"
        if ! setpriv --reuid="$HOST_CONTAINER_UID" --regid="$HOST_CONTAINER_UID" --clear-groups touch "$dir/.homelab-write-test" 2>/dev/null; then
            if media_is_local; then
                dir="$(media_source)${dir#"$MEDIA_MOUNT"}"
                die "containers can't write to $dir. Give them that folder with: chown -R $HOST_CONTAINER_UID:$HOST_CONTAINER_UID '$dir'"
            fi
            die "containers can't write to ${dir#"$MEDIA_MOUNT"/}. On the NAS, give the NFS user write access to that folder: set squash to map all users to one NAS user with read/write access, and make sure that the folder itself allows writing"
        fi
        rm -f "$dir/.homelab-write-test"
    done
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
        --description "$(app_description "Homelab media stack")"

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
        remove_media_mount
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
    push_stack_files

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
JELLYFIN_ADMIN_USERNAME=$JELLYFIN_ADMIN_USERNAME
OPENSUBTITLES_USERNAME=$OPENSUBTITLES_USERNAME
EOF
    pct push "$CT_ID" "$env_file" /opt/arr/.env --perms 0600
}

# push_stack_files copies the files of this release into the container.
push_stack_files() {
    pct push "$CT_ID" "$STACK_DIR/compose.yaml" /opt/arr/compose.yaml --perms 0644
    pct push "$CT_ID" "$STACK_DIR/recyclarr.yml" /opt/arr/config/recyclarr/recyclarr.yml --perms 0644 --user 1000 --group 1000
    pct push "$CT_ID" "$STACK_DIR/homelab-arr" /usr/local/bin/homelab-arr --perms 0755
}

start_stack() {
    log "Starting the stack (the first image download takes a few minutes)"
    # qBittorrent and Prowlarr wait for a healthy VPN, so a VPN problem can already fail here.
    if ! pct exec "$CT_ID" -- docker compose --project-directory /opt/arr up -d --remove-orphans --quiet-pull; then
        vpn_failed
    fi
    wait_for_vpn
}

wait_for_vpn() {
    vpn_healthy || vpn_failed
}

vpn_healthy() {
    log "Waiting for the VPN"
    for _ in $(seq 1 60); do
        if [[ "$(pct exec "$CT_ID" -- docker inspect -f '{{.State.Health.Status}}' gluetun)" == "healthy" ]]; then
            return 0
        fi
        sleep 3
    done

    return 1
}

show_vpn_log() {
    echo "Last lines of the VPN log (gluetun):" >&2
    pct exec "$CT_ID" -- docker logs --tail 50 gluetun >&2 || true
}

# vpn_failed shows the end of the VPN log, because the container may be removed after this.
vpn_failed() {
    show_vpn_log
    die "the VPN did not connect. Check the WireGuard key and the server countries, and read the VPN log above"
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
    # The full path, because pct sets PATH to /sbin:/bin:/usr/sbin:/usr/bin inside the container.
    # The passwords go on stdin, so they never end up in a file in the container.
    printf '%s\n%s\n%s\n' "$ARR_PASSWORD" "$JELLYFIN_ADMIN_PASSWORD" "$OPENSUBTITLES_PASSWORD" |
        pct exec "$CT_ID" -- /usr/local/bin/homelab-arr configure
}

# update upgrades the packages, copies the stack of this release, and starts
# the new images. The settings in the apps stay: they need no passwords.
update() {
    require_proxmox
    for file in compose.yaml recyclarr.yml homelab-arr; do
        [[ -f "$STACK_DIR/$file" ]] || die "missing $file next to install.sh"
    done
    require_app_container "$CT_ID" media
    require_running "$CT_ID"

    upgrade_packages "$CT_ID"

    log "Copying the stack of Homelab $HOMELAB_VERSION into the container"
    push_stack_files

    log "Downloading the new images"
    pct exec "$CT_ID" -- docker compose --project-directory /opt/arr pull --quiet
    start_stack
    pct exec "$CT_ID" -- docker image prune -f >/dev/null

    pct set "$CT_ID" --description "$(app_description "Homelab media stack")"
    log "Done"
}

# restart_vpn recreates Gluetun and the apps that share its network
# (see compose.yaml), so they all use the new VPN connection.
restart_vpn() {
    log "Restarting the VPN and the apps behind it"
    pct exec "$CT_ID" -- docker compose --project-directory /opt/arr up -d --force-recreate \
        gluetun qbittorrent prowlarr flaresolverr && vpn_healthy
}

# change_vpn writes the new VPN settings in .env. When the VPN does not
# connect with them, it puts the old .env back.
change_vpn() {
    require_proxmox
    require_app_container "$CT_ID" media
    require_running "$CT_ID"

    local line key value countries="" private_key=""
    while IFS= read -r line || [[ -n "$line" ]]; do
        [[ -z "$line" ]] && continue
        key="${line%%=*}" value="${line#*=}"
        case "$key" in
            VPN_COUNTRIES) countries="$value" ;;
            WIREGUARD_PRIVATE_KEY) private_key="$value" ;;
            *) die "unknown answer: $key" ;;
        esac
    done <"$ANSWERS"
    # It holds the VPN key.
    rm -f "$ANSWERS"
    [[ -n "$countries" ]] || die "give the VPN countries"

    log "Saving the new VPN settings"
    pct exec "$CT_ID" -- cp -p /opt/arr/.env /opt/arr/.env.before-vpn
    # The values go on stdin, so the key is not in the process list.
    # shellcheck disable=SC2016 # expands inside the container
    printf '%s\n%s\n' "$countries" "$private_key" | pct exec "$CT_ID" -- bash -euc '
        read -r countries
        read -r key
        temp="$(mktemp /opt/arr/.env.XXXXXX)"
        while IFS= read -r line; do
            case "$line" in
                VPN_COUNTRIES=*) printf "VPN_COUNTRIES=%s\n" "$countries" ;;
                WIREGUARD_PRIVATE_KEY=*) [[ -n "$key" ]] && printf "WIREGUARD_PRIVATE_KEY=%s\n" "$key" || printf "%s\n" "$line" ;;
                *) printf "%s\n" "$line" ;;
            esac
        done </opt/arr/.env >"$temp"
        chmod 0600 "$temp"
        mv -f "$temp" /opt/arr/.env
    '

    if restart_vpn; then
        pct exec "$CT_ID" -- rm -f /opt/arr/.env.before-vpn
        log "Done"
        return 0
    fi

    show_vpn_log
    log "The VPN did not connect, going back to the old settings"
    pct exec "$CT_ID" -- mv -f /opt/arr/.env.before-vpn /opt/arr/.env
    restart_vpn || log "The VPN does not connect with the old settings either"
    die "the VPN did not connect with the new settings, so the old settings are back. Check the WireGuard key and the countries, and read the VPN log above"
}

main() {
    case "$MODE" in
        vpn) change_vpn; return ;;
        update) update; return ;;
        remove) require_proxmox; remove_app "$CT_ID" media; return ;;
    esac

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
