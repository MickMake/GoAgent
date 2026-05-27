package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

type CloudflareConfig struct {
	DefaultToken string `json:"default_token"`
	Enabled      bool   `json:"enabled"`
	Mode         string `json:"mode"`
	LogLevel     string `json:"log_level"`
}

const cloudflaredCatalinaVersion = "2025.6.0"

var cloudflareTunnelURLPattern = regexp.MustCompile(`https://[-a-zA-Z0-9]+\.trycloudflare\.com`)

func ensureCloudflared(cfg AppConfig) (string, error) {
	if err := os.MkdirAll(cfg.Global.CacheDir, 0o755); err != nil {
		return "", err
	}

	assetName, archive, err := cloudflaredAssetName(runtime.GOOS, effectiveCloudflaredArch(runtime.GOOS, runtime.GOARCH))
	if err != nil {
		return "", err
	}
	exeName := "cloudflared"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	destination := filepath.Join(cfg.Global.CacheDir, exeName)
	if fileExists(destination) {
		if err := validateCloudflared(destination); err == nil {
			log.Printf("using cached cloudflared: %s", destination)
			return destination, nil
		} else {
			log.Printf("cached cloudflared failed validation and will be replaced: %v", err)
			if removeErr := os.Remove(destination); removeErr != nil {
				return "", fmt.Errorf("remove invalid cached cloudflared: %w", removeErr)
			}
		}
	}

	downloadURL := cloudflaredDownloadURL(runtime.GOOS, assetName)
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
	if err := validateCloudflared(destination); err != nil {
		_ = os.Remove(destination)
		return "", fmt.Errorf("downloaded cloudflared failed validation: %w", err)
	}
	return destination, nil
}

func cloudflaredDownloadURL(goos, assetName string) string {
	if isMacOSCatalina(goos) {
		log.Printf("macOS Catalina detected; using cloudflared %s", cloudflaredCatalinaVersion)
		return fmt.Sprintf("https://github.com/cloudflare/cloudflared/releases/download/%s/%s", cloudflaredCatalinaVersion, assetName)
	}
	return fmt.Sprintf("https://github.com/cloudflare/cloudflared/releases/latest/download/%s", assetName)
}

func isMacOSCatalina(goos string) bool {
	if goos != "darwin" {
		return false
	}
	if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
		version := strings.TrimSpace(string(out))
		if strings.HasPrefix(version, "10.15.") || version == "10.15" {
			return true
		}
	}
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		return strings.HasPrefix(strings.TrimSpace(string(out)), "19.")
	}
	return false
}

func effectiveCloudflaredArch(goos, goarch string) string {
	if goos != "darwin" {
		return goarch
	}
	out, err := exec.Command("uname", "-m").Output()
	if err != nil {
		return goarch
	}
	switch strings.TrimSpace(string(out)) {
	case "arm64", "aarch64":
		return "arm64"
	case "x86_64", "amd64":
		return "amd64"
	default:
		return goarch
	}
}

func validateCloudflared(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("%s --version failed: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	if strings.TrimSpace(string(out)) == "" {
		return fmt.Errorf("%s --version returned empty output", path)
	}
	return nil
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
	if cfg.Cloudflare.LogLevel != "" {
		args = append(args, "--loglevel", cfg.Cloudflare.LogLevel)
	}

	token, tokenErr := readNamedSecret(cloudflareTokenPath(cfg, cfg.Cloudflare.DefaultToken))
	useToken := false
	switch cfg.Cloudflare.Mode {
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
		return nil, fmt.Errorf("invalid cloudflare.tunnel_mode %q", cfg.Cloudflare.Mode)
	}

	if useToken {
		args = append(args, "run", "--token", token)
		log.Printf("starting authenticated Cloudflare tunnel using token %q", cfg.Cloudflare.DefaultToken)
	} else {
		args = append(args, "--url", "http://"+cfg.Listener.ListenAddr)
		log.Println("starting temporary Cloudflare tunnel")
	}

	cmd := exec.CommandContext(ctx, cloudflaredPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	urlOnce := &sync.Once{}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go relayCloudflaredOutput(stdout, os.Stdout, urlOnce)
	go relayCloudflaredOutput(stderr, os.Stderr, urlOnce)
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("cloudflared exited: %v", err)
		}
	}()
	log.Printf("cloudflared started with pid %d", cmd.Process.Pid)
	return cmd, nil
}

func relayCloudflaredOutput(reader io.Reader, writer io.Writer, urlOnce *sync.Once) {
	buffer := make([]byte, 1024)
	seen := ""
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			chunk := string(buffer[:n])
			_, _ = writer.Write(buffer[:n])
			seen += chunk
			if len(seen) > 8192 {
				seen = seen[len(seen)-8192:]
			}
			if url := cloudflareTunnelURLPattern.FindString(seen); url != "" {
				urlOnce.Do(func() {
					log.Printf("Cloudflare tunnel URL: %s", strings.TrimSpace(url))
				})
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("cloudflared output read error: %v", err)
			}
			return
		}
	}
}

func cloudflareTokenPath(cfg AppConfig, name string) string {
	return filepath.Join(cfg.Global.KeyDir, "token-"+name+".token")
}
