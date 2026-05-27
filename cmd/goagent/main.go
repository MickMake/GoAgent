package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
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

	if len(os.Args) == 1 {
		printHelp()
		return
	}

	command := os.Args[1]
	switch command {
	case "help", "-h", "--help":
		printHelp()
		return
	case "serve":
		if err := runServeCommand(cfg, os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
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
	default:
		printHelp()
		log.Fatalf("unknown command %q", command)
	}
}

func printHelp() {
	fmt.Fprintln(os.Stdout, `GoAgent - minimal local ChatGPT-style agent

Usage:
  GoAgent help
  GoAgent serve
  GoAgent key create [name]
  GoAgent key ls
  GoAgent key rm <name>
  GoAgent token add [name] <token>
  GoAgent token ls
  GoAgent token rm <name>
  GoAgent config show
  GoAgent config set <section.key> <value>
  GoAgent config reset

Examples:
  GoAgent serve
  GoAgent key create
  GoAgent config set listener.listen_addr 127.0.0.1:8080
  GoAgent config show`)
}

func runServeCommand(cfg AppConfig, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("serve does not accept arguments; set runtime options in config")
	}

	apiKey, err := loadAPIKey(cfg)
	if err != nil {
		return err
	}

	tunnelRequested := cfg.Cloudflare.Enabled
	if cfg.Cloudflare.Mode == "disabled" {
		tunnelRequested = false
	}

	return runDaemon(cfg, apiKey, tunnelRequested)
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

	var tunnelCmd *exec.Cmd
	if tunnel {
		cmd, err := startCloudflareTunnel(ctx, cfg)
		if err != nil {
			shutdownServer(server, cfg)
			return fmt.Errorf("cloudflared tunnel failed: %w", err)
		}
		tunnelCmd = cmd
	}

	select {
	case <-ctx.Done():
		log.Println("shutdown requested")
	case err := <-serverErr:
		if err != nil {
			return err
		}
		return nil
	}

	shutdownServer(server, cfg)

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

func shutdownServer(server *http.Server, cfg AppConfig) {
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
}

func health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, Response{Quote: "ok"})
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
