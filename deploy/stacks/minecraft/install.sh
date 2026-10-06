#!/usr/bin/env bash
# Installs a Minecraft Java server (Paper) in a new LXC on this Proxmox host,
# with playit.gg next to it when you give a secret key, so friends can join
# without open ports.
#
#   ./install.sh [--storage local-lvm] [--bridge vmbr0] [--hostname minecraft] [--ctid 150] [--answers file]
#   ./install.sh --update --ctid 150
#   ./install.sh --remove --ctid 150
#   ./install.sh --players --ctid 150 --answers file    (WHITELIST and OPS, names split by commas)
# With --answers, it asks nothing and reads the answers from a KEY=value file.
# The host agent uses this for the Apps page, and also --update, --remove and --players.
set -euo pipefail

STACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=deploy/lib.sh
source "$STACK_DIR/../../lib.sh"

STORAGE="local-lvm"
BRIDGE="vmbr0"
CT_HOSTNAME="minecraft"
CT_ID=""
ANSWERS=""
MODE="install"
MEMORY_GB=4
MOTD="A Homelab Minecraft server"
DIFFICULTY="normal"
GAME_MODE="survival"
MAX_PLAYERS=10
VIEW_DISTANCE=10
SEED=""
OPERATOR=""
WHITELIST=""
PLAYIT_SECRET_KEY=""
PUBLIC_ADDRESS=""
WORLD_SIZE=20

NAME_LIST='^([A-Za-z0-9_]{3,16}(,[A-Za-z0-9_]{3,16})*)?$'

while [[ $# -gt 0 ]]; do
    case "$1" in
        --storage) STORAGE="$2"; shift 2 ;;
        --bridge) BRIDGE="$2"; shift 2 ;;
        --hostname) CT_HOSTNAME="$2"; shift 2 ;;
        --ctid) CT_ID="$2"; shift 2 ;;
        --answers) ANSWERS="$2"; shift 2 ;;
        --update) MODE="update"; shift ;;
        --remove) MODE="remove"; shift ;;
        --players) MODE="players"; shift ;;
        *) die "unknown option: $1" ;;
    esac
done

preflight() {
    require_proxmox
    require_docker_lxc
    [[ -f "$STACK_DIR/compose.yaml" ]] || die "missing compose.yaml next to install.sh"
    container_exists "$CT_HOSTNAME" && die "a container named '$CT_HOSTNAME' already exists"
}

# load_answers reads KEY=value lines. Values are only stored, never run.
load_answers() {
    local line key value
    while IFS= read -r line || [[ -n "$line" ]]; do
        [[ -z "$line" ]] && continue
        key="${line%%=*}" value="${line#*=}"
        case "$key" in
            MEMORY_GB | MOTD | DIFFICULTY | MAX_PLAYERS | VIEW_DISTANCE | SEED | OPERATOR | WHITELIST | \
                PLAYIT_SECRET_KEY | PUBLIC_ADDRESS | STORAGE | WORLD_SIZE)
                printf -v "$key" '%s' "$value"
                ;;
            # MODE is taken by this script.
            MODE) GAME_MODE="$value" ;;
            *) die "unknown answer: $key" ;;
        esac
    done <"$ANSWERS"
    # It holds the playit.gg secret key.
    rm -f "$ANSWERS"
}

ask_settings() {
    if [[ -n "$ANSWERS" ]]; then
        load_answers
    else
        log "A few questions first"
        echo "The server is Paper, a fast version of the Minecraft Java server."
        echo "To run it, you agree to the Minecraft EULA: https://aka.ms/MinecraftEULA"
        local eula
        ask eula "Do you agree to the Minecraft EULA? (y/n)" "n"
        [[ "$eula" == "y" ]] || die "the server can't run without the EULA"
        ask OPERATOR "Your Minecraft Java name (you become the operator)"
        ask WHITELIST "Names of your friends, split by commas (empty for none)"
        ask MEMORY_GB "Memory in GB (2, 4, 6 or 8)" "$MEMORY_GB"
        echo "For friends outside your network, make a Docker agent on playit.gg and paste its secret key."
        ask PLAYIT_SECRET_KEY "playit.gg secret key (empty to skip)" "" secret
    fi

    [[ "$OPERATOR" =~ ^[A-Za-z0-9_]{3,16}$ ]] || die "'$OPERATOR' is not a Minecraft name"
    [[ "$WHITELIST" =~ $NAME_LIST ]] || die "the whitelist must be Minecraft names, split by commas"
    [[ "$MEMORY_GB" =~ ^(2|4|6|8)$ ]] || die "the memory must be 2, 4, 6 or 8 GB"
    [[ "$WORLD_SIZE" =~ ^[0-9]+$ ]] || die "the world disk must be a size in GB"
    # The MOTD goes between single quotes in .env.
    [[ "$MOTD" != *"'"* ]] || die "the message of the day can't have an apostrophe"

    # The operator is on the whitelist too.
    if [[ ",$WHITELIST," != *",$OPERATOR,"* ]]; then
        WHITELIST="$OPERATOR${WHITELIST:+,$WHITELIST}"
    fi
}

