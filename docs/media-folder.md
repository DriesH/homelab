# Media folder

Jellyfin and the media stack use one media folder for your movies and series. It is on a NAS, or on a disk of the Proxmox host. You choose it in the form of the first app that you install.

The installer mounts the media folder on the Proxmox host at `/mnt/homelab/media`. Both containers see it as `/data/media`. The second app uses the same media folder, and its form shows which one.

## On a NAS (NFS)

1. On the UGREEN NAS, turn on NFS: Control Panel > File Services > NFS.
2. Add an NFS permission rule to the media share for the IP of the Proxmox host: Read/Write, and squash "Map all users to admin". Jellyfin only needs read access, but the media stack needs read/write.
3. Make sure that the movies and series folders allow writing over NFS. The installer of the media stack checks this, and names the folder that fails. NFS only looks at the folder permissions on the NAS disk, not at the share permissions for SMB users.
4. In the form, choose **On a NAS (NFS)**, and give the address of the NAS and the NFS export path (UGOS shows it on the NFS page).

## On a disk of the host

**CAUTION:** Only use an empty disk for a new Directory. Proxmox makes a new file system on the disk.

1. Put the media on a disk of its own, not on the system disk of Proxmox.
2. In Proxmox, select the node and open **Disks > Directory > Create: Directory**.
3. Choose the disk, the file system `ext4` and the name `media`. Keep **Add Storage** on: the form then suggests the folder.
4. Proxmox formats the disk and mounts it at `/mnt/pve/media`.
5. In the form, choose **On this host**, and pick `/mnt/pve/media`.

A disk that already holds your media also works. Mount it on the host in your own way, then give its folder in the form. A ZFS dataset works too: `zfs create tank/media` gives `/tank/media`.

The form refuses the folders of the system (like `/etc`, `/usr`, `/var` and `/root`) and the folders of Homelab in `/mnt/homelab`.

### Owners

The apps of the media stack write as user 1000 in the container. On the host, that is user 101000, because Proxmox shifts the users of unprivileged containers by 100000.

- If the movies or series folder does not exist, the installer makes it, with owner 101000.
- The installer never changes the owner of a folder or file that exists.
- If the media stack can't write to a folder that exists, the install stops and shows the command that gives the folder to the containers, for example `chown -R 101000:101000 '/mnt/pve/media/movies'`. Run it on the host, then click **Try again**.

Jellyfin only reads. It can read every file that other users on the host can read.

### What the installer sets up

The installer writes the systemd unit `mnt-homelab-media.mount`. This unit bind-mounts the folder at `/mnt/homelab/media` when the host starts. It waits for the disk of the folder, so the apps never get the empty folder under a missing disk. The Health page shows the media folder with the network shares, with its free space.

## Good to know

- Proxmox does not back up the media folder with the containers: a backup of a container skips its bind mounts. Back up your media in another way, for example on the NAS.
- An install that fails removes the mount that it made. The movies and series folders that it made stay.
- Uninstall keeps the mount and the media.
- To move the media to another place, first remove the apps. Then, on the host, run `systemctl disable --now mnt-homelab-media.mount` and remove `/etc/systemd/system/mnt-homelab-media.mount`.
