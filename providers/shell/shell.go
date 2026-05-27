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
}

type Config struct {
	Endpoints map[string]Endpoint `json:"endpoints"`
}

type Response struct {
	Endpoint string   `json:"endpoint,omitempty"`
	Command  string   `json:"command,omitempty"`
	Args     []string `json:"args,omitempty"`
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

		mux.HandleFunc(path, protect(func(w http.ResponseWriter, r *http.Request) {
			resolvedArgs, err := resolveArgs(ep.Args, r)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, Response{
					Endpoint: path,
					Error:    err.Error(),
				})
				return
			}

			out, err := exec.Command(ep.Command, resolvedArgs...).CombinedOutput()
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, Response{
					Endpoint: path,
					Command:  ep.Command,
					Args:     resolvedArgs,
					Error:    err.Error(),
					Output:   strings.TrimSpace(string(out)),
				})
				return
			}

			writeJSON(w, http.StatusOK, Response{
				Endpoint: path,
				Command:  ep.Command,
				Args:     resolvedArgs,
				Output:   strings.TrimSpace(string(out)),
			})
		}))
	}

	return nil
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
			return Config{}, nil
		}
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(contents, &cfg); err != nil {
		return Config{}, fmt.Errorf("invalid shell provider config %s: %w", path, err)
	}

	return cfg, nil
}

func writeJSON(w http.ResponseWriter, status int, payload Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}
