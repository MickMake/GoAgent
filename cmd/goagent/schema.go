package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type shellSchemaConfig struct {
	Endpoints map[string]shellSchemaEndpoint `json:"endpoints"`
}

type shellSchemaEndpoint struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Chroot  string   `json:"chroot,omitempty"`
}

func runShowCommand(cfg AppConfig, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: GoAgent show schema [server-url]")
	}

	switch args[0] {
	case "schema":
		return runShowSchemaCommand(cfg, args[1:])
	default:
		return fmt.Errorf("unknown show command %q", args[0])
	}
}

func runShowSchemaCommand(cfg AppConfig, args []string) error {
	if len(args) > 1 {
		return errors.New("usage: GoAgent show schema [server-url]")
	}

	serverURL := "http://" + cfg.Listener.ListenAddr
	if len(args) == 1 {
		serverURL = normalizeSchemaServerURL(args[0])
	}
	if _, err := url.ParseRequestURI(serverURL); err != nil {
		return fmt.Errorf("invalid server URL %q: %w", serverURL, err)
	}

	shellCfg, err := loadShellSchemaConfig(cfg.Global.ProviderBaseDir)
	if err != nil {
		return err
	}

	writeGPTActionSchema(os.Stdout, serverURL, shellCfg)
	return nil
}

func normalizeSchemaServerURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if strings.Contains(raw, "://") {
		return strings.TrimRight(raw, "/")
	}
	return "https://" + strings.TrimRight(raw, "/")
}

func loadShellSchemaConfig(providerBaseDir string) (shellSchemaConfig, error) {
	path := filepath.Join(providerBaseDir, "shell", "config.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return shellSchemaConfig{}, nil
		}
		return shellSchemaConfig{}, err
	}

	var cfg shellSchemaConfig
	if err := json.Unmarshal(contents, &cfg); err != nil {
		return shellSchemaConfig{}, fmt.Errorf("invalid shell provider config %s: %w", path, err)
	}
	return cfg, nil
}

func writeGPTActionSchema(out *os.File, serverURL string, shellCfg shellSchemaConfig) {
	fmt.Fprintln(out, "openapi: 3.1.0")
	fmt.Fprintln(out, "info:")
	fmt.Fprintln(out, "  title: GoAgent")
	fmt.Fprintln(out, "  version: 0.1.0")
	fmt.Fprintln(out, "  description: Local GoAgent actions exposed through an authenticated HTTP listener.")
	fmt.Fprintln(out, "servers:")
	fmt.Fprintf(out, "  - url: %s\n", yamlString(serverURL))
	fmt.Fprintln(out, "security:")
	fmt.Fprintln(out, "  - ApiKeyAuth: []")
	fmt.Fprintln(out, "paths:")
	writeHealthPath(out)
	writeFortunePath(out)
	writeFortuneConfigPath(out)
	writeShellPaths(out, shellCfg)
	fmt.Fprintln(out, "components:")
	fmt.Fprintln(out, "  securitySchemes:")
	fmt.Fprintln(out, "    ApiKeyAuth:")
	fmt.Fprintln(out, "      type: apiKey")
	fmt.Fprintln(out, "      in: header")
	fmt.Fprintln(out, "      name: X-API-Key")
}

func writeHealthPath(out *os.File) {
	fmt.Fprintln(out, "  /health:")
	fmt.Fprintln(out, "    get:")
	fmt.Fprintln(out, "      operationId: health")
	fmt.Fprintln(out, "      summary: Check GoAgent health")
	fmt.Fprintln(out, "      responses:")
	fmt.Fprintln(out, "        '200':")
	fmt.Fprintln(out, "          description: GoAgent is running")
}

func writeFortunePath(out *os.File) {
	fmt.Fprintln(out, "  /fortune:")
	fmt.Fprintln(out, "    get:")
	fmt.Fprintln(out, "      operationId: getFortune")
	fmt.Fprintln(out, "      summary: Get a fortune quote")
	fmt.Fprintln(out, "      parameters:")
	fmt.Fprintln(out, "        - name: length")
	fmt.Fprintln(out, "          in: query")
	fmt.Fprintln(out, "          required: false")
	fmt.Fprintln(out, "          schema:")
	fmt.Fprintln(out, "            type: string")
	fmt.Fprintln(out, "            enum:")
	fmt.Fprintln(out, "              - short")
	fmt.Fprintln(out, "              - long")
	fmt.Fprintln(out, "      responses:")
	fmt.Fprintln(out, "        '200':")
	fmt.Fprintln(out, "          description: Fortune quote response")
}

