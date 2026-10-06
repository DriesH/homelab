package cloud

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"strings"
)

const keyPrefix = "HLC1"

var (
	ErrInvalidKey = errors.New("this is not a valid recovery key, check for typos")
	keyEncoding   = base32.StdEncoding.WithPadding(base32.NoPadding)
)

// RecoveryKey holds the two passwords of rclone crypt: 16 random bytes each.
// Written out, it is HLC1 and groups of 4 letters and digits, with a checksum
// at the end that catches typos.
type RecoveryKey [32]byte

func NewRecoveryKey() (RecoveryKey, error) {
	var key RecoveryKey
	_, err := rand.Read(key[:])

	return key, err
}

func (k RecoveryKey) String() string {
	sum := sha256.Sum256(k[:])
	encoded := keyEncoding.EncodeToString(append(k[:], sum[:2]...))

	groups := []string{keyPrefix}
	for len(encoded) > 0 {
		size := min(4, len(encoded))
		groups = append(groups, encoded[:size])
		encoded = encoded[size:]
	}

	return strings.Join(groups, "-")
}

// The key has no 0, 1 or 8, so these are typos for the letters that look like them.
var lookAlikes = strings.NewReplacer("0", "O", "1", "I", "8", "B")

// clean reads a key or part of it as people type it: in any case, with or
// without dashes and spaces.
func clean(text string) string {
	return strings.ToUpper(strings.Join(strings.FieldsFunc(text, func(r rune) bool { return r == '-' || r == ' ' }), ""))
}

func ParseRecoveryKey(text string) (RecoveryKey, error) {
	var key RecoveryKey

	encoded, found := strings.CutPrefix(clean(text), keyPrefix)
	if !found {
		return key, ErrInvalidKey
	}
	data, err := keyEncoding.DecodeString(lookAlikes.Replace(encoded))
	if err != nil || len(data) != len(key)+2 {
		return key, ErrInvalidKey
	}

	copy(key[:], data)
	sum := sha256.Sum256(key[:])
	if !bytes.Equal(sum[:2], data[len(key):]) {
		return key, ErrInvalidKey
	}

	return key, nil
}

// CryptPasswords are the password and the salt for rclone crypt.
func (k RecoveryKey) CryptPasswords() (password, salt string) {
	return base64.RawURLEncoding.EncodeToString(k[:16]), base64.RawURLEncoding.EncodeToString(k[16:])
}

// endsWith is true when typed is the last two groups of the key. People type
// it to show that they saved the key.
func (k RecoveryKey) endsWith(typed string) bool {
	groups := strings.Split(k.String(), "-")
	want := strings.Join(groups[len(groups)-2:], "")
	got := lookAlikes.Replace(clean(typed))

	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
