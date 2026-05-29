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
	Prefix       string                         `json:"prefix,omitempty"`
	Instructions []string                       `json:"instructions,omitempty"`
	Endpoints    map[string]shellSchemaEndpoint `json:"endpoints"`
}

type shellSchemaEndpoint struct {
	Command              string   `json:"command"`
	Args                 []string `json:"args"`
	Chroot               string   `json:"chroot,omitempty"`
	Description          string   `json:"description,omitempty"`
	Instruction          string   `json:"instruction,omitempty"`
	ConversationStarters []string `json:"conversation_starters,omitempty"`
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
	fmt.Fprintln(out, "Use the GoAgent Action only when the user asks for GoAgent capabilities such as checking service health, checking the GoAgent version, fetching a fortune quote, or using a documented local helper endpoint.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "When checking whether GoAgent is running, call getGoAgentHealth.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "When checking the GoAgent version, call getGoAgentVersion.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "When the user asks for a fortune quote, call getFortune. Use length=short unless the user asks for a long quote.")
	fmt.Fprintln(out)
	writeShellInstructions(out, shellCfg)
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

func writeShellInstructions(out io.Writer, shellCfg shellSchemaConfig) {
	entries := sortedShellEndpointNames(shellCfg)
	if len(entries) == 0 {
		return
	}

	if prefix := strings.TrimSpace(shellCfg.Prefix); prefix != "" {
		fmt.Fprintf(out, "When a GoAgent shell endpoint response includes prefix=%q, begin the final answer with that exact prefix.\n", prefixWithTrailingSpace(prefix))
		fmt.Fprintln(out)
	}
	for _, instruction := range shellCfg.Instructions {
		instruction = strings.TrimSpace(instruction)
		if instruction == "" {
			continue
		}
		fmt.Fprintln(out, instruction)
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, "Documented shell helper endpoints:")
	for _, name := range entries {
		endpoint := shellCfg.Endpoints[name]
		operationID := "runShell" + operationName(strings.Trim(name, "/"))
		if endpoint.Description != "" {
			fmt.Fprintf(out, "- %s: %s\n", operationID, endpoint.Description)
		} else {
			fmt.Fprintf(out, "- %s: configured shell endpoint %s.\n", operationID, strings.Trim(name, "/"))
		}
		if endpoint.Instruction != "" {
			fmt.Fprintf(out, "  %s\n", endpoint.Instruction)
		}
	}
	fmt.Fprintln(out)
}

func prefixWithTrailingSpace(prefix string) string {
	if prefix != "" && !strings.HasSuffix(prefix, " ") {
		return prefix + " "
	}
	return prefix
}

func conversationStarters(shellCfg shellSchemaConfig) []string {
	starters := []string{
		"Is GoAgent running.",
		"What version of GoAgent is running.",
		"Show me all available GoAgent endpoints.",
		"GoAgent, get me a short fortune quote.",
		"GoAgent, get me a long fortune quote.",
	}

	for _, name := range sortedShellEndpointNames(shellCfg) {
		endpoint := shellCfg.Endpoints[name]
		if len(endpoint.ConversationStarters) > 0 {
			starters = append(starters, endpoint.ConversationStarters...)
			continue
		}
		trimmed := strings.Trim(name, "/")
		if trimmed != "" {
			starters = append(starters, "GoAgent, run the "+trimmed+" helper.")
		}
	}
	return starters
}

func sortedShellEndpointNames(shellCfg shellSchemaConfig) []string {
	if len(shellCfg.Endpoints) == 0 {
		return nil
	}

	names := make([]string, 0, len(shellCfg.Endpoints))
	for name := range shellCfg.Endpoints {
		trimmed := strings.Trim(name, "/")
		if trimmed != "" {
			names = append(names, trimmed)
		}
	}
	sort.Strings(names)
	return names
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

type schemaYAMLWriter struct {
	out io.Writer
}

func (w schemaYAMLWriter) line(format string, args ...any) {
	if len(args) == 0 {
		fmt.Fprintln(w.out, format)
		return
	}
	fmt.Fprintf(w.out, format+"\n", args...)
}

func writeGPTActionSchema(out io.Writer, serverURL string, shellCfg shellSchemaConfig) {
	w := schemaYAMLWriter{out: out}
	writeOpenAPIHeader(w, serverURL)
	writeCorePaths(w)
	writeShellPaths(w, shellCfg)
	writeOpenAPIComponents(w)
}

func writeOpenAPIHeader(w schemaYAMLWriter, serverURL string) {
	w.line("openapi: 3.1.0")
	w.line("info:")
	w.line("  title: GoAgent")
	w.line("  version: 0.1.0")
	w.line("  description: Local GoAgent actions exposed through an authenticated HTTP listener.")
	w.line("servers:")
	w.line("  - url: %s", yamlString(serverURL))
	w.line("security:")
	w.line("  - ApiKeyAuth: []")
	w.line("paths:")
}

func writeCorePaths(w schemaYAMLWriter) {
	writeSimpleGetPath(w, "/health", "getGoAgentHealth", "Check GoAgent health", "GoAgent is running")
	writeSimpleGetPath(w, "/version", "getGoAgentVersion", "Get GoAgent version", "GoAgent version response")
	writeFortunePath(w)
	writeFortuneConfigPath(w)
}

func writeOpenAPIComponents(w schemaYAMLWriter) {
	w.line("components:")
	w.line("  schemas: {}")
	w.line("  securitySchemes:")
	w.line("    ApiKeyAuth:")
	w.line("      type: apiKey")
	w.line("      in: header")
	w.line("      name: X-API-Key")
}

func writeSimpleGetPath(w schemaYAMLWriter, path, operationID, summary, responseDescription string) {
	w.line("  %s:", path)
	w.line("    get:")
	w.line("      operationId: %s", yamlString(operationID))
	w.line("      summary: %s", yamlString(summary))
	w.line("      responses:")
	w.line("        '200':")
	w.line("          description: %s", yamlString(responseDescription))
}

func writeFortunePath(w schemaYAMLWriter) {
	w.line("  /fortune:")
	w.line("    get:")
	w.line("      operationId: getFortune")
	w.line("      summary: Get a fortune quote")
	w.line("      parameters:")
	writeStringEnumQueryParam(w, "length", false, []string{"short", "long"})
	w.line("      responses:")
	w.line("        '200':")
	w.line("          description: Fortune quote response")
}

func writeFortuneConfigPath(w schemaYAMLWriter) {
	w.line("  /fortune/config:")
	w.line("    get:")
	w.line("      operationId: getFortuneConfig")
	w.line("      summary: Get fortune provider configuration")
	w.line("      responses:")
	w.line("        '200':")
	w.line("          description: Current fortune provider configuration")
	w.line("    post:")
	w.line("      operationId: setFortuneConfig")
	w.line("      summary: Set fortune provider configuration")
	w.line("      requestBody:")
	w.line("        required: true")
	w.line("        content:")
	w.line("          application/json:")
	w.line("            schema:")
	w.line("              type: object")
	w.line("              properties:")
	w.line("                default_length:")
	w.line("                  type: string")
	w.line("                  enum:")
	w.line("                    - short")
	w.line("                    - long")
	w.line("              required:")
	w.line("                - default_length")
	w.line("      responses:")
	w.line("        '200':")
	w.line("          description: Updated fortune provider configuration")
}

func writeShellPaths(w schemaYAMLWriter, shellCfg shellSchemaConfig) {
	for _, name := range sortedShellEndpointNames(shellCfg) {
		endpoint := shellCfg.Endpoints[name]
		pathName := strings.Trim(name, "/")
		if pathName == "" {
			continue
		}

		w.line("  /shell/%s:", yamlPathSegment(pathName))
		w.line("    get:")
		w.line("      operationId: %s", yamlString("runShell"+operationName(pathName)))
		if endpoint.Description != "" {
			w.line("      summary: %s", yamlString(endpoint.Description))
		} else {
			w.line("      summary: %s", yamlString("Run configured shell endpoint "+pathName))
		}
		params := shellQueryParams(endpoint.Args)
		if len(params) > 0 {
			w.line("      parameters:")
			for _, param := range params {
				writeStringQueryParam(w, param, true)
			}
		}
		w.line("      responses:")
		w.line("        '200':")
		w.line("          description: Shell command output")
	}
}

func writeStringQueryParam(w schemaYAMLWriter, name string, required bool) {
	w.line("        - name: %s", yamlString(name))
	w.line("          in: query")
	w.line("          required: %t", required)
	w.line("          schema:")
	w.line("            type: string")
}

func writeStringEnumQueryParam(w schemaYAMLWriter, name string, required bool, values []string) {
	writeStringQueryParam(w, name, required)
	w.line("            enum:")
	for _, value := range values {
		w.line("              - %s", yamlString(value))
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
