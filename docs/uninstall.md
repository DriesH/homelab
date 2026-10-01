# Uninstall

Run this as root on the Proxmox host:

```bash
homelab-uninstall
```

It removes the host agent, the Proxmox user, the API token and the role. It asks before it deletes:

- the manager container (yes by default). Tailscale logs out first. If you keep the container, only its mount of the agent is removed, so it still starts.
- the snapshots that Homelab made before updates (yes by default)
- the backup job (no by default, because it also works without Homelab)

Nothing changes until you answer "yes" to the last question. The backup files, the apps (Jellyfin and the media stack) and the media mount always stay.

Installs from before v0.2.0 have no `homelab-uninstall` yet. Update Homelab first, or run `./install.sh --uninstall` from the release bundle.
