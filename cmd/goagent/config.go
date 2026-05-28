package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type AppConfig struct {
	Global     GlobalConfig     `json:"global"`
	Listener   ListenerConfig   `json:"listener"`
	Cloudflare CloudflareConfig `json:"cloudflare"`
	GPT        GPTConfig        `json:"gpt"`
}

type GlobalConfig struct {
	CacheDir               string `json:"cache_dir"`
	KeyDir                 string `json:"key_dir"`
	ProviderBaseDir        string `json:"provider_base_dir"`
	ShutdownTimeoutSeconds int    `json:"shutdown_timeout_seconds"`
}

type ListenerConfig struct {
	ListenAddr         string `json:"address"`
	DefaultAPIKey      string `json:"default_api_key"`
	DefaultQuoteLength string `json:"default_quote_length"`
}

type GPTConfig struct {
	ServerURL  string `json:"server_url"`
	PrivacyURL string `json:"privacy_url"`
}

func defaultConfig() AppConfig {
	base := mustGoAgentDir()
	return AppConfig{
		Global: GlobalConfig{
			CacheDir:               filepath.Join(base, "cache"),
			KeyDir:                 filepath.Join(base, "keys"),
			ProviderBaseDir:        filepath.Join(base, "providers"),
			ShutdownTimeoutSeconds: 5,
		},
		Listener: ListenerConfig{
			ListenAddr:         "127.0.0.1:8080",
			DefaultAPIKey:      "default",
			DefaultQuoteLength: "short",
		},
		Cloudflare: CloudflareConfig{
			DefaultToken: "default",
			Enabled:      false,
			Mode:         "auto",
			LogLevel:     "info",
		},
		GPT: GPTConfig{},
	}
}

func loadConfig() (AppConfig, error) {
	cfg := defaultConfig()
	path, err := configPath()
	if err != nil {
		return cfg, err
	}

	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(contents, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid config %s: %w", path, err)
	}
	return normalizeConfig(cfg), nil
}

func normalizeConfig(cfg AppConfig) AppConfig {
	defaults := defaultConfig()
	if cfg.Global.CacheDir == "" {
		cfg.Global.CacheDir = defaults.Global.CacheDir
	}
	if cfg.Global.KeyDir == "" {
		cfg.Global.KeyDir = defaults.Global.KeyDir
	}
	if cfg.Global.ProviderBaseDir == "" {
		cfg.Global.ProviderBaseDir = defaults.Global.ProviderBaseDir
	}
	if cfg.Global.ShutdownTimeoutSeconds <= 0 {
		cfg.Global.ShutdownTimeoutSeconds = defaults.Global.ShutdownTimeoutSeconds
	}
	if cfg.Listener.ListenAddr == "" {
		cfg.Listener.ListenAddr = defaults.Listener.ListenAddr
	}
	if cfg.Listener.DefaultAPIKey == "" {
		cfg.Listener.DefaultAPIKey = defaults.Listener.DefaultAPIKey
	}
	if cfg.Listener.DefaultQuoteLength == "" {
		cfg.Listener.DefaultQuoteLength = defaults.Listener.DefaultQuoteLength
	}
	if cfg.Cloudflare.DefaultToken == "" {
		cfg.Cloudflare.DefaultToken = defaults.Cloudflare.DefaultToken
	}
	if cfg.Cloudflare.Mode == "" {
		cfg.Cloudflare.Mode = defaults.Cloudflare.Mode
	}
	if cfg.Cloudflare.LogLevel == "" {
		cfg.Cloudflare.LogLevel = defaults.Cloudflare.LogLevel
	}
	cfg.Global.CacheDir = expandPath(cfg.Global.CacheDir)
	cfg.Global.KeyDir = expandPath(cfg.Global.KeyDir)
	cfg.Global.ProviderBaseDir = expandPath(cfg.Global.ProviderBaseDir)
	cfg.GPT.ServerURL = normalizeSchemaServerURL(cfg.GPT.ServerURL)
	cfg.GPT.PrivacyURL = normalizeSchemaServerURL(cfg.GPT.PrivacyURL)
	return cfg
}

