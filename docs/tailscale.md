# Tailscale

The installer puts Tailscale in the manager container. Connect it on the Tailscale page, with your Tailscale account or an auth key.

- **Open Homelab from anywhere**: Tailscale Serve gives Homelab an HTTPS address like `https://homelab.<tailnet>.ts.net`, with a trusted certificate. Only devices in your tailnet can open it. Turn on HTTPS certificates for your tailnet first (admin console, DNS page).
- **Share your home network**: Homelab becomes a subnet router, so your devices can reach Jellyfin and the NAS at their normal address. Approve the route in the admin console after you turn it on.

Tailscale keeps the container's own DNS (`--accept-dns=false`), so the LAN names keep working.
