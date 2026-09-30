#!/usr/bin/env bash
# Installs the homelab manager on a Proxmox VE host.
# Run it as root on the host, from the extracted release bundle:
#   ./install.sh [--storage local-lvm] [--bridge vmbr0] [--hostname homelab] [--ctid 120]
# The host agent runs "./install.sh --upgrade" to install a new release.
set -euo pipefail

BUNDLE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STORAGE="local-lvm"
BRIDGE="vmbr0"
CT_HOSTNAME="homelab"
CT_ID=""

# Fixed uid/gid of the "homelab" user inside the LXC. Unprivileged LXCs shift
# ids by 100000 on the host, which is the group that may use the agent socket.
SERVICE_ID=1990
HOST_SOCKET_GID=$((100000 + SERVICE_ID))
AGENT_SOCKET_DIR="/var/lib/homelab-agent/socket"

PVE_USER="homelab@pve"
PVE_ROLE="HomelabManager"
# VM.Backup and Datastore.AllocateSpace are for backups and restores. The backup
# schedule itself is set by the host agent, so the token needs no Sys.Modify.
PVE_PRIVS="Sys.Audit,VM.Audit,VM.PowerMgmt,VM.Snapshot,VM.Snapshot.Rollback,VM.Backup,Datastore.Audit,Datastore.AllocateSpace"
PVE_TOKEN="manager"

UPGRADE_DIR="/var/lib/homelab-agent/upgrade"
UPGRADE=""

# shellcheck source=deploy/lib.sh
source "$BUNDLE_DIR/lib.sh"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --storage) STORAGE="$2"; shift 2 ;;
        --bridge) BRIDGE="$2"; shift 2 ;;
        --hostname) CT_HOSTNAME="$2"; shift 2 ;;
        --ctid) CT_ID="$2"; shift 2 ;;
        --upgrade) UPGRADE=1; shift ;;
        *) die "unknown option: $1" ;;
    esac
done

preflight() {
    require_proxmox

    for file in VERSION homelab homelab-agent homelab.service homelab-agent.service; do
        [[ -f "$BUNDLE_DIR/$file" ]] || die "missing $file next to install.sh"
    done

    if container_exists "$CT_HOSTNAME"; then
        die "a container named '$CT_HOSTNAME' already exists"
    fi

    ip -4 -o addr show dev "$BRIDGE" >/dev/null 2>&1 || die "bridge $BRIDGE not found"
}

install_agent() {
    log "Installing host agent"
    install -d -m 0755 /var/lib/homelab-agent
    install -d -m 0750 -o root -g "$HOST_SOCKET_GID" "$AGENT_SOCKET_DIR"
    install -m 0755 "$BUNDLE_DIR/homelab-agent" /usr/local/bin/homelab-agent
    sed "s/@SOCKET_GID@/$HOST_SOCKET_GID/" "$BUNDLE_DIR/homelab-agent.service" >/etc/systemd/system/homelab-agent.service
    systemctl daemon-reload
    systemctl enable --now homelab-agent
}

# update_role gives the manager's role the privileges of this version.
update_role() {
    if pvesh get "/access/roles/$PVE_ROLE" >/dev/null 2>&1; then
        pvesh set "/access/roles/$PVE_ROLE" --privs "$PVE_PRIVS"
    else
        pvesh create /access/roles --roleid "$PVE_ROLE" --privs "$PVE_PRIVS"
    fi
}

create_api_token() {
    log "Creating Proxmox API user $PVE_USER"
    update_role

    if ! pvesh get "/access/users/$PVE_USER" >/dev/null 2>&1; then
        pvesh create /access/users --userid "$PVE_USER" --comment "Homelab manager"
    fi

    if pvesh get "/access/users/$PVE_USER/token/$PVE_TOKEN" >/dev/null 2>&1; then
        pvesh delete "/access/users/$PVE_USER/token/$PVE_TOKEN"
    fi

    pvesh set /access/acl --path / --users "$PVE_USER" --roles "$PVE_ROLE"

    TOKEN_SECRET="$(
        pvesh create "/access/users/$PVE_USER/token/$PVE_TOKEN" --privsep 0 --output-format json |
            perl -MJSON -0ne 'print decode_json($_)->{value}'
    )"
    [[ -n "$TOKEN_SECRET" ]] || die "could not create API token"
}

