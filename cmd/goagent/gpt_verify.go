package main

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type verifyStatus string

const (
	verifyPass verifyStatus = "PASS"
	verifyWarn verifyStatus = "WARN"
	verifyFail verifyStatus = "FAIL"
)

type verifyCheck struct {
	Status verifyStatus
	Name   string
	Detail string
}

func runGPTCommand(cfg AppConfig, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: GoAgent gpt create|verify|config|key|token|cloudflared")
	}

	switch args[0] {
	case "create":
		return runGPTCreateCommand(cfg, args[1:])
	case "verify":
		if len(args) != 1 {
			return errors.New("usage: GoAgent gpt verify")
		}
		return runGPTVerifyCommand(cfg)
	case "config":
		return runScopedConfigCommand(cfg, "gpt", args[1:])
	case "key":
		return runGPTKeyCommand(cfg, args[1:])
	case "token":
		return runGPTTokenCommand(cfg, args[1:])
	case "cloudflared":
		return runGPTCloudflaredCommand(cfg, args[1:])
	default:
		return fmt.Errorf("unknown gpt command %q", args[0])
	}
}

func runGPTKeyCommand(cfg AppConfig, args []string) error {
	if len(args) == 0 {
		return listSecrets(cfg, "GoAgent-", ".key")
	}
	switch args[0] {
	case "create":
		if len(args) > 2 {
			return errors.New("usage: GoAgent gpt key create [name]")
		}
		name := cfg.Listener.DefaultAPIKey
		if len(args) == 2 {
			name = args[1]
		}
		return createGeneratedSecret("GoAgent API key", func(name string) string { return goagentAPIKeyPath(cfg, name) }, name, true)
	case "rm":
		if len(args) != 2 {
			return errors.New("usage: GoAgent gpt key rm <name>")
		}
		return removeSecret("GoAgent API key", func(name string) string { return goagentAPIKeyPath(cfg, name) }, args[1])
	default:
		return fmt.Errorf("unknown gpt key command %q", args[0])
	}
}

func runGPTCloudflaredCommand(cfg AppConfig, args []string) error {
	if len(args) != 1 || args[0] != "update" {
		return errors.New("usage: GoAgent gpt cloudflared update")
	}
	path, err := updateCloudflared(cfg)
	if err != nil {
		return err
	}
	fmt.Printf("updated cloudflared: %s\n", path)
	return nil
}

func runGPTVerifyCommand(cfg AppConfig) error {
	checks := verifyGPTConfig(cfg)
	failures := 0
	warnings := 0

	fmt.Println("GoAgent GPT verify")
	fmt.Println()
	for _, check := range checks {
		fmt.Printf("[%s] %s", check.Status, check.Name)
		if check.Detail != "" {
			fmt.Printf(" - %s", check.Detail)
		}
		fmt.Println()
		switch check.Status {
		case verifyFail:
			failures++
		case verifyWarn:
			warnings++
		}
	}

	fmt.Println()
	fmt.Printf("Summary: %d failed, %d warning(s), %d passed\n", failures, warnings, len(checks)-failures-warnings)
	if failures > 0 {
		return fmt.Errorf("gpt verification failed")
	}
	return nil
}

func verifyGPTConfig(cfg AppConfig) []verifyCheck {
	checks := []verifyCheck{}
	serverURL := defaultSetupServerURL(cfg)
	privacyURL := defaultSetupPrivacyURL(cfg, serverURL)
	schemaURL := strings.TrimRight(serverURL, "/") + "/config/schema"

	if err := validateSetupURL("server URL", serverURL, true); err != nil {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "server URL", Detail: err.Error()})
	} else {
		checks = append(checks, verifyCheck{Status: verifyPass, Name: "server URL", Detail: serverURL})
	}

	if err := validateSetupURL("privacy URL", privacyURL, true); err != nil {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "privacy URL", Detail: err.Error()})
	} else {
		checks = append(checks, verifyCheck{Status: verifyPass, Name: "privacy URL", Detail: privacyURL})
	}

	if apiKey, err := readNamedSecret(goagentAPIKeyPath(cfg, cfg.Listener.DefaultAPIKey)); err != nil || apiKey == "" {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "default API key", Detail: fmt.Sprintf("missing key %q; run GoAgent key create", cfg.Listener.DefaultAPIKey)})
	} else {
		checks = append(checks, verifyCheck{Status: verifyPass, Name: "default API key", Detail: fmt.Sprintf("key %q is present", cfg.Listener.DefaultAPIKey)})
	}

	shellCfg, err := loadShellSchemaConfig(cfg.Global.ProviderBaseDir)
	if err != nil {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "shell provider config", Detail: err.Error()})
	} else if len(shellCfg.Endpoints) == 0 {
		checks = append(checks, verifyCheck{Status: verifyWarn, Name: "shell provider config", Detail: "loaded, but no shell endpoints are configured"})
	} else {
		checks = append(checks, verifyCheck{Status: verifyPass, Name: "shell provider config", Detail: fmt.Sprintf("%d endpoint(s) configured", len(shellCfg.Endpoints))})
	}

	if err == nil {
		var schema bytes.Buffer
		writeGPTActionSchema(&schema, serverURL, shellCfg)
		text := schema.String()
		if !strings.Contains(text, "operationId: getGoAgentHealth") || !strings.Contains(text, "ApiKeyAuth") {
			checks = append(checks, verifyCheck{Status: verifyFail, Name: "action schema generation", Detail: "schema missing required health operation or API key auth"})
		} else {
			checks = append(checks, verifyCheck{Status: verifyPass, Name: "action schema generation", Detail: fmt.Sprintf("%d bytes generated", len(text))})
		}
	}

	checks = append(checks, verifyCloudflareConfig(cfg)...)
	checks = append(checks, verifyKnowledgeDir()...)
	checks = append(checks, verifyHTTPReachability("local health endpoint", "http://"+cfg.Listener.ListenAddr+"/health", false))
	checks = append(checks, verifyHTTPReachability("schema URL", schemaURL, true))

	return checks
}

