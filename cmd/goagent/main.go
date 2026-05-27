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
	"strings"
	"sync"
	"time"
)

const (
	markerQuoteEndpoint      = "GOAGENT_QUOTE_ENDPOINT_REACHED"
	markerConfigGetEndpoint  = "GOAGENT_CONFIG_GET_ENDPOINT_REACHED"
	markerConfigPostEndpoint = "GOAGENT_CONFIG_POST_ENDPOINT_REACHED"
)

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
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "key":
			if err := runAPIKeyCommand(os.Args[2:]); err != nil {
				log.Fatal(err)
			}
			return
		case "cloudflare":
			if err := runCloudflareCommand(os.Args[2:]); err != nil {
				log.Fatal(err)
			}
			return
		}
	}

	tunnel := flag.Bool("tunnel", false, "auto-download and run cloudflared tunnel")
	listen := flag.String("listen", defaultListenAddr(), "HTTP listen address")
	flag.Parse()

	apiKey, err := loadAPIKey()
	if err != nil {
		log.Fatal(err)
	}

	if err := runDaemon(*listen, apiKey, *tunnel); err != nil {
		log.Fatal(err)
	}
}

func runDaemon(listenAddr, apiKey string, tunnel bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", health)
	mux.HandleFunc("/quote", requireAPIKey(apiKey, quote))
	mux.HandleFunc("/config", requireAPIKey(apiKey, config))

	server := &http.Server{Addr: listenAddr, Handler: mux}

	var tunnelCmd *exec.Cmd
	if tunnel {
		cmd, err := startCloudflareTunnel(ctx, listenAddr)
		if err != nil {
			return fmt.Errorf("cloudflared tunnel failed: %w", err)
		}
		tunnelCmd = cmd
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("GoAgent listening on %s", listenAddr)
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

func defaultListenAddr() string {
	if value := strings.TrimSpace(os.Getenv("GOAGENT_LISTEN_ADDR")); value != "" {
		return value
	}
	return "127.0.0.1:8080"
}

func health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, Response{Quote: "ok"})
}

func quote(w http.ResponseWriter, r *http.Request) {
	length := r.URL.Query().Get("length")
	if length == "" {
		length = getDefaultLength()
	}

	args := fortuneArgs(length)
	if args == nil {
		writeJSON(w, 400, Response{Error: "use length=short or length=long"})
		return
	}

	out, err := exec.Command("fortune", args...).Output()
	if err != nil {
		writeJSON(w, 500, Response{Error: err.Error()})
		return
	}

	writeJSON(w, 200, Response{
		Endpoint:      "quote",
		Marker:        markerQuoteEndpoint,
		Quote:         strings.TrimSpace(string(out)),
		DefaultLength: getDefaultLength(),
	})
}

func config(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, Response{Endpoint: "config", Marker: markerConfigGetEndpoint, DefaultLength: getDefaultLength()})
	case http.MethodPost:
		var req ConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, Response{Error: "invalid JSON body"})
			return
		}
		if fortuneArgs(req.DefaultLength) == nil {
			writeJSON(w, 400, Response{Error: "default_length must be short or long"})
			return
		}
		setDefaultLength(req.DefaultLength)
		writeJSON(w, 200, Response{Endpoint: "config", Marker: markerConfigPostEndpoint, DefaultLength: getDefaultLength()})
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

func loadAPIKey() (string, error) {
	if envKey := strings.TrimSpace(os.Getenv("GOAGENT_API_KEY")); envKey != "" {
		return envKey, nil
	}
	key, err := readNamedSecret(goagentAPIKeyPath("default"))
	if err == nil && key != "" {
		return key, nil
	}
	return "", errors.New("GOAGENT_API_KEY not set and ~/.GoAgent/keys/goagent-default.key not found; run: goagent key create")
}

func runAPIKeyCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: goagent key create [name] | goagent key ls | goagent key rm <name>")
	}
	switch args[0] {
	case "create":
		name := "default"
		if len(args) > 1 {
			name = args[1]
		}
		return createGeneratedSecret("goagent", goagentAPIKeyPath, name, true)
	case "ls":
		return listSecrets("goagent", ".key")
	case "rm":
		if len(args) < 2 {
			return errors.New("usage: goagent key rm <name>")
		}
		return removeSecret("goagent", goagentAPIKeyPath, args[1])
	default:
		return fmt.Errorf("unknown key command %q", args[0])
	}
}

func runCloudflareCommand(args []string) error {
	if len(args) < 1 || args[0] != "token" {
		return errors.New("usage: goagent cloudflare token add [name] <token> | goagent cloudflare token ls | goagent cloudflare token rm <name>")
	}
	if len(args) < 2 {
		return errors.New("usage: goagent cloudflare token add [name] <token> | goagent cloudflare token ls | goagent cloudflare token rm <name>")
	}
	switch args[1] {
	case "add":
		name := "default"
		var token string
		if len(args) == 3 {
			token = args[2]
		} else if len(args) == 4 {
			name = args[2]
			token = args[3]
		} else {
			return errors.New("usage: goagent cloudflare token add [name] <token>")
		}
		return createProvidedSecret("cloudflare", cloudflareTokenPath, name, token)
	case "ls":
		return listSecrets("cloudflare", ".token")
	case "rm":
		if len(args) < 3 {
			return errors.New("usage: goagent cloudflare token rm <name>")
		}
		return removeSecret("cloudflare", cloudflareTokenPath, args[2])
	default:
		return fmt.Errorf("unknown cloudflare token command %q", args[1])
	}
}

func startCloudflareTunnel(ctx context.Context, listenAddr string) (*exec.Cmd, error) {
	cloudflaredPath, err := ensureCloudflared()
	if err != nil {
		return nil, err
	}

	args := []string{"tunnel"}
	if token, err := readNamedSecret(cloudflareTokenPath("default")); err == nil && token != "" {
		args = append(args, "run", "--token", token)
		log.Println("starting named Cloudflare tunnel using default token")
	} else {
		args = append(args, "--url", "http://"+listenAddr)
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
	keysDir, err := goagentKeysDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(keysDir, 0o700); err != nil {
		return err
	}
	path := pathFunc(name)
	if fileExists(path) {
		return fmt.Errorf("%s credential %q already exists: %s", kind, name, path)
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Printf("created %s credential %q: %s\n", kind, name, path)
	return nil
}

func listSecrets(prefix, suffix string) error {
	keysDir, err := goagentKeysDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(keysDir)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("no credentials found")
		return nil
	}
	if err != nil {
		return err
	}
	needlePrefix := prefix + "-"
	items := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fileName := entry.Name()
		if strings.HasPrefix(fileName, needlePrefix) && strings.HasSuffix(fileName, suffix) {
			name := strings.TrimSuffix(strings.TrimPrefix(fileName, needlePrefix), suffix)
			items = append(items, name)
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
			return fmt.Errorf("%s credential %q does not exist", kind, name)
		}
		return err
	}
	fmt.Printf("removed %s credential %q\n", kind, name)
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

func goagentAPIKeyPath(name string) string {
	return filepath.Join(mustKeysDir(), "goagent-"+name+".key")
}

func cloudflareTokenPath(name string) string {
	return filepath.Join(mustKeysDir(), "cloudflare-"+name+".token")
}

func mustKeysDir() string {
	keysDir, err := goagentKeysDir()
	if err != nil {
		return "."
	}
	return keysDir
}

func ensureCloudflared() (string, error) {
	cacheDir, err := goagentCacheDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
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
	destination := filepath.Join(cacheDir, exeName)
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

func goagentDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".GoAgent"), nil
}

func goagentCacheDir() (string, error) {
	base, err := goagentDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "cache"), nil
}

func goagentKeysDir() (string, error) {
	base, err := goagentDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "keys"), nil
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
