package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	markerQuoteEndpoint      = "GOAGENT_QUOTE_ENDPOINT_REACHED"
	markerConfigGetEndpoint  = "GOAGENT_CONFIG_GET_ENDPOINT_REACHED"
	markerConfigPostEndpoint = "GOAGENT_CONFIG_POST_ENDPOINT_REACHED"
)

type AppConfig struct {
	Global     GlobalConfig     `json:"global"`
	Listener   ListenerConfig   `json:"listener"`
	Cloudflare CloudflareConfig `json:"cloudflare"`
}

type GlobalConfig struct {
	CacheDir               string `json:"cache_dir"`
	KeyDir                 string `json:"key_dir"`
	ShutdownTimeoutSeconds int    `json:"shutdown_timeout_seconds"`
}

type ListenerConfig struct {
	ListenAddr         string `json:"listen_addr"`
	DefaultAPIKey      string `json:"default_api_key"`
	DefaultQuoteLength string `json:"default_quote_length"`
}

type CloudflareConfig struct {
	DefaultToken        string `json:"default_token"`
	TunnelEnabled       bool   `json:"tunnel_enabled"`
	TunnelMode          string `json:"tunnel_mode"`
	CloudflaredLogLevel string `json:"cloudflared_log_level"`
}

type Response struct {
	Endpoint      string `json:"endpoint,omitempty"`
	Marker        string `json:"marker,omitempty"`
	Quote         string `json:"quote,omitempty"`
	DefaultLength string `json:"default_length,omitempty"`
	Error         string `json:"error,omitempty"`
}

type ConfigRequest struct {
	DefaultLength string `json:"default_length"`
}

var (
	configMu      sync.RWMutex
	defaultLength = "short"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "key":
			if err := runAPIKeyCommand(cfg, os.Args[2:]); err != nil {
				log.Fatal(err)
			}
			return
		case "token":
			if err := runTokenCommand(cfg, os.Args[2:]); err != nil {
				log.Fatal(err)
			}
			return
		case "config":
			if err := runConfigCommand(cfg, os.Args[2:]); err != nil {
				log.Fatal(err)
			}
			return
		}
	}

	tunnelFlag := flag.Bool("tunnel", false, "auto-download and run cloudflared tunnel")
	listenFlag := flag.String("listen", cfg.Listener.ListenAddr, "HTTP listen address")
	flag.Parse()

	cfg.Listener.ListenAddr = *listenFlag
	setDefaultLength(cfg.Listener.DefaultQuoteLength)

	apiKey, err := loadAPIKey(cfg)
	if err != nil {
		log.Fatal(err)
	}

	tunnelRequested := *tunnelFlag || cfg.Cloudflare.TunnelEnabled
	if cfg.Cloudflare.TunnelMode == "disabled" {
		tunnelRequested = false
	}

	if err := runDaemon(cfg, apiKey, tunnelRequested); err != nil {
		log.Fatal(err)
	}
}

func runDaemon(cfg AppConfig, apiKey string, tunnel bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", health)
	mux.HandleFunc("/quote", requireAPIKey(apiKey, quote))
	mux.HandleFunc("/config", requireAPIKey(apiKey, configEndpoint))

	server := &http.Server{Addr: cfg.Listener.ListenAddr, Handler: mux}

	var tunnelCmd *exec.Cmd
	if tunnel {
		cmd, err := startCloudflareTunnel(ctx, cfg)
		if err != nil {
			return fmt.Errorf("cloudflared tunnel failed: %w", err)
		}
		tunnelCmd = cmd
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("GoAgent listening on %s", cfg.Listener.ListenAddr)
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case <-ctx.Done():
		log.Println("shutdown requested")
	case err := <-serverErr:
		if err != nil {
			return err
		}
		return nil
	}

	shutdownTimeout := time.Duration(cfg.Global.ShutdownTimeoutSeconds) * time.Second
	if shutdownTimeout <= 0 {
		shutdownTimeout = 5 * time.Second
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	} else {
		log.Println("HTTP server stopped")
	}

	if tunnelCmd != nil && tunnelCmd.Process != nil {
		if err := tunnelCmd.Process.Kill(); err != nil {
			log.Printf("cloudflared kill error: %v", err)
		} else {
			log.Println("cloudflared stopped")
		}
		_ = tunnelCmd.Wait()
	}

	return nil
}

func health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, Response{Quote: "ok"})
}

