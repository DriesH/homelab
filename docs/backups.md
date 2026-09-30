# Backups

The Backups page makes a normal Proxmox backup job (`homelab-backup`), so it also runs while Homelab is down, and you can see it in the Proxmox UI.

- Choose the days, the time, the storage and how many daily, weekly and monthly backups to keep. By default, every guest is backed up at 03:00 on `local`.
- New containers and VMs are included automatically. Turn one off with its switch.
- Back up one guest now, restore a backup or delete it. A restore shuts the guest down, restores it to the storage of its current disk, and starts it again.
- Homelab can't restore its own container. Use `homelab-restore` on the host (see below).
- Failed scheduled backups are sent to Telegram.

The host agent creates the job with checked settings, so the API token needs no `Sys.Modify`. The token only has `VM.Backup` and `Datastore.AllocateSpace` for backups.

## Restoring the manager

The backup job also backs up the manager container. To restore it, run this as root on the Proxmox host:

```bash
homelab-restore                                                          # choose from the backups of the manager
homelab-restore nas:backup/vzdump-lxc-120-2026_09_29-03_00_02.tar.zst   # or give one
```

It stops the manager, restores the backup to the storage of its current disk, and waits until the manager runs again.

On a new Proxmox host, extract the release bundle and run `./install.sh --restore <backup>` with a backup on the NAS. It also installs the host agent and makes a new API token, because the old token only works on the old host.

## Data backup

The Settings page can also download an encrypted copy of the data of Homelab: the account, tokens, settings, history and the certificate authority. Use it when the Proxmox backups are gone. You need your password and a passphrase of at least 12 characters. Keep the passphrase: without it, nobody can open the file.

To restore it, install Homelab, log in, and upload the file on the Settings page. Homelab restarts with the data from the file, and you log in with the account from the backup. The file uses AES-256-GCM with a key from Argon2id.
