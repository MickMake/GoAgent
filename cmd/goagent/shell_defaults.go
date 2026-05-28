package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type defaultShellConfig struct {
	Endpoints map[string]defaultShellEndpoint `json:"endpoints"`
}

type defaultShellEndpoint struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Chroot  string   `json:"chroot,omitempty"`
}

func init() {
	seedDefaultShellProviderConfig()
}

func seedDefaultShellProviderConfig() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	path := filepath.Join(home, ".GoAgent", "providers", "shell", "config.json")
	if fileExists(path) {
		return
	}

	cfg := defaultShellConfig{
		Endpoints: map[string]defaultShellEndpoint{
			"os-version": {
				Command: "/usr/bin/uname",
				Args:    []string{"-v"},
			},
		},
	}

	contents, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, append(contents, '\n'), 0o600)
}
