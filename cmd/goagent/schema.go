package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

func runSetupCommand(cfg AppConfig, args []string) error {
	if len(args) > 2 {
		return errors.New("usage: GoAgent setup [server-url] [privacy-url]")
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
		apiKey = "<run GoAgent key create and paste the generated X-API-Key here>"
	}

	shellCfg, err := loadShellSchemaConfig(cfg.Global.ProviderBaseDir)
	if err != nil {
		return err
	}

	knowledgeFiles, err := listKnowledgeFiles()
	if err != nil {
		return err
	}

	writeGPTSetup(os.Stdout, serverURL, privacyURL, apiKey, shellCfg, knowledgeFiles)
	return nil
}

func promptForSetupURL(reader *bufio.Reader, label, defaultValue string, required bool) (string, error) {
	for {
		if defaultValue != "" {
			fmt.Fprintf(os.Stderr, "%s [%s]: ", label, defaultValue)
		} else {
			fmt.Fprintf(os.Stderr, "%s: ", label)
		}

		value, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		value = strings.TrimSpace(value)
		if value == "" {
			value = defaultValue
		}
		value = normalizeSchemaServerURL(value)

		if err := validateSetupURL(strings.ToLower(label), value, required); err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		return value, nil
	}
}

func validateSetupURL(label, value string, required bool) error {
	if value == "" {
		if required {
			return fmt.Errorf("%s is required", label)
		}
		return nil
	}
	if _, err := url.ParseRequestURI(value); err != nil {
		return fmt.Errorf("invalid %s %q: %w", label, value, err)
	}
	return nil
}

func defaultSetupServerURL(cfg AppConfig) string {
	if cfg.GPT.ServerURL != "" {
		return cfg.GPT.ServerURL
	}
	return "http://" + cfg.Listener.ListenAddr
}

func defaultSetupPrivacyURL(cfg AppConfig, serverURL string) string {
	if cfg.GPT.PrivacyURL != "" {
		return cfg.GPT.PrivacyURL
	}
	return strings.TrimRight(serverURL, "/") + "/config/privacy"
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

func listKnowledgeFiles() ([]string, error) {
	knowledgeDir := filepath.Join(mustGoAgentDir(), "knowledge")
	entries, err := os.ReadDir(knowledgeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	files := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)
	return files, nil
}

func writeGPTSetup(out io.Writer, serverURL, privacyURL, apiKey string, shellCfg shellSchemaConfig, knowledgeFiles []string) {
	fmt.Fprintln(out, "GPT Configure:")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "===== Name =====")
	fmt.Fprintln(out, "GoAgent")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "===== Description =====")
	fmt.Fprintln(out, "A locally run helper GPT that uses the GoAgent service to call endpoints backed by shell scripts.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "===== Instructions =====")
	fmt.Fprintln(out, "You are GoAgent, a GPT connects to a locally run GoAgent service through configured Actions.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Use the GoAgent Action only when the user asks for GoAgent capabilities such as checking service health, fetching a fortune quote, or using a documented local helper endpoint.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "When checking whether GoAgent is running, call getGoAgentHealth.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "When the user asks for a fortune quote, call getFortune. Use length=short unless the user asks for a long quote.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "If GoAgent returns a marker field, include it verbatim in the final response so the user can confirm the endpoint was reached.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Do not invent endpoints, shell commands, file paths, or tool names. Only call endpoints that are explicitly present in the Action schema.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Do not call shell-style endpoints unless they are explicitly documented in the Action schema and the user clearly asks for that capability.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "If the GoAgent Action fails, briefly report the failure and suggest checking:")
	fmt.Fprintln(out, "- whether GoAgent is running,")
	fmt.Fprintln(out, "- whether the Cloudflare tunnel URL is current,")
	fmt.Fprintln(out, "- whether the X-API-Key value matches the running GoAgent key.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Keep responses concise and practical.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "===== Conversation starters =====")
	for _, starter := range conversationStarters(shellCfg) {
		fmt.Fprintf(out, "- %s\n", starter)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "===== Knowledge =====")
	fmt.Fprintln(out, "Files in ~/.GoAgent/knowledge/")
	if len(knowledgeFiles) == 0 {
		fmt.Fprintln(out, "None")
	} else {
		for _, file := range knowledgeFiles {
			fmt.Fprintf(out, "%s/config/knowledge/%s\n", serverURL, pathEscape(file))
		}
	}
	fmt.Fprintln(out, "Knowledge URLs require the X-API-Key header.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "===== Recommended Model =====")
	fmt.Fprintln(out, "Any.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "===== Capabilities =====")
	fmt.Fprintln(out, "_ Web Search")
	fmt.Fprintln(out, "_ Apps")
	fmt.Fprintln(out, "_ Canvas")
	fmt.Fprintln(out, "_ Image Generation")
	fmt.Fprintln(out, "_ Code Interpreter & Data Analysis")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "===== Actions =====")
	fmt.Fprintln(out, "Action:")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "===== Authentication =====")
	fmt.Fprintln(out, "API Key")
	fmt.Fprintln(out, "Header name: X-API-Key")
	fmt.Fprintf(out, "Key value: %s\n", apiKey)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "===== Schema =====")
	fmt.Fprintf(out, "%s/config/schema\n", serverURL)
	fmt.Fprintln(out, "This URL does not require an API key.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "===== Privacy Policy =====")
	fmt.Fprintln(out, privacyURL)
}

func conversationStarters(shellCfg shellSchemaConfig) []string {
	starters := []string{
		"Is GoAgent running.",
		"What version of GoAgent is running.",
		"Show me all available GoAgent endpoints.",
		"GoAgent, get me a short fortune quote.",
		"GoAgent, get me a long fortune quote.",
	}

	if len(shellCfg.Endpoints) == 0 {
		return starters
	}
	names := make([]string, 0, len(shellCfg.Endpoints))
	for name := range shellCfg.Endpoints {
		name = strings.Trim(name, "/")
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		starters = append(starters, "GoAgent, run the "+name+" helper.")
	}
	return starters
}

func configSchemaHandler(cfg AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeJSON(w, http.StatusMethodNotAllowed, Response{Error: "method not allowed"})
			return
		}
		shellCfg, err := loadShellSchemaConfig(cfg.Global.ProviderBaseDir)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, Response{Error: err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		writeGPTActionSchema(w, defaultSetupServerURL(cfg), shellCfg)
	}
}

