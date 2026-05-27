package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

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
