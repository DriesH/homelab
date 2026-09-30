# Development

You need Go and Node.js 22.

## Run it on your computer

```sh
make dev        # fake Proxmox, fake host agent, API and Vite
make dev-code   # the current login code, in a second terminal
```

Open http://localhost:5173 and log in with `admin` / `homelab-dev-password` and the code from `make dev-code`.

- The fake Proxmox has one node, the manager, Jellyfin, a Docker container and a VM, with disks, storage, backups and tasks.
- The fake host agent answers update checks, logs, backup jobs and app installs. A media stack install takes a few seconds. The password `failfailfail12` makes it fail.
- A fake `tailscale` CLI (`dev/bin/tailscale`) logs in 5 seconds after you click Connect.
- The console opens a shell on your own computer, not in a container.
- Everything you change stays in `.dev/`. Delete that folder to start again.
- Jellyfin and the self-update check need the real services.

## Against a real Proxmox

The API only reads from Proxmox, except for the actions you click. Use a token with the role from [architecture.md](architecture.md).

```sh
HOMELAB_DATA_DIR=.data go run ./cmd/homelab admin   # once: create an admin

export PROXMOX_URL=https://<proxmox-ip>:8006
export PROXMOX_TOKEN_ID='homelab@pve!manager'
export PROXMOX_TOKEN_SECRET=<secret>
make dev-api    # terminal 1: API on 127.0.0.1:8080
make dev-web    # terminal 2: Vite, proxies /api to the API
```

Without a host agent, the pages that need it show "the host agent did not answer".

## Tests

```sh
make test       # Go vet and tests, ESLint and the TypeScript check
```

## Test the real install

The installers only run on a Proxmox host. Test them in a nested Proxmox VM on your server:

1. Create a VM with CPU type **host**, 4 cores, 8 GB RAM, a 64 GB disk and the network on `vmbr0`.
2. Install Proxmox VE 9 from its ISO.
3. Build a bundle with `make bundle`, copy it to the VM, and run `./install.sh` as root.

## Making a release

1. One time: create the signing key. The private key goes straight into a GitHub secret. Commit the public key.

   ```sh
   go run ./cmd/homelab-release keygen -public internal/release/signing.pub | gh secret set HOMELAB_SIGNING_KEY
   ```

2. Tag a version and push the tag. GitHub Actions builds, tests, signs and publishes the release.

   ```sh
   git tag v0.3.0 && git push origin v0.3.0
   ```

A build without the key in `signing.pub` can't install updates. If you lose the private key, make a new one and install the next version by hand once.