func quote(w http.ResponseWriter, r *http.Request) {
	length := r.URL.Query().Get("length")
	if length == "" {
		length = getDefaultLength()
	}

	args := fortuneArgs(length)
	if args == nil {
		writeJSON(w, http.StatusBadRequest, Response{Error: "use length=short or length=long"})
		return
	}

	out, err := exec.Command("fortune", args...).Output()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, Response{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, Response{
		Endpoint:      "quote",
		Marker:        markerQuoteEndpoint,
		Quote:         strings.TrimSpace(string(out)),
		DefaultLength: getDefaultLength(),
	})
}

func configEndpoint(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, Response{Endpoint: "config", Marker: markerConfigGetEndpoint, DefaultLength: getDefaultLength()})
	case http.MethodPost:
		var req ConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, Response{Error: "invalid JSON body"})
			return
		}
		if fortuneArgs(req.DefaultLength) == nil {
			writeJSON(w, http.StatusBadRequest, Response{Error: "default_length must be short or long"})
			return
		}
		setDefaultLength(req.DefaultLength)
		writeJSON(w, http.StatusOK, Response{Endpoint: "config", Marker: markerConfigPostEndpoint, DefaultLength: getDefaultLength()})
	default:
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, Response{Error: "method not allowed"})
	}
}

func fortuneArgs(length string) []string {
	switch length {
	case "short":
		return []string{"-s"}
	case "long":
		return []string{}
	default:
		return nil
	}
}

func getDefaultLength() string {
	configMu.RLock()
	defer configMu.RUnlock()
	return defaultLength
}

func setDefaultLength(length string) {
	if fortuneArgs(length) == nil {
		length = "short"
	}
	configMu.Lock()
	defer configMu.Unlock()
	defaultLength = length
}

func requireAPIKey(expected string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != expected {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

func defaultConfig() AppConfig {
	base := mustGoAgentDir()
	return AppConfig{
		Global: GlobalConfig{
			CacheDir:               filepath.Join(base, "cache"),
			KeyDir:                 filepath.Join(base, "keys"),
			ShutdownTimeoutSeconds: 5,
		},
		Listener: ListenerConfig{
			ListenAddr:         "127.0.0.1:8080",
			DefaultAPIKey:      "default",
			DefaultQuoteLength: "short",
		},
		Cloudflare: CloudflareConfig{
			DefaultToken:        "default",
			TunnelEnabled:       false,
			TunnelMode:          "auto",
			CloudflaredLogLevel: "info",
		},
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
	if cfg.Cloudflare.TunnelMode == "" {
		cfg.Cloudflare.TunnelMode = defaults.Cloudflare.TunnelMode
	}
	if cfg.Cloudflare.CloudflaredLogLevel == "" {
		cfg.Cloudflare.CloudflaredLogLevel = defaults.Cloudflare.CloudflaredLogLevel
	}
	cfg.Global.CacheDir = expandPath(cfg.Global.CacheDir)
	cfg.Global.KeyDir = expandPath(cfg.Global.KeyDir)
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
	case "global.shutdown_timeout_seconds":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return cfg, err
		}
		if parsed <= 0 {
			return cfg, errors.New("global.shutdown_timeout_seconds must be greater than zero")
		}
		cfg.Global.ShutdownTimeoutSeconds = parsed
	case "listener.listen_addr":
		cfg.Listener.ListenAddr = value
	case "listener.default_api_key":
		cfg.Listener.DefaultAPIKey = value
	case "listener.default_quote_length":
		if fortuneArgs(value) == nil {
			return cfg, errors.New("listener.default_quote_length must be short or long")
		}
		cfg.Listener.DefaultQuoteLength = value
	case "cloudflare.default_token":
		cfg.Cloudflare.DefaultToken = value
	case "cloudflare.tunnel_enabled":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return cfg, err
		}
		cfg.Cloudflare.TunnelEnabled = parsed
	case "cloudflare.tunnel_mode":
		if value != "auto" && value != "temporary" && value != "authenticated" && value != "disabled" {
			return cfg, errors.New("cloudflare.tunnel_mode must be auto, temporary, authenticated, or disabled")
		}
		cfg.Cloudflare.TunnelMode = value
	case "cloudflare.cloudflared_log_level":
		cfg.Cloudflare.CloudflaredLogLevel = value
	default:
		return cfg, fmt.Errorf("unknown config key %q", key)
	}
	return normalizeConfig(cfg), nil
}

func normalizeConfigKey(key string) string {
	switch key {
	case "cache_dir", "key_dir", "shutdown_timeout_seconds":
		return "global." + key
	case "listen_addr", "default_api_key", "default_quote_length":
		return "listener." + key
	case "default_token", "tunnel_enabled", "tunnel_mode", "cloudflared_log_level":
		return "cloudflare." + key
	default:
		return key
	}
}

