package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
)

func TestSignAndVerify(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	hash, _ := Hash(strings.NewReader("bundle"))

	signature, err := Sign(privateKey, "v1.2.0", hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyWith(publicKey, signature, hash); err != nil {
		t.Fatalf("valid signature: %v", err)
	}

	otherHash, _ := Hash(strings.NewReader("other bundle"))
	if err := verifyWith(publicKey, signature, otherHash); !errors.Is(err, ErrBadSignature) {
		t.Errorf("other bundle: err = %v", err)
	}

	moved := signature
	moved.Version = "v9.0.0"
	if err := verifyWith(publicKey, moved, hash); !errors.Is(err, ErrBadSignature) {
		t.Errorf("changed version: err = %v", err)
	}

	otherKey, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := verifyWith(otherKey, signature, hash); !errors.Is(err, ErrBadSignature) {
		t.Errorf("other key: err = %v", err)
	}

	if _, err := Sign(privateKey, "latest", hash); err == nil {
		t.Error("signing a version that is not v1.2.3 should fail")
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		version, current string
		want             bool
	}{
		{"v1.2.0", "v1.1.9", true},
		{"v1.10.0", "v1.9.0", true},
		{"v1.2.0", "v1.2.0", false},
		{"v1.1.0", "v1.2.0", false},
		{"v1.2.0", "dev", true},
		{"v1.2.0", "v1.1.0-3-gabcdef-dirty", true},
		{"latest", "v1.0.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.version, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.version, c.current, got, c.want)
		}
	}
}
