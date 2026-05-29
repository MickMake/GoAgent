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
	Serve      ServeConfig      `json:"serve"`
	Listener   ListenerConfig   `json:"listener"`
	Cloudflare CloudflareConfig `json:"cloudflare"`
	GPT        GPTConfig        `json:"gpt"`
}

type GlobalConfig struct {
	CacheDir               string `json:"cache_dir"`
	KeyDir                 string `json:"key_dir"`
	ProviderBaseDir        string `json:"provider_base_dir"`
	ArtifactDir            string `json:"artifact_dir"`
	ShutdownTimeoutSeconds int    `json:"shutdown_timeout_seconds"`
}

type ServeConfig struct {
	GPTEnabled bool `json:"gpt_enabled"`
	MCPEnabled bool `json:"mcp_enabled"`
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
			ArtifactDir:            filepath.Join(base, "artifacts"),
			ShutdownTimeoutSeconds: 5,
		},
		Serve: ServeConfig{
			GPTEnabled: true,
			MCPEnabled: false,
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
			Version:      cloudflaredDefaultVersion,
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
	if cfg.Global.ArtifactDir == "" {
		cfg.Global.ArtifactDir = defaults.Global.ArtifactDir
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
	if cfg.Cloudflare.Version == "" {
		cfg.Cloudflare.Version = defaults.Cloudflare.Version
	}
	cfg.Global.CacheDir = expandPath(cfg.Global.CacheDir)
	cfg.Global.KeyDir = expandPath(cfg.Global.KeyDir)
	cfg.Global.ProviderBaseDir = expandPath(cfg.Global.ProviderBaseDir)
	cfg.Global.ArtifactDir = expandPath(cfg.Global.ArtifactDir)
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
		return printConfig(cfg)
	}
	switch args[0] {
	case "show":
		return printConfig(cfg)
	case "set":
		if len(args) != 3 {
			return errors.New("usage: GoAgent config set <section.key> <value>")
		}
		return runConfigSet(cfg, args[1], args[2])
	case "reset":
		if len(args) > 2 {
			return errors.New("usage: GoAgent config reset [section.key]")
		}
		if len(args) == 2 {
			return runConfigResetKey(cfg, args[1])
		}
		if err := saveConfig(defaultConfig()); err != nil {
			return err
		}
		fmt.Println("config reset")
		return nil
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

func runScopedConfigCommand(cfg AppConfig, scope string, args []string) error {
	if len(args) == 0 {
		return printConfigSection(cfg, scope)
	}
	switch args[0] {
	case "set":
		if len(args) != 3 {
			return fmt.Errorf("usage: GoAgent %s config set <key> <value>", scope)
		}
		return runConfigSet(cfg, scopedConfigKey(scope, args[1]), args[2])
	case "reset":
		if len(args) != 2 {
			return fmt.Errorf("usage: GoAgent %s config reset <key>", scope)
		}
		return runConfigResetKey(cfg, scopedConfigKey(scope, args[1]))
	default:
		return fmt.Errorf("unknown %s config command %q", scope, args[0])
	}
}

func printConfig(cfg AppConfig) error {
	contents, err := json.MarshalIndent(normalizeConfig(cfg), "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(contents))
	return nil
}

func printConfigSection(cfg AppConfig, section string) error {
	cfg = normalizeConfig(cfg)
	var value any
	switch section {
	case "gpt":
		value = cfg.GPT
	case "mcp":
		value = cfg.Serve
	case "skill":
		value = map[string]string{"artifact_dir": filepath.Join(cfg.Global.ArtifactDir, "skill")}
	default:
		return fmt.Errorf("unknown config section %q", section)
	}
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(contents))
	return nil
}

func runConfigSet(cfg AppConfig, key, value string) error {
	updated, err := setConfigValue(cfg, key, value)
	if err != nil {
		return err
	}
	if err := saveConfig(updated); err != nil {
		return err
	}
	fmt.Printf("set %s=%s\n", normalizeConfigKey(key), value)
	return nil
}

func runConfigResetKey(cfg AppConfig, key string) error {
	key = normalizeConfigKey(key)
	defaultValue, err := configDefaultString(defaultConfig(), key)
	if err != nil {
		return err
	}
	return runConfigSet(cfg, key, defaultValue)
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
	case "global.artifact_dir":
		cfg.Global.ArtifactDir = expandPath(value)
	case "global.shutdown_timeout_seconds":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return cfg, err
		}
		if parsed <= 0 {
			return cfg, errors.New("global.shutdown_timeout_seconds must be greater than zero")
		}
		cfg.Global.ShutdownTimeoutSeconds = parsed
	case "serve.gpt_enabled":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return cfg, err
		}
		cfg.Serve.GPTEnabled = parsed
	case "serve.mcp_enabled":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return cfg, err
		}
		cfg.Serve.MCPEnabled = parsed
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
	case "cloudflare.version":
		if strings.TrimSpace(value) == "" {
			return cfg, errors.New("cloudflare.version cannot be empty")
		}
		cfg.Cloudflare.Version = strings.TrimSpace(value)
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
	case "cache_dir", "key_dir", "provider_base_dir", "artifact_dir", "shutdown_timeout_seconds":
		return "global." + key
	case "gpt_enabled", "mcp_enabled":
		return "serve." + key
	case "address", "default_api_key", "default_quote_length":
		return "listener." + key
	case "default_token", "enabled", "mode", "log_level", "version":
		return "cloudflare." + key
	case "server_url", "privacy_url":
		return "gpt." + key
	default:
		return key
	}
}

