package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type LastAPI struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

func WriteLastAPI(appName, host string, port int) error {
	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir = filepath.Join(dir, appName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	b, err := json.MarshalIndent(LastAPI{Host: host, Port: port}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "last_api.json"), b, 0o644)
}

