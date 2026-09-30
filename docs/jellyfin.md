# Jellyfin

Connect Jellyfin on the Jellyfin page with its URL and an API key (Jellyfin: Dashboard > API Keys). The page shows library counts, what is playing (and whether it transcodes), and recently added items.

The page can also turn on a Netflix-style theme for the Jellyfin web client. The theme is written for the Modern layout of Jellyfin 12 and lives in `internal/jellyfin/netflix.css`. The manager adds it to the custom CSS of Jellyfin between two marker comments, so your own custom CSS stays. The TV and phone apps do not use custom CSS.
