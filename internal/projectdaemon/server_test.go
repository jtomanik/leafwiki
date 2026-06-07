package projectdaemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/perber/wiki/internal/agenthooks"
)

func TestControlServerRejectsUnauthorizedBeforeRouting(t *testing.T) {
	sessions := NewSessionRegistry(time.Minute, nil)
	var mcpCalled bool
	handler := NewControlServer(ControlServerOptions{
		Token:    "control-token",
		Sessions: sessions,
		PrivateMCP: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			mcpCalled = true
			w.WriteHeader(http.StatusNoContent)
		}),
		AuthDisabled: true,
	})

	resp := controlServerRequest(t, handler, http.MethodPost, "/sessions", "wrong-token", nil)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("POST /sessions status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
	if sessions.Count() != 0 {
		t.Fatalf("sessions count = %d, want 0 after unauthorized register", sessions.Count())
	}

	resp = controlServerRequest(t, handler, http.MethodPost, "/mcp", "", strings.NewReader("{}"))
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("POST /mcp status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
	if mcpCalled {
		t.Fatalf("private MCP handler was called for unauthorized request")
	}
}

func TestControlServerSessionLifecycle(t *testing.T) {
	var counts []int
	sessions := NewSessionRegistry(time.Minute, func(count int) {
		counts = append(counts, count)
	})
	handler := NewControlServer(ControlServerOptions{
		Token:        "control-token",
		Sessions:     sessions,
		AuthDisabled: true,
	})

	resp := controlServerRequest(t, handler, http.MethodPost, "/sessions", "control-token", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST /sessions status = %d, want %d: %s", resp.Code, http.StatusOK, resp.Body.String())
	}
	var handle SessionHandle
	if err := json.Unmarshal(resp.Body.Bytes(), &handle); err != nil {
		t.Fatalf("decode session handle: %v", err)
	}
	if strings.TrimSpace(handle.ID) == "" {
		t.Fatalf("session id is empty")
	}
	if sessions.Count() != 1 || joinTestCounts(counts) != "1" {
		t.Fatalf("session count/counts = %d/%s, want 1/1", sessions.Count(), joinTestCounts(counts))
	}

	resp = controlServerRequest(t, handler, http.MethodPost, "/sessions/"+handle.ID+"/heartbeat", "control-token", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d, want %d: %s", resp.Code, http.StatusOK, resp.Body.String())
	}
	resp = controlServerRequest(t, handler, http.MethodPost, "/sessions/missing/heartbeat", "control-token", nil)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("missing heartbeat status = %d, want %d", resp.Code, http.StatusNotFound)
	}

	resp = controlServerRequest(t, handler, http.MethodDelete, "/sessions/"+handle.ID, "control-token", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("release status = %d, want %d: %s", resp.Code, http.StatusOK, resp.Body.String())
	}
	if sessions.Count() != 0 || joinTestCounts(counts) != "1,0" {
		t.Fatalf("session count/counts = %d/%s, want 0/1,0", sessions.Count(), joinTestCounts(counts))
	}

	resp = controlServerRequest(t, handler, http.MethodDelete, "/sessions/missing", "control-token", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("missing release status = %d, want %d", resp.Code, http.StatusOK)
	}
}

func TestControlServerAgentPresenceRequiresTokenAndRecordsSanitizedEvents(t *testing.T) {
	presence := NewAgentPresenceRegistry(time.Minute, nil)
	handler := NewControlServer(ControlServerOptions{
		Token:         "control-token",
		Sessions:      NewSessionRegistry(time.Minute, nil),
		AgentPresence: presence,
		AuthDisabled:  true,
	})

	rawEvent, ok := agenthooks.Normalize(
		agenthooks.ProviderCodex,
		[]byte(`{"hook_event_name":"SessionStart","session_id":"codex-session","model":"gpt-5.4"}`),
		time.Date(2026, 6, 7, 15, 0, 0, 0, time.UTC),
	)
	if !ok {
		t.Fatalf("Normalize returned false")
	}
	body, err := json.Marshal(rawEvent)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	resp := controlServerRequest(t, handler, http.MethodPost, "/agent-presence/events", "", bytes.NewReader(body))
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized POST status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
	if presence.Count() != 0 {
		t.Fatalf("presence count after unauthorized POST = %d, want 0", presence.Count())
	}
	resp = controlServerRequest(t, handler, http.MethodGet, "/agent-presence", "wrong-token", nil)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized GET status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}

	resp = controlServerRequest(t, handler, http.MethodPost, "/agent-presence/events", "control-token", bytes.NewReader(body))
	if resp.Code != http.StatusOK {
		t.Fatalf("authorized POST status = %d, want %d: %s", resp.Code, http.StatusOK, resp.Body.String())
	}
	if presence.Count() != 1 {
		t.Fatalf("presence count after authorized POST = %d, want 1", presence.Count())
	}

	resp = controlServerRequest(t, handler, http.MethodGet, "/agent-presence", "control-token", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("authorized GET status = %d, want %d: %s", resp.Code, http.StatusOK, resp.Body.String())
	}
	var sessions []AgentPresenceSession
	if err := json.Unmarshal(resp.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("decode presence sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].SessionIDHash != rawEvent.SessionIDHash || sessions[0].Provider != agenthooks.ProviderCodex {
		t.Fatalf("sessions = %#v, want one sanitized codex session", sessions)
	}
	if strings.Contains(resp.Body.String(), "raw") || strings.Contains(resp.Body.String(), "session_id") {
		t.Fatalf("presence response leaked raw-ish fields: %s", resp.Body.String())
	}
}

