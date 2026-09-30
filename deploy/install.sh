#!/usr/bin/env bash
# Installs the homelab manager on a Proxmox VE host.
# Run it as root on the host, from the extracted release bundle:
#   ./install.sh [--storage local-lvm] [--bridge vmbr0] [--hostname homelab] [--ctid 120]
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
PVE_PRIVS="Sys.Audit,VM.Audit,VM.PowerMgmt,VM.Snapshot,VM.Snapshot.Rollback,Datastore.Audit"
PVE_TOKEN="manager"

# shellcheck source=deploy/lib.sh
source "$BUNDLE_DIR/lib.sh"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --storage) STORAGE="$2"; shift 2 ;;
        --bridge) BRIDGE="$2"; shift 2 ;;
        --hostname) CT_HOSTNAME="$2"; shift 2 ;;
        --ctid) CT_ID="$2"; shift 2 ;;
        *) die "unknown option: $1" ;;
    esac
done

preflight() {
    require_proxmox

    for file in homelab homelab-agent homelab.service homelab-agent.service; do
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

create_api_token() {
    log "Creating Proxmox API user $PVE_USER"
    if pvesh get "/access/roles/$PVE_ROLE" >/dev/null 2>&1; then
        pvesh set "/access/roles/$PVE_ROLE" --privs "$PVE_PRIVS"
    else
        pvesh create /access/roles --roleid "$PVE_ROLE" --privs "$PVE_PRIVS"
    fi

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

create_admin() {
    log "Create your admin account"
    lxc-attach -n "$CT_ID" -- runuser -u homelab -- /usr/local/bin/homelab admin </dev/tty
    pct exec "$CT_ID" -- systemctl daemon-reload
    pct exec "$CT_ID" -- systemctl enable --now homelab
}

main() {
    preflight
    install_agent
    create_api_token
    download_template
    create_container
    setup_container
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
