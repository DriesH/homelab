# Tailscale

The installer puts Tailscale in the manager container. Connect it on the Tailscale page, with your Tailscale account or an auth key.

- **Open Homelab from anywhere**: Tailscale Serve gives Homelab an HTTPS address like `https://homelab.<tailnet>.ts.net`, with a trusted certificate. Only devices in your tailnet can open it. Turn on HTTPS certificates for your tailnet first (admin console, DNS page).
- **Share your home network**: Homelab becomes a subnet router, so your devices can reach Jellyfin and the NAS at their normal address. Approve the route in the admin console after you turn it on.

Tailscale keeps the container's own DNS (`--accept-dns=false`), so the LAN names keep working.

## Apps on your tailnet (Tailscale Services)

Tailscale Services give Seerr and Jellyfin their own address on your tailnet, like `https://seerr.<tailnet>.ts.net`, with a trusted certificate.

A Service needs a host with a tag, so do this once in the [Tailscale admin console](https://login.tailscale.com/admin):

1. In **Access controls**, add to your policy file:

   ```json
   "tagOwners": {
     "tag:homelab": ["autogroup:admin"]
   },
   "autoApprovers": {
     "services": {
       "svc:seerr": ["tag:homelab"],
       "svc:jellyfin": ["tag:homelab"]
     }
   }
   ```

2. In **Services**, choose Advertise > Define a Service, and add `svc:seerr` and `svc:jellyfin`, each with the endpoint `tcp:443`.
3. On the Tailscale page of Homelab, click **Use tag:homelab**, and log in again with the link on the page.
4. Turn on Seerr and Jellyfin under **Apps on your tailnet**.

After step 3, the manager belongs to `tag:homelab` instead of your account, and its key no longer expires. If your access controls only allow your own devices, also allow `tag:homelab`.

Tailscale forwards each Service to a small local proxy in the manager (`127.0.0.1:18081` for Seerr, `127.0.0.1:18082` for Jellyfin). These proxies send the request to the app, like the LAN names in [app-names.md](app-names.md).
