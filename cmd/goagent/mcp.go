package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/MickMake/GoAgent/providers/fortune"
	"github.com/MickMake/GoAgent/providers/shell"
)

const mcpProtocolVersion = "2025-06-18"

type mcpServer struct {
	cfg           AppConfig
	quote         func(length string) (fortune.Response, error)
	shellProvider func() (*shell.Provider, error)
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any           `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

type mcpTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type mcpToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

func runMCPServeCommand(cfg AppConfig) error {
	server := newMCPServer(cfg)
	return server.serve(os.Stdin, os.Stdout)
}

func newMCPServer(cfg AppConfig) *mcpServer {
	return &mcpServer{
		cfg:   cfg,
		quote: fortune.Quote,
		shellProvider: func() (*shell.Provider, error) {
			return shell.New(cfg.Global.ProviderBaseDir)
		},
	}
}

func (s *mcpServer) serve(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	encoder := json.NewEncoder(out)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			response := jsonRPCResponse{JSONRPC: "2.0", Error: &jsonRPCError{Code: -32700, Message: "parse error", Data: err.Error()}}
			if err := encoder.Encode(response); err != nil {
				return err
			}
			continue
		}

		response, shouldReply := s.handle(req)
		if !shouldReply {
			continue
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}

	return scanner.Err()
}

func (s *mcpServer) handle(req jsonRPCRequest) (jsonRPCResponse, bool) {
	if len(req.ID) == 0 {
		if req.Method == "notifications/initialized" || strings.HasPrefix(req.Method, "notifications/") {
			return jsonRPCResponse{}, false
		}
		return jsonRPCResponse{}, false
	}

	base := jsonRPCResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		return s.initializeResponse(base, req), true
	case "tools/list":
		tools, rpcErr := s.tools()
		if rpcErr != nil {
			base.Error = rpcErr
		} else {
			base.Result = map[string]any{"tools": tools}
		}
		return base, true
	case "tools/call":
		result, rpcErr := s.callTool(req.Params)
		if rpcErr != nil {
			base.Error = rpcErr
		} else {
			base.Result = result
		}
		return base, true
	default:
		base.Error = &jsonRPCError{Code: -32601, Message: "method not found", Data: req.Method}
		return base, true
	}
}

func (s *mcpServer) initializeResponse(base jsonRPCResponse, req jsonRPCRequest) jsonRPCResponse {
	protocolVersion := mcpProtocolVersion
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(req.Params) > 0 && json.Unmarshal(req.Params, &params) == nil && strings.TrimSpace(params.ProtocolVersion) != "" {
		protocolVersion = params.ProtocolVersion
	}
	base.Result = map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "GoAgent",
			"version": GoAgentVersion,
		},
	}
	return base
}

func (s *mcpServer) tools() ([]mcpTool, *jsonRPCError) {
	tools := []mcpTool{
		{
			Name:        "goagent_health",
			Description: "Check whether GoAgent is running.",
			InputSchema: emptyObjectSchema(),
		},
		{
			Name:        "goagent_version",
			Description: "Return the GoAgent service name and version.",
			InputSchema: emptyObjectSchema(),
		},
		{
			Name:        "goagent_fortune",
			Description: "Return a fortune quote. Optional length argument must be short or long.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"length": map[string]any{
						"type": "string",
						"enum": []string{"short", "long"},
					},
				},
				"additionalProperties": false,
			},
		},
	}

	shellTools, rpcErr := s.shellTools()
	if rpcErr != nil {
		return nil, rpcErr
	}
	tools = append(tools, shellTools...)
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

func (s *mcpServer) shellTools() ([]mcpTool, *jsonRPCError) {
	provider, err := s.shellProvider()
	if err != nil {
		return nil, &jsonRPCError{Code: -32000, Message: "shell provider failed", Data: err.Error()}
	}

	toolNames := map[string]bool{}
	tools := []mcpTool{}
	for _, endpointName := range provider.EndpointNames() {
		endpoint, ok := provider.Endpoint(endpointName)
		if !ok {
			continue
		}
		toolName := shellToolName(endpointName)
		if toolNames[toolName] {
			return nil, &jsonRPCError{Code: -32000, Message: "duplicate shell MCP tool name", Data: toolName}
		}
		toolNames[toolName] = true

		description := strings.TrimSpace(endpoint.Description)
		if description == "" {
			description = "Run configured GoAgent shell endpoint " + endpointName + "."
		}
		tools = append(tools, mcpTool{
			Name:        toolName,
			Description: description,
			InputSchema: shellInputSchema(endpoint.Args),
		})
	}
	return tools, nil
}

func (s *mcpServer) callTool(rawParams json.RawMessage) (map[string]any, *jsonRPCError) {
	var params mcpToolCallParams
	if len(rawParams) == 0 {
		return nil, &jsonRPCError{Code: -32602, Message: "missing tool call parameters"}
	}
	if err := json.Unmarshal(rawParams, &params); err != nil {
		return nil, &jsonRPCError{Code: -32602, Message: "invalid tool call parameters", Data: err.Error()}
	}
	if strings.TrimSpace(params.Name) == "" {
		return nil, &jsonRPCError{Code: -32602, Message: "missing tool name"}
	}

	switch params.Name {
	case "goagent_health":
		return toolTextResult("ok"), nil
	case "goagent_version":
		return toolTextResult(fmt.Sprintf("GoAgent %s", GoAgentVersion)), nil
	case "goagent_fortune":
		length, err := optionalStringArg(params.Arguments, "length")
		if err != nil {
			return nil, &jsonRPCError{Code: -32602, Message: err.Error()}
		}
		if length == "" {
			length = s.cfg.Listener.DefaultQuoteLength
		}
		response, err := s.quote(length)
		if err != nil {
			code := -32000
			if strings.Contains(err.Error(), "use length=short or length=long") {
				code = -32602
			}
			return nil, &jsonRPCError{Code: code, Message: "tool execution failed", Data: err.Error()}
		}
		return toolTextResult(response.Quote), nil
	default:
		if strings.HasPrefix(params.Name, "goagent_shell_") {
			return s.callShellTool(params)
		}
		return nil, &jsonRPCError{Code: -32601, Message: "unknown tool", Data: params.Name}
	}
}

func (s *mcpServer) callShellTool(params mcpToolCallParams) (map[string]any, *jsonRPCError) {
	provider, err := s.shellProvider()
	if err != nil {
		return nil, &jsonRPCError{Code: -32000, Message: "shell provider failed", Data: err.Error()}
	}
	endpointName, ok := shellEndpointForTool(provider, params.Name)
	if !ok {
		return nil, &jsonRPCError{Code: -32601, Message: "unknown tool", Data: params.Name}
	}

	args, err := stringArgumentMap(params.Arguments)
	if err != nil {
		return nil, &jsonRPCError{Code: -32602, Message: err.Error()}
	}
	response, err := provider.Run(endpointName, args)
	if err != nil {
		data := map[string]any{
			"error": err.Error(),
		}
		if response.Output != "" {
			data["output"] = response.Output
		}
		code := -32000
		if shell.IsMissingParameterError(err) {
			code = -32602
		}
		return nil, &jsonRPCError{Code: code, Message: "tool execution failed", Data: data}
	}
	return toolTextResult(response.Output), nil
}

func shellEndpointForTool(provider *shell.Provider, toolName string) (string, bool) {
	for _, endpointName := range provider.EndpointNames() {
		if shellToolName(endpointName) == toolName {
			return endpointName, true
		}
	}
	return "", false
}

var unsafeToolNameChars = regexp.MustCompile(`[^a-z0-9]+`)

func shellToolName(endpointName string) string {
	endpointName = strings.ToLower(strings.Trim(endpointName, "/"))
	endpointName = unsafeToolNameChars.ReplaceAllString(endpointName, "_")
	endpointName = strings.Trim(endpointName, "_")
	if endpointName == "" {
		endpointName = "endpoint"
	}
	return "goagent_shell_" + endpointName
}

func shellInputSchema(configuredArgs []string) map[string]any {
	properties := map[string]any{}
	params := shell.Params(configuredArgs)
	for _, param := range params {
		properties[param] = map[string]any{"type": "string"}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(params) > 0 {
		schema["required"] = params
	}
	return schema
}

func stringArgumentMap(args map[string]any) (map[string]string, error) {
	values := map[string]string{}
	for key, value := range args {
		if value == nil {
			continue
		}
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be a string", key)
		}
		values[key] = text
	}
	return values, nil
}

func optionalStringArg(args map[string]any, name string) (string, error) {
	if args == nil {
		return "", nil
	}
	value, ok := args[name]
	if !ok || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", name)
	}
	text = strings.TrimSpace(text)
	if name == "length" && text != "" && text != "short" && text != "long" {
		return "", errors.New("length must be short or long")
	}
	return text, nil
}

func toolTextResult(text string) map[string]any {
	return map[string]any{
		"content": []mcpTextContent{{Type: "text", Text: text}},
		"isError": false,
	}
}

func emptyObjectSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}
