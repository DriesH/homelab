// homelab-jellyfin does the first setup of a new Jellyfin server. The Jellyfin
// installer runs it on the Proxmox host.
//
//	homelab-jellyfin setup --url http://192.168.1.20:8096 --admin dries \
//	    --movies /data/media/movies --series /data/media/series [--gpu] [--theme] < password
//
// The last line it prints is the API key for Homelab:
//
//	HOMELAB jellyfin-key <key>
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"homelab/internal/stacks/jellyfinsetup"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "setup" {
		fmt.Fprintln(os.Stderr, "usage: homelab-jellyfin setup --url URL --admin NAME --movies PATH --series PATH [--gpu] [--theme] < password")
		os.Exit(2)
	}

	if err := setup(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func setup(args []string) error {
	flags := flag.NewFlagSet("setup", flag.ExitOnError)
	url := flags.String("url", "", "address of Jellyfin")
	admin := flags.String("admin", "", "username of the Jellyfin admin")
	movies := flags.String("movies", "", "folder of the Movies library, as Jellyfin sees it")
	series := flags.String("series", "", "folder of the Series library, as Jellyfin sees it")
	gpu := flags.Bool("gpu", false, "turn on VA-API hardware transcoding")
	theme := flags.Bool("theme", false, "turn on the Netflix theme")
	flags.Parse(args)

	if *url == "" || *admin == "" || *movies == "" || *series == "" {
		return errors.New("--url, --admin, --movies and --series are required")
	}

	// The password comes on stdin so it never lands in a file or process list.
	password, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && password == "" {
		return errors.New("pass the admin password on stdin")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	key, err := jellyfinsetup.Setup(ctx, jellyfinsetup.Config{
		URL:           *url,
		AdminUsername: *admin,
		AdminPassword: strings.TrimRight(password, "\r\n"),
		MoviesPath:    *movies,
		SeriesPath:    *series,
		GPU:           *gpu,
		Theme:         *theme,
		Logf: func(format string, args ...any) {
			fmt.Printf("==> "+format+"\n", args...)
		},
	})
	if err != nil {
		return err
	}

	fmt.Println("HOMELAB jellyfin-key " + key)

	return nil
}
