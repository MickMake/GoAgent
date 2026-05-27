package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
	tunnel := flag.Bool("tunnel", false, "auto-download and run cloudflared tunnel")
	flag.Parse()

	apiKey := os.Getenv("GOAGENT_API_KEY")
	if apiKey == "" {
		log.Fatal("GOAGENT_API_KEY not set")
	}

	http.HandleFunc("/health", health)
	http.HandleFunc("/quote", requireAPIKey(apiKey, quote))
	http.HandleFunc("/config", requireAPIKey(apiKey, config))

	if *tunnel {
		if err := startCloudflareTunnel("http://127.0.0.1:8080"); err != nil {
			log.Fatalf("cloudflared tunnel failed: %v", err)
		}
	}

	log.Println("GoAgent listening on 127.0.0.1:8080")
	log.Fatal(http.ListenAndServe("127.0.0.1:8080", nil))
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
		writeJSON(w, 200, Response{
			Endpoint:      "config",
			Marker:        markerConfigGetEndpoint,
			DefaultLength: getDefaultLength(),
		})
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
		writeJSON(w, 200, Response{
			Endpoint:      "config",
			Marker:        markerConfigPostEndpoint,
			DefaultLength: getDefaultLength(),
		})
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
		// normal fortune output
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

func startCloudflareTunnel(url string) error {
	cloudflaredPath, err := ensureCloudflared()
	if err != nil {
		return err
	}

	cmd := exec.Command(cloudflaredPath, "tunnel", "--url", url)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	log.Printf("cloudflared started with pid %d", cmd.Process.Pid)
	return nil
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

		if header.Typeflag != tar.TypeReg {
			continue
		}

		if filepath.Base(header.Name) != "cloudflared" {
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
