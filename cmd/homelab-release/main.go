// homelab-release makes the signing key and signs release bundles.
//
//	homelab-release keygen -public internal/release/signing.pub | gh secret set HOMELAB_SIGNING_KEY
//	HOMELAB_SIGNING_KEY=... homelab-release sign -version v1.2.3 dist/homelab-v1.2.3-linux-amd64.tar.gz
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"homelab/internal/release"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: homelab-release [keygen|sign]")
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Args[2:])
	case "sign":
		err = sign(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// keygen writes the public key to a file and prints the private key, so it
// can go straight into a secret store without touching the disk.
func keygen(args []string) error {
	flags := flag.NewFlagSet("keygen", flag.ExitOnError)
	publicPath := flags.String("public", "internal/release/signing.pub", "where to write the public key")
	flags.Parse(args)

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}

	if err := os.WriteFile(*publicPath, []byte(base64.StdEncoding.EncodeToString(publicKey)+"\n"), 0o644); err != nil {
		return err
	}

	_, err = fmt.Println(base64.StdEncoding.EncodeToString(privateKey.Seed()))
	return err
}

func sign(args []string) error {
	flags := flag.NewFlagSet("sign", flag.ExitOnError)
	version := flags.String("version", "", "release version, like v1.2.3")
	flags.Parse(args)

	if flags.NArg() != 1 {
		return errors.New("usage: homelab-release sign -version v1.2.3 BUNDLE")
	}
	bundlePath := flags.Arg(0)

	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("HOMELAB_SIGNING_KEY")))
	if err != nil || len(seed) != ed25519.SeedSize {
		return errors.New("HOMELAB_SIGNING_KEY must hold the private key from keygen")
	}
	privateKey := ed25519.NewKeyFromSeed(seed)

	// Refuse to sign with a key that the binaries won't accept.
	if builtIn, err := release.PublicKey(); err != nil || !builtIn.Equal(privateKey.Public()) {
		return errors.New("HOMELAB_SIGNING_KEY does not match internal/release/signing.pub")
	}

	bundle, err := os.Open(bundlePath)
	if err != nil {
		return err
	}
	defer bundle.Close()

	hash, err := release.Hash(bundle)
	if err != nil {
		return err
	}

	signature, err := release.Sign(privateKey, *version, hash)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(signature, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(bundlePath+".sig", append(data, '\n'), 0o644)
}