create_container() {
    CT_ID="${CT_ID:-$(pvesh get /cluster/nextid)}"
    log "Creating container $CT_ID ($CT_HOSTNAME)"

    pct create "$CT_ID" "$TEMPLATE_STORAGE:vztmpl/$TEMPLATE" \
        --hostname "$CT_HOSTNAME" \
        --unprivileged 1 \
        --features nesting=1 \
        --cores 1 --memory 512 --swap 256 \
        --rootfs "$STORAGE:4" \
        --net0 "name=eth0,bridge=$BRIDGE,ip=dhcp" \
        --onboot 1 \
        --dev0 /dev/net/tun \
        --mp0 "$AGENT_SOCKET_DIR,mp=/mnt/homelab-agent" \
        --tags homelab \
        --description "Homelab manager"

    pct start "$CT_ID"
    wait_for_network "$CT_ID"
}

setup_container() {
    log "Installing the manager in the container"
    pct exec "$CT_ID" -- bash -euc "
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -q
        apt-get install -y -q avahi-daemon ca-certificates
        groupadd --system --gid $SERVICE_ID homelab
        useradd --system --uid $SERVICE_ID --gid homelab --home-dir /var/lib/homelab --create-home --shell /usr/sbin/nologin homelab
        install -d -m 0750 -o root -g homelab /etc/homelab
    "

    pct push "$CT_ID" "$BUNDLE_DIR/homelab" /usr/local/bin/homelab --perms 0755
    pct push "$CT_ID" "$BUNDLE_DIR/homelab.service" /etc/systemd/system/homelab.service --perms 0644
    pct push "$CT_ID" /etc/pve/pve-root-ca.pem /etc/homelab/proxmox-ca.pem --perms 0644

    local host_ip env_file
    host_ip="$(ip -4 -o addr show dev "$BRIDGE" | awk '{ print $4 }' | cut -d/ -f1 | head -n1)"
    env_file="$(mktemp)"
    trap 'rm -f "$env_file"' RETURN

    cat >"$env_file" <<EOF
HOMELAB_HOSTNAME=$CT_HOSTNAME.local
HOMELAB_SELF_VMID=$CT_ID
TZ=$(timedatectl show -p Timezone --value)
PROXMOX_URL=https://$host_ip:8006
PROXMOX_TOKEN_ID=$PVE_USER!$PVE_TOKEN
PROXMOX_TOKEN_SECRET=$TOKEN_SECRET
PROXMOX_CA_FILE=/etc/homelab/proxmox-ca.pem
EOF
    pct push "$CT_ID" "$env_file" /etc/homelab/homelab.env --perms 0640 --group "$SERVICE_ID"
}

# ensure_tailscale installs Tailscale in the manager container and lets the
# manager's user run it. It is safe to run again.
ensure_tailscale() {
    log "Installing Tailscale in the container"
    pct exec "$CT_ID" -- bash -euc "
        export DEBIAN_FRONTEND=noninteractive
        if ! command -v tailscale >/dev/null; then
            apt-get update -q
            apt-get install -y -q curl
            curl -fsSL https://pkgs.tailscale.com/stable/debian/trixie.noarmor.gpg -o /usr/share/keyrings/tailscale-archive-keyring.gpg
            curl -fsSL https://pkgs.tailscale.com/stable/debian/trixie.tailscale-keyring.list -o /etc/apt/sources.list.d/tailscale.list
            apt-get update -q
            apt-get install -y -q tailscale
        fi
        # Needed to share the home network with the tailnet (subnet router).
        install -d /etc/sysctl.d
        printf 'net.ipv4.ip_forward = 1\nnet.ipv6.conf.all.forwarding = 1\n' >/etc/sysctl.d/99-tailscale.conf
        sysctl -q -p /etc/sysctl.d/99-tailscale.conf || echo 'Could not turn on IP forwarding, sharing the home network will not work'
        systemctl enable --now tailscaled
        tailscale set --operator=homelab
    "
}

create_admin() {
    log "Create your admin account"
    lxc-attach -n "$CT_ID" -- runuser -u homelab -- /usr/local/bin/homelab admin </dev/tty
    pct exec "$CT_ID" -- systemctl daemon-reload
    pct exec "$CT_ID" -- systemctl enable --now homelab
}

# find_manager_container prints the ID of the container that mounts the agent socket.
find_manager_container() {
    local id
    for id in $(pct list | awk 'NR > 1 { print $1 }'); do
        if pct config "$id" | grep -qE "^mp[0-9]+: $AGENT_SOCKET_DIR,"; then
            echo "$id"
            return 0
        fi
    done

    return 1
}

write_upgrade_status() {
    local state="$1" message="$2"
    printf '{"state":"%s","version":"%s","message":"%s","finishedAt":"%s"}\n' \
        "$state" "$VERSION" "$message" "$(date -Is)" >"$UPGRADE_DIR/status.json"
}

