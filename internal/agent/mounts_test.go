package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTestMounts(t *testing.T, mountInfo string) *Mounts {
	t.Helper()
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "mnt-homelab-media.mount"), `[Unit]
Description=Homelab media share on the NAS

[Mount]
What=192.168.1.10:/volume1/media
Where=/mnt/homelab/media
Type=nfs
Options=_netdev,hard,noatime
`)
	writeFile(t, filepath.Join(dir, "boot.mount"), "[Mount]\nWhat=/dev/sda1\nWhere=/boot\nType=ext4\n")
	writeFile(t, filepath.Join(dir, "fstab"), `# <file system> <mount point> <type> <options>
/dev/pve/root / ext4 errors=remount-ro 0 1
//nas/backup\040files /mnt/backup cifs credentials=/root/.smb 0 0
nas:/volume1/old /mnt/old nfs noauto 0 0
`)
	writeFile(t, filepath.Join(dir, "mountinfo"), mountInfo)

	return &Mounts{
		UnitDir:    dir,
		Fstab:      filepath.Join(dir, "fstab"),
		MountInfo:  filepath.Join(dir, "mountinfo"),
		CloudLabel: filepath.Join(dir, "label"),
		Timeout:    100 * time.Millisecond,
		statfs: func(string) (int64, int64, error) {
			return 1000, 950, nil
		},
		pending: map[string]bool{},
	}
}

func TestMountsListsExpectedAndMountedShares(t *testing.T) {
	mounts := newTestMounts(t, `22 1 0:21 / / rw,relatime shared:1 - ext4 /dev/mapper/pve-root rw
90 22 0:50 / /mnt/homelab/media rw,noatime shared:40 - nfs 192.168.1.10:/volume1/media rw,vers=4.2
91 22 0:51 / /mnt/pve/nas-backups rw shared:41 - nfs 192.168.1.10:/volume1/pve rw
92 22 0:52 / /mnt/extra rw shared:42 - nfs4 nas:/volume1/extra rw
`)

	list := mounts.List()
	if len(list) != 3 {
		t.Fatalf("got %d mounts, want 3: %+v", len(list), list)
	}

	backup, extra, media := list[0], list[1], list[2]
	if backup.Path != "/mnt/backup" || backup.Source != "//nas/backup files" || backup.Mounted {
		t.Errorf("backup = %+v, want an unmounted cifs share", backup)
	}
	if extra.Path != "/mnt/extra" || !extra.Mounted || extra.FSType != "nfs4" {
		t.Errorf("extra = %+v", extra)
	}
	if media.Source != "192.168.1.10:/volume1/media" || !media.Mounted || media.Size != 1000 || media.Used != 950 {
		t.Errorf("media = %+v", media)
	}
}

func TestMountsReportsHungShare(t *testing.T) {
	mounts := newTestMounts(t, "90 22 0:50 / /mnt/homelab/media rw - nfs 192.168.1.10:/volume1/media rw\n")
	release := make(chan struct{})
	mounts.statfs = func(string) (int64, int64, error) {
		<-release
		return 0, 0, nil
	}
	defer close(release)

	media := mounts.List()[1]
	if media.Error != "not responding" {
		t.Fatalf("media = %+v, want not responding", media)
	}

	// The first statfs is still stuck, so the second call must not wait again.
	started := time.Now()
	if media := mounts.List()[1]; media.Error != "not responding" || time.Since(started) > 50*time.Millisecond {
		t.Fatalf("second call: %+v after %s", media, time.Since(started))
	}
}

func TestMountsListsTieredMedia(t *testing.T) {
	mounts := newTestMounts(t, `22 1 0:21 / / rw,relatime shared:1 - ext4 /dev/mapper/pve-root rw
90 22 0:50 / /mnt/homelab/local rw,noatime shared:40 - nfs 192.168.1.10:/volume1/media rw,vers=4.2
93 22 0:53 / /mnt/homelab/cloud rw,nosuid shared:43 - fuse.rclone media-crypt: rw,user_id=0
94 22 0:54 / /mnt/homelab/media rw,relatime shared:44 - fuse.mergerfs homelab-media rw,user_id=0
`)
	dir := mounts.UnitDir
	writeFile(t, filepath.Join(dir, "mnt-homelab-media.mount"), `[Mount]
What=/mnt/homelab/local=RW:/mnt/homelab/cloud=NC
Where=/mnt/homelab/media
Type=fuse.mergerfs
Options=allow_other,cache.files=off,category.create=ff
`)
	writeFile(t, filepath.Join(dir, "mnt-homelab-local.mount"), `[Mount]
What=192.168.1.10:/volume1/media
Where=/mnt/homelab/local
Type=nfs
`)
	writeFile(t, filepath.Join(dir, "homelab-cloud.service"), "[Service]\nType=notify\n")
	writeFile(t, filepath.Join(dir, "label"), "Cloudflare R2: media\n")

	byPath := map[string]Mount{}
	for _, mount := range mounts.List() {
		byPath[mount.Path] = mount
	}

	media := byPath["/mnt/homelab/media"]
	if media.FSType != "tiered" || media.Source != "192.168.1.10:/volume1/media" || media.Role != RoleMedia || !media.Mounted {
		t.Errorf("media = %+v", media)
	}
	local := byPath["/mnt/homelab/local"]
	if local.FSType != "nfs" || local.Role != RoleMediaLocal || !local.Mounted || local.Size != 1000 {
		t.Errorf("local = %+v", local)
	}
	cloud := byPath["/mnt/homelab/cloud"]
	if cloud.FSType != "cloud" || cloud.Source != "Cloudflare R2: media" || cloud.Role != RoleMediaCloud || !cloud.Mounted || cloud.Size != 0 || cloud.Used != 0 {
		t.Errorf("cloud = %+v", cloud)
	}
}

func TestMountsReportsCloudThatIsDown(t *testing.T) {
	mounts := newTestMounts(t, "")
	writeFile(t, filepath.Join(mounts.UnitDir, "homelab-cloud.service"), "[Service]\n")

	for _, mount := range mounts.List() {
		if mount.Path == "/mnt/homelab/cloud" {
			if mount.Mounted || mount.Role != RoleMediaCloud {
				t.Fatalf("cloud = %+v", mount)
			}
			return
		}
	}
	t.Fatal("the cloud mount is missing")
}
