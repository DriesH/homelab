# Logs and console

## Logs

The Logs page shows:

- **Host**: the journal of the Proxmox host;
- **Container**: the journal inside a container;
- **Docker**: the logs of the Docker apps in a container, like Radarr and qBittorrent;
- **Tasks**: the Proxmox tasks, like backups and snapshots. Click a task to see its log.

Filter by level, by source and by text. Turn on "Follow" to refresh every 5 seconds. Docker has no log levels, so Homelab guesses them from words like `ERROR` and `[Warn]`.

The host agent reads the journals and Docker logs with fixed, read-only commands.

## Console

Open the console of a running container from its menu on the Overview. It is the same tty login as the Console tab in Proxmox. Containers from community-scripts.org log in as root automatically.

- The host agent runs `pct console <id>` and nothing else, so a console has the same power as the Proxmox `VM.Console` privilege.
- Only the Homelab page itself can open a console (the websocket checks the origin).
- Opening a console sends a Telegram message.
- The agent allows 4 consoles at the same time.
