package databackup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

const passphrase = "correct horse battery"

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestBackupAndRestore(t *testing.T) {
	old := t.TempDir()
	writeFile(t, filepath.Join(old, "admin.json"), `{"username":"old"}`)
	writeFile(t, filepath.Join(old, "updates.json"), `{"history":[]}`)
	writeFile(t, filepath.Join(old, "tls", "ca.pem"), "old ca")
	writeFile(t, filepath.Join(old, "sessions.json"), "old sessions")
	writeFile(t, filepath.Join(old, ".health.json.tmp"), "temp")

	backup, err := Create(old, passphrase)
	if err != nil {
		t.Fatal(err)
	}

	fresh := t.TempDir()
	writeFile(t, filepath.Join(fresh, "admin.json"), `{"username":"new"}`)
	writeFile(t, filepath.Join(fresh, "tls", "ca.pem"), "new ca")
	writeFile(t, filepath.Join(fresh, "tls", "leaf.pem"), "new leaf")
	writeFile(t, filepath.Join(fresh, "sessions.json"), "new sessions")
	writeFile(t, filepath.Join(fresh, "jellyfin.json"), "only here")

	if err := Stage(fresh, backup, passphrase); err != nil {
		t.Fatal(err)
	}
	// Nothing changes before the next start.
	if got := readFile(t, filepath.Join(fresh, "admin.json")); got != `{"username":"new"}` {
		t.Fatalf("admin.json changed before Apply: %s", got)
	}

	applied, err := Apply(fresh)
	if err != nil || !applied {
		t.Fatalf("applied = %v, err = %v", applied, err)
	}

	if got := readFile(t, filepath.Join(fresh, "admin.json")); got != `{"username":"old"}` {
		t.Fatalf("admin.json = %s", got)
	}
	if got := readFile(t, filepath.Join(fresh, "tls", "ca.pem")); got != "old ca" {
		t.Fatalf("ca.pem = %s", got)
	}
	// The whole tls folder is replaced.
	if _, err := os.Stat(filepath.Join(fresh, "tls", "leaf.pem")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("leaf.pem of the fresh install is still there: %v", err)
	}
	if got := readFile(t, filepath.Join(fresh, "jellyfin.json")); got != "only here" {
		t.Fatalf("a file that is not in the backup changed: %s", got)
	}
	for _, name := range []string{"sessions.json", ".health.json.tmp", pendingDir} {
		if _, err := os.Stat(filepath.Join(fresh, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s exists after the restore: %v", name, err)
		}
	}
	info, err := os.Stat(filepath.Join(fresh, "updates.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("updates.json: %v %v", info.Mode(), err)
	}
	entries, _ := os.ReadDir(fresh)
	if len(entries) != 4 {
		t.Fatalf("left over files: %v", entries)
	}

	if applied, err := Apply(fresh); applied || err != nil {
		t.Fatalf("second Apply: applied = %v, err = %v", applied, err)
	}
}

func TestStageRejectsBadFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "admin.json"), "{}")
	backup, err := Create(dir, passphrase)
	if err != nil {
		t.Fatal(err)
	}

	tampered := bytes.Clone(backup)
	tampered[len(tampered)-5] ^= 1

	noAdmin := t.TempDir()
	writeFile(t, filepath.Join(noAdmin, "health.json"), "{}")
	withoutAdmin, err := Create(noAdmin, passphrase)
	if err != nil {
		t.Fatal(err)
	}

	for name, test := range map[string]struct {
		data       []byte
		passphrase string
		want       error
	}{
		"wrong passphrase": {backup, "wrong passphrase!", ErrWrongPassphrase},
		"damaged":          {tampered, passphrase, ErrWrongPassphrase},
		"not a backup":     {[]byte("hello"), passphrase, ErrInvalidBackup},
		"no admin":         {withoutAdmin, passphrase, ErrInvalidBackup},
		"outside the dir":  {encrypt(t, tarEntry{name: "../escape.json", body: "x"}), passphrase, ErrInvalidBackup},
		"symlink":          {encrypt(t, tarEntry{name: "admin.json", link: "/etc/passwd"}), passphrase, ErrInvalidBackup},
		"sessions":         {encrypt(t, tarEntry{name: "sessions.json", body: "x"}), passphrase, ErrInvalidBackup},
		"too large":        {make([]byte, MaxSize+1), passphrase, ErrInvalidBackup},
	} {
		target := t.TempDir()
		err := Stage(target, test.data, test.passphrase)
		if !errors.Is(err, test.want) {
			t.Errorf("%s: err = %v, want %v", name, err, test.want)
		}
		if _, err := os.Stat(filepath.Join(target, pendingDir)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: a failed stage left files", name)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(target), "escape.json")); err == nil {
			t.Errorf("%s: wrote outside the data dir", name)
		}
	}
}

func TestWeakPassphrase(t *testing.T) {
	if _, err := Create(t.TempDir(), "short"); !errors.Is(err, ErrWeakPassphrase) {
		t.Fatalf("err = %v", err)
	}
}

type tarEntry struct {
	name, body, link string
}

// encrypt builds a backup by hand, to test files that Create never makes.
func encrypt(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()

	var archive bytes.Buffer
	zipped := gzip.NewWriter(&archive)
	writer := tar.NewWriter(zipped)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o600, Typeflag: tar.TypeReg, Size: int64(len(entry.body))}
		if entry.link != "" {
			header.Typeflag, header.Linkname, header.Size = tar.TypeSymlink, entry.link, 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		writer.Write([]byte(entry.body))
	}
	writer.Close()
	zipped.Close()

	salt := make([]byte, saltSize)
	rand.Read(salt)
	aead, err := newAEAD(passphrase, salt)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, aead.NonceSize())
	rand.Read(nonce)
	out := append([]byte(magic), salt...)
	out = append(out, nonce...)

	return aead.Seal(out, nonce, archive.Bytes(), out[:len(magic)+saltSize])
}
