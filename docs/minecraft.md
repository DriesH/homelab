# Minecraft

The Apps page installs a Minecraft Java server in its own container. The server is [Paper](https://papermc.io): the normal Minecraft Java server, but faster and lighter.

## Before you start

1. Read the [Minecraft EULA](https://aka.ms/MinecraftEULA). The form asks you to agree to it.
2. Find your Java profile name, and the names of your friends, on the [Minecraft profile page](https://www.minecraft.net/en-us/msaprofile/mygames/editprofile). Use Java profile names, not Xbox gamertags.
3. Optional, for friends outside your network: make a free account on [playit.gg](https://playit.gg). See below.

## Install

1. On the Apps page, click **Install** on Minecraft.
2. Fill in your Minecraft name and the names of your friends. You become the operator, so you can run commands.
3. Choose the memory. 4 GB is enough for about 10 players. The container gets a bit more for Java and Debian.
4. Agree to the EULA, and click **Install**.

The first start downloads Paper and makes the world. This takes a few minutes. If the install fails, Homelab removes the container. Then click **Try again**, or **Change answers** to fix an answer. Your answers stay on the host for 24 hours. The playit.gg secret key never goes back to the browser.

You can also run the installer on the Proxmox host, as root: `bash /usr/local/lib/homelab/stacks/minecraft/install.sh`. It asks the main questions.

### What the installer does

1. Makes a Debian 13 container named `minecraft` (4 cores, 16 GB disk) with the tags `homelab` and `minecraft`.
2. Gives the container a second disk for the world at `/opt/minecraft/data`. Proxmox backs it up with the container.
3. Installs Docker, and starts the server with the image [`itzg/minecraft-server`](https://docker-minecraft-server.readthedocs.io).
4. Turns on the whitelist with you and your friends, and makes you the operator.
5. Adds the plugin [ViaVersion](https://modrinth.com/plugin/viaversion), so players with a newer Minecraft can join while Paper catches up.
6. Starts the playit.gg agent next to the server, when you gave a secret key.

## Join the server

In Minecraft, go to **Multiplayer > Add Server**:

- **At home:** use the address on the Apps page, like `192.168.1.60:25565`.
- **Friends outside your network:** use the playit.gg address, like `name.joinmc.link`.

## Friends outside your network (playit.gg)

playit.gg gives the server a fixed public address, so you open no ports on your router. Your friends install nothing.

1. On playit.gg, add an agent of the type **Docker**. Copy its secret key. playit.gg shows it only once.
2. Add a tunnel of the type **Minecraft Java**, to the local address `localhost:25565`.
3. Copy the address of the tunnel.
4. In the install form of Homelab, paste the secret key, and the address if you want to see it on the Apps page.

The free plan of playit.gg is enough for Minecraft Java. The traffic goes through the servers of playit.gg. Everybody can find the address, so keep the whitelist on.

## Players

Click **Players** on the app to change the whitelist and the operators. The server applies the change at once, without a restart.

Minecraft only adds names that have a Java profile. With a typo, the change stops and says which name is wrong.

## Version and updates

The server runs the newest **stable** Paper. A new Minecraft version can take a few weeks before Paper has a stable build. Until then, ViaVersion lets players with the newest Minecraft join.

**Update** on the Apps page makes a snapshot, updates the packages and the images, and restarts the server. When a newer stable Paper is out, the server starts with it. The weekly container updates on the Updates page do the same. A world can't go back to an older Minecraft version, so restore the snapshot or a backup if you need to.

## Backups

The Proxmox backups copy the world disk with the container. The backup runs while the server runs, so the copy of the world can miss the last minute of play.

## Commands

As an operator, type commands in the chat of Minecraft, like `/gamemode creative`.

To run a command on the server itself, run this on the Proxmox host as root, with the ID of the container: `pct exec 150 -- docker exec mc rcon-cli "list"`. Replace `list` with the command, without the `/`.

## Remove

**Remove** deletes the container with the world. The backups stay, so you can restore it on the Backups page.