func scopedConfigKey(scope, key string) string {
	key = strings.TrimSpace(key)
	if strings.Contains(key, ".") {
		return key
	}
	switch scope {
	case "gpt":
		switch key {
		case "server_url", "privacy_url":
			return "gpt." + key
		case "default_api_key", "default_quote_length", "address":
			return "listener." + key
		case "enabled", "mode", "default_token", "log_level", "version":
			return "cloudflare." + key
		}
	case "mcp":
		switch key {
		case "enabled":
			return "serve.mcp_enabled"
		case "gpt_enabled", "mcp_enabled":
			return "serve." + key
		}
	case "skill":
		if key == "artifact_dir" {
			return "global.artifact_dir"
		}
	}
	return normalizeConfigKey(key)
}

func configDefaultString(cfg AppConfig, key string) (string, error) {
	switch normalizeConfigKey(key) {
	case "global.cache_dir":
		return cfg.Global.CacheDir, nil
	case "global.key_dir":
		return cfg.Global.KeyDir, nil
	case "global.provider_base_dir":
		return cfg.Global.ProviderBaseDir, nil
	case "global.artifact_dir":
		return cfg.Global.ArtifactDir, nil
	case "global.shutdown_timeout_seconds":
		return strconv.Itoa(cfg.Global.ShutdownTimeoutSeconds), nil
	case "serve.gpt_enabled":
		return strconv.FormatBool(cfg.Serve.GPTEnabled), nil
	case "serve.mcp_enabled":
		return strconv.FormatBool(cfg.Serve.MCPEnabled), nil
	case "listener.address":
		return cfg.Listener.ListenAddr, nil
	case "listener.default_api_key":
		return cfg.Listener.DefaultAPIKey, nil
	case "listener.default_quote_length":
		return cfg.Listener.DefaultQuoteLength, nil
	case "cloudflare.default_token":
		return cfg.Cloudflare.DefaultToken, nil
	case "cloudflare.enabled":
		return strconv.FormatBool(cfg.Cloudflare.Enabled), nil
	case "cloudflare.mode":
		return cfg.Cloudflare.Mode, nil
	case "cloudflare.log_level":
		return cfg.Cloudflare.LogLevel, nil
	case "cloudflare.version":
		return cfg.Cloudflare.Version, nil
	case "gpt.server_url":
		return cfg.GPT.ServerURL, nil
	case "gpt.privacy_url":
		return cfg.GPT.PrivacyURL, nil
	default:
		return "", fmt.Errorf("unknown config key %q", key)
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