func loadAPIKey(cfg AppConfig) (string, error) {
	if envKey := strings.TrimSpace(os.Getenv("GOAGENT_API_KEY")); envKey != "" {
		return envKey, nil
	}
	key, err := readNamedSecret(goagentAPIKeyPath(cfg, cfg.Listener.DefaultAPIKey))
	if err == nil && key != "" {
		return key, nil
	}
	return "", fmt.Errorf("GOAGENT_API_KEY not set and %s not found; run: GoAgent key create", goagentAPIKeyPath(cfg, cfg.Listener.DefaultAPIKey))
}

func runAPIKeyCommand(cfg AppConfig, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: GoAgent key create [name] | GoAgent key ls | GoAgent key rm <name>")
	}
	switch args[0] {
	case "create":
		name := "default"
		if len(args) > 1 {
			name = args[1]
		}
		return createGeneratedSecret("GoAgent API key", func(name string) string { return goagentAPIKeyPath(cfg, name) }, name, true)
	case "ls":
		return listSecrets(cfg, "GoAgent-", ".key")
	case "rm":
		if len(args) < 2 {
			return errors.New("usage: GoAgent key rm <name>")
		}
		return removeSecret("GoAgent API key", func(name string) string { return goagentAPIKeyPath(cfg, name) }, args[1])
	default:
		return fmt.Errorf("unknown key command %q", args[0])
	}
}

func runTokenCommand(cfg AppConfig, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: GoAgent token add [name] <token> | GoAgent token ls | GoAgent token rm <name>")
	}
	switch args[0] {
	case "add":
		name := "default"
		var token string
		if len(args) == 2 {
			token = args[1]
		} else if len(args) == 3 {
			name = args[1]
			token = args[2]
		} else {
			return errors.New("usage: GoAgent token add [name] <token>")
		}
		return createProvidedSecret("Cloudflare tunnel token", func(name string) string { return cloudflareTokenPath(cfg, name) }, name, token)
	case "ls":
		return listSecrets(cfg, "token-", ".token")
	case "rm":
		if len(args) < 2 {
			return errors.New("usage: GoAgent token rm <name>")
		}
		return removeSecret("Cloudflare tunnel token", func(name string) string { return cloudflareTokenPath(cfg, name) }, args[1])
	default:
		return fmt.Errorf("unknown token command %q", args[0])
	}
}

func startCloudflareTunnel(ctx context.Context, cfg AppConfig) (*exec.Cmd, error) {
	cloudflaredPath, err := ensureCloudflared(cfg)
	if err != nil {
		return nil, err
	}

	args := []string{"tunnel"}
	if cfg.Cloudflare.CloudflaredLogLevel != "" {
		args = append(args, "--loglevel", cfg.Cloudflare.CloudflaredLogLevel)
	}

	token, tokenErr := readNamedSecret(cloudflareTokenPath(cfg, cfg.Cloudflare.DefaultToken))
	useToken := false
	switch cfg.Cloudflare.TunnelMode {
	case "auto":
		useToken = tokenErr == nil && token != ""
	case "authenticated":
		if tokenErr != nil || token == "" {
			return nil, fmt.Errorf("authenticated tunnel requested but token %q was not found", cfg.Cloudflare.DefaultToken)
		}
		useToken = true
	case "temporary":
		useToken = false
	case "disabled":
		return nil, errors.New("cloudflare.tunnel_mode is disabled")
	default:
		return nil, fmt.Errorf("invalid cloudflare.tunnel_mode %q", cfg.Cloudflare.TunnelMode)
	}

	if useToken {
		args = append(args, "run", "--token", token)
		log.Printf("starting authenticated Cloudflare tunnel using token %q", cfg.Cloudflare.DefaultToken)
	} else {
		args = append(args, "--url", "http://"+cfg.Listener.ListenAddr)
		log.Println("starting temporary Cloudflare tunnel")
	}

	cmd := exec.CommandContext(ctx, cloudflaredPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	log.Printf("cloudflared started with pid %d", cmd.Process.Pid)
	return cmd, nil
}

func createGeneratedSecret(kind string, pathFunc func(string) string, name string, printValue bool) error {
	secret, err := generateAPIKey()
	if err != nil {
		return err
	}
	if err := createProvidedSecret(kind, pathFunc, name, secret); err != nil {
		return err
	}
	if printValue {
		fmt.Printf("X-API-Key: %s\n", secret)
	}
	return nil
}

func createProvidedSecret(kind string, pathFunc func(string) string, name string, secret string) error {
	name, err := cleanSecretName(name)
	if err != nil {
		return err
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return errors.New("secret cannot be empty")
	}
	path := pathFunc(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if fileExists(path) {
		return fmt.Errorf("%s %q already exists: %s", kind, name, path)
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Printf("created %s %q: %s\n", kind, name, path)
	return nil
}

func listSecrets(cfg AppConfig, prefix, suffix string) error {
	entries, err := os.ReadDir(cfg.Global.KeyDir)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("no credentials found")
		return nil
	}
	if err != nil {
		return err
	}
	items := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fileName := entry.Name()
		if strings.HasPrefix(fileName, prefix) && strings.HasSuffix(fileName, suffix) {
			items = append(items, strings.TrimSuffix(strings.TrimPrefix(fileName, prefix), suffix))
		}
	}
	if len(items) == 0 {
		fmt.Println("no credentials found")
		return nil
	}
	sort.Strings(items)
	for _, item := range items {
		fmt.Println(item)
	}
	return nil
}

func removeSecret(kind string, pathFunc func(string) string, name string) error {
	name, err := cleanSecretName(name)
	if err != nil {
		return err
	}
	path := pathFunc(name)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s %q does not exist", kind, name)
		}
		return err
	}
	fmt.Printf("removed %s %q\n", kind, name)
	return nil
}

