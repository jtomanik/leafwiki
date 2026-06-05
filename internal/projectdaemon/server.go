package projectdaemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var ErrInvalidAPIKey = errors.New("invalid api key")

type ControlServerOptions struct {
	Token        string
	Sessions     *SessionRegistry
	PrivateMCP   http.Handler
	AuthDisabled bool
	VerifyAPIKey func(string) error
	Health       DaemonHealth
}

type ControlServer struct {
	token        string
	sessions     *SessionRegistry
	mcp          http.Handler
	authDisabled bool
	verifyAPIKey func(string) error
	health       DaemonHealth
}

func NewControlServer(opts ControlServerOptions) http.Handler {
	health := opts.Health
	health.OK = true
	return &ControlServer{
		token:        opts.Token,
		sessions:     opts.Sessions,
		mcp:          opts.PrivateMCP,
		authDisabled: opts.AuthDisabled,
		verifyAPIKey: opts.VerifyAPIKey,
		health:       health,
	}
}

func (s *ControlServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Header.Get(ControlTokenHeader) != s.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	path := strings.TrimRight(req.URL.Path, "/")
	switch {
	case req.Method == http.MethodGet && path == "/health":
		writeJSON(w, s.health)
	case req.Method == http.MethodPost && path == "/sessions":
		id, err := s.sessions.Register()
		if err != nil {
			http.Error(w, "register session", http.StatusInternalServerError)
			return
		}
		writeJSON(w, SessionHandle{ID: id})
	case req.Method == http.MethodPost && strings.HasPrefix(path, "/sessions/") && strings.HasSuffix(path, "/heartbeat"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/sessions/"), "/heartbeat")
		if !s.sessions.Heartbeat(id) {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case req.Method == http.MethodDelete && strings.HasPrefix(path, "/sessions/"):
		id := strings.TrimPrefix(path, "/sessions/")
		s.sessions.Release(id)
		writeJSON(w, map[string]any{"ok": true})
	case req.Method == http.MethodPost && path == "/stdio-auth/verify":
		s.verifyStdioAuth(w, req)
	case path == "/mcp" && s.mcp != nil:
		s.mcp.ServeHTTP(w, req)
	default:
		http.NotFound(w, req)
	}
}

func (s *ControlServer) verifyStdioAuth(w http.ResponseWriter, req *http.Request) {
	var body struct {
		APIKey string `json:"apiKey"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if s.authDisabled {
		if strings.TrimSpace(body.APIKey) != "" {
			http.Error(w, "disabled-auth daemon rejects API-key STDIO attach", http.StatusConflict)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	if strings.TrimSpace(body.APIKey) == "" {
		http.Error(w, "native STDIO requires an API key", http.StatusUnauthorized)
		return
	}
	if s.verifyAPIKey == nil {
		http.Error(w, "api key verifier unavailable", http.StatusInternalServerError)
		return
	}
	if err := s.verifyAPIKey(body.APIKey); err != nil {
		if errors.Is(err, ErrInvalidAPIKey) {
			http.Error(w, fmt.Sprintf("invalid api key: %v", err), http.StatusUnauthorized)
			return
		}
		http.Error(w, fmt.Sprintf("api key verifier failed: %v", err), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, "encode response", http.StatusInternalServerError)
	}
}