func TestControlServerForwardsPrivateMCPAfterControlToken(t *testing.T) {
	var seenPath, seenAuth, seenBody string
	handler := NewControlServer(ControlServerOptions{
		Token:    "control-token",
		Sessions: NewSessionRegistry(time.Minute, nil),
		PrivateMCP: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seenPath = req.URL.Path
			seenAuth = req.Header.Get("Authorization")
			raw, _ := io.ReadAll(req.Body)
			seenBody = string(raw)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte("mcp-ok"))
		}),
		AuthDisabled: true,
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0"}`))
	req.Header.Set(ControlTokenHeader, "control-token")
	req.Header.Set("Authorization", "Bearer session-api-key")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusAccepted || resp.Body.String() != "mcp-ok" {
		t.Fatalf("private MCP response = %d %q, want 202 mcp-ok", resp.Code, resp.Body.String())
	}
	if seenPath != "/mcp" || seenAuth != "Bearer session-api-key" || !strings.Contains(seenBody, "jsonrpc") {
		t.Fatalf("forwarded request path/auth/body = %q/%q/%q", seenPath, seenAuth, seenBody)
	}
}

func TestControlServerVerifyStdioAuthBoundary(t *testing.T) {
	tests := []struct {
		name         string
		authDisabled bool
		verify       func(string) error
		body         string
		wantStatus   int
	}{
		{name: "malformed json", authDisabled: true, body: "{", wantStatus: http.StatusBadRequest},
		{name: "disabled auth accepts empty key", authDisabled: true, body: `{}`, wantStatus: http.StatusOK},
		{name: "disabled auth rejects api key", authDisabled: true, body: `{"apiKey":"lwk_key"}`, wantStatus: http.StatusConflict},
		{name: "enabled auth requires api key", body: `{}`, wantStatus: http.StatusUnauthorized},
		{name: "enabled auth requires verifier", body: `{"apiKey":"lwk_key"}`, wantStatus: http.StatusInternalServerError},
		{name: "enabled auth rejects invalid api key", body: `{"apiKey":"lwk_key"}`, verify: func(string) error {
			return ErrInvalidAPIKey
		}, wantStatus: http.StatusUnauthorized},
		{name: "enabled auth reports verifier storage failure", body: `{"apiKey":"lwk_key"}`, verify: func(string) error {
			return errors.New("database is locked")
		}, wantStatus: http.StatusServiceUnavailable},
		{name: "enabled auth accepts valid api key", body: `{"apiKey":"lwk_key"}`, verify: func(key string) error {
			if key != "lwk_key" {
				return errors.New("wrong key")
			}
			return nil
		}, wantStatus: http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := NewControlServer(ControlServerOptions{
				Token:        "control-token",
				Sessions:     NewSessionRegistry(time.Minute, nil),
				AuthDisabled: tc.authDisabled,
				VerifyAPIKey: tc.verify,
			})
			resp := controlServerRequest(t, handler, http.MethodPost, "/stdio-auth/verify", "control-token", strings.NewReader(tc.body))
			if resp.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%q", resp.Code, tc.wantStatus, resp.Body.String())
			}
		})
	}
}