create_container() {
    CT_ID="${CT_ID:-$(pvesh get /cluster/nextid)}"
    # The JVM needs about 25% more than its heap, plus room for Debian and Docker.
    local memory=$((MEMORY_GB * 1280 + 512))
    log "Creating container $CT_ID ($CT_HOSTNAME) with $((memory / 1024)) GB memory"

    pct create "$CT_ID" "$TEMPLATE_STORAGE:vztmpl/$TEMPLATE" \
        --hostname "$CT_HOSTNAME" \
        --unprivileged 1 \
        --features nesting=1,keyctl=1 \
        --cores 4 --memory "$memory" --swap 1024 \
        --rootfs "$STORAGE:16" \
        --net0 "name=eth0,bridge=$BRIDGE,ip=dhcp" \
        --onboot 1 \
        --mp0 "$STORAGE:$WORLD_SIZE,mp=/opt/minecraft/data,backup=1" \
        --tags "homelab;minecraft" \
        --description "$(description)"

    CREATED_CT="$CT_ID"
    pct start "$CT_ID"
    wait_for_network "$CT_ID"
}

# description is the description of the container. The Apps page shows the
# public address from it.
description() {
    app_description "Homelab Minecraft server (Paper)"
    if [[ -n "$PUBLIC_ADDRESS" ]]; then
        printf 'homelab-address: %s\n' "$PUBLIC_ADDRESS"
    fi
}

# on_exit removes the container that a failed install from the Apps page
# made, so the next try can start clean.
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
}

push_stack() {
    log "Copying the stack into the container"
    # The server runs as uid 1000 and writes the world here.
    pct exec "$CT_ID" -- bash -euc 'install -d -m 0755 /opt/minecraft && chown 1000:1000 /opt/minecraft/data'
    pct push "$CT_ID" "$STACK_DIR/compose.yaml" /opt/minecraft/compose.yaml --perms 0644

    local env_file profiles=""
    env_file="$(mktemp)"
    trap 'rm -f "$env_file"; trap - RETURN' RETURN

    [[ -n "$PLAYIT_SECRET_KEY" ]] && profiles="playit"
    cat >"$env_file" <<EOF
COMPOSE_PROFILES=$profiles
TZ=$(timedatectl show -p Timezone --value)
MEMORY=${MEMORY_GB}G
MOTD='$MOTD'
DIFFICULTY=$DIFFICULTY
GAME_MODE=$GAME_MODE
MAX_PLAYERS=$MAX_PLAYERS
VIEW_DISTANCE=$VIEW_DISTANCE
SEED=$SEED
WHITELIST=$WHITELIST
OPS=$OPERATOR
RCON_PASSWORD=$(openssl rand -hex 24)
PLAYIT_SECRET_KEY=$PLAYIT_SECRET_KEY
PUBLIC_ADDRESS=$PUBLIC_ADDRESS
EOF
    pct push "$CT_ID" "$env_file" /opt/minecraft/.env --perms 0600
}

start_stack() {
    log "Starting the server (the first start downloads Paper and makes the world, this takes a few minutes)"
    pct exec "$CT_ID" -- docker compose --project-directory /opt/minecraft up -d --remove-orphans --quiet-pull
    wait_for_server
}

wait_for_server() {
    log "Waiting for the server"
    for _ in $(seq 1 120); do
        if [[ "$(pct exec "$CT_ID" -- docker inspect -f '{{.State.Health.Status}}' mc)" == "healthy" ]]; then
            return 0
        fi
        sleep 5
    done

    echo "Last lines of the server log:" >&2
    pct exec "$CT_ID" -- docker logs --tail 50 mc >&2 || true
    die "the server did not start, read its log above"
}

