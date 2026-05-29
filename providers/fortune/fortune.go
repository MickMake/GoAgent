package fortune

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
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

type Runtime struct {
	CommandTimeoutSeconds int
	OutputLimitBytes      int
	ChildEnv              []string
}

var (
	configMu      sync.RWMutex
	defaultLength = "short"
	runtimeMu     sync.RWMutex
	runtimeConfig = normalizeRuntime(Runtime{})
)

func Register(mux *http.ServeMux, protect Middleware, initialDefaultLength string) {
	RegisterWithRuntime(mux, protect, initialDefaultLength, Runtime{})
}

func RegisterWithRuntime(mux *http.ServeMux, protect Middleware, initialDefaultLength string, runtime Runtime) {
	setDefaultLength(initialDefaultLength)
	setRuntime(runtime)
	mux.HandleFunc("/fortune", protect(quote))
	mux.HandleFunc("/fortune/config", protect(config))
}

// Quote returns a fortune response using the same validation and command execution
// as the HTTP provider. If length is empty, the current default length is used.
func Quote(length string) (Response, error) {
	return QuoteWithRuntime(length, getRuntime())
}

func QuoteWithRuntime(length string, runtime Runtime) (Response, error) {
	runtime = normalizeRuntime(runtime)
	if length == "" {
		length = getDefaultLength()
	}

	args := fortuneArgs(length)
	if args == nil {
		return Response{}, fmt.Errorf("use length=short or length=long")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(runtime.CommandTimeoutSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "fortune", args...)
	if len(runtime.ChildEnv) > 0 {
		cmd.Env = runtime.ChildEnv
	}
	output := &limitedBuffer{limit: int64(runtime.OutputLimitBytes)}
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return Response{}, fmt.Errorf("command timed out after %d second(s)", runtime.CommandTimeoutSeconds)
	}
	if output.Err() != nil {
		return Response{}, output.Err()
	}
	if err != nil {
		return Response{}, err
	}

	return Response{
		Endpoint:      "/fortune",
		Marker:        markerQuoteEndpoint,
		Quote:         strings.TrimSpace(output.String()),
		DefaultLength: getDefaultLength(),
	}, nil
}

func normalizeRuntime(runtime Runtime) Runtime {
	if runtime.CommandTimeoutSeconds <= 0 {
		runtime.CommandTimeoutSeconds = 30
	}
	if runtime.OutputLimitBytes <= 0 {
		runtime.OutputLimitBytes = 1024 * 1024
	}
	if len(runtime.ChildEnv) == 0 {
		runtime.ChildEnv = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	}
	return runtime
}

func setRuntime(runtime Runtime) {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	runtimeConfig = normalizeRuntime(runtime)
}

func getRuntime() Runtime {
	runtimeMu.RLock()
	defer runtimeMu.RUnlock()
	runtime := runtimeConfig
	runtime.ChildEnv = append([]string(nil), runtime.ChildEnv...)
	return runtime
}

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int64
	err   error
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	remaining := b.limit - int64(b.buf.Len())
	if remaining <= 0 {
		b.err = fmt.Errorf("command output exceeded limit of %d bytes", b.limit)
		return 0, b.err
	}
	if int64(len(p)) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.err = fmt.Errorf("command output exceeded limit of %d bytes", b.limit)
		return int(remaining), b.err
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) String() string { return b.buf.String() }

func (b *limitedBuffer) Err() error {
	if b.err == io.ErrShortWrite {
		return fmt.Errorf("command output exceeded limit of %d bytes", b.limit)
	}
	return b.err
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
