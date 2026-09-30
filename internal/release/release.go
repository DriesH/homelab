// Package release signs and verifies release bundles. The host agent only
// installs a bundle with a valid signature from the key in signing.pub.
package release

import (
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

//go:embed signing.pub
var publicKey string

var (
	ErrNoKey        = errors.New("this build has no release signing key")
	ErrBadSignature = errors.New("the release signature is not valid")
	ErrOldVersion   = errors.New("the release is not newer than the installed version")
)

var versionPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// Signature is the content of the .sig file next to a bundle.
type Signature struct {
	Version   string `json:"version"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
}

// message is what the key signs, so a signature can't be moved to another version.
func message(version, sha256Hex string) []byte {
	return []byte("homelab-release\n" + version + "\n" + sha256Hex + "\n")
}

// Hash returns the SHA-256 of a bundle as hex.
func Hash(bundle io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, bundle); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func Sign(privateKey ed25519.PrivateKey, version, sha256Hex string) (Signature, error) {
	if !versionPattern.MatchString(version) {
		return Signature{}, fmt.Errorf("version %q must look like v1.2.3", version)
	}

	signature := ed25519.Sign(privateKey, message(version, sha256Hex))

	return Signature{Version: version, SHA256: sha256Hex, Signature: base64.StdEncoding.EncodeToString(signature)}, nil
}

// Verify checks the signature against the built-in public key and that the
// bundle hash matches.
func Verify(signature Signature, sha256Hex string) error {
	key, err := PublicKey()
	if err != nil {
		return err
	}

	return verifyWith(key, signature, sha256Hex)
}

func verifyWith(key ed25519.PublicKey, signature Signature, sha256Hex string) error {
	raw, err := base64.StdEncoding.DecodeString(signature.Signature)
	if err != nil || !versionPattern.MatchString(signature.Version) || signature.SHA256 != sha256Hex {
		return ErrBadSignature
	}
	if !ed25519.Verify(key, message(signature.Version, signature.SHA256), raw) {
		return ErrBadSignature
	}

	return nil
}

func PublicKey() (ed25519.PublicKey, error) {
	encoded := strings.TrimSpace(publicKey)
	if encoded == "" {
		return nil, ErrNoKey
	}

	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, ErrNoKey
	}

	return ed25519.PublicKey(key), nil
}

// Newer reports whether version is newer than current. Builds that are not a
// release (like "dev" or "v1.2.3-4-gabcdef") accept any release.
func Newer(version, current string) bool {
	next := versionPattern.FindStringSubmatch(version)
	if next == nil {
		return false
	}

	installed := versionPattern.FindStringSubmatch(current)
	if installed == nil {
		return true
	}

	for index := 1; index <= 3; index++ {
		a, _ := strconv.Atoi(next[index])
		b, _ := strconv.Atoi(installed[index])
		if a != b {
			return a > b
		}
	}

	return false
}