func TestClientCallsControlAPIAndPropagatesErrors(t *testing.T) {
	var verifiedKey string
	handler := NewControlServer(ControlServerOptions{
		Token:         "control-token",
		Sessions:      NewSessionRegistry(time.Minute, nil),
		AgentPresence: NewAgentPresenceRegistry(time.Minute, nil),
		Health: DaemonHealth{
			SchemaVersion: DescriptorSchemaVersion,
			PID:           123,
			DataDir:       "/data",
			RootDir:       "/root",
			ConfigHash:    "hash",
		},
		VerifyAPIKey: func(key string) error {
			verifiedKey = key
			return nil
		},
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := NewClient(server.URL, "control-token")
	ctx := context.Background()

	health, err := client.Health(ctx)
	if err != nil {
		t.Fatalf("Health failed: %v", err)
	}
	if health.PID != 123 || !health.OK {
		t.Fatalf("health = %#v, want PID 123 and OK", health)
	}
	handle, err := client.RegisterSession(ctx)
	if err != nil {
		t.Fatalf("RegisterSession failed: %v", err)
	}
	if err := client.HeartbeatSession(ctx, handle.ID); err != nil {
		t.Fatalf("HeartbeatSession failed: %v", err)
	}
	if err := client.ReleaseSession(ctx, handle.ID); err != nil {
		t.Fatalf("ReleaseSession failed: %v", err)
	}
	if err := client.VerifyStdioAuth(ctx, "lwk_valid"); err != nil {
		t.Fatalf("VerifyStdioAuth failed: %v", err)
	}
	cursorEvent, ok := agenthooks.Normalize(
		agenthooks.ProviderCursor,
		[]byte(`{"hook_event_name":"sessionStart","session_id":"cursor-session"}`),
		time.Date(2026, 6, 7, 15, 30, 0, 0, time.UTC),
	)
	if !ok {
		t.Fatalf("Normalize returned false")
	}
	if err := client.RecordAgentPresence(ctx, cursorEvent); err != nil {
		t.Fatalf("RecordAgentPresence failed: %v", err)
	}
	presenceSessions, err := client.ListAgentPresence(ctx)
	if err != nil {
		t.Fatalf("ListAgentPresence failed: %v", err)
	}
	if len(presenceSessions) != 1 || presenceSessions[0].SessionIDHash != cursorEvent.SessionIDHash {
		t.Fatalf("presence sessions = %#v, want cursor session", presenceSessions)
	}
	if verifiedKey != "lwk_valid" {
		t.Fatalf("verified key = %q, want lwk_valid", verifiedKey)
	}

	if err := client.HeartbeatSession(ctx, "missing"); err == nil || !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("missing heartbeat error = %v, want session not found", err)
	}
	badTokenClient := NewClient(server.URL, "wrong-token")
	if err := badTokenClient.Ping(ctx); err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("bad token error = %v, want unauthorized", err)
	}
}

func TestClientReportsMalformedJSONResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get(ControlTokenHeader) != "control-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "control-token")
	if _, err := client.Health(context.Background()); err == nil {
		t.Fatalf("Health succeeded with malformed JSON response")
	}
}

func TestAuthRoundTripperAddsControlAndBearerTokens(t *testing.T) {
	var seenControlToken, seenBearerToken string
	client := &http.Client{
		Transport: AuthRoundTripper{
			Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				seenControlToken = req.Header.Get(ControlTokenHeader)
				seenBearerToken = req.Header.Get("Authorization")
				return &http.Response{
					StatusCode: http.StatusNoContent,
					Body:       io.NopCloser(bytes.NewReader(nil)),
					Header:     http.Header{},
					Request:    req,
				}, nil
			}),
			ControlToken: "control-token",
			BearerToken:  "stdio-api-key",
		},
	}

	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1/mcp", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do failed: %v", err)
	}
	_ = resp.Body.Close()

	if seenControlToken != "control-token" {
		t.Fatalf("control token header = %q, want control-token", seenControlToken)
	}
	if seenBearerToken != "Bearer stdio-api-key" {
		t.Fatalf("authorization header = %q, want bearer API key", seenBearerToken)
	}
	if req.Header.Get(ControlTokenHeader) != "" || req.Header.Get("Authorization") != "" {
		t.Fatalf("original request headers were mutated: %#v", req.Header)
	}
}

func controlServerRequest(t *testing.T, handler http.Handler, method string, path string, token string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()

	if body == nil {
		body = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, body)
	if token != "" {
		req.Header.Set(ControlTokenHeader, token)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func joinTestCounts(counts []int) string {
	if len(counts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(counts))
	for _, count := range counts {
		parts = append(parts, string(rune('0'+count)))
	}
	return strings.Join(parts, ",")
}
