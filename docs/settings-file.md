# Settings file

On the Settings page (account menu), download all settings as `homelab.yaml`. Keep it in git, or import it on a new server.

```yaml
version: 1
notifications:
  telegram:
    chatId: "123456789"
updates:
  enabled: true
  day: sun
  time: "04:00"
  exclude: [105]
backups:
  enabled: true
  days: [] # no days means every day
  time: "03:00"
  storage: local
  exclude: []
  keep: { daily: 7, weekly: 4, monthly: 3 }
health:
  checks:
    - { name: Jellyfin, type: http, target: "http://192.168.1.20:8096" }
    - { name: SSH, type: tcp, target: "192.168.1.2:22" }
tailscale:
  serve: true
  shareSubnet: true
  subnet: 192.168.1.0/24
jellyfin:
  url: http://192.168.1.20:8096
  theme: true
selfUpdate:
  repo: DriesH/homelab
  autoInstall: false
```

- Tokens, passwords and API keys are not in the file. Enter the Telegram bot token and the Jellyfin API key on their pages. Until then, an import skips those sections.
- A section that is not in the file stays as it is.
- An import first shows which sections change. Nothing is saved until you apply it.
- Unknown keys are an error, so a typo does not go unnoticed.
