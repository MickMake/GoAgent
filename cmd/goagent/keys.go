package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func requireAPIKey(expected string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provided := r.Header.Get("X-API-Key")
		if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

func loadAPIKey(cfg AppConfig) (string, error) {
	if envKey := strings.TrimSpace(os.Getenv("GOAGENT_API_KEY")); envKey != "" {
		return envKey, nil
	}
	key, err := readNamedSecret(goagentAPIKeyPath(cfg, cfg.Listener.DefaultAPIKey))
	if err == nil && key != "" {
		return key, nil
	}
	return "", fmt.Errorf("GOAGENT_API_KEY not set and %s not found; run: GoAgent gpt key create", goagentAPIKeyPath(cfg, cfg.Listener.DefaultAPIKey))
}

func runAPIKeyCommand(cfg AppConfig, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: GoAgent gpt key create [name] | GoAgent gpt key | GoAgent gpt key rm <name>")
	}
	switch args[0] {
	case "create":
		name := "default"
		if len(args) > 1 {
			name = args[1]
		}
		return createGeneratedSecret("GoAgent API key", func(name string) string { return goagentAPIKeyPath(cfg, name) }, name, true)
	case "ls":
		return listSecrets(cfg, "GoAgent-", ".key")
	case "rm":
		if len(args) < 2 {
			return errors.New("usage: GoAgent gpt key rm <name>")
		}
		return removeSecret("GoAgent API key", func(name string) string { return goagentAPIKeyPath(cfg, name) }, args[1])
	default:
		return fmt.Errorf("unknown key command %q", args[0])
	}
}

func createGeneratedSecret(kind string, pathFunc func(string) string, name string, printValue bool) error {
	secret, err := generateAPIKey()
	if err != nil {
		return err
	}
	if err := createProvidedSecret(kind, pathFunc, name, secret); err != nil {
		return err
	}
	if printValue {
		fmt.Printf("X-API-Key: %s\n", secret)
	}
	return nil
}

func createProvidedSecret(kind string, pathFunc func(string) string, name string, secret string) error {
	name, err := cleanSecretName(name)
	if err != nil {
		return err
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return errors.New("secret cannot be empty")
	}
	path := pathFunc(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if fileExists(path) {
		return fmt.Errorf("%s %q already exists: %s", kind, name, path)
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Printf("created %s %q: %s\n", kind, name, path)
	return nil
}

func listSecrets(cfg AppConfig, prefix, suffix string) error {
	entries, err := os.ReadDir(cfg.Global.KeyDir)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("no credentials found")
		return nil
	}
	if err != nil {
		return err
	}
	items := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fileName := entry.Name()
		if strings.HasPrefix(fileName, prefix) && strings.HasSuffix(fileName, suffix) {
			items = append(items, strings.TrimSuffix(strings.TrimPrefix(fileName, prefix), suffix))
		}
	}
	if len(items) == 0 {
		fmt.Println("no credentials found")
		return nil
	}
	sort.Strings(items)
	for _, item := range items {
		fmt.Println(item)
	}
	return nil
}

func removeSecret(kind string, pathFunc func(string) string, name string) error {
	name, err := cleanSecretName(name)
	if err != nil {
		return err
	}
	path := pathFunc(name)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s %q does not exist", kind, name)
		}
		return err
	}
	fmt.Printf("removed %s %q\n", kind, name)
	return nil
}

func readNamedSecret(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(contents)), nil
}

func generateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func cleanSecretName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("credential name cannot be empty")
	}
	if name != filepath.Base(name) {
		return "", errors.New("credential name must not contain path separators")
	}
	if strings.Contains(name, "..") {
		return "", errors.New("credential name must not contain '..'")
	}
	return name, nil
}

func goagentAPIKeyPath(cfg AppConfig, name string) string {
	return filepath.Join(cfg.Global.KeyDir, "GoAgent-"+name+".key")
}
