// Package databackup makes an encrypted copy of the manager's data folder:
// the admin account, tokens, settings, history and the TLS certificate
// authority. A restore is only applied at the next start, so no running
// service can write over it.
package databackup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	magic = "HOMELAB-DATA-1\n"
	// MaxSize limits the encrypted file.
	MaxSize = 32 << 20
	// maxUnpacked stops a small file from unpacking to a huge one.
	maxUnpacked   = 128 << 20
	MinPassphrase = 12

	pendingDir = ".restore-pending"
	saltSize   = 16
)

var (
	ErrInvalidBackup   = errors.New("this is not a Homelab data backup")
	ErrWrongPassphrase = errors.New("wrong passphrase, or the file is damaged")
	ErrWeakPassphrase  = fmt.Errorf("the passphrase needs at least %d characters", MinPassphrase)
)

// skipped files are not part of a backup. Sessions belong to this server,
// and a restore signs everyone out.
func skipped(name string) bool {
	base := filepath.Base(name)
	return name == "sessions.json" || strings.HasPrefix(base, ".")
}

// Create packs and encrypts the data folder.
func Create(dataDir, passphrase string) ([]byte, error) {
	if len([]rune(passphrase)) < MinPassphrase {
		return nil, ErrWeakPassphrase
	}

	var archive bytes.Buffer
	zipped := gzip.NewWriter(&archive)
	writer := tar.NewWriter(zipped)

	err := filepath.WalkDir(dataDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name, err := filepath.Rel(dataDir, path)
		if err != nil || name == "." {
			return err
		}
		if skipped(name) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		header := &tar.Header{Name: filepath.ToSlash(name), Mode: int64(info.Mode().Perm()), ModTime: info.ModTime()}
		if entry.IsDir() {
			header.Typeflag, header.Name = tar.TypeDir, header.Name+"/"
			return writer.WriteHeader(header)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		header.Typeflag, header.Size = tar.TypeReg, int64(len(data))
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		_, err = writer.Write(data)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if err := zipped.Close(); err != nil {
		return nil, err
	}

	salt := make([]byte, saltSize)
	rand.Read(salt)
	aead, err := newAEAD(passphrase, salt)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	rand.Read(nonce)

	out := append([]byte(magic), salt...)
	out = append(out, nonce...)
	out = aead.Seal(out, nonce, archive.Bytes(), out[:len(magic)+saltSize])
	if len(out) > MaxSize {
		return nil, fmt.Errorf("the backup is larger than %d MB", MaxSize>>20)
	}

	return out, nil
}

func newAEAD(passphrase string, salt []byte) (cipher.AEAD, error) {
	key := argon2.IDKey([]byte(passphrase), salt, 3, 64*1024, 4, 32)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	return cipher.NewGCM(block)
}

// Stage decrypts and checks a backup, and unpacks it next to the data. Apply
// moves it in place at the next start.
func Stage(dataDir string, data []byte, passphrase string) error {
	archive, err := decrypt(data, passphrase)
	if err != nil {
		return err
	}

	pending := filepath.Join(dataDir, pendingDir)
	if err := os.RemoveAll(pending); err != nil {
		return err
	}
	if err := unpack(archive, pending); err != nil {
		os.RemoveAll(pending)
		return err
	}
	if _, err := os.Stat(filepath.Join(pending, "admin.json")); err != nil {
		os.RemoveAll(pending)
		return fmt.Errorf("%w: it has no admin account", ErrInvalidBackup)
	}

	return nil
}

func decrypt(data []byte, passphrase string) ([]byte, error) {
	if len(data) > MaxSize {
		return nil, fmt.Errorf("%w: the file is larger than %d MB", ErrInvalidBackup, MaxSize>>20)
	}
	if !bytes.HasPrefix(data, []byte(magic)) || len(data) < len(magic)+saltSize+12 {
		return nil, ErrInvalidBackup
	}

	header := data[:len(magic)+saltSize]
	aead, err := newAEAD(passphrase, header[len(magic):])
	if err != nil {
		return nil, err
	}
	nonce := data[len(header) : len(header)+aead.NonceSize()]
	archive, err := aead.Open(nil, nonce, data[len(header)+aead.NonceSize():], header)
	if err != nil {
		return nil, ErrWrongPassphrase
	}

	return archive, nil
}

// unpack only writes regular files and folders inside dir.
func unpack(archive []byte, dir string) error {
	zipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidBackup, err)
	}
	reader := tar.NewReader(zipped)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	var total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidBackup, err)
		}

		name := filepath.FromSlash(strings.TrimSuffix(header.Name, "/"))
		if !filepath.IsLocal(name) || skipped(name) {
			return fmt.Errorf("%w: unexpected file %q", ErrInvalidBackup, header.Name)
		}
		path := filepath.Join(dir, name)
		mode := fs.FileMode(header.Mode).Perm() & 0o700

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, mode|0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			total += header.Size
			if total > maxUnpacked {
				return fmt.Errorf("%w: it unpacks to more than %d MB", ErrInvalidBackup, maxUnpacked>>20)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return err
			}
			file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode|0o600)
			if err != nil {
				return err
			}
			_, err = io.Copy(file, io.LimitReader(reader, header.Size))
			if closeErr := file.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: unexpected file %q", ErrInvalidBackup, header.Name)
		}
	}
}

// Apply moves a staged restore in place. It runs at start, before anything
// reads the data. It reports whether there was a restore.
func Apply(dataDir string) (bool, error) {
	pending := filepath.Join(dataDir, pendingDir)
	entries, err := os.ReadDir(pending)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	old := filepath.Join(dataDir, fmt.Sprintf(".restore-old-%d", time.Now().UnixNano()))
	if err := os.Mkdir(old, 0o700); err != nil {
		return false, err
	}
	for _, entry := range entries {
		target := filepath.Join(dataDir, entry.Name())
		if err := os.Rename(target, filepath.Join(old, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		if err := os.Rename(filepath.Join(pending, entry.Name()), target); err != nil {
			return false, err
		}
	}
	// Sessions of the old data don't belong to the restored account.
	if err := os.Remove(filepath.Join(dataDir, "sessions.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.RemoveAll(pending); err != nil {
		return false, err
	}

	return true, os.RemoveAll(old)
}
