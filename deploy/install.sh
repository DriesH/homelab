#!/usr/bin/env bash
# Installs the homelab manager on a Proxmox VE host.
# Run it as root on the host, from the extracted release bundle:
#   ./install.sh [--storage local-lvm] [--bridge vmbr0] [--hostname homelab] [--ctid 120]
# The host agent runs "./install.sh --upgrade" to install a new release.
# "homelab-restore [backup]" restores the manager container from a Proxmox backup.
# "homelab-uninstall" removes Homelab from the host.
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
RESTORE=""
RESTORE_VOLID=""
UNINSTALL=""
BACKUP_JOB="homelab-backup"

# The installer stays on the host, for restores.
SCRIPTS_DIR="/usr/local/lib/homelab"

# shellcheck source=deploy/lib.sh
source "$BUNDLE_DIR/lib.sh"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --storage) STORAGE="$2"; shift 2 ;;
        --bridge) BRIDGE="$2"; shift 2 ;;
        --hostname) CT_HOSTNAME="$2"; shift 2 ;;
        --ctid) CT_ID="$2"; shift 2 ;;
        --upgrade) UPGRADE=1; shift ;;
        --uninstall) UNINSTALL=1; shift ;;
        --restore)
            RESTORE=1
            if [[ $# -gt 1 && "$2" != --* ]]; then
                RESTORE_VOLID="$2"
                shift
            fi
            shift
            ;;
        *) die "unknown option: $1" ;;
    esac
done

preflight() {
    require_proxmox

    for file in VERSION homelab homelab-agent homelab.service homelab-agent.service homelab-mdns homelab-mdns.service; do
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
    trap 'rm -f "$env_file"; trap - RETURN' RETURN

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

# install_scripts keeps the installer on the host and adds the homelab-restore command.
install_scripts() {
    install -d -m 0755 "$SCRIPTS_DIR"
    install -m 0755 "$BUNDLE_DIR/install.sh" "$SCRIPTS_DIR/install.sh.new"
    install -m 0644 "$BUNDLE_DIR/lib.sh" "$SCRIPTS_DIR/lib.sh"
    mv -f "$SCRIPTS_DIR/install.sh.new" "$SCRIPTS_DIR/install.sh"
    # The Apps page installs stacks from here.
    if [[ -d "$BUNDLE_DIR/stacks/arr" ]]; then
        install -d -m 0755 "$SCRIPTS_DIR/stacks/arr"
        install -m 0755 "$BUNDLE_DIR/stacks/arr/install.sh" "$BUNDLE_DIR/stacks/arr/homelab-arr" "$SCRIPTS_DIR/stacks/arr/"
        install -m 0644 "$BUNDLE_DIR/stacks/arr/compose.yaml" "$BUNDLE_DIR/stacks/arr/recyclarr.yml" "$SCRIPTS_DIR/stacks/arr/"
    fi
    local command
    for command in restore uninstall; do
        printf '#!/bin/sh\nexec %s/install.sh --%s "$@"\n' "$SCRIPTS_DIR" "$command" >"/usr/local/sbin/homelab-$command"
        chmod 0755 "/usr/local/sbin/homelab-$command"
    done
}

# ensure_app_names announces app names like seerr.homelab.local over mDNS,
# at the IP of the manager, which forwards them. It is safe to run again.
ensure_app_names() {
    log "Announcing the app names on the LAN"
    pct exec "$CT_ID" -- bash -euc '
        export DEBIAN_FRONTEND=noninteractive
        if ! command -v avahi-publish >/dev/null; then
            apt-get update -q
            apt-get install -y -q avahi-utils
        fi
    '
    pct push "$CT_ID" "$BUNDLE_DIR/homelab-mdns" /usr/local/bin/homelab-mdns --perms 0755
    pct push "$CT_ID" "$BUNDLE_DIR/homelab-mdns.service" /etc/systemd/system/homelab-mdns.service --perms 0644
    pct exec "$CT_ID" -- systemctl daemon-reload
    pct exec "$CT_ID" -- systemctl enable homelab-mdns
    pct exec "$CT_ID" -- systemctl restart homelab-mdns
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

# manager_healthy is true when both services stay up and the manager listens
# on port 443, three checks in a row. With a version, the manager must run it.
manager_healthy() {
    local version="${1:-}" passed=0
    for _ in $(seq 1 30); do
        sleep 2
        if systemctl is-active --quiet homelab-agent &&
            pct exec "$CT_ID" -- systemctl is-active --quiet homelab &&
            pct exec "$CT_ID" -- bash -c 'exec 3<>/dev/tcp/127.0.0.1/443' 2>/dev/null &&
            [[ -z "$version" || "$(pct exec "$CT_ID" -- /usr/local/bin/homelab version)" == "$version" ]]; then
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
    # Older installs have no app names yet. Without them, only seerr.homelab.local and the like don't work.
    ensure_app_names || log "Could not announce the app names, continuing without them"

    log "Installing the new version"
    UPGRADE_STEP="replaced"
    replace_files "$BUNDLE_DIR"

    log "Waiting for the new version to start"
    manager_healthy "$VERSION" || die "the new version did not start"

    UPGRADE_STEP="done"
    install_scripts || log "Could not update homelab-restore"
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

# backup_list prints the backups of a container on all backup storages, newest first.
backup_list() {
    local vmid="$1" storage
    for storage in $(pvesm status --content backup | awk 'NR > 1 && $3 == "active" { print $1 }'); do
        pvesm list "$storage" --content backup --vmid "$vmid" | awk 'NR > 1 { print $1 }'
    done | awk '{ name = $0; sub(/.*\//, "", name); print name, $0 }' | sort -r | cut -d' ' -f2
}

choose_backup() {
    local backups=() choice i
    mapfile -t backups < <(backup_list "$CT_ID")
    ((${#backups[@]} > 0)) || die "no backups of container $CT_ID found. Pass one, like: homelab-restore nas:backup/vzdump-lxc-$CT_ID-....tar.zst"

    echo "Backups of container $CT_ID, newest first:"
    for i in "${!backups[@]}"; do
        ((i < 10)) && printf '  %2d) %s\n' $((i + 1)) "${backups[$i]}"
    done
    ask choice "Restore which backup" 1
    if ! [[ "$choice" =~ ^[0-9]+$ ]] || ((choice < 1 || choice > ${#backups[@]} || choice > 10)); then
        die "no backup number $choice"
    fi
    RESTORE_VOLID="${backups[$((choice - 1))]}"
}

# update_container_env points the restored manager at this host. On a new
# host it also gets a new API token.
update_container_env() {
    local host_ip
    host_ip="$(ip -4 -o addr show dev "$BRIDGE" | awk '{ print $4 }' | cut -d/ -f1 | head -n1)"
    [[ -n "$host_ip" ]] || die "bridge $BRIDGE has no IPv4 address"

    local changes=(-e "s|^PROXMOX_URL=.*|PROXMOX_URL=https://$host_ip:8006|" -e "s|^HOMELAB_SELF_VMID=.*|HOMELAB_SELF_VMID=$CT_ID|")
    if [[ -n "${TOKEN_SECRET:-}" ]]; then
        changes+=(-e "s|^PROXMOX_TOKEN_SECRET=.*|PROXMOX_TOKEN_SECRET=$TOKEN_SECRET|")
    fi

    pct push "$CT_ID" /etc/pve/pve-root-ca.pem /etc/homelab/proxmox-ca.pem --perms 0644
    pct exec "$CT_ID" -- sed -i "${changes[@]}" /etc/homelab/homelab.env
    pct exec "$CT_ID" -- systemctl restart homelab
}

restore() {
    require_proxmox
    CT_ID="$(find_manager_container)" || CT_ID=""

    if [[ -z "$RESTORE_VOLID" ]]; then
        [[ -n "$CT_ID" ]] || die "could not find the manager container. Pass the backup, like: homelab-restore nas:backup/vzdump-lxc-120-....tar.zst"
        choose_backup
    fi

    local vmid
    vmid="$(sed -nE 's#^.*vzdump-lxc-([0-9]+)-.*$#\1#p' <<<"$RESTORE_VOLID")"
    [[ -n "$vmid" ]] || die "$RESTORE_VOLID is not a container backup"
    if [[ -n "$CT_ID" && "$vmid" != "$CT_ID" ]]; then
        die "$RESTORE_VOLID is a backup of container $vmid, but the manager is container $CT_ID"
    fi
    if [[ -z "$CT_ID" ]] && pct status "$vmid" >/dev/null 2>&1; then
        die "container $vmid exists and is not the manager, remove it first"
    fi

    local exists="$CT_ID" storage="$STORAGE"
    CT_ID="$vmid"
    if [[ -n "$exists" ]]; then
        # Keep the disk on the storage it uses now.
        storage="$(pct config "$CT_ID" | sed -nE 's/^rootfs: ([^:]+):.*/\1/p')"
    fi

    # Not "answer": ask has a local variable with that name.
    local confirm
    echo "This restores container $CT_ID from $RESTORE_VOLID on storage $storage."
    [[ -n "$exists" ]] && echo "Changes to the manager since this backup are lost."
    ask confirm "Continue? (yes/no)" no
    [[ "$confirm" == "yes" ]] || die "stopped, nothing changed"

    if [[ ! -x /usr/local/bin/homelab-agent ]]; then
        [[ -f "$BUNDLE_DIR/homelab-agent" ]] || die "the host agent is not installed. Run this from the release bundle."
        install_agent
    fi
    # A new host has no API token yet. The old token only works on the old host.
    if ! pvesh get "/access/users/$PVE_USER/token/$PVE_TOKEN" >/dev/null 2>&1; then
        create_api_token
    else
        update_role
    fi

    if [[ -n "$exists" && "$(pct status "$CT_ID")" == "status: running" ]]; then
        log "Stopping container $CT_ID"
        pct shutdown "$CT_ID" --timeout 60 || pct stop "$CT_ID"
    fi

    log "Restoring container $CT_ID"
    pct restore "$CT_ID" "$RESTORE_VOLID" --storage "$storage" --force 1
    pct start "$CT_ID"
    update_container_env

    log "Waiting for the manager to start"
    manager_healthy || die "the manager did not start after the restore, look at: pct exec $CT_ID -- journalctl -u homelab"

    [[ -f "$BUNDLE_DIR/homelab-agent" ]] && install_scripts
    log "Restored container $CT_ID from $RESTORE_VOLID"
    echo "The manager runs the version from the backup. The Updates page offers a newer version, if there is one."
}

# yes_no VAR "question" default. Only "yes" and "no" are answers.
yes_no() {
    local var="$1" question="$2" default="$3" reply
    while true; do
        ask reply "$question (yes/no)" "$default"
        if [[ "$reply" == "yes" || "$reply" == "no" ]]; then
            printf -v "$var" '%s' "$reply"
            return
        fi
    done
}

# homelab_snapshots prints "vmid name" for every snapshot that Homelab made before an update.
homelab_snapshots() {
    local id
    for id in $(pct list | awk 'NR > 1 { print $1 }'); do
        pct listsnapshot "$id" 2>/dev/null | grep -oE 'homelab_[0-9]{8}_[0-9]{6}' | sed "s/^/$id /"
    done
}

uninstall() {
    require_proxmox
    CT_ID="$(find_manager_container)" || CT_ID=""

    local snapshots=() has_job="" delete_ct="no" delete_snapshots="no" delete_job="no" go
    mapfile -t snapshots < <(homelab_snapshots)
    pvesh get "/cluster/backup/$BACKUP_JOB" >/dev/null 2>&1 && has_job=1

    echo "This removes Homelab from this host:"
    echo "  - the host agent (homelab-agent) and its files in /var/lib/homelab-agent"
    echo "  - the Proxmox user $PVE_USER, its API token and the role $PVE_ROLE"
    echo "  - the homelab-restore and homelab-uninstall commands"
    echo "Backup files, the media stack and the NAS mount stay."
    echo

    if [[ -n "$CT_ID" ]]; then
        yes_no delete_ct "Delete the manager container $CT_ID? Its backups stay" yes
    fi
    if ((${#snapshots[@]} > 0)); then
        yes_no delete_snapshots "Delete the ${#snapshots[@]} snapshots that Homelab made before updates?" yes
    fi
    if [[ -n "$has_job" ]]; then
        echo "The backup job '$BACKUP_JOB' also works without Homelab."
        yes_no delete_job "Delete the backup job?" no
    fi
    yes_no go "Uninstall now?" no
    [[ "$go" == "yes" ]] || die "stopped, nothing changed"

    if [[ -n "$CT_ID" && "$delete_ct" == "yes" ]]; then
        log "Deleting container $CT_ID"
        if [[ "$(pct status "$CT_ID")" == "status: running" ]]; then
            # Removes this device from the tailnet. Best effort: Tailscale may be off.
            pct exec "$CT_ID" -- tailscale logout >/dev/null 2>&1 || true
            pct stop "$CT_ID"
        fi
        pct destroy "$CT_ID" --purge 1
    elif [[ -n "$CT_ID" ]]; then
        # Without the agent folder, the container would not start anymore.
        local mount
        mount="$(pct config "$CT_ID" | sed -nE "s#^(mp[0-9]+): $AGENT_SOCKET_DIR,.*#\\1#p")"
        log "Keeping container $CT_ID, removing its $mount mount of the agent"
        pct set "$CT_ID" --delete "$mount"
    fi

    if [[ "$delete_snapshots" == "yes" ]]; then
        local snapshot
        for snapshot in "${snapshots[@]}"; do
            # The snapshots of a deleted container are already gone.
            pct status "${snapshot% *}" >/dev/null 2>&1 || continue
            log "Deleting snapshot ${snapshot#* } of container ${snapshot% *}"
            pct delsnapshot "${snapshot% *}" "${snapshot#* }" || log "Could not delete it, continuing"
        done
    fi

    if [[ "$delete_job" == "yes" ]]; then
        log "Deleting the backup job"
        pvesh delete "/cluster/backup/$BACKUP_JOB"
    fi

    log "Removing the host agent"
    systemctl disable --now homelab-agent 2>/dev/null || true
    rm -f /etc/systemd/system/homelab-agent.service /usr/local/bin/homelab-agent
    systemctl daemon-reload
    rm -rf /var/lib/homelab-agent

    log "Removing the Proxmox user and role"
    # Deleting the user also deletes its token and permissions.
    if pvesh get "/access/users/$PVE_USER" >/dev/null 2>&1; then
        pvesh delete "/access/users/$PVE_USER"
    fi
    if pvesh get "/access/roles/$PVE_ROLE" >/dev/null 2>&1; then
        pvesh delete "/access/roles/$PVE_ROLE"
    fi

    # Bash already has this script open, so it can delete it while it runs.
    rm -rf "$SCRIPTS_DIR" /usr/local/sbin/homelab-restore /usr/local/sbin/homelab-uninstall

    log "Homelab is uninstalled"
    cat <<EOF

  Still there, remove them yourself if you want:
  - the backup files on your storages (Proxmox UI > storage > Backups)
  - the device in the Tailscale admin console, if you used Tailscale
  - the Netflix theme in Jellyfin (Dashboard > General > Custom CSS)
  - the media stack container and the NAS mount, if you installed them

EOF
}

main() {
    if [[ -n "$UNINSTALL" ]]; then
        uninstall
        return
    fi
    if [[ -n "$UPGRADE" ]]; then
        upgrade
        return
    fi
    if [[ -n "$RESTORE" ]]; then
        restore
        return
    fi

    preflight
    install_agent
    create_api_token
    download_template
    create_container
    setup_container
    ensure_tailscale
    ensure_app_names
    create_admin
    install_scripts

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
