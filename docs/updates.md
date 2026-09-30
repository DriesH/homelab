# Updates

## Containers and the host

The Updates page checks the Proxmox host and all running containers for updates. It uses apt, the `update` command of community-scripts.org containers, and Docker Compose images.

- **Containers**: once a week (Sunday 04:00 by default), each container with auto-update on gets a snapshot and then its updates. If the update fails, or the container stops, the manager rolls it back to the snapshot. The last 3 snapshots are kept.
- **Proxmox host**: the schedule only checks it, because the host has no snapshots. Install host updates from the dashboard. The page tells you when a new kernel needs a reboot.
- **The manager itself** is never updated by the schedule, and a failed update of it is not rolled back automatically.

## Telegram

Optional. Create a bot with @BotFather, send it a message, and get your chat ID from @userinfobot. Then enter both on the Updates page. Health alerts, backup failures and console logins use the same bot.

## Updating Homelab

The manager checks the GitHub releases every 6 hours. When there is a new version, the header shows "Update available" and you get a Telegram message. Install it from the Updates page. You can also turn on automatic installs there.

The host agent only installs a bundle that is signed with the release key. It keeps the previous version, and brings it back when the new version does not start within a minute.

For a private repo, create a fine-grained token with read-only access to Contents of this repo only, and enter it on the Updates page.
