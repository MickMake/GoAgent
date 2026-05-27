package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

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