func knowledgeHandler(apiKey string) http.HandlerFunc {
	return requireAPIKey(apiKey, func(w http.ResponseWriter, r *http.Request) {
		prefix := "/config/knowledge/"
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeJSON(w, http.StatusMethodNotAllowed, Response{Error: "method not allowed"})
			return
		}
		name := strings.TrimPrefix(r.URL.Path, prefix)
		if name == "" || name != filepath.Base(name) || strings.Contains(name, "..") {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(mustGoAgentDir(), "knowledge", name))
	})
}

func writeGPTActionSchema(out io.Writer, serverURL string, shellCfg shellSchemaConfig) {
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
	fmt.Fprintln(out, "  schemas: {}")
	fmt.Fprintln(out, "  securitySchemes:")
	fmt.Fprintln(out, "    ApiKeyAuth:")
	fmt.Fprintln(out, "      type: apiKey")
	fmt.Fprintln(out, "      in: header")
	fmt.Fprintln(out, "      name: X-API-Key")
}

func writeHealthPath(out io.Writer) {
	fmt.Fprintln(out, "  /health:")
	fmt.Fprintln(out, "    get:")
	fmt.Fprintln(out, "      operationId: getGoAgentHealth")
	fmt.Fprintln(out, "      summary: Check GoAgent health")
	fmt.Fprintln(out, "      responses:")
	fmt.Fprintln(out, "        '200':")
	fmt.Fprintln(out, "          description: GoAgent is running")
}

func writeFortunePath(out io.Writer) {
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

func writeFortuneConfigPath(out io.Writer) {
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

func writeShellPaths(out io.Writer, shellCfg shellSchemaConfig) {
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

func pathEscape(segment string) string {
	return url.PathEscape(segment)
}

func yamlPathSegment(segment string) string {
	return strings.ReplaceAll(segment, " ", "%20")
}

func yamlString(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\r", "\\r", "\t", "\\t")
	return "\"" + replacer.Replace(value) + "\""
}
