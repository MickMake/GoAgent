package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MickMake/GoAgent/providers/fortune"
	"github.com/MickMake/GoAgent/providers/shell"
)

func TestMCPToolsListIncludesCoreTools(t *testing.T) {
	server := testMCPServerWithoutShell()
	tools, rpcErr := server.tools()
	if rpcErr != nil {
		t.Fatalf("tools returned error: %+v", rpcErr)
	}

	got := make([]string, 0, len(tools))
	for _, tool := range tools {
		got = append(got, tool.Name)
	}
	want := []string{"goagent_fortune", "goagent_health", "goagent_version"}
	if len(got) != len(want) {
		t.Fatalf("tool count = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tool[%d] = %q, want %q; all tools: %v", i, got[i], want[i], got)
		}
	}
}

func TestMCPToolsListIncludesShellTools(t *testing.T) {
	server := testMCPServerWithShellProvider(&shell.Provider{Config: shell.Config{Endpoints: map[string]shell.Endpoint{
		"os-version": {
			Args:        []string{"-v"},
			Description: "Return OS version.",
		},
		"upper": {
			Args:        []string{"$text"},
			Description: "Uppercase text.",
		},
	}}})

	tools, rpcErr := server.tools()
	if rpcErr != nil {
		t.Fatalf("tools returned error: %+v", rpcErr)
	}

	byName := map[string]mcpTool{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	if _, ok := byName["goagent_shell_os_version"]; !ok {
		t.Fatalf("missing shell tool goagent_shell_os_version; tools=%v", toolNames(tools))
	}
	upper, ok := byName["goagent_shell_upper"]
	if !ok {
		t.Fatalf("missing shell tool goagent_shell_upper; tools=%v", toolNames(tools))
	}
	if upper.Description != "Uppercase text." {
		t.Fatalf("description = %q", upper.Description)
	}
	required, ok := upper.InputSchema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "text" {
		t.Fatalf("required schema = %#v", upper.InputSchema["required"])
	}
}

func TestShellToolNameGeneration(t *testing.T) {
	cases := map[string]string{
		"os-version":      "goagent_shell_os_version",
		"upper":           "goagent_shell_upper",
		"/spaces and-Dots": "goagent_shell_spaces_and_dots",
		"!!!":             "goagent_shell_endpoint",
	}
	for input, want := range cases {
		if got := shellToolName(input); got != want {
			t.Fatalf("shellToolName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestShellInputSchemaFromParams(t *testing.T) {
	schema := shellInputSchema([]string{"$text", "literal", "$name", "$text"})
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("required has type %T", schema["required"])
	}
	want := []string{"name", "text"}
	if len(required) != len(want) {
		t.Fatalf("required = %#v", required)
	}
	for i := range want {
		if required[i] != want[i] {
			t.Fatalf("required[%d] = %q, want %q", i, required[i], want[i])
		}
	}
}

func TestMCPCallHealth(t *testing.T) {
	server := testMCPServerWithoutShell()
	result, rpcErr := server.callTool(json.RawMessage(`{"name":"goagent_health"}`))
	if rpcErr != nil {
		t.Fatalf("callTool returned error: %+v", rpcErr)
	}
	if got := firstToolText(t, result); got != "ok" {
		t.Fatalf("health text = %q, want ok", got)
	}
}

func TestMCPCallVersion(t *testing.T) {
	server := testMCPServerWithoutShell()
	result, rpcErr := server.callTool(json.RawMessage(`{"name":"goagent_version"}`))
	if rpcErr != nil {
		t.Fatalf("callTool returned error: %+v", rpcErr)
	}
	if got := firstToolText(t, result); got != "GoAgent "+GoAgentVersion {
		t.Fatalf("version text = %q", got)
	}
}

func TestMCPCallFortune(t *testing.T) {
	server := testMCPServerWithoutShell()
	server.quote = func(length string) (fortune.Response, error) {
		if length != "long" {
			t.Fatalf("length = %q, want long", length)
		}
		return fortune.Response{Quote: "measure twice, cut once"}, nil
	}

	result, rpcErr := server.callTool(json.RawMessage(`{"name":"goagent_fortune","arguments":{"length":"long"}}`))
	if rpcErr != nil {
		t.Fatalf("callTool returned error: %+v", rpcErr)
	}
	if got := firstToolText(t, result); got != "measure twice, cut once" {
		t.Fatalf("fortune text = %q", got)
	}
}

func TestMCPCallDynamicShellTool(t *testing.T) {
	providerBaseDir := t.TempDir()
	binPath := writeExecutable(t, providerBaseDir, "echo-arg", "#!/bin/sh\nprintf '%s' \"$1\"\n")
	writeShellConfig(t, providerBaseDir, map[string]shell.Endpoint{
		"echo-text": {
			Command:     binPath,
			Args:        []string{"$text"},
			Description: "Echo supplied text.",
		},
	})
	cfg := defaultConfig()
	cfg.Global.ProviderBaseDir = providerBaseDir
	server := newMCPServer(cfg)

	result, rpcErr := server.callTool(json.RawMessage(`{"name":"goagent_shell_echo_text","arguments":{"text":"hello goblin"}}`))
	if rpcErr != nil {
		t.Fatalf("callTool returned error: %+v", rpcErr)
	}
	if got := firstToolText(t, result); got != "hello goblin" {
		t.Fatalf("shell text = %q", got)
	}
}

func TestMCPCallDynamicShellToolMissingParam(t *testing.T) {
	providerBaseDir := t.TempDir()
	binPath := writeExecutable(t, providerBaseDir, "echo-arg", "#!/bin/sh\nprintf '%s' \"$1\"\n")
	writeShellConfig(t, providerBaseDir, map[string]shell.Endpoint{
		"echo-text": {
			Command: binPath,
			Args:    []string{"$text"},
		},
	})
	cfg := defaultConfig()
	cfg.Global.ProviderBaseDir = providerBaseDir
	server := newMCPServer(cfg)

	_, rpcErr := server.callTool(json.RawMessage(`{"name":"goagent_shell_echo_text","arguments":{}}`))
	if rpcErr == nil {
		t.Fatal("expected missing parameter error")
	}
	if rpcErr.Code != -32000 {
		t.Fatalf("error code = %d, want -32000", rpcErr.Code)
	}
}

func TestMCPUnknownTool(t *testing.T) {
	server := testMCPServerWithoutShell()
	_, rpcErr := server.callTool(json.RawMessage(`{"name":"goagent_missing"}`))
	if rpcErr == nil {
		t.Fatal("expected error for unknown tool")
	}
	if rpcErr.Code != -32601 {
		t.Fatalf("error code = %d, want -32601", rpcErr.Code)
	}
}

func TestMCPFortuneRejectsBadLength(t *testing.T) {
	server := testMCPServerWithoutShell()
	_, rpcErr := server.callTool(json.RawMessage(`{"name":"goagent_fortune","arguments":{"length":"medium"}}`))
	if rpcErr == nil {
		t.Fatal("expected error for invalid length")
	}
	if rpcErr.Code != -32602 {
		t.Fatalf("error code = %d, want -32602", rpcErr.Code)
	}
}

func TestMCPServeLineDelimitedJSON(t *testing.T) {
	server := testMCPServerWithoutShell()
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n"

	var output bytes.Buffer
	if err := server.serve(strings.NewReader(input), &output); err != nil {
		t.Fatalf("serve returned error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("response line count = %d, want 2; output=%q", len(lines), output.String())
	}
	for _, line := range lines {
		var response jsonRPCResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("invalid response JSON %q: %v", line, err)
		}
		if response.Error != nil {
			t.Fatalf("unexpected JSON-RPC error: %+v", response.Error)
		}
	}
}

func testMCPServerWithoutShell() *mcpServer {
	server := newMCPServer(defaultConfig())
	server.shellProvider = func() (*shell.Provider, error) {
		return &shell.Provider{Config: shell.Config{Endpoints: map[string]shell.Endpoint{}}}, nil
	}
	return server
}

func testMCPServerWithShellProvider(provider *shell.Provider) *mcpServer {
	server := newMCPServer(defaultConfig())
	server.shellProvider = func() (*shell.Provider, error) {
		return provider, nil
	}
	return server
}

func firstToolText(t *testing.T, result map[string]any) string {
	t.Helper()
	content, ok := result["content"].([]mcpTextContent)
	if !ok {
		t.Fatalf("content has type %T, want []mcpTextContent", result["content"])
	}
	if len(content) == 0 {
		t.Fatal("content is empty")
	}
	return content[0].Text
}

func toolNames(tools []mcpTool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func writeExecutable(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	return path
}

func writeShellConfig(t *testing.T, providerBaseDir string, endpoints map[string]shell.Endpoint) {
	t.Helper()
	path := filepath.Join(providerBaseDir, "shell", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir shell config: %v", err)
	}
	contents, err := json.MarshalIndent(shell.Config{Endpoints: endpoints}, "", "  ")
	if err != nil {
		t.Fatalf("marshal shell config: %v", err)
	}
	if err := os.WriteFile(path, append(contents, '\n'), 0o600); err != nil {
		t.Fatalf("write shell config: %v", err)
	}
}
