package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/MickMake/GoAgent/providers/fortune"
	"github.com/MickMake/GoAgent/providers/shell"
)

const GoAgentVersion = "0.1.0"

type Response struct {
	Quote string `json:"quote,omitempty"`
	Error string `json:"error,omitempty"`
}

type VersionResponse struct {
	Service string `json:"service"`
	Version string `json:"version"`
}

type RootResponse struct {
	Service   string   `json:"service"`
	Status    string   `json:"status"`
	Version   string   `json:"version"`
	Endpoints []string `json:"endpoints"`
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
	case "setup":
		if err := runSetupCommand(cfg, os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	case "gpt":
		if err := runGPTCommand(cfg, os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	case "skill":
		if err := runSkillCommand(cfg, os.Args[2:]); err != nil {
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
	case "cloudflared":
		if err := runCloudflaredCommand(cfg, os.Args[2:]); err != nil {
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
  GoAgent serve gpt
  GoAgent serve mcp
  GoAgent setup [server-url] [privacy-url]
  GoAgent gpt verify
  GoAgent skill create
  GoAgent skill verify
  GoAgent key create [name]
  GoAgent key ls
  GoAgent key rm <name>
  GoAgent token add [name] <token>
  GoAgent token ls
  GoAgent token rm <name>
  GoAgent cloudflared update
  GoAgent config show
  GoAgent config set <section.key> <value>
  GoAgent config reset

Examples:
  GoAgent serve
  GoAgent serve gpt
  GoAgent serve mcp
  GoAgent setup https://example.trycloudflare.com
  GoAgent setup https://example.trycloudflare.com https://example.com/privacy
  GoAgent gpt verify
  GoAgent skill create
  GoAgent skill verify
  GoAgent key create
  GoAgent cloudflared update
  GoAgent config set serve.mcp_enabled true
  GoAgent config set listener.address 127.0.0.1:8080
  GoAgent config show`)
}

func runServeCommand(cfg AppConfig, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: GoAgent serve [gpt|mcp]")
	}
	if len(args) == 1 {
		switch args[0] {
		case "gpt":
			return runGPTServeCommand(cfg)
		case "mcp":
			return runMCPServeCommand(cfg)
		default:
			return fmt.Errorf("unknown serve target %q; use gpt or mcp", args[0])
		}
	}

	if !cfg.Serve.GPTEnabled && !cfg.Serve.MCPEnabled {
		return fmt.Errorf("nothing to serve; enable serve.gpt_enabled or serve.mcp_enabled in config")
	}
	if cfg.Serve.GPTEnabled && cfg.Serve.MCPEnabled {
		errCh := make(chan error, 1)
		go func() {
			errCh <- runGPTServeCommand(cfg)
		}()
		select {
		case err := <-errCh:
			return err
		case <-time.After(100 * time.Millisecond):
		}
		return runMCPServeCommand(cfg)
	}
	if cfg.Serve.MCPEnabled {
		return runMCPServeCommand(cfg)
	}
	return runGPTServeCommand(cfg)
}

func runGPTServeCommand(cfg AppConfig) error {
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("/", root)
	mux.HandleFunc("/health", health)
	mux.HandleFunc("/version", version)
	mux.HandleFunc("/config/schema", configSchemaHandler(cfg))
	mux.HandleFunc("/config/privacy", configPrivacyHandler())
	mux.HandleFunc("/config/knowledge/", knowledgeHandler(apiKey))

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
		log.Printf("GoAgent GPT Actions listening on %s", cfg.Listener.ListenAddr)
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
	stopCloudflared(tunnelCmd)

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

func stopCloudflared(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		log.Printf("cloudflared interrupt error: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			log.Printf("cloudflared stopped: %v", err)
		} else {
			log.Println("cloudflared stopped")
		}
	case <-time.After(3 * time.Second):
		log.Println("cloudflared did not stop after interrupt; killing")
		if err := cmd.Process.Kill(); err != nil {
			log.Printf("cloudflared kill error: %v", err)
		}
		if err := <-done; err != nil {
			log.Printf("cloudflared stopped: %v", err)
		} else {
			log.Println("cloudflared stopped")
		}
	}
}

func root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	writeRootJSON(w, http.StatusOK, RootResponse{
		Service: "GoAgent",
		Status:  "ok",
		Version: GoAgentVersion,
		Endpoints: []string{
			"/",
			"/health",
			"/version",
			"/config/schema",
			"/config/privacy",
			"/config/knowledge/{filename}",
			"/fortune",
			"/fortune/config",
			"/shell/{name}",
		},
	})
}

func health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, Response{Quote: "ok"})
}

func version(w http.ResponseWriter, r *http.Request) {
	writeVersionJSON(w, http.StatusOK, VersionResponse{Service: "GoAgent", Version: GoAgentVersion})
}

func writeRootJSON(w http.ResponseWriter, status int, payload RootResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}

func writeVersionJSON(w http.ResponseWriter, status int, payload VersionResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}