# The new version is healthy when both services stay up and the manager
# listens on port 443, three checks in a row.
upgrade_healthy() {
    local passed=0
    for _ in $(seq 1 30); do
        sleep 2
        if systemctl is-active --quiet homelab-agent &&
            pct exec "$CT_ID" -- systemctl is-active --quiet homelab &&
            pct exec "$CT_ID" -- bash -c 'exec 3<>/dev/tcp/127.0.0.1/443' 2>/dev/null &&
            [[ "$(pct exec "$CT_ID" -- /usr/local/bin/homelab version)" == "$VERSION" ]]; then
            passed=$((passed + 1))
            ((passed >= 3)) && return 0
        else
            passed=0
        fi
    done

    return 1
}

# replace_files installs the agent and the manager from a folder that has
# homelab, homelab-agent and both service files.
replace_files() {
    local from="$1"

    install -m 0755 "$from/homelab-agent" /usr/local/bin/homelab-agent.new
    mv -f /usr/local/bin/homelab-agent.new /usr/local/bin/homelab-agent
    sed "s/@SOCKET_GID@/$HOST_SOCKET_GID/" "$from/homelab-agent.service" >/etc/systemd/system/homelab-agent.service

    # Push next to the running binary and rename, because a running binary can't be overwritten.
    pct push "$CT_ID" "$from/homelab" /usr/local/bin/homelab.new --perms 0755
    pct exec "$CT_ID" -- mv -f /usr/local/bin/homelab.new /usr/local/bin/homelab
    pct push "$CT_ID" "$from/homelab.service" /etc/systemd/system/homelab.service --perms 0644

    systemctl daemon-reload
    pct exec "$CT_ID" -- systemctl daemon-reload
    pct exec "$CT_ID" -- systemctl restart homelab
    systemctl restart homelab-agent
}

upgrade() {
    VERSION="$(cat "$BUNDLE_DIR/VERSION")"
    UPGRADE_BACKUP="$UPGRADE_DIR/previous"
    UPGRADE_STEP="checks"
    trap on_upgrade_exit EXIT

    require_proxmox
    CT_ID="$(find_manager_container)" || die "could not find the manager container"
    [[ "$(pct status "$CT_ID")" == "status: running" ]] || die "container $CT_ID is not running"
    log "Upgrading to $VERSION (manager container $CT_ID)"

    log "Saving the current version"
    rm -rf "$UPGRADE_BACKUP"
    install -d -m 0700 "$UPGRADE_BACKUP"
    install -m 0755 /usr/local/bin/homelab-agent "$UPGRADE_BACKUP/homelab-agent"
    sed "s/--socket-gid $HOST_SOCKET_GID/--socket-gid @SOCKET_GID@/" /etc/systemd/system/homelab-agent.service >"$UPGRADE_BACKUP/homelab-agent.service"
    pct pull "$CT_ID" /usr/local/bin/homelab "$UPGRADE_BACKUP/homelab"
    pct pull "$CT_ID" /etc/systemd/system/homelab.service "$UPGRADE_BACKUP/homelab.service"

    log "Updating the privileges of the API token"
    update_role

    # Older installs have no Tailscale yet. Without it, only the Tailscale page doesn't work.
    ensure_tailscale || log "Could not install Tailscale, continuing without it"

    log "Installing the new version"
    UPGRADE_STEP="replaced"
    replace_files "$BUNDLE_DIR"

    log "Waiting for the new version to start"
    upgrade_healthy || die "the new version did not start"

    UPGRADE_STEP="done"
    write_upgrade_status succeeded ""
    log "Upgraded to $VERSION"
}

# on_upgrade_exit restores the previous version when the upgrade stops early.
on_upgrade_exit() {
    case "$UPGRADE_STEP" in
        done) ;;
        replaced)
            log "Upgrade failed, restoring the previous version"
            replace_files "$UPGRADE_BACKUP" || true
            write_upgrade_status failed "the new version did not work, so the previous version is back"
            ;;
        *) write_upgrade_status failed "the upgrade stopped before anything changed" ;;
    esac
}

main() {
    if [[ -n "$UPGRADE" ]]; then
        upgrade
        return
    fi

    preflight
    install_agent
    create_api_token
    download_template
    create_container
    setup_container
    ensure_tailscale
    create_admin

    local ct_ip
    ct_ip="$(container_ip "$CT_ID")"

    log "Done"
    cat <<EOF

  Open:      https://$CT_HOSTNAME.local  (or https://$ct_ip)
  Trust it:  download http://$CT_HOSTNAME.local/ca.crt and install it as a trusted root on each device.
  Tip:       give container $CT_ID a fixed IP (DHCP reservation) in your router.

EOF
}

main
