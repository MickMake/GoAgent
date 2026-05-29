package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MickMake/GoAgent/providers/fortune"
)

func TestMCPToolsListIncludesPhaseOneTools(t *testing.T) {
	server := newMCPServer(defaultConfig())
	tools := server.tools()

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

func TestMCPCallHealth(t *testing.T) {
	server := newMCPServer(defaultConfig())
	result, rpcErr := server.callTool(json.RawMessage(`{"name":"goagent_health"}`))
	if rpcErr != nil {
		t.Fatalf("callTool returned error: %+v", rpcErr)
	}
	if got := firstToolText(t, result); got != "ok" {
		t.Fatalf("health text = %q, want ok", got)
	}
}

func TestMCPCallVersion(t *testing.T) {
	server := newMCPServer(defaultConfig())
	result, rpcErr := server.callTool(json.RawMessage(`{"name":"goagent_version"}`))
	if rpcErr != nil {
		t.Fatalf("callTool returned error: %+v", rpcErr)
	}
	if got := firstToolText(t, result); got != "GoAgent "+GoAgentVersion {
		t.Fatalf("version text = %q", got)
	}
}

func TestMCPCallFortune(t *testing.T) {
	server := newMCPServer(defaultConfig())
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

func TestMCPUnknownTool(t *testing.T) {
	server := newMCPServer(defaultConfig())
	_, rpcErr := server.callTool(json.RawMessage(`{"name":"goagent_missing"}`))
	if rpcErr == nil {
		t.Fatal("expected error for unknown tool")
	}
	if rpcErr.Code != -32601 {
		t.Fatalf("error code = %d, want -32601", rpcErr.Code)
	}
}

func TestMCPFortuneRejectsBadLength(t *testing.T) {
	server := newMCPServer(defaultConfig())
	_, rpcErr := server.callTool(json.RawMessage(`{"name":"goagent_fortune","arguments":{"length":"medium"}}`))
	if rpcErr == nil {
		t.Fatal("expected error for invalid length")
	}
	if rpcErr.Code != -32602 {
		t.Fatalf("error code = %d, want -32602", rpcErr.Code)
	}
}

func TestMCPServeLineDelimitedJSON(t *testing.T) {
	server := newMCPServer(defaultConfig())
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
