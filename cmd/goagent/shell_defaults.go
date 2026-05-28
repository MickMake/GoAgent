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
	Command              string   `json:"command"`
	Args                 []string `json:"args"`
	Chroot               string   `json:"chroot,omitempty"`
	Description          string   `json:"description,omitempty"`
	Instruction          string   `json:"instruction,omitempty"`
	ConversationStarters []string `json:"conversation_starters,omitempty"`
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
				Command:              "/usr/bin/uname",
				Args:                 []string{"-v"},
				Description:          "Return the operating system version string from uname -v.",
				Instruction:          "When the user asks for the local operating system version, call runShellOsVersion.",
				ConversationStarters: []string{"GoAgent, what OS version is this running on?"},
			},
			"upper": {
				Command:              "/usr/bin/awk",
				Args:                 []string{"BEGIN { print toupper(ARGV[1]); exit }", "$text"},
				Description:          "Uppercase supplied text using a fixed awk program.",
				Instruction:          "When the user asks to uppercase text, call runShellUpper with the text parameter set to only the text to transform. Do not pass commands, awk programs, file paths, flags, or shell syntax.",
				ConversationStarters: []string{"GoAgent, uppercase this text: measure twice, cut once"},
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
