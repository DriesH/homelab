# App names

Homelab gives some apps their own name on your LAN, with HTTPS:

| Name | App | Where it goes |
| --- | --- | --- |
| `https://seerr.homelab.local` | Seerr | the media stack container, port 5055 |
| `https://jellyfin.homelab.local` | Jellyfin | the Jellyfin URL on the Jellyfin page |

## How it works

- The manager container announces these names over mDNS, at its own IP address (`homelab-mdns` service).
- The manager forwards a request for such a name to the app. It looks the app up in Proxmox, so a new IP of the app is no problem.
- The certificate of the manager also has these names. The Homelab CA can sign names below `homelab.local`, so a device that trusts `homelab.local` also trusts these names. You install nothing new.
- If an app is not installed or does not answer, the page says why.

## Devices

mDNS names with more than one part before `.local` work on macOS, iOS and most Linux desktops. Some Android phones and some Windows setups do not look them up. Then use the address of the app on the Apps page.

Give the manager container a fixed IP in your router (a DHCP reservation). The names point to the IP the container had when it started.

## Jellyfin apps

In the Jellyfin app on a phone or TV, you can use `https://jellyfin.homelab.local` as the server address, if that device trusts the Homelab CA. TV apps often do not, so keep the IP address there.

Video through this name goes through the manager container. For heavy 4K streams, the direct address of Jellyfin is a bit faster.
