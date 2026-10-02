package agent

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestValidMediaFolder(t *testing.T) {
	for _, path := range []string{"/mnt/pve/media", "/tank/media", "/srv/media", "/home/media/Films_1", "/data"} {
		if !ValidMediaFolder(path) {
			t.Errorf("%q is refused", path)
		}
	}
	for _, path := range []string{
		"", "/", "media", "/mnt/pve/media/", "/mnt//media", "/mnt/./media", "/mnt/../etc", "/mnt/my media",
		"/etc", "/etc/pve", "/var/lib/vz", "/usr/local", "/root/media", "/boot/media",
		"/mnt", "/mnt/homelab", "/mnt/homelab/media", "/mnt/homelab/media/movies", "/mnt/homelab/other",
		"/mnt/media;rm", "/mnt/$HOME",
	} {
		if ValidMediaFolder(path) {
			t.Errorf("%q is allowed", path)
		}
	}
}

func TestMediaFoldersReadsDirectoryStorages(t *testing.T) {
	config := filepath.Join(t.TempDir(), "storage.cfg")
	writeFile(t, config, `dir: local
	path /var/lib/vz
	content iso,vztmpl,backup

lvmthin: local-lvm
	thinpool data
	vgname pve

# A disk from Disks > Directory.
dir: media
	path /mnt/pve/media
	content backup
	is_mountpoint 1

nfs: nas
	path /mnt/pve/nas
	server 192.168.1.5

dir: tank
	content images
	path /tank/films
`)

	got := MediaFolders(config)
	want := []MediaFolder{{Storage: "media", Path: "/mnt/pve/media"}, {Storage: "tank", Path: "/tank/films"}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	if got := MediaFolders(filepath.Join(t.TempDir(), "missing")); got == nil || len(got) != 0 {
		t.Fatalf("a missing config gives %+v, want an empty list", got)
	}
}

func TestAnswersTakeANASOrAFolder(t *testing.T) {
	media := validAnswers()
	jellyfin := validJellyfin()

	for name, change := range map[string]func(server, export, folder *string){
		"folder": func(server, export, folder *string) { *server, *export, *folder = "", "", "/mnt/pve/media" },
		"nas":    func(server, export, folder *string) {},
		"none":   func(server, export, folder *string) { *server, *export = "", "" },
	} {
		mediaAnswers, jellyfinAnswers := media, jellyfin
		change(&mediaAnswers.NASServer, &mediaAnswers.NASExport, &mediaAnswers.MediaFolder)
		change(&jellyfinAnswers.NASServer, &jellyfinAnswers.NASExport, &jellyfinAnswers.MediaFolder)
		if err := mediaAnswers.Validate(); err != nil {
			t.Errorf("media stack, %s: %v", name, err)
		}
		if err := jellyfinAnswers.Validate(); err != nil {
			t.Errorf("jellyfin, %s: %v", name, err)
		}
	}

	for name, change := range map[string]func(server, export, folder *string){
		"both":          func(server, export, folder *string) { *folder = "/mnt/pve/media" },
		"system folder": func(server, export, folder *string) { *server, *export, *folder = "", "", "/etc" },
		"relative":      func(server, export, folder *string) { *server, *export, *folder = "", "", "media" },
		"half a share":  func(server, export, folder *string) { *export = "" },
	} {
		mediaAnswers, jellyfinAnswers := media, jellyfin
		change(&mediaAnswers.NASServer, &mediaAnswers.NASExport, &mediaAnswers.MediaFolder)
		change(&jellyfinAnswers.NASServer, &jellyfinAnswers.NASExport, &jellyfinAnswers.MediaFolder)
		if err := mediaAnswers.Validate(); !errors.Is(err, ErrInvalidAnswers) {
			t.Errorf("media stack, %s: got %v", name, err)
		}
		if err := jellyfinAnswers.Validate(); !errors.Is(err, ErrInvalidAnswers) {
			t.Errorf("jellyfin, %s: got %v", name, err)
		}
	}
}

func TestMountsListsTheMediaFolder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mnt-homelab-media.mount"), `[Mount]
What=/mnt/pve/media
Where=/mnt/homelab/media
Type=none
Options=bind
`)
	writeFile(t, filepath.Join(dir, "mountinfo"), `22 1 0:21 / / rw,relatime shared:1 - ext4 /dev/mapper/pve-root rw
95 22 8:17 / /mnt/pve/media rw,relatime shared:50 - ext4 /dev/sdb1 rw
96 22 8:17 / /mnt/homelab/media rw,relatime shared:50 - ext4 /dev/sdb1 rw
`)
	mounts := &Mounts{
		UnitDir:   dir,
		Fstab:     filepath.Join(dir, "fstab"),
		MountInfo: filepath.Join(dir, "mountinfo"),
		Timeout:   100 * time.Millisecond,
		statfs:    func(string) (int64, int64, error) { return 1000, 100, nil },
		pending:   map[string]bool{},
	}

	list := mounts.List()
	want := Mount{Path: MediaMount, Source: "/mnt/pve/media", FSType: "folder", Mounted: true, Size: 1000, Used: 100, Role: RoleMedia}
	if len(list) != 1 || list[0] != want {
		t.Fatalf("got %+v, want only %+v", list, want)
	}
}