func verifyCloudflareConfig(cfg AppConfig) []verifyCheck {
	checks := []verifyCheck{}
	mode := cfg.Cloudflare.Mode
	if mode == "" {
		mode = "auto"
	}

	switch mode {
	case "auto", "temporary", "authenticated", "disabled":
		checks = append(checks, verifyCheck{Status: verifyPass, Name: "cloudflare mode", Detail: mode})
	default:
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "cloudflare mode", Detail: fmt.Sprintf("invalid mode %q", cfg.Cloudflare.Mode)})
		return checks
	}

	if !cfg.Cloudflare.Enabled || mode == "disabled" {
		checks = append(checks, verifyCheck{Status: verifyWarn, Name: "cloudflare tunnel", Detail: "disabled; Custom GPT Actions need a public HTTPS URL unless using another reachable endpoint"})
		return checks
	}

	if mode == "authenticated" || mode == "auto" {
		token, err := readNamedSecret(cloudflareTokenPath(cfg, cfg.Cloudflare.DefaultToken))
		if mode == "authenticated" && (err != nil || token == "") {
			checks = append(checks, verifyCheck{Status: verifyFail, Name: "cloudflare token", Detail: fmt.Sprintf("authenticated mode requires token %q", cfg.Cloudflare.DefaultToken)})
		} else if mode == "auto" && (err != nil || token == "") {
			checks = append(checks, verifyCheck{Status: verifyWarn, Name: "cloudflare token", Detail: "no token found; auto mode will use a temporary tunnel"})
		} else {
			checks = append(checks, verifyCheck{Status: verifyPass, Name: "cloudflare token", Detail: fmt.Sprintf("token %q is present", cfg.Cloudflare.DefaultToken)})
		}
	}

	return checks
}

func verifyKnowledgeDir() []verifyCheck {
	files, err := listKnowledgeFiles()
	if err != nil {
		return []verifyCheck{{Status: verifyFail, Name: "knowledge directory", Detail: err.Error()}}
	}
	if len(files) == 0 {
		return []verifyCheck{{Status: verifyWarn, Name: "knowledge files", Detail: "none found; this is fine if no GoAgent knowledge files are needed"}}
	}
	return []verifyCheck{{Status: verifyPass, Name: "knowledge files", Detail: fmt.Sprintf("%d file(s) available", len(files))}}
}

func verifyHTTPReachability(name, rawURL string, allowRemote bool) verifyCheck {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return verifyCheck{Status: verifyFail, Name: name, Detail: err.Error()}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return verifyCheck{Status: verifyFail, Name: name, Detail: fmt.Sprintf("unsupported URL scheme %q", parsed.Scheme)}
	}
	if !allowRemote && parsed.Host == "" {
		return verifyCheck{Status: verifyFail, Name: name, Detail: "missing host"}
	}

	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(rawURL)
	if err != nil {
		return verifyCheck{Status: verifyWarn, Name: name, Detail: fmt.Sprintf("not reachable now: %v", err)}
	}
	defer response.Body.Close()

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return verifyCheck{Status: verifyPass, Name: name, Detail: fmt.Sprintf("reachable: %s", response.Status)}
	}
	return verifyCheck{Status: verifyWarn, Name: name, Detail: fmt.Sprintf("reachable but returned %s", response.Status)}
}
