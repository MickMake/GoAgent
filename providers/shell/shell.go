package shell

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Middleware func(http.HandlerFunc) http.HandlerFunc

type Endpoint struct {
	Command              string   `json:"command"`
	Args                 []string `json:"args"`
	Chroot               string   `json:"chroot,omitempty"`
	Description          string   `json:"description,omitempty"`
	Instruction          string   `json:"instruction,omitempty"`
	ConversationStarters []string `json:"conversation_starters,omitempty"`
}

type Config struct {
	Prefix       string              `json:"prefix,omitempty"`
	Instructions []string            `json:"instructions,omitempty"`
	Endpoints    map[string]Endpoint `json:"endpoints"`
}

type Provider struct {
	Config Config
	prefix string
}

type Response struct {
	Prefix   string   `json:"prefix,omitempty"`
	Endpoint string   `json:"endpoint,omitempty"`
	Command  string   `json:"command,omitempty"`
	Args     []string `json:"args,omitempty"`
	Chroot   string   `json:"chroot,omitempty"`
	Output   string   `json:"output,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func New(providerBaseDir string) (*Provider, error) {
	cfg, err := loadConfig(providerBaseDir)
	if err != nil {
		return nil, err
	}
	return &Provider{
		Config: cfg,
		prefix: normalizePrefix(cfg.Prefix),
	}, nil
}

func Register(mux *http.ServeMux, protect Middleware, providerBaseDir string) error {
	provider, err := New(providerBaseDir)
	if err != nil {
		return err
	}

	for _, endpointName := range provider.EndpointNames() {
		name := endpointName
		path := "/shell/" + name
		if _, _, err := validateEndpoint(provider.Config.Endpoints[name]); err != nil {
			return fmt.Errorf("invalid shell endpoint %q: %w", name, err)
		}

		mux.HandleFunc(path, protect(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				writeJSON(w, http.StatusMethodNotAllowed, provider.withPrefix(Response{Endpoint: path, Error: "method not allowed"}))
				return
			}

			args := map[string]string{}
			for key, values := range r.URL.Query() {
				if len(values) > 0 {
					args[key] = values[0]
				}
			}

			response, err := provider.Run(name, args)
			if err != nil {
				status := http.StatusInternalServerError
				if isMissingParameterError(err) {
					status = http.StatusBadRequest
				}
				if response.Endpoint == "" {
					response.Endpoint = path
				}
				response.Error = err.Error()
				writeJSON(w, status, response)
				return
			}

			writeJSON(w, http.StatusOK, response)
		}))
	}

	return nil
}

func (p *Provider) EndpointNames() []string {
	if p == nil || len(p.Config.Endpoints) == 0 {
		return nil
	}
	names := make([]string, 0, len(p.Config.Endpoints))
	for name := range p.Config.Endpoints {
		trimmed := strings.Trim(name, "/")
		if trimmed != "" {
			names = append(names, trimmed)
		}
	}
	sort.Strings(names)
	return names
}

func (p *Provider) Endpoint(name string) (Endpoint, bool) {
	if p == nil {
		return Endpoint{}, false
	}
	name = strings.Trim(name, "/")
	endpoint, ok := p.Config.Endpoints[name]
	if ok {
		return endpoint, true
	}
	endpoint, ok = p.Config.Endpoints["/"+name]
	return endpoint, ok
}

func (p *Provider) Run(name string, input map[string]string) (Response, error) {
	name = strings.Trim(name, "/")
	path := "/shell/" + name
	endpoint, ok := p.Endpoint(name)
	if !ok {
		return p.withPrefix(Response{Endpoint: path}), fmt.Errorf("unknown shell endpoint %q", name)
	}

	command, chroot, err := validateEndpoint(endpoint)
	if err != nil {
		return p.withPrefix(Response{Endpoint: path}), err
	}

	resolvedArgs, err := ResolveArgs(endpoint.Args, input)
	if err != nil {
		return p.withPrefix(Response{Endpoint: path}), err
	}

	cmd := exec.Command(command, resolvedArgs...)
	if err := applyChroot(cmd, chroot); err != nil {
		return p.withPrefix(Response{
			Endpoint: path,
			Command:  command,
			Args:     resolvedArgs,
			Chroot:   chroot,
		}), err
	}

	out, err := cmd.CombinedOutput()
	response := p.withPrefix(Response{
		Endpoint: path,
		Command:  command,
		Args:     resolvedArgs,
		Chroot:   chroot,
		Output:   strings.TrimSpace(string(out)),
	})
	if err != nil {
		return response, err
	}
	return response, nil
}

func Params(configuredArgs []string) []string {
	seen := map[string]bool{}
	params := []string{}
	for _, arg := range configuredArgs {
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

func ResolveArgs(configuredArgs []string, values map[string]string) ([]string, error) {
	resolved := make([]string, 0, len(configuredArgs))
	for _, arg := range configuredArgs {
		if !strings.HasPrefix(arg, "$") || len(arg) == 1 {
			resolved = append(resolved, arg)
			continue
		}

		name := strings.TrimPrefix(arg, "$")
		value := strings.TrimSpace(values[name])
		if value == "" {
			return nil, missingParameterError{name: name}
		}

		resolved = append(resolved, value)
	}
	return resolved, nil
}

type missingParameterError struct {
	name string
}

func (e missingParameterError) Error() string {
	return fmt.Sprintf("missing required parameter %q", e.name)
}

func isMissingParameterError(err error) bool {
	_, ok := err.(missingParameterError)
	return ok
}

func normalizePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix != "" && !strings.HasSuffix(prefix, " ") {
		prefix += " "
	}
	return prefix
}

func (p *Provider) withPrefix(response Response) Response {
	if p != nil && p.prefix != "" {
		response.Prefix = p.prefix
	}
	return response
}

func validateEndpoint(endpoint Endpoint) (string, string, error) {
	command := expandPath(endpoint.Command)
	if !filepath.IsAbs(command) {
		return "", "", fmt.Errorf("command must be an absolute path or start with ~/; got %q", endpoint.Command)
	}

	chroot := strings.TrimSpace(endpoint.Chroot)
	if chroot != "" {
		chroot = expandPath(chroot)
		if !filepath.IsAbs(chroot) {
			return "", "", fmt.Errorf("chroot must be an absolute path or start with ~/; got %q", endpoint.Chroot)
		}
		chroot = filepath.Clean(chroot)
	}

	return filepath.Clean(command), chroot, nil
}

func loadConfig(providerBaseDir string) (Config, error) {
	path := filepath.Join(providerBaseDir, "shell", "config.json")
	contents, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := defaultConfig()
			if err := writeDefaultConfig(path, cfg); err != nil {
				return Config{}, err
			}
			return cfg, nil
		}
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(contents, &cfg); err != nil {
		return Config{}, fmt.Errorf("invalid shell provider config %s: %w", path, err)
	}

	return cfg, nil
}

func defaultConfig() Config {
	return Config{
		Prefix: "GoAgent: ",
		Instructions: []string{
			"When a shell endpoint response includes a prefix field, begin the final answer with that exact prefix.",
		},
		Endpoints: map[string]Endpoint{
			"os-version": {
				Command:              "/usr/bin/uname",
				Args:                 []string{"-v"},
				Description:          "Return the operating system version string from uname -v.",
				Instruction:          "When the user asks for the local operating system version, call runShellOsVersion.",
				ConversationStarters: []string{"GoAgent, what OS version is this running on?"},
			},
			"upper": {
				Command:              "/usr/bin/awk",
				Args:                 []string{"BEGIN { print toupper(ARGV[1]); exit }", "$text"},
				Description:          "Uppercase supplied text using a fixed awk program.",
				Instruction:          "When the user asks to uppercase text, call runShellUpper with the text parameter set to only the text to transform. Do not pass commands, awk programs, file paths, flags, or shell syntax.",
				ConversationStarters: []string{"GoAgent, uppercase this text: measure twice, cut once"},
			},
		},
	}
}

func writeDefaultConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	contents, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, append(contents, '\n'), 0o600)
}

func expandPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return path
}

func writeJSON(w http.ResponseWriter, status int, payload Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}
