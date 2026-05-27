package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type Response struct {
	Quote         string `json:"quote,omitempty"`
	DefaultLength string `json:"default_length,omitempty"`
	Error         string `json:"error,omitempty"`
}

type ConfigRequest struct {
	DefaultLength string `json:"default_length"`
}

var (
	configMu      sync.RWMutex
	defaultLength = "short"
)

func main() {
	apiKey := os.Getenv("GOAGENT_API_KEY")
	if apiKey == "" {
		log.Fatal("GOAGENT_API_KEY not set")
	}

	http.HandleFunc("/health", health)
	http.HandleFunc("/quote", requireAPIKey(apiKey, quote))
	http.HandleFunc("/config", requireAPIKey(apiKey, config))

	log.Println("GoAgent listening on :8080")
	log.Fatal(http.ListenAndServe("127.0.0.1:8080", nil))
}

func health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, Response{Quote: "ok"})
}

func quote(w http.ResponseWriter, r *http.Request) {
	length := r.URL.Query().Get("length")
	if length == "" {
		length = getDefaultLength()
	}

	args := fortuneArgs(length)
	if args == nil {
		writeJSON(w, 400, Response{Error: "use length=short or length=long"})
		return
	}

	out, err := exec.Command("fortune", args...).Output()
	if err != nil {
		writeJSON(w, 500, Response{Error: err.Error()})
		return
	}

	writeJSON(w, 200, Response{
		Quote:         strings.TrimSpace(string(out)),
		DefaultLength: getDefaultLength(),
	})
}

func config(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, Response{DefaultLength: getDefaultLength()})
	case http.MethodPost:
		var req ConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, Response{Error: "invalid JSON body"})
			return
		}

		if fortuneArgs(req.DefaultLength) == nil {
			writeJSON(w, 400, Response{Error: "default_length must be short or long"})
			return
		}

		setDefaultLength(req.DefaultLength)
		writeJSON(w, 200, Response{DefaultLength: getDefaultLength()})
	default:
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, Response{Error: "method not allowed"})
	}
}

func fortuneArgs(length string) []string {
	switch length {
	case "short":
		return []string{"-s"}
	case "long":
		// normal fortune output
		return []string{}
	default:
		return nil
	}
}

func getDefaultLength() string {
	configMu.RLock()
	defer configMu.RUnlock()
	return defaultLength
}

func setDefaultLength(length string) {
	configMu.Lock()
	defer configMu.Unlock()
	defaultLength = length
}

func requireAPIKey(expected string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != expected {
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
