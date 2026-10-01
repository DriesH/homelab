package agent

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// MediaMount is where the installers mount the media of the apps: a NAS
// share, or a folder on a disk of the host.
const MediaMount = "/mnt/homelab/media"

// ProxmoxStorageConfig lists the storages of Proxmox, with their paths.
const ProxmoxStorageConfig = "/etc/pve/storage.cfg"

// MediaFolder is a Directory storage of Proxmox: a likely place for the
// media on a disk of the host.
type MediaFolder struct {
	Storage string `json:"storage"`
	Path    string `json:"path"`
}

var (
	mediaFolderPattern = regexp.MustCompile(`^(/[A-Za-z0-9._-]+)+$`)
	// The folders of the system, and the default storage of Proxmox in /var/lib/vz.
	systemFolders = []string{"bin", "boot", "dev", "etc", "lib", "lib32", "lib64", "libx32", "proc", "root", "run", "sbin", "sys", "tmp", "usr", "var"}
)

// ValidMediaFolder allows a full path on the host, outside the folders of the
// system and of Homelab. The installer checks the same.
func ValidMediaFolder(path string) bool {
	if len(path) > 255 || !mediaFolderPattern.MatchString(path) || filepath.Clean(path) != path {
		return false
	}
	first, _, _ := strings.Cut(path[1:], "/")
	if slices.Contains(systemFolders, first) {
		return false
	}

	// Not the media mount itself, a folder above it, or a folder in it.
	return !strings.HasPrefix(MediaMount+"/", path+"/") && !strings.HasPrefix(path, "/mnt/homelab/")
}

// MediaFolders reads the Directory storages from the storage config of Proxmox:
//
//	dir: media
//		path /mnt/pve/media
//		content backup
func MediaFolders(storageConfig string) []MediaFolder {
	folders := []MediaFolder{}
	file, err := os.Open(storageConfig)
	if err != nil {
		return folders
	}
	defer file.Close()

	// current is the index of the dir storage that is being read, or -1.
	current := -1
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		if line[0] != ' ' && line[0] != '\t' {
			current = -1
			kind, id, found := strings.Cut(line, ":")
			if found && strings.TrimSpace(kind) == "dir" {
				folders = append(folders, MediaFolder{Storage: strings.TrimSpace(id)})
				current = len(folders) - 1
			}
			continue
		}

		fields := strings.Fields(line)
		if current >= 0 && len(fields) == 2 && fields[0] == "path" {
			folders[current].Path = fields[1]
		}
	}

	return slices.DeleteFunc(folders, func(folder MediaFolder) bool { return !ValidMediaFolder(folder.Path) })
}
