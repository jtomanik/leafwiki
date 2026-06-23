package projectdaemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/perber/wiki/internal/agenthooks"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

var ErrInvalidAPIKey = errors.New("invalid api key")

const (
	errCodeDaemonControlUnauthorized          sharederrors.ErrorCode = "daemon_control_unauthorized"
	errCodeDaemonSessionRegisterFailed        sharederrors.ErrorCode = "daemon_session_register_failed"
	errCodeDaemonSessionNotFound              sharederrors.ErrorCode = "daemon_session_not_found"
	errCodeDaemonAgentPresenceInvalidRequest  sharederrors.ErrorCode = "daemon_agent_presence_invalid_request"
	errCodeStdioAuthInvalidRequest            sharederrors.ErrorCode = "stdio_auth_invalid_request"
	errCodeStdioAuthAPIKeyRejected            sharederrors.ErrorCode = "stdio_auth_api_key_rejected"
	errCodeStdioAuthAPIKeyRequired            sharederrors.ErrorCode = "stdio_auth_api_key_required"
	errCodeStdioAuthAPIKeyVerifierUnavailable sharederrors.ErrorCode = "stdio_auth_api_key_verifier_unavailable"
	errCodeStdioAuthAPIKeyInvalid             sharederrors.ErrorCode = "stdio_auth_api_key_invalid"
	errCodeStdioAuthAPIKeyVerifierFailed      sharederrors.ErrorCode = "stdio_auth_api_key_verifier_failed"
)

type ControlServerOptions struct {
	Token         string
	Sessions      *SessionRegistry
	AgentPresence *AgentPresenceRegistry
	PrivateMCP    http.Handler
	AuthDisabled  bool
	VerifyAPIKey  func(string) error
	Health        DaemonHealth
}

type ControlServer struct {
	token         string
	sessions      *SessionRegistry
	agentPresence *AgentPresenceRegistry
	mcp           http.Handler
	authDisabled  bool
	verifyAPIKey  func(string) error
	health        DaemonHealth
}

func NewControlServer(opts ControlServerOptions) http.Handler {
	health := opts.Health
	health.OK = true
	return &ControlServer{
		token:         opts.Token,
		sessions:      opts.Sessions,
		agentPresence: opts.AgentPresence,
		mcp:           opts.PrivateMCP,
		authDisabled:  opts.AuthDisabled,
		verifyAPIKey:  opts.VerifyAPIKey,
		health:        health,
	}
}

func (s *ControlServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Header.Get(ControlTokenHeader) != s.token {
		writeControlError(w, http.StatusUnauthorized, errCodeDaemonControlUnauthorized, "unauthorized")
		return
	}
	path := strings.TrimRight(req.URL.Path, "/")
	switch {
	case req.Method == http.MethodGet && path == "/health":
		writeJSON(w, s.health)
	case req.Method == http.MethodPost && path == "/sessions":
		id, err := s.sessions.Register()
		if err != nil {
			writeControlError(w, http.StatusInternalServerError, errCodeDaemonSessionRegisterFailed, "register session")
			return
		}
		writeJSON(w, SessionHandle{ID: id})
	case req.Method == http.MethodPost && strings.HasPrefix(path, "/sessions/") && strings.HasSuffix(path, "/heartbeat"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/sessions/"), "/heartbeat")
		if !s.sessions.Heartbeat(SessionID(id)) {
			writeControlError(w, http.StatusNotFound, errCodeDaemonSessionNotFound, "session not found")
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case req.Method == http.MethodDelete && strings.HasPrefix(path, "/sessions/"):
		id := strings.TrimPrefix(path, "/sessions/")
		s.sessions.Release(SessionID(id))
		writeJSON(w, map[string]any{"ok": true})
	case req.Method == http.MethodPost && path == "/agent-presence/events" && s.agentPresence != nil:
		s.recordAgentPresence(w, req)
	case req.Method == http.MethodGet && path == "/agent-presence" && s.agentPresence != nil:
		writeJSON(w, s.agentPresence.List())
	case req.Method == http.MethodPost && path == "/stdio-auth/verify":
		s.verifyStdioAuth(w, req)
	case path == "/mcp" && s.mcp != nil:
		s.mcp.ServeHTTP(w, req)
	default:
		http.NotFound(w, req)
	}
}

func (s *ControlServer) recordAgentPresence(w http.ResponseWriter, req *http.Request) {
	var event agenthooks.Event
	if err := json.NewDecoder(req.Body).Decode(&event); err != nil {
		writeControlError(w, http.StatusBadRequest, errCodeDaemonAgentPresenceInvalidRequest, "invalid request")
		return
	}
	s.agentPresence.Record(event)
	writeJSON(w, map[string]any{"ok": true})
}

func (s *ControlServer) verifyStdioAuth(w http.ResponseWriter, req *http.Request) {
	var body struct {
		APIKey string `json:"apiKey"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeControlError(w, http.StatusBadRequest, errCodeStdioAuthInvalidRequest, "invalid request")
		return
	}
	if s.authDisabled {
		if strings.TrimSpace(body.APIKey) != "" {
			writeControlError(w, http.StatusConflict, errCodeStdioAuthAPIKeyRejected, "disabled-auth daemon rejects API-key STDIO attach")
			return
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	if strings.TrimSpace(body.APIKey) == "" {
		writeControlError(w, http.StatusUnauthorized, errCodeStdioAuthAPIKeyRequired, "native STDIO requires an API key")
		return
	}
	if s.verifyAPIKey == nil {
		writeControlError(w, http.StatusInternalServerError, errCodeStdioAuthAPIKeyVerifierUnavailable, "api key verifier unavailable")
		return
	}
	if err := s.verifyAPIKey(body.APIKey); err != nil {
		if errors.Is(err, ErrInvalidAPIKey) {
			writeControlError(w, http.StatusUnauthorized, errCodeStdioAuthAPIKeyInvalid, "invalid api key")
			return
		}
		writeControlError(w, http.StatusServiceUnavailable, errCodeStdioAuthAPIKeyVerifierFailed, "api key verifier failed")
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func writeControlError(w http.ResponseWriter, status int, code sharederrors.ErrorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	writeJSON(w, map[string]any{
		"error": sharederrors.NewLocalizedErrorDetail(code, message, message),
	})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, "encode response", http.StatusInternalServerError)
	}
}
