package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	connectorName        = "goagent"
	connectorDisplayName = "GoAgent"
	connectorAuthHeader  = "X-API-Key"
)

type connectorManifest struct {
	Name        string            `json:"name"`
	DisplayName string            `json:"display_name"`
	Description string            `json:"description"`
	Transport   string            `json:"transport"`
	ServerURL   string            `json:"server_url"`
	SchemaURL   string            `json:"schema_url"`
	Auth        connectorAuth     `json:"auth"`
	Artifacts   map[string]string `json:"artifacts"`
	Notes       []string          `json:"notes"`
}

type connectorAuth struct {
	Type string `json:"type"`
	In   string `json:"in"`
	Name string `json:"name"`
}

func runConnectorCommand(cfg AppConfig, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: GoAgent connector create|verify|config")
	}
	switch args[0] {
	case "create":
		if len(args) != 1 {
			return errors.New("usage: GoAgent connector create")
		}
		return runConnectorCreateCommand(cfg)
	case "verify":
		if len(args) != 1 {
			return errors.New("usage: GoAgent connector verify")
		}
		return runConnectorVerifyCommand(cfg)
	case "config":
		return runConnectorConfigCommand(cfg, args[1:])
	default:
		return fmt.Errorf("unknown connector command %q", args[0])
	}
}

func runConnectorCreateCommand(cfg AppConfig) error {
	serverURL := defaultSetupServerURL(cfg)
	if err := validateSetupURL("server URL", serverURL, true); err != nil {
		return err
	}
	shellCfg, err := loadShellSchemaConfig(cfg.Global.ProviderBaseDir)
	if err != nil {
		return err
	}
	schema := &bytes.Buffer{}
	writeGPTActionSchema(schema, serverURL, shellCfg)
	manifest := newConnectorManifest(cfg, serverURL)
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	manifestBytes = append(manifestBytes, '\n')
	guide := &bytes.Buffer{}
	writeConnectorGuide(guide, manifest)
	manifestPath := artifactPath(cfg, "connector", "connector.json")
	guidePath := artifactPath(cfg, "connector", "connector.md")
	schemaPath := artifactPath(cfg, "connector", "openapi.yaml")
	if err := writeArtifactFile(manifestPath, manifestBytes); err != nil {
		return err
	}
	if err := writeArtifactFile(guidePath, guide.Bytes()); err != nil {
		return err
	}
	if err := writeArtifactFile(schemaPath, schema.Bytes()); err != nil {
		return err
	}
	fmt.Printf("wrote connector manifest artifact: %s\n", manifestPath)
	fmt.Printf("wrote connector guide artifact: %s\n", guidePath)
	fmt.Printf("wrote connector OpenAPI artifact: %s\n", schemaPath)
	return nil
}

func runConnectorVerifyCommand(cfg AppConfig) error {
	checks := []verifyCheck{}
	manifestPath := artifactPath(cfg, "connector", "connector.json")
	guidePath := artifactPath(cfg, "connector", "connector.md")
	schemaPath := artifactPath(cfg, "connector", "openapi.yaml")
	if contents, err := os.ReadFile(manifestPath); err != nil {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "connector manifest", Detail: err.Error()})
	} else {
		var manifest connectorManifest
		if err := json.Unmarshal(contents, &manifest); err != nil {
			checks = append(checks, verifyCheck{Status: verifyFail, Name: "connector manifest JSON", Detail: err.Error()})
		} else if manifest.Transport != "https" || manifest.Auth.Name != connectorAuthHeader {
			checks = append(checks, verifyCheck{Status: verifyFail, Name: "connector manifest shape", Detail: "expected https transport with X-API-Key auth"})
		} else {
			checks = append(checks, verifyCheck{Status: verifyPass, Name: "connector manifest", Detail: manifestPath})
		}
	}
	if _, err := os.Stat(guidePath); err != nil {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "connector guide", Detail: err.Error()})
	} else {
		checks = append(checks, verifyCheck{Status: verifyPass, Name: "connector guide", Detail: guidePath})
	}
	if contents, err := os.ReadFile(schemaPath); err != nil {
		checks = append(checks, verifyCheck{Status: verifyFail, Name: "connector OpenAPI schema", Detail: err.Error()})
	} else {
		text := string(contents)
		if !strings.Contains(text, "openapi: 3.1.0") || !strings.Contains(text, connectorAuthHeader) {
			checks = append(checks, verifyCheck{Status: verifyFail, Name: "connector OpenAPI schema sanity", Detail: "expected OpenAPI 3.1 schema with X-API-Key auth"})
		} else {
			checks = append(checks, verifyCheck{Status: verifyPass, Name: "connector OpenAPI schema", Detail: schemaPath})
		}
	}
	printVerifyReport("GoAgent connector verify", checks)
	if countVerifyFailures(checks) > 0 {
		return errors.New("connector verification failed")
	}
	return nil
}

