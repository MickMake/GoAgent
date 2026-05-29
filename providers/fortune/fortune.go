package fortune

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
)

const (
	markerQuoteEndpoint      = "GOAGENT_FORTUNE_ENDPOINT_REACHED"
	markerConfigGetEndpoint  = "GOAGENT_FORTUNE_CONFIG_GET_ENDPOINT_REACHED"
	markerConfigPostEndpoint = "GOAGENT_FORTUNE_CONFIG_POST_ENDPOINT_REACHED"
)

type Middleware func(http.HandlerFunc) http.HandlerFunc

type Response struct {
	Endpoint      string `json:"endpoint,omitempty"`
	Marker        string `json:"marker,omitempty"`
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

func Register(mux *http.ServeMux, protect Middleware, initialDefaultLength string) {
	setDefaultLength(initialDefaultLength)
	mux.HandleFunc("/fortune", protect(quote))
	mux.HandleFunc("/fortune/config", protect(config))
}

// Quote returns a fortune response using the same validation and command execution
// as the HTTP provider. If length is empty, the current default length is used.
func Quote(length string) (Response, error) {
	if length == "" {
		length = getDefaultLength()
	}

	args := fortuneArgs(length)
	if args == nil {
		return Response{}, fmt.Errorf("use length=short or length=long")
	}

	out, err := exec.Command("fortune", args...).Output()
	if err != nil {
		return Response{}, err
	}

	return Response{
		Endpoint:      "/fortune",
		Marker:        markerQuoteEndpoint,
		Quote:         strings.TrimSpace(string(out)),
		DefaultLength: getDefaultLength(),
	}, nil
}

func quote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeJSON(w, http.StatusMethodNotAllowed, Response{Error: "method not allowed"})
		return
	}

	response, err := Quote(r.URL.Query().Get("length"))
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "use length=short or length=long") {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, Response{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func config(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, Response{Endpoint: "/fortune/config", Marker: markerConfigGetEndpoint, DefaultLength: getDefaultLength()})
	case http.MethodPost:
		var req ConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, Response{Error: "invalid JSON body"})
			return
		}
		if fortuneArgs(req.DefaultLength) == nil {
			writeJSON(w, http.StatusBadRequest, Response{Error: "default_length must be short or long"})
			return
		}
		setDefaultLength(req.DefaultLength)
		writeJSON(w, http.StatusOK, Response{Endpoint: "/fortune/config", Marker: markerConfigPostEndpoint, DefaultLength: getDefaultLength()})
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
	if fortuneArgs(length) == nil {
		length = "short"
	}
	configMu.Lock()
	defer configMu.Unlock()
	defaultLength = length
}

func writeJSON(w http.ResponseWriter, status int, payload Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}
