package agent

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Mount is a network share on the host, like the NAS media share, or the
// media folder of the apps.
type Mount struct {
	Path    string `json:"path"`
	Source  string `json:"source"`
	FSType  string `json:"fsType"`
	Mounted bool   `json:"mounted"`
	Size    int64  `json:"size"`
	Used    int64  `json:"used"`
	// Error is set when the share is mounted but doesn't answer.
	Error string `json:"error,omitempty"`
}

var networkFSTypes = []string{"nfs", "nfs4", "cifs", "smb3"}

// Proxmox storage mounts itself here, and the manager already watches it through the API.
const proxmoxStorageDir = "/mnt/pve/"

// Mounts finds the network shares that should be mounted, from systemd
// .mount units and /etc/fstab, and checks each one.
type Mounts struct {
	UnitDir   string
	Fstab     string
	MountInfo string
	Timeout   time.Duration
	// statfs is replaced in tests.
	statfs func(path string) (size, used int64, err error)

	mu sync.Mutex
	// pending holds paths whose statfs has not returned yet. A hard NFS mount
	// can block forever, so we don't start a second one for the same path.
	pending map[string]bool
}

func NewMounts() *Mounts {
	return &Mounts{
		UnitDir:   "/etc/systemd/system",
		Fstab:     "/etc/fstab",
		MountInfo: "/proc/self/mountinfo",
		Timeout:   5 * time.Second,
		statfs:    statfs,
		pending:   map[string]bool{},
	}
}

func (m *Mounts) List() []Mount {
	byPath := map[string]*Mount{}
	for _, mount := range append(m.unitMounts(), m.fstabMounts()...) {
		byPath[mount.Path] = &mount
	}

	for _, mounted := range m.mounted() {
		if strings.HasPrefix(mounted.Path, proxmoxStorageDir) {
			continue
		}
		if expected, ok := byPath[mounted.Path]; ok {
			expected.Mounted = true
			continue
		}
		byPath[mounted.Path] = &mounted
	}

	mounts := make([]Mount, 0, len(byPath))
	for _, mount := range byPath {
		if mount.Mounted {
			m.usage(mount)
		}
		mounts = append(mounts, *mount)
	}
	slices.SortFunc(mounts, func(a, b Mount) int { return strings.Compare(a.Path, b.Path) })

	return mounts
}

func (m *Mounts) usage(mount *Mount) {
	m.mu.Lock()
	if m.pending[mount.Path] {
		m.mu.Unlock()
		mount.Error = "not responding"
		return
	}
	m.pending[mount.Path] = true
	m.mu.Unlock()

	type result struct {
		size, used int64
		err        error
	}
	done := make(chan result, 1)
	go func() {
		size, used, err := m.statfs(mount.Path)
		done <- result{size, used, err}

		m.mu.Lock()
		delete(m.pending, mount.Path)
		m.mu.Unlock()
	}()

	select {
	case result := <-done:
		mount.Size, mount.Used = result.size, result.used
		if result.err != nil {
			mount.Error = result.err.Error()
		}
	case <-time.After(m.Timeout):
		mount.Error = "not responding"
	}
}

func statfs(path string) (size, used int64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}

	blockSize := int64(stat.Bsize)
	// Like df: used plus available, so space reserved for root doesn't count.
	used = int64(stat.Blocks-stat.Bfree) * blockSize
	size = used + int64(stat.Bavail)*blockSize

	return size, used, nil
}

// unitMounts reads network mounts and the media mount from systemd .mount unit files.
func (m *Mounts) unitMounts() []Mount {
	files, _ := filepath.Glob(filepath.Join(m.UnitDir, "*.mount"))

	mounts := []Mount{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var mount Mount
		for line := range strings.Lines(string(data)) {
			key, value, found := strings.Cut(strings.TrimSpace(line), "=")
			if !found {
				continue
			}
			switch strings.TrimSpace(key) {
			case "What":
				mount.Source = strings.TrimSpace(value)
			case "Where":
				mount.Path = filepath.Clean(strings.TrimSpace(value))
			case "Type":
				mount.FSType = strings.TrimSpace(value)
			}
		}

		switch {
		case mount.Path != "" && slices.Contains(networkFSTypes, mount.FSType):
			mounts = append(mounts, mount)
		case mount.Path == MediaMount:
			// A bind mount of a folder on a disk of this host.
			mount.FSType = "folder"
			mounts = append(mounts, mount)
		}
	}

	return mounts
}

// fstabMounts reads network mounts from fstab, except those marked noauto.
func (m *Mounts) fstabMounts() []Mount {
	file, err := os.Open(m.Fstab)
	if err != nil {
		return nil
	}
	defer file.Close()

	mounts := []Mount{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 || strings.HasPrefix(fields[0], "#") || !slices.Contains(networkFSTypes, fields[2]) {
			continue
		}
		if len(fields) >= 4 && slices.Contains(strings.Split(fields[3], ","), "noauto") {
			continue
		}

		mounts = append(mounts, Mount{Source: unescape(fields[0]), Path: filepath.Clean(unescape(fields[1])), FSType: fields[2]})
	}

	return mounts
}

// mounted reads the network shares and the media mount that are mounted right now.
func (m *Mounts) mounted() []Mount {
	file, err := os.Open(m.MountInfo)
	if err != nil {
		return nil
	}
	defer file.Close()

	mounts := []Mount{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		// Format: id parent major:minor root path options [optional...] - type source superoptions
		before, after, found := strings.Cut(scanner.Text(), " - ")
		fields, tail := strings.Fields(before), strings.Fields(after)
		if !found || len(fields) < 5 || len(tail) < 2 {
			continue
		}
		path := unescape(fields[4])
		if !slices.Contains(networkFSTypes, tail[0]) && path != MediaMount {
			continue
		}

		mounts = append(mounts, Mount{Path: path, Source: unescape(tail[1]), FSType: tail[0], Mounted: true})
	}

	return mounts
}

// unescape decodes the octal escapes that fstab and mountinfo use, like \040 for a space.
func unescape(value string) string {
	if !strings.Contains(value, `\`) {
		return value
	}

	var builder strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] == '\\' && index+4 <= len(value) {
			if code, err := strconv.ParseUint(value[index+1:index+4], 8, 8); err == nil {
				builder.WriteByte(byte(code))
				index += 3
				continue
			}
		}
		builder.WriteByte(value[index])
	}

	return builder.String()
}
