# How it works

- **Manager**: a Go binary in its own LXC container. It serves the React app and talks to the Proxmox API with a limited API token.
- **Host agent**: a small Go service on the Proxmox host. It does what the API can't do, like `apt` upgrades, backup jobs, consoles and app installs. It only listens on a Unix socket that is shared with the manager container.
- **HTTPS**: the manager runs its own certificate authority. That CA can only sign certificates for `homelab.local`.
- **Login**: one admin account with a password and an authenticator app code (TOTP).
- **Data**: JSON files in `/var/lib/homelab` inside the manager container. There is no database.

## Permissions

The API token of the manager has the role `HomelabManager` with these privileges:

`Sys.Audit, VM.Audit, VM.PowerMgmt, VM.Snapshot, VM.Snapshot.Rollback, VM.Backup, Datastore.Audit, Datastore.AllocateSpace`

Everything that needs more, like the backup job or a console, goes through the host agent. The agent only runs fixed commands with checked input.