func readNamedSecret(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(contents)), nil
}

func generateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func cleanSecretName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("credential name cannot be empty")
	}
	if name != filepath.Base(name) {
		return "", errors.New("credential name must not contain path separators")
	}
	if strings.Contains(name, "..") {
		return "", errors.New("credential name must not contain '..'")
	}
	return name, nil
}

func goagentAPIKeyPath(cfg AppConfig, name string) string {
	return filepath.Join(cfg.Global.KeyDir, "GoAgent-"+name+".key")
}

func cloudflareTokenPath(cfg AppConfig, name string) string {
	return filepath.Join(cfg.Global.KeyDir, "token-"+name+".token")
}

func ensureCloudflared(cfg AppConfig) (string, error) {
	if err := os.MkdirAll(cfg.Global.CacheDir, 0o755); err != nil {
		return "", err
	}

	assetName, archive, err := cloudflaredAssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	exeName := "cloudflared"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	destination := filepath.Join(cfg.Global.CacheDir, exeName)
	if fileExists(destination) {
		log.Printf("using cached cloudflared: %s", destination)
		return destination, nil
	}

	downloadURL := fmt.Sprintf("https://github.com/cloudflare/cloudflared/releases/latest/download/%s", assetName)
	log.Printf("downloading cloudflared from %s", downloadURL)
	if archive {
		if err := downloadAndExtractCloudflared(downloadURL, destination); err != nil {
			return "", err
		}
	} else {
		if err := downloadFile(downloadURL, destination); err != nil {
			return "", err
		}
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(destination, 0o755); err != nil {
			return "", err
		}
	}
	return destination, nil
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

func cloudflaredAssetName(goos, goarch string) (string, bool, error) {
	switch goos {
	case "linux":
		switch goarch {
		case "amd64":
			return "cloudflared-linux-amd64", false, nil
		case "arm64":
			return "cloudflared-linux-arm64", false, nil
		case "386":
			return "cloudflared-linux-386", false, nil
		case "arm":
			return "cloudflared-linux-arm", false, nil
		}
	case "darwin":
		switch goarch {
		case "amd64":
			return "cloudflared-darwin-amd64.tgz", true, nil
		case "arm64":
			return "cloudflared-darwin-arm64.tgz", true, nil
		}
	case "windows":
		switch goarch {
		case "amd64":
			return "cloudflared-windows-amd64.exe", false, nil
		case "386":
			return "cloudflared-windows-386.exe", false, nil
		}
	}
	return "", false, fmt.Errorf("unsupported platform: %s/%s", goos, goarch)
}

func downloadFile(url, destination string) error {
	response, err := http.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", response.Status)
	}
	tempFile := destination + ".tmp"
	out, err := os.Create(tempFile)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, response.Body)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tempFile, destination)
}

func downloadAndExtractCloudflared(url, destination string) error {
	response, err := http.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", response.Status)
	}
	gzipReader, err := gzip.NewReader(response.Body)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != "cloudflared" {
			continue
		}
		tempFile := destination + ".tmp"
		out, err := os.Create(tempFile)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tarReader)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		return os.Rename(tempFile, destination)
	}
	return errors.New("cloudflared binary not found in archive")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