func saveConfig(cfg AppConfig) error {
	cfg = normalizeConfig(cfg)
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	contents, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(contents, '\n'), 0o600)
}

func runConfigCommand(cfg AppConfig, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: GoAgent config show | GoAgent config set <section.key> <value> | GoAgent config reset")
	}
	switch args[0] {
	case "show":
		contents, err := json.MarshalIndent(normalizeConfig(cfg), "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(contents))
		return nil
	case "set":
		if len(args) != 3 {
			return errors.New("usage: GoAgent config set <section.key> <value>")
		}
		updated, err := setConfigValue(cfg, args[1], args[2])
		if err != nil {
			return err
		}
		if err := saveConfig(updated); err != nil {
			return err
		}
		fmt.Printf("set %s=%s\n", args[1], args[2])
		return nil
	case "reset":
		if err := saveConfig(defaultConfig()); err != nil {
			return err
		}
		fmt.Println("config reset")
		return nil
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

func setConfigValue(cfg AppConfig, key, value string) (AppConfig, error) {
	key = normalizeConfigKey(key)
	switch key {
	case "global.cache_dir":
		cfg.Global.CacheDir = expandPath(value)
	case "global.key_dir":
		cfg.Global.KeyDir = expandPath(value)
	case "global.provider_base_dir":
		cfg.Global.ProviderBaseDir = expandPath(value)
	case "global.shutdown_timeout_seconds":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return cfg, err
		}
		if parsed <= 0 {
			return cfg, errors.New("global.shutdown_timeout_seconds must be greater than zero")
		}
		cfg.Global.ShutdownTimeoutSeconds = parsed
	case "listener.address":
		cfg.Listener.ListenAddr = value
	case "listener.default_api_key":
		cfg.Listener.DefaultAPIKey = value
	case "listener.default_quote_length":
		if value != "short" && value != "long" {
			return cfg, errors.New("listener.default_quote_length must be short or long")
		}
		cfg.Listener.DefaultQuoteLength = value
	case "cloudflare.default_token":
		cfg.Cloudflare.DefaultToken = value
	case "cloudflare.enabled":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return cfg, err
		}
		cfg.Cloudflare.Enabled = parsed
	case "cloudflare.mode":
		if value != "auto" && value != "temporary" && value != "authenticated" && value != "disabled" {
			return cfg, errors.New("cloudflare.mode must be auto, temporary, authenticated, or disabled")
		}
		cfg.Cloudflare.Mode = value
	case "cloudflare.log_level":
		cfg.Cloudflare.LogLevel = value
	case "gpt.server_url":
		cfg.GPT.ServerURL = normalizeSchemaServerURL(value)
	case "gpt.privacy_url":
		cfg.GPT.PrivacyURL = normalizeSchemaServerURL(value)
	default:
		return cfg, fmt.Errorf("unknown config key %q", key)
	}
	return normalizeConfig(cfg), nil
}

func normalizeConfigKey(key string) string {
	switch key {
	case "cache_dir", "key_dir", "provider_base_dir", "shutdown_timeout_seconds":
		return "global." + key
	case "address", "default_api_key", "default_quote_length":
		return "listener." + key
	case "default_token", "enabled", "mode", "log_level":
		return "cloudflare." + key
	case "server_url", "privacy_url":
		return "gpt." + key
	default:
		return key
	}
}

func goAgentDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".GoAgent"), nil
}

func mustGoAgentDir() string {
	dir, err := goAgentDir()
	if err != nil {
		return ".GoAgent"
	}
	return dir
}

func configPath() (string, error) {
	base, err := goAgentDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "config.json"), nil
}

func expandPath(path string) string {
	if path == "~" {
		return mustGoAgentDir()
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return path
}