func runConnectorConfigCommand(cfg AppConfig, args []string) error {
	if len(args) == 0 {
		contents, err := json.MarshalIndent(effectiveConnectorConfig(cfg), "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(contents))
		return nil
	}
	switch args[0] {
	case "set":
		if len(args) != 3 {
			return errors.New("usage: GoAgent connector config set <key> <value>")
		}
		return runConfigSet(cfg, connectorConfigKey(args[1]), args[2])
	case "reset":
		if len(args) != 2 {
			return errors.New("usage: GoAgent connector config reset <key>")
		}
		return runConfigResetKey(cfg, connectorConfigKey(args[1]))
	default:
		return fmt.Errorf("unknown connector config command %q", args[0])
	}
}

func connectorConfigKey(key string) string {
	key = strings.TrimSpace(key)
	if strings.Contains(key, ".") {
		return key
	}
	switch key {
	case "server_url":
		return "gpt.server_url"
	case "address":
		return "listener.address"
	case "artifact_dir":
		return "global.artifact_dir"
	case "provider_base_dir":
		return "global.provider_base_dir"
	default:
		return normalizeConfigKey(key)
	}
}

func effectiveConnectorConfig(cfg AppConfig) map[string]any {
	cfg = normalizeConfig(cfg)
	serverURL := defaultSetupServerURL(cfg)
	return map[string]any{
		"name":         connectorName,
		"display_name": connectorDisplayName,
		"transport":    "https",
		"server_url":   serverURL,
		"schema_url":   connectorSchemaURL(serverURL),
		"auth": map[string]any{"type": "api_key", "in": "header", "name": connectorAuthHeader},
		"artifacts": map[string]any{
			"manifest": artifactPath(cfg, "connector", "connector.json"),
			"guide":    artifactPath(cfg, "connector", "connector.md"),
			"openapi":  artifactPath(cfg, "connector", "openapi.yaml"),
		},
		"providers": map[string]any{"base_dir": cfg.Global.ProviderBaseDir},
	}
}

func newConnectorManifest(cfg AppConfig, serverURL string) connectorManifest {
	return connectorManifest{
		Name:        connectorName,
		DisplayName: connectorDisplayName,
		Description: "Hosted HTTP bridge for a locally running GoAgent service.",
		Transport:   "https",
		ServerURL:   serverURL,
		SchemaURL:   connectorSchemaURL(serverURL),
		Auth: connectorAuth{Type: "api_key", In: "header", Name: connectorAuthHeader},
		Artifacts: map[string]string{
			"openapi": artifactPath(cfg, "connector", "openapi.yaml"),
			"guide":   artifactPath(cfg, "connector", "connector.md"),
		},
		Notes: []string{
			"This is a hosted HTTP tool bridge artifact, not a local stdio MCP client config.",
			"GoAgent must be reachable at server_url for a remote runtime to call it.",
			"Use an API key created with GoAgent gpt key create.",
			"Skills are not a live transport layer.",
			"MCP remains available separately through GoAgent mcp create and GoAgent serve mcp.",
		},
	}
}

func connectorSchemaURL(serverURL string) string {
	return strings.TrimRight(serverURL, "/") + "/config/schema"
}

func writeConnectorGuide(out *bytes.Buffer, manifest connectorManifest) {
	fmt.Fprintln(out, "# GoAgent hosted HTTP connector bridge")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "This artifact describes the existing hosted HTTPS GoAgent HTTP surface for tool runtimes that can register an OpenAPI schema and call it with header API key authentication.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "This is not local stdio MCP. MCP remains available separately through `GoAgent mcp create` and `GoAgent serve mcp`.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "## Requirements")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "- GoAgent must be running with the GPT HTTP server enabled, for example `GoAgent serve gpt` or config-driven `GoAgent serve`.")
	fmt.Fprintf(out, "- GoAgent must be reachable at `%s`.\n", manifest.ServerURL)
	fmt.Fprintln(out, "- Remote runtimes need an HTTPS route, such as Cloudflare Tunnel or another hosted HTTPS reverse proxy.")
	fmt.Fprintln(out, "- Protected calls use API key authentication with the `X-API-Key` header.")
	fmt.Fprintln(out, "- Create an API key with `GoAgent gpt key create` and use that value in the connector runtime authentication field.")
	fmt.Fprintln(out, "- Skills are not a live transport layer; Skill artifacts are guidance and reference packaging only.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "## Generated artifacts")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "```text")
	fmt.Fprintln(out, manifest.Artifacts["openapi"])
	fmt.Fprintln(out, manifest.Artifacts["guide"])
	fmt.Fprintln(out, "```")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "## Connector manifest")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "```json")
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	fmt.Fprintln(out, string(manifestBytes))
	fmt.Fprintln(out, "```")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "## OpenAPI schema")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Schema URL: `%s`\n", manifest.SchemaURL)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "The local generated copy is `openapi.yaml`. It is produced from the same schema writer used by GPT Custom GPT Actions, so configured shell endpoints and their `$param` query arguments stay aligned.")
}

func countVerifyFailures(checks []verifyCheck) int {
	failures := 0
	for _, check := range checks {
		if check.Status == verifyFail {
			failures++
		}
	}
	return failures
}
