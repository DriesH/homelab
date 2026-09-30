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
