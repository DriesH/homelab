// homelab-arr runs inside the media LXC and connects the apps of the arr stack.
//
//	homelab-arr configure --env /opt/arr/.env < passwords
//
// The first line on stdin is the password for the apps. Optional next lines
// are the password of the Jellyfin admin, for the setup of Seerr, and the
// password of the OpenSubtitles.com account, for Bazarr.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"homelab/internal/stacks/arr"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "configure" {
		fmt.Fprintln(os.Stderr, "usage: homelab-arr configure [--env FILE] [--data-dir DIR] [--bazarr-config FILE] < passwords")
		os.Exit(2)
	}

	if err := configure(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func configure(args []string) error {
	flags := flag.NewFlagSet("configure", flag.ExitOnError)
	envPath := flags.String("env", "/opt/arr/.env", "stack .env file")
	dataDir := flags.String("data-dir", "/data", "data folder with media/ and downloads/")
	bazarrConfig := flags.String("bazarr-config", "/opt/arr/config/bazarr/config/config.yaml", "Bazarr config file")
	flags.Parse(args)

	env, err := readEnv(*envPath)
	if err != nil {
		return err
	}

	// The passwords come on stdin so they never land in a file or process list.
	stdin := bufio.NewReader(os.Stdin)
	password, err := stdin.ReadString('\n')
	if err != nil && password == "" {
		return errors.New("pass the admin password on stdin")
	}
	password = strings.TrimRight(password, "\r\n")
	jellyfinAdminPassword, _ := stdin.ReadString('\n')
	jellyfinAdminPassword = strings.TrimRight(jellyfinAdminPassword, "\r\n")
	openSubtitlesPassword, _ := stdin.ReadString('\n')
	openSubtitlesPassword = strings.TrimRight(openSubtitlesPassword, "\r\n")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = arr.Configure(ctx, arr.Config{
		Username:              env["ARR_USERNAME"],
		Password:              password,
		RadarrAPIKey:          env["RADARR_API_KEY"],
		SonarrAPIKey:          env["SONARR_API_KEY"],
		ProwlarrAPIKey:        env["PROWLARR_API_KEY"],
		JellyfinURL:           env["JELLYFIN_URL"],
		JellyfinInternalURL:   env["JELLYFIN_INTERNAL_URL"],
		JellyfinAPIKey:        env["JELLYFIN_API_KEY"],
		JellyfinAdminUsername: env["JELLYFIN_ADMIN_USERNAME"],
		JellyfinAdminPassword: jellyfinAdminPassword,
		OpenSubtitlesUsername: env["OPENSUBTITLES_USERNAME"],
		OpenSubtitlesPassword: openSubtitlesPassword,
		SubtitleLanguages:     strings.Split(env["SUBTITLE_LANGUAGES"], ","),
		MoviesFolder:          env["MOVIES_FOLDER"],
		SeriesFolder:          env["SERIES_FOLDER"],
		DataDir:               *dataDir,
		BazarrConfigPath:      *bazarrConfig,
		Endpoints:             arr.DefaultEndpoints,
		QBittorrentTempPassword: func(ctx context.Context) (string, error) {
			return qbittorrentTempPassword(ctx)
		},
		Logf: func(format string, args ...any) {
			fmt.Printf("==> "+format+"\n", args...)
		},
	})
	if err != nil {
		return err
	}

	fmt.Println("==> Done")

	return nil
}

// readEnv parses the simple KEY=VALUE lines Docker Compose reads too.
func readEnv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	env := map[string]string{}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key, value, found := strings.Cut(line, "="); found {
			env[key] = value
		}
	}

	for _, key := range []string{"ARR_USERNAME", "RADARR_API_KEY", "SONARR_API_KEY", "PROWLARR_API_KEY", "SUBTITLE_LANGUAGES"} {
		if env[key] == "" {
			return nil, fmt.Errorf("%s is missing in %s", key, path)
		}
	}

	return env, nil
}

// qbittorrentTempPassword reads the one-time password qBittorrent logs on first start.
func qbittorrentTempPassword(ctx context.Context) (string, error) {
	output, err := exec.CommandContext(ctx, "docker", "logs", "qbittorrent").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker logs qbittorrent: %w", err)
	}

	const marker = "temporary password is provided for this session: "
	password := ""
	for line := range strings.Lines(string(output)) {
		if _, after, found := strings.Cut(line, marker); found {
			password = strings.TrimSpace(after)
		}
	}
	if password == "" {
		return "", errors.New("qbittorrent: no temporary password in logs")
	}

	return password, nil
}
