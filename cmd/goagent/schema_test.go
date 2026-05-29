package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGPTActionSchemaIncludesCoreAndShellPaths(t *testing.T) {
	shellCfg := shellSchemaConfig{Endpoints: map[string]shellSchemaEndpoint{
		"upper": {
			Args:        []string{"$text"},
			Description: "Uppercase supplied text.",
		},
		"os-version": {
			Args:        []string{"-v"},
			Description: "Return OS version.",
		},
	}}

	var out bytes.Buffer
	writeGPTActionSchema(&out, "https://goagent.example", shellCfg)
	schema := out.String()

	mustContainAll(t, schema,
		"openapi: 3.1.0",
		"  - url: \"https://goagent.example\"",
		"  /health:",
		"      operationId: getGoAgentHealth",
		"  /version:",
		"      operationId: getGoAgentVersion",
		"  /fortune:",
		"      operationId: getFortune",
		"  /fortune/config:",
		"      operationId: getFortuneConfig",
		"      operationId: setFortuneConfig",
		"  /shell/os-version:",
		"      operationId: \"runShellOsVersion\"",
		"  /shell/upper:",
		"      operationId: \"runShellUpper\"",
		"        - name: \"text\"",
		"    ApiKeyAuth:",
		"      name: X-API-Key",
	)
}

func TestGPTActionSchemaEscapesDynamicStrings(t *testing.T) {
	shellCfg := shellSchemaConfig{Endpoints: map[string]shellSchemaEndpoint{
		"say hello": {
			Args:        []string{"$text"},
			Description: "Say \"hello\" with\\slash and tab\tend",
		},
	}}

	var out bytes.Buffer
	writeGPTActionSchema(&out, "https://goagent.example/root", shellCfg)
	schema := out.String()

	mustContainAll(t, schema,
		"  /shell/say%20hello:",
		"      operationId: \"runShellSayHello\"",
		"      summary: \"Say \\\"hello\\\" with\\\\slash and tab\\tend\"",
		"        - name: \"text\"",
	)
}

func TestShellQueryParamsAreSortedDeduplicatedAndRequiredOnlyForDollarArgs(t *testing.T) {
	got := shellQueryParams([]string{"literal", "$beta", "$alpha", "$beta", "$", "--flag"})
	want := []string{"alpha", "beta"}
	if len(got) != len(want) {
		t.Fatalf("shellQueryParams length = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("shellQueryParams[%d] = %q, want %q; all=%#v", i, got[i], want[i], got)
		}
	}
}

func TestOperationNameFallsBackForEmptyOrUnsafeNames(t *testing.T) {
	cases := map[string]string{
		"":                "Endpoint",
		"!!!":             "Endpoint",
		"os-version":      "OsVersion",
		"spaces and dots": "SpacesAndDots",
	}
	for input, want := range cases {
		if got := operationName(input); got != want {
			t.Fatalf("operationName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestConfigSchemaHandlerIsPublicGetOnlyAndUsesConfiguredShellConfig(t *testing.T) {
	providerBaseDir := t.TempDir()
	writeShellSchemaConfig(t, providerBaseDir, `{
  "endpoints": {
    "echo-text": {
      "command": "/bin/echo",
      "args": ["$text"],
      "description": "Echo supplied text."
    }
  }
}`)

	cfg := defaultConfig()
	cfg.Global.ProviderBaseDir = providerBaseDir
	cfg.GPT.ServerURL = "https://goagent.example"

	handler := configSchemaHandler(cfg)

	getReq := httptest.NewRequest(http.MethodGet, "/config/schema", nil)
	getRes := httptest.NewRecorder()
	handler(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("GET /config/schema status = %d, want %d; body=%s", getRes.Code, http.StatusOK, getRes.Body.String())
	}
	if got := getRes.Header().Get("Content-Type"); !strings.Contains(got, "application/yaml") {
		t.Fatalf("Content-Type = %q, want application/yaml", got)
	}
	mustContainAll(t, getRes.Body.String(),
		"  - url: \"https://goagent.example\"",
		"  /shell/echo-text:",
		"      operationId: \"runShellEchoText\"",
		"        - name: \"text\"",
	)

	postReq := httptest.NewRequest(http.MethodPost, "/config/schema", nil)
	postRes := httptest.NewRecorder()
	handler(postRes, postReq)
	if postRes.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /config/schema status = %d, want %d", postRes.Code, http.StatusMethodNotAllowed)
	}
	if got := postRes.Header().Get("Allow"); got != "GET" {
		t.Fatalf("Allow header = %q, want GET", got)
	}
}

func mustContainAll(t *testing.T, haystack string, needles ...string) {
	t.Helper()
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			t.Fatalf("generated schema missing %q\n--- schema ---\n%s", needle, haystack)
		}
	}
}

func writeShellSchemaConfig(t *testing.T, providerBaseDir, contents string) {
	t.Helper()
	path := filepath.Join(providerBaseDir, "shell", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir shell config: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents+"\n"), 0o600); err != nil {
		t.Fatalf("write shell config: %v", err)
	}
}
