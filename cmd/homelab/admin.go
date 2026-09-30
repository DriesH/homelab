package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"homelab/internal/auth"
	"homelab/internal/config"
)

const minPasswordLength = 12

// setupAdmin creates or replaces the single admin account.
func setupAdmin() error {
	reader := bufio.NewReader(os.Stdin)

	username, err := prompt(reader, "Username [admin]: ", false)
	if err != nil {
		return err
	}
	if username == "" {
		username = "admin"
	}

	password, err := prompt(reader, fmt.Sprintf("Password (min %d characters): ", minPasswordLength), true)
	if err != nil {
		return err
	}
	if len(password) < minPasswordLength {
		return fmt.Errorf("password must be at least %d characters", minPasswordLength)
	}

	confirmation, err := prompt(reader, "Repeat password: ", true)
	if err != nil {
		return err
	}
	if password != confirmation {
		return errors.New("passwords do not match")
	}

	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		return err
	}

	fmt.Println("\nAdd this account to your authenticator app (1Password, Aegis, Google Authenticator, ...):")
	fmt.Println("  Secret:", secret)
	fmt.Println("  URI:   ", auth.TOTPURI("Homelab", username, secret))

	code, err := prompt(reader, "\nEnter the 6-digit code from the app: ", false)
	if err != nil {
		return err
	}
	if !auth.VerifyTOTP(secret, code) {
		return errors.New("code is not valid, check the time on this server and your phone")
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	path := auth.AdminPath(config.DataDir())
	if err := auth.SaveAdmin(path, auth.Admin{Username: username, PasswordHash: hash, TOTPSecret: secret}); err != nil {
		return err
	}

	fmt.Println("Admin saved to", path, "- restart the service to use it.")

	return nil
}

func prompt(reader *bufio.Reader, label string, secret bool) (string, error) {
	fmt.Print(label)

	if secret && term.IsTerminal(int(os.Stdin.Fd())) {
		value, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		return string(value), err
	}

	value, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(value), nil
}
