package shell

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Middleware func(http.HandlerFunc) http.HandlerFunc

type Endpoint struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Chroot  string   `json:"chroot,omitempty"`
}

type Config struct {
	Endpoints map[string]Endpoint `json:"endpoints"`
}

type Response struct {
	Endpoint string   `json:"endpoint,omitempty"`
	Command  string   `json:"command,omitempty"`
	Args     []string `json:"args,omitempty"`
	Chroot   string   `json:"chroot,omitempty"`
	Output   string   `json:"output,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func Register(mux *http.ServeMux, protect Middleware, providerBaseDir string) error {
	cfg, err := loadConfig(providerBaseDir)
	if err != nil {
		return err
	}

	for endpointName, endpoint := range cfg.Endpoints {
		ep := endpoint
		name := strings.Trim(endpointName, "/")
		path := "/shell/" + name

		command, chroot, err := validateEndpoint(ep)
		if err != nil {
			return fmt.Errorf("invalid shell endpoint %q: %w", endpointName, err)
		}

		mux.HandleFunc(path, protect(func(w http.ResponseWriter, r *http.Request) {
			resolvedArgs, err := resolveArgs(ep.Args, r)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, Response{
					Endpoint: path,
					Error:    err.Error(),
				})
				return
			}

			cmd := exec.Command(command, resolvedArgs...)
			if err := applyChroot(cmd, chroot); err != nil {
				writeJSON(w, http.StatusInternalServerError, Response{
					Endpoint: path,
					Command:  command,
					Args:     resolvedArgs,
					Chroot:   chroot,
					Error:    err.Error(),
				})
				return
			}

			out, err := cmd.CombinedOutput()
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, Response{
					Endpoint: path,
					Command:  command,
					Args:     resolvedArgs,
					Chroot:   chroot,
					Error:    err.Error(),
					Output:   strings.TrimSpace(string(out)),
				})
				return
			}

			writeJSON(w, http.StatusOK, Response{
				Endpoint: path,
				Command:  command,
				Args:     resolvedArgs,
				Chroot:   chroot,
				Output:   strings.TrimSpace(string(out)),
			})
		}))
	}

	return nil
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
	}

	return filepath.Clean(command), filepath.Clean(chroot), nil
}

func resolveArgs(configuredArgs []string, r *http.Request) ([]string, error) {
	resolved := make([]string, 0, len(configuredArgs))
	query := r.URL.Query()

	for _, arg := range configuredArgs {
		if !strings.HasPrefix(arg, "$") || len(arg) == 1 {
			resolved = append(resolved, arg)
			continue
		}

		name := strings.TrimPrefix(arg, "$")
		value := query.Get(name)
		if value == "" {
			return nil, fmt.Errorf("missing required query parameter %q", name)
		}

		resolved = append(resolved, value)
	}

	return resolved, nil
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
		Endpoints: map[string]Endpoint{
			"os-version": {
				Command: "/usr/bin/uname",
				Args:    []string{"-v"},
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