# update upgrades the packages, copies the compose file of this release, and
# starts the new images. The world and the players stay.
update() {
    require_proxmox
    [[ -f "$STACK_DIR/compose.yaml" ]] || die "missing compose.yaml next to install.sh"
    require_app_container "$CT_ID" minecraft
    require_running "$CT_ID"

    upgrade_packages "$CT_ID"

    log "Copying the stack of Homelab $HOMELAB_VERSION into the container"
    pct push "$CT_ID" "$STACK_DIR/compose.yaml" /opt/minecraft/compose.yaml --perms 0644

    log "Downloading the new images"
    pct exec "$CT_ID" -- docker compose --project-directory /opt/minecraft pull --quiet
    start_stack
    pct exec "$CT_ID" -- docker image prune -f >/dev/null

    PUBLIC_ADDRESS="$(pct exec "$CT_ID" -- sed -n 's/^PUBLIC_ADDRESS=//p' /opt/minecraft/.env)"
    pct set "$CT_ID" --description "$(description)"
    log "Done"
}

rcon() {
    pct exec "$CT_ID" -- docker exec mc rcon-cli "$@"
}

# names_in FILE prints the player names in whitelist.json or ops.json.
names_in() {
    pct exec "$CT_ID" -- grep -o '"name" *: *"[A-Za-z0-9_]*"' "/opt/minecraft/data/$1" | sed 's/.*"\([A-Za-z0-9_]*\)"$/\1/' || true
}

# change_players gives the server the whitelist and the operators from the
# answers file. The server applies them at once, without a restart.
change_players() {
    require_proxmox
    require_app_container "$CT_ID" minecraft
    require_running "$CT_ID"

    local line key value new_whitelist="" new_ops=""
    while IFS= read -r line || [[ -n "$line" ]]; do
        [[ -z "$line" ]] && continue
        key="${line%%=*}" value="${line#*=}"
        case "$key" in
            WHITELIST) new_whitelist="$value" ;;
            OPS) new_ops="$value" ;;
            *) die "unknown answer: $key" ;;
        esac
    done <"$ANSWERS"
    rm -f "$ANSWERS"
    [[ "$new_whitelist" =~ $NAME_LIST && "$new_ops" =~ $NAME_LIST ]] || die "the players must be Minecraft names, split by commas"

    [[ "$(pct exec "$CT_ID" -- docker inspect -f '{{.State.Health.Status}}' mc 2>/dev/null)" == "healthy" ]] ||
        die "the Minecraft server is not running, start it first"

    local -a whitelist ops current_whitelist current_ops
    IFS=, read -r -a whitelist <<<"$new_whitelist"
    IFS=, read -r -a ops <<<"$new_ops"
    mapfile -t current_whitelist < <(names_in whitelist.json)
    mapfile -t current_ops < <(names_in ops.json)

    local name
    for name in "${whitelist[@]}"; do
        contains "$name" "${current_whitelist[@]}" || { log "Adding $name to the whitelist"; rcon whitelist add "$name"; }
    done
    for name in "${current_whitelist[@]}"; do
        contains "$name" "${whitelist[@]}" || { log "Removing $name from the whitelist"; rcon whitelist remove "$name"; }
    done
    for name in "${ops[@]}"; do
        contains "$name" "${current_ops[@]}" || { log "Making $name an operator"; rcon op "$name"; }
    done
    for name in "${current_ops[@]}"; do
        contains "$name" "${ops[@]}" || { log "$name is no operator anymore"; rcon deop "$name"; }
    done

    # Minecraft only adds names that have a Java profile.
    mapfile -t current_whitelist < <(names_in whitelist.json)
    for name in "${whitelist[@]}"; do
        contains "$name" "${current_whitelist[@]}" ||
            die "Minecraft does not know the player $name. Check the Java profile name on minecraft.net"
    done
    log "Done"
}

# contains NAME LIST... is true when the list has the name, in any case.
contains() {
    local name="${1,,}" item
    shift
    for item in "$@"; do
        [[ "${item,,}" == "$name" ]] && return 0
    done

    return 1
}

main() {
    case "$MODE" in
        players) change_players; return ;;
        update) update; return ;;
        remove) require_proxmox; remove_app "$CT_ID" minecraft; return ;;
    esac

    trap on_exit EXIT
    preflight
    ask_settings
    download_template
    create_container
    install_docker "$CT_ID"
    push_stack
    start_stack

    local ip
    ip="$(container_ip "$CT_ID")"

    log "Done"
    # For the host agent, which shows the addresses on the Apps page.
    echo "HOMELAB app minecraft $CT_ID $ip"
    cat <<EOF

  On your network:  $ip:25565
  For friends:      ${PUBLIC_ADDRESS:-add a Minecraft Java tunnel on playit.gg to localhost:25565}

  In Minecraft: Multiplayer > Add Server, and use one of the addresses above.

EOF
}

main
