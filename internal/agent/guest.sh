#!/bin/sh
# Runs inside an LXC through `pct exec`. Usage: guest.sh check|update
# Lines that start with "HOMELAB " are results for the agent. Everything else is log.
set -eu

mode="${1:-}"
# On the Proxmox host itself we only touch apt.
apt_only="${HOMELAB_APT_ONLY:-0}"
export DEBIAN_FRONTEND=noninteractive

has() {
    command -v "$1" >/dev/null 2>&1
}

apt_check() {
    apt-get update -q
    # "name/suite new arch [upgradable from: old]" -> "HOMELAB package name old new"
    apt list --upgradable 2>/dev/null |
        sed -n 's#^\([^/]*\)/[^ ]* \([^ ]*\) [^ ]* \[upgradable from: \([^]]*\)\]$#HOMELAB package \1 \3 \2#p'
}

apt_update() {
    apt-get update -q
    apt-get -y -q -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold dist-upgrade
}

# Prints "project|file1,file2" for each running Compose project.
compose_projects() {
    docker ps --format '{{.Label "com.docker.compose.project"}}|{{.Label "com.docker.compose.project.config_files"}}' |
        grep -v '^|' | sort -u || true
}

compose() {
    project="$1"
    files="$2"
    shift 2

    set -- -p "$project" "$@"
    for file in $(echo "$files" | tr ',' ' '); do
        set -- -f "$file" "$@"
    done

    docker compose "$@"
}

# The loops read from a here-document, not a pipe, so a failing command stops
# the whole script instead of only a subshell.
docker_check() {
    projects="$(compose_projects)"
    while IFS='|' read -r project files; do
        [ -n "$project" ] || continue
        compose "$project" "$files" pull -q --ignore-buildable
        for container in $(compose "$project" "$files" ps -q); do
            image="$(docker inspect -f '{{.Config.Image}}' "$container")"
            running="$(docker inspect -f '{{.Image}}' "$container")"
            latest="$(docker image inspect -f '{{.Id}}' "$image")"
            if [ "$running" != "$latest" ]; then
                service="$(docker inspect -f '{{index .Config.Labels "com.docker.compose.service"}}' "$container")"
                echo "HOMELAB image $service $image"
            fi
        done
    done <<EOF
$projects
EOF
}

docker_update() {
    projects="$(compose_projects)"
    while IFS='|' read -r project files; do
        [ -n "$project" ] || continue
        compose "$project" "$files" pull -q --ignore-buildable
        compose "$project" "$files" up -d
    done <<EOF
$projects
EOF
    docker image prune -f
}

# Containers from community-scripts.org have their own updater.
community_script() {
    [ -x /usr/bin/update ] && grep -q community-scripts /usr/bin/update
}

case "$mode" in
    check)
        if has apt-get; then apt_check; else echo "HOMELAB unsupported no apt-get"; fi
        if [ "$apt_only" != 1 ] && has docker; then docker_check; fi
        ;;
    update)
        if [ "$apt_only" != 1 ] && community_script; then PHS_SILENT=1 /usr/bin/update; fi
        if has apt-get; then apt_update; else echo "HOMELAB unsupported no apt-get"; fi
        if [ "$apt_only" != 1 ] && has docker; then docker_update; fi
        ;;
    *)
        echo "usage: guest.sh check|update" >&2
        exit 2
        ;;
esac

echo "HOMELAB done"
