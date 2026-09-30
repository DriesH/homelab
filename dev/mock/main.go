// Command mock runs a fake Proxmox API and a fake host agent, so you can run
// Homelab on your own computer. Start it with `make dev`.
//
//	go run ./dev/mock        start the fakes and create a dev admin
//	go run ./dev/mock code   print the current login code
package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"homelab/internal/auth"
)

const (
	stateDir     = ".dev"
	proxmoxAddr  = "127.0.0.1:18006"
	devUsername  = "admin"
	devPassword  = "homelab-dev-password"
	agentSocket  = ".dev/agent.sock"
	dataDir      = ".dev/data"
	totpPeriod   = 30
	totpDigitMod = 1_000_000
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "code" {
		admin, err := auth.LoadAdmin(auth.AdminPath(dataDir))
		if err != nil {
			log.Fatalf("no dev admin yet, run `make dev` first: %v", err)
		}
		fmt.Println(totp(admin.TOTPSecret, time.Now()))
		return
	}

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		log.Fatal(err)
	}
	secret, err := ensureAdmin()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf(`
  Fake Proxmox on http://%s and a fake host agent on %s.
  Log in with %s / %s.
  Login code: run "make dev-code", or add this secret to an authenticator app: %s

`, proxmoxAddr, agentSocket, devUsername, devPassword, secret)

	go func() { log.Fatal(http.ListenAndServe(proxmoxAddr, newProxmox())) }()
	log.Fatal(serveAgent(agentSocket))
}

// ensureAdmin creates the dev admin once and returns its TOTP secret.
func ensureAdmin() (string, error) {
	path := auth.AdminPath(dataDir)
	if admin, err := auth.LoadAdmin(path); err == nil {
		return admin.TOTPSecret, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	hash, err := auth.HashPassword(devPassword)
	if err != nil {
		return "", err
	}
	key := make([]byte, 20)
	rand.Read(key)
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)

	return secret, auth.SaveAdmin(path, auth.Admin{Username: devUsername, PasswordHash: hash, TOTPSecret: secret})
}

// totp is RFC 6238 with SHA-1, 6 digits and 30 seconds, like the manager.
func totp(secret string, now time.Time) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		log.Fatal(err)
	}

	message := make([]byte, 8)
	binary.BigEndian.PutUint64(message, uint64(now.Unix()/totpPeriod))
	mac := hmac.New(sha1.New, key)
	mac.Write(message)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f

	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[offset:offset+4])&0x7fffffff)%totpDigitMod)
}

// statePath is where the fakes keep what you change, like the backup job.
func statePath(name string) string {
	return filepath.Join(stateDir, name)
}
