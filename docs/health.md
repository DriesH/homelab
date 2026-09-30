# Health

The Health page shows the SMART status of each disk, the ZFS pools, the Proxmox storage and the network shares (like the NAS media share). You can also add services to watch, by URL or by TCP port.

The manager sends a Telegram alert when:

- a disk fails SMART or is 90% worn out;
- a ZFS pool is not `ONLINE`;
- a storage or network share is 90% full;
- a network share is not mounted or does not answer;
- a service fails two checks in a row (services are checked every minute).

When the problem is fixed, you get a second message. Disks and shares are checked every 10 minutes, because each disk check runs SMART.

The host agent finds network shares in the systemd `.mount` units and `/etc/fstab` of the Proxmox host. It can't read the SMART status of the disks inside the NAS. Use the disk warnings of the NAS itself for that.