func writeFortuneConfigPath(out *os.File) {
	fmt.Fprintln(out, "  /fortune/config:")
	fmt.Fprintln(out, "    get:")
	fmt.Fprintln(out, "      operationId: getFortuneConfig")
	fmt.Fprintln(out, "      summary: Get fortune provider configuration")
	fmt.Fprintln(out, "      responses:")
	fmt.Fprintln(out, "        '200':")
	fmt.Fprintln(out, "          description: Current fortune provider configuration")
	fmt.Fprintln(out, "    post:")
	fmt.Fprintln(out, "      operationId: setFortuneConfig")
	fmt.Fprintln(out, "      summary: Set fortune provider configuration")
	fmt.Fprintln(out, "      requestBody:")
	fmt.Fprintln(out, "        required: true")
	fmt.Fprintln(out, "        content:")
	fmt.Fprintln(out, "          application/json:")
	fmt.Fprintln(out, "            schema:")
	fmt.Fprintln(out, "              type: object")
	fmt.Fprintln(out, "              properties:")
	fmt.Fprintln(out, "                default_length:")
	fmt.Fprintln(out, "                  type: string")
	fmt.Fprintln(out, "                  enum:")
	fmt.Fprintln(out, "                    - short")
	fmt.Fprintln(out, "                    - long")
	fmt.Fprintln(out, "              required:")
	fmt.Fprintln(out, "                - default_length")
	fmt.Fprintln(out, "      responses:")
	fmt.Fprintln(out, "        '200':")
	fmt.Fprintln(out, "          description: Updated fortune provider configuration")
}

func writeShellPaths(out *os.File, shellCfg shellSchemaConfig) {
	if len(shellCfg.Endpoints) == 0 {
		return
	}

	names := make([]string, 0, len(shellCfg.Endpoints))
	for name := range shellCfg.Endpoints {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, rawName := range names {
		endpoint := shellCfg.Endpoints[rawName]
		name := strings.Trim(rawName, "/")
		if name == "" {
			continue
		}

		fmt.Fprintf(out, "  /shell/%s:\n", yamlPathSegment(name))
		fmt.Fprintln(out, "    get:")
		fmt.Fprintf(out, "      operationId: %s\n", yamlString("runShell"+operationName(name)))
		fmt.Fprintf(out, "      summary: Run configured shell endpoint %s\n", yamlString(name))
		params := shellQueryParams(endpoint.Args)
		if len(params) > 0 {
			fmt.Fprintln(out, "      parameters:")
			for _, param := range params {
				fmt.Fprintf(out, "        - name: %s\n", yamlString(param))
				fmt.Fprintln(out, "          in: query")
				fmt.Fprintln(out, "          required: true")
				fmt.Fprintln(out, "          schema:")
				fmt.Fprintln(out, "            type: string")
			}
		}
		fmt.Fprintln(out, "      responses:")
		fmt.Fprintln(out, "        '200':")
		fmt.Fprintln(out, "          description: Shell command output")
	}
}

func shellQueryParams(args []string) []string {
	seen := map[string]bool{}
	params := []string{}
	for _, arg := range args {
		if !strings.HasPrefix(arg, "$") || len(arg) == 1 {
			continue
		}
		name := strings.TrimPrefix(arg, "$")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		params = append(params, name)
	}
	sort.Strings(params)
	return params
}

var nonOperationNameChars = regexp.MustCompile(`[^A-Za-z0-9]+`)

func operationName(name string) string {
	parts := nonOperationNameChars.Split(name, -1)
	var b strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		if len(part) > 1 {
			b.WriteString(part[1:])
		}
	}
	if b.Len() == 0 {
		return "Endpoint"
	}
	return b.String()
}

func yamlPathSegment(segment string) string {
	return strings.ReplaceAll(segment, " ", "%20")
}

func yamlString(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\r", "\\r", "\t", "\\t")
	return "\"" + replacer.Replace(value) + "\""
}
