package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

type Response struct {
	Quote string `json:"quote,omitempty"`
	Error string `json:"error,omitempty"`
}

func main() {
	apiKey := os.Getenv("GOAGENT_API_KEY")
	if apiKey == "" {
		log.Fatal("GOAGENT_API_KEY not set")
	}

	http.HandleFunc("/health", health)
	http.HandleFunc("/quote", requireAPIKey(apiKey, quote))

	log.Println("GoAgent listening on :8080")
	log.Fatal(http.ListenAndServe("127.0.0.1:8080", nil))
}

func health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, Response{Quote: "ok"})
}

func quote(w http.ResponseWriter, r *http.Request) {
	length := r.URL.Query().Get("length")

	args := []string{}

	switch length {
	case "", "short":
		args = append(args, "-s")
	case "long":
		// normal fortune output
	default:
		writeJSON(w, 400, Response{Error: "use length=short or length=long"})
		return
	}

	out, err := exec.Command("fortune", args...).Output()
	if err != nil {
		writeJSON(w, 500, Response{Error: err.Error()})
		return
	}

	writeJSON(w, 200, Response{
		Quote: strings.TrimSpace(string(out)),
	})
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
