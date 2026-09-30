package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Admin struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
	TOTPSecret   string `json:"totp_secret"`
}

func AdminPath(dataDir string) string {
	return filepath.Join(dataDir, "admin.json")
}

func LoadAdmin(path string) (Admin, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Admin{}, err
	}

	var admin Admin
	if err := json.Unmarshal(data, &admin); err != nil {
		return Admin{}, err
	}

	return admin, nil
}

func SaveAdmin(path string, admin Admin) error {
	data, err := json.MarshalIndent(admin, "", "  ")
	if err != nil {
		return err
	}

	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}

	return os.Rename(temp, path)
}
