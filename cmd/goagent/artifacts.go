package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func artifactPath(cfg AppConfig, parts ...string) string {
	items := append([]string{cfg.Global.ArtifactDir}, parts...)
	return filepath.Join(items...)
}

func writeArtifactFile(filename string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return err
	}
	return os.WriteFile(filename, contents, 0o600)
}

func runGPTCreateCommand(cfg AppConfig, args []string) error {
	if len(args) > 2 {
		return errors.New("usage: GoAgent gpt create [server-url] [privacy-url]")
	}

	serverURL := defaultSetupServerURL(cfg)
	if len(args) >= 1 {
		serverURL = normalizeSchemaServerURL(args[0])
	}

	reader := bufio.NewReader(os.Stdin)
	var err error
	if len(args) < 1 {
		serverURL, err = promptForSetupURL(reader, "Server URL", serverURL, true)
		if err != nil {
			return err
		}
	}

	privacyURL := defaultSetupPrivacyURL(cfg, serverURL)
	if len(args) >= 2 {
		privacyURL = normalizeSchemaServerURL(args[1])
	} else if cfg.GPT.PrivacyURL == "" {
		fmt.Fprintf(os.Stderr, "Privacy URL [%s]: using default\n", privacyURL)
	}

	if err := validateSetupURL("server URL", serverURL, true); err != nil {
		return err
	}
	if err := validateSetupURL("privacy URL", privacyURL, true); err != nil {
		return err
	}

	if cfg.GPT.ServerURL != serverURL || cfg.GPT.PrivacyURL != privacyURL {
		cfg.GPT.ServerURL = serverURL
		cfg.GPT.PrivacyURL = privacyURL
		if err := saveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "saved GPT setup URLs to config")
	}

	apiKey, err := readNamedSecret(goagentAPIKeyPath(cfg, cfg.Listener.DefaultAPIKey))
	if err != nil || apiKey == "" {
		apiKey = "<run GoAgent gpt key create and paste the generated X-API-Key here>"
	}

	shellCfg, err := loadShellSchemaConfig(cfg.Global.ProviderBaseDir)
	if err != nil {
		return err
	}
	knowledgeFiles, err := listKnowledgeFiles()
	if err != nil {
		return err
	}

	setup := &bytes.Buffer{}
	writeGPTSetup(setup, serverURL, privacyURL, apiKey, shellCfg, knowledgeFiles)

	schema := &bytes.Buffer{}
	writeGPTActionSchema(schema, serverURL, shellCfg)

	setupPath := artifactPath(cfg, "gpt", "setup.md")
	schemaPath := artifactPath(cfg, "gpt", "action-schema.yaml")
	if err := writeArtifactFile(setupPath, setup.Bytes()); err != nil {
		return err
	}
	if err := writeArtifactFile(schemaPath, schema.Bytes()); err != nil {
		return err
	}

	fmt.Printf("wrote GPT setup artifact: %s\n", setupPath)
	fmt.Printf("wrote GPT action schema artifact: %s\n", schemaPath)
	return nil
}

func runMCPCommand(cfg AppConfig, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: GoAgent mcp create|verify|config")
	}
	switch args[0] {
	case "create":
		if len(args) != 1 {
			return errors.New("usage: GoAgent mcp create")
		}
		return runMCPCreateCommand(cfg)
	case "verify":
		if len(args) != 1 {
			return errors.New("usage: GoAgent mcp verify")
		}
		return runMCPVerifyCommand(cfg)
	case "config":
		return runScopedConfigCommand(cfg, "mcp", args[1:])
	default:
		return fmt.Errorf("unknown mcp command %q", args[0])
	}
}

func runMCPCreateCommand(cfg AppConfig) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"mcpServers": map[string]any{
			"goagent": map[string]any{
				"command": exe,
				"args":    []string{"serve", "mcp"},
			},
		},
	}
	contents, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	contents = append(contents, '\n')

	jsonPath := artifactPath(cfg, "mcp", "client-config.json")
	mdPath := artifactPath(cfg, "mcp", "client-config.md")
	if err := writeArtifactFile(jsonPath, contents); err != nil {
		return err
	}

	var md strings.Builder
	md.WriteString("# GoAgent MCP client config\n\n")
	md.WriteString("Copy this snippet into an MCP-capable client's config.\n\n")
	md.WriteString("```json\n")
	md.Write(contents)
	md.WriteString("```\n")
	if err := writeArtifactFile(mdPath, []byte(md.String())); err != nil {
		return err
	}

	fmt.Printf("wrote MCP client config artifact: %s\n", jsonPath)
	fmt.Printf("wrote MCP client config notes: %s\n", mdPath)
	return nil
}

func runMCPVerifyCommand(cfg AppConfig) error {
	checks := []verifyCheck{}
	jsonPath := artifactPath(cfg, "mcp", "client-config.json")
	mdPath := artifactPath(cfg, "mcp", "client-config.md")
	if _, err := os.Stat(jsonPath); err != nil {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "MCP client config JSON", Detail: err.Error()})
	} else {
		checks = append(checks, verifyCheck{Status: verifyPass, Name: "MCP client config JSON", Detail: jsonPath})
	}
	if _, err := os.Stat(mdPath); err != nil {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "MCP client config markdown", Detail: err.Error()})
	} else {
		checks = append(checks, verifyCheck{Status: verifyPass, Name: "MCP client config markdown", Detail: mdPath})
	}
	printVerifyReport("GoAgent MCP verify", checks)
	if countSkillFailures(checks) > 0 {
		return errors.New("mcp verification failed")
	}
	return nil
}

func runGPTTokenCommand(cfg AppConfig, args []string) error {
	if len(args) == 0 {
		return listSecrets(cfg, "token-", ".token")
	}
	switch args[0] {
	case "create":
		if len(args) > 2 {
			return errors.New("usage: GoAgent gpt token create [name]")
		}
		name := cfg.Cloudflare.DefaultToken
		if len(args) == 2 {
			name = args[1]
		}
		fmt.Fprint(os.Stderr, "Cloudflare tunnel token: ")
		token, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return err
		}
		return createProvidedSecret("Cloudflare tunnel token", func(name string) string { return cloudflareTokenPath(cfg, name) }, name, strings.TrimSpace(token))
	case "rm":
		if len(args) != 2 {
			return errors.New("usage: GoAgent gpt token rm <name>")
		}
		return removeSecret("Cloudflare tunnel token", func(name string) string { return cloudflareTokenPath(cfg, name) }, args[1])
	default:
		return fmt.Errorf("unknown gpt token command %q", args[0])
	}
}

func printVerifyReport(title string, checks []verifyCheck) {
	failures := 0
	warnings := 0
	passed := 0
	fmt.Println(title)
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
		case verifyPass:
			passed++
		}
	}
	fmt.Println()
	fmt.Printf("Summary: %d failed, %d warning(s), %d passed\n", failures, warnings, passed)
}
