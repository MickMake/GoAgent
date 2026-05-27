package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
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
	"time"

	"github.com/MickMake/GoAgent/providers/fortune"
	"github.com/MickMake/GoAgent/providers/shell"
)

type Response struct {
	Quote string `json:"quote,omitempty"`
	Error string `json:"error,omitempty"`
}

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

	protect := func(next http.HandlerFunc) http.HandlerFunc {
		return requireAPIKey(apiKey, next)
	}

	fortune.Register(mux, protect, cfg.Listener.DefaultQuoteLength)
	if err := shell.Register(mux, protect, cfg.Global.ProviderBaseDir); err != nil {
		return fmt.Errorf("shell provider failed: %w", err)
	}

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

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

type CloudflareConfig struct {
	DefaultToken        string `json:"default_token"`
	TunnelEnabled       bool   `json:"tunnel_enabled"`
	TunnelMode          string `json:"tunnel_mode"`
	CloudflaredLogLevel string `json:"cloudflared_log_level"`
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
