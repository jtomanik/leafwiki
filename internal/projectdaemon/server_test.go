package projectdaemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	ginkgo "github.com/onsi/ginkgo/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/perber/wiki/internal/agenthooks"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

var _ = ginkgo.It("TestControlServerRejectsUnauthorizedBeforeRouting", func() {
	t := ginkgo.GinkgoT()
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
	assertControlStructuredError(t, resp, "daemon_control_unauthorized", "errors.daemon.control_unauthorized")
	if sessions.Count() != 0 {
		t.Fatalf("sessions count = %d, want 0 after unauthorized register", sessions.Count())
	}

	resp = controlServerRequest(t, handler, http.MethodPost, "/mcp", "", strings.NewReader("{}"))
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("POST /mcp status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
	assertControlStructuredError(t, resp, "daemon_control_unauthorized", "errors.daemon.control_unauthorized")
	if mcpCalled {
		t.Fatalf("private MCP handler was called for unauthorized request")
	}

})

var _ = ginkgo.It("TestControlServerSessionLifecycle", func() {
	t := ginkgo.GinkgoT()
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
	if handle.ID == "" {
		t.Fatalf("session id is empty")
	}
	if sessions.Count() != 1 || joinTestCounts(counts) != "1" {
		t.Fatalf("session count/counts = %d/%s, want 1/1", sessions.Count(), joinTestCounts(counts))
	}

	resp = controlServerRequest(t, handler, http.MethodPost, sessionHeartbeatPath(handle.ID), "control-token", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d, want %d: %s", resp.Code, http.StatusOK, resp.Body.String())
	}
	resp = controlServerRequest(t, handler, http.MethodPost, "/sessions/missing/heartbeat", "control-token", nil)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("missing heartbeat status = %d, want %d", resp.Code, http.StatusNotFound)
	}
	assertControlStructuredError(t, resp, "daemon_session_not_found", "errors.daemon.session_not_found")

	resp = controlServerRequest(t, handler, http.MethodDelete, sessionPath(handle.ID), "control-token", nil)
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

})

var _ = ginkgo.It("TestControlServerAgentPresenceRequiresTokenAndRecordsSanitizedEvents", func() {
	t := ginkgo.GinkgoT()
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
	assertControlStructuredError(t, resp, "daemon_control_unauthorized", "errors.daemon.control_unauthorized")
	if presence.Count() != 0 {
		t.Fatalf("presence count after unauthorized POST = %d, want 0", presence.Count())
	}
	resp = controlServerRequest(t, handler, http.MethodGet, "/agent-presence", "wrong-token", nil)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized GET status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
	assertControlStructuredError(t, resp, "daemon_control_unauthorized", "errors.daemon.control_unauthorized")

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

})

var _ = ginkgo.It("TestControlServerForwardsPrivateMCPAfterControlToken", func() {
	t := ginkgo.GinkgoT()
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

})

type stdioAuthBoundaryCase struct {
	name          string
	authDisabled  bool
	verify        func(string) error
	body          string
	wantStatus    int
	wantCode      string
	wantMessageID string
}

var _ = ginkgo.DescribeTable("TestControlServerVerifyStdioAuthBoundary",
	func(tc stdioAuthBoundaryCase) {
		t := ginkgo.GinkgoT()
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
		if tc.wantCode != "" {
			var body struct {
				Error struct {
					Code      string `json:"code"`
					MessageID string `json:"messageId"`
				} `json:"error"`
			}
			if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error response: %v; body=%q", err, resp.Body.String())
			}
			if body.Error.Code != tc.wantCode || body.Error.MessageID != tc.wantMessageID {
				t.Fatalf("error = %#v, want code=%q messageId=%q", body.Error, tc.wantCode, tc.wantMessageID)
			}
		}
	},
	ginkgo.Entry("malformed json", stdioAuthBoundaryCase{authDisabled: true, body: "{", wantStatus: http.StatusBadRequest, wantCode: "stdio_auth_invalid_request", wantMessageID: "errors.stdio.auth_invalid_request"}),
	ginkgo.Entry("disabled auth accepts empty key", stdioAuthBoundaryCase{authDisabled: true, body: `{}`, wantStatus: http.StatusOK}),
	ginkgo.Entry("disabled auth rejects api key", stdioAuthBoundaryCase{authDisabled: true, body: `{"apiKey":"lwk_key"}`, wantStatus: http.StatusConflict, wantCode: "stdio_auth_api_key_rejected", wantMessageID: "errors.stdio.auth_api_key_rejected"}),
	ginkgo.Entry("enabled auth requires api key", stdioAuthBoundaryCase{body: `{}`, wantStatus: http.StatusUnauthorized, wantCode: "stdio_auth_api_key_required", wantMessageID: "errors.stdio.auth_api_key_required"}),
	ginkgo.Entry("enabled auth requires verifier", stdioAuthBoundaryCase{body: `{"apiKey":"lwk_key"}`, wantStatus: http.StatusInternalServerError, wantCode: "stdio_auth_api_key_verifier_unavailable", wantMessageID: "errors.stdio.auth_api_key_verifier_unavailable"}),
	ginkgo.Entry("enabled auth rejects invalid api key", stdioAuthBoundaryCase{body: `{"apiKey":"lwk_key"}`, verify: func(string) error { return ErrInvalidAPIKey }, wantStatus: http.StatusUnauthorized, wantCode: "stdio_auth_api_key_invalid", wantMessageID: "errors.stdio.auth_api_key_invalid"}),
	ginkgo.Entry("enabled auth reports verifier storage failure", stdioAuthBoundaryCase{body: `{"apiKey":"lwk_key"}`, verify: func(string) error { return errors.New("database is locked") }, wantStatus: http.StatusServiceUnavailable, wantCode: "stdio_auth_api_key_verifier_failed", wantMessageID: "errors.stdio.auth_api_key_verifier_failed"}),
	ginkgo.Entry("enabled auth accepts valid api key", stdioAuthBoundaryCase{body: `{"apiKey":"lwk_key"}`, verify: func(key string) error {
		if key != "lwk_key" {
			return errors.New("wrong key")
		}
		return nil
	}, wantStatus: http.StatusOK}),
)

var _ = ginkgo.It("TestClientCallsControlAPIAndPropagatesErrors", func() {
	t := ginkgo.GinkgoT()
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

	if err := client.HeartbeatSession(ctx, newFixtureSessionID("missing")); err == nil {
		t.Fatalf("missing heartbeat succeeded")
	} else {
		assertControlHTTPError(t, err, http.StatusNotFound, errCodeDaemonSessionNotFound)
	}
	badTokenClient := NewClient(server.URL, "wrong-token")
	if err := badTokenClient.Ping(ctx); err == nil {
		t.Fatalf("bad token ping succeeded")
	} else {
		assertControlHTTPError(t, err, http.StatusUnauthorized, errCodeDaemonControlUnauthorized)
	}

})

func assertControlHTTPError(t projectdaemonTestT, err error, wantStatus int, wantCode sharederrors.ErrorCode) {
	t.Helper()
	var controlErr *ControlHTTPError
	if !errors.As(err, &controlErr) {
		t.Fatalf("error = %T %[1]v, want ControlHTTPError", err)
	}
	wantMessageID := sharederrors.MessageIDForCode(wantCode)
	if controlErr.StatusCode != wantStatus || controlErr.Code != wantCode || controlErr.MessageID != wantMessageID || controlErr.Message == "" {
		t.Fatalf("control error = %#v, want status=%d code=%q messageId=%q", controlErr, wantStatus, wantCode, wantMessageID)
	}
}

var _ = ginkgo.It("ControlHTTPError and IsControlStatus expose structured status matching", func() {
	t := ginkgo.GinkgoT()
	err := &ControlHTTPError{
		StatusCode: http.StatusConflict,
		Code:       "daemon_control_conflict",
		MessageID:  "errors.daemon.control_conflict",
		Message:    "daemon already owns this project",
	}
	if got := err.Error(); !strings.Contains(got, "daemon already owns this project") {
		t.Fatalf("Error() = %q, want message text", got)
	}
	if !IsControlStatus(err, http.StatusConflict) {
		t.Fatalf("IsControlStatus returned false for direct control error")
	}
	if !IsControlStatus(fmt.Errorf("wrapped: %w", err), http.StatusConflict) {
		t.Fatalf("IsControlStatus returned false for wrapped control error")
	}
	if IsControlStatus(err, http.StatusUnauthorized) {
		t.Fatalf("IsControlStatus matched wrong status")
	}
	if IsControlStatus(errors.New("plain"), http.StatusConflict) {
		t.Fatalf("IsControlStatus matched non-control error")
	}
	if got := (*ControlHTTPError)(nil).Error(); got != "" {
		t.Fatalf("nil ControlHTTPError Error() = %q, want empty", got)
	}
})

var _ = ginkgo.It("TestClientReportsMalformedJSONResponses", func() {
	t := ginkgo.GinkgoT()
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

})

var _ = ginkgo.It("TestAuthRoundTripperAddsControlBearerAndActorContext", func() {
	t := ginkgo.GinkgoT()
	var seenControlToken, seenBearerToken, seenActorContext string
	client := &http.Client{
		Transport: AuthRoundTripper{
			Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				seenControlToken = req.Header.Get(ControlTokenHeader)
				seenBearerToken = req.Header.Get("Authorization")
				seenActorContext = req.Header.Get(ActorContextHeader)
				return &http.Response{
					StatusCode: http.StatusNoContent,
					Body:       io.NopCloser(bytes.NewReader(nil)),
					Header:     http.Header{},
					Request:    req,
				}, nil
			}),
			ControlToken: "control-token",
			BearerToken:  "stdio-api-key",
			ActorContext: "encoded-actor",
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
	if seenActorContext != "encoded-actor" {
		t.Fatalf("actor context header = %q, want encoded actor", seenActorContext)
	}
	if req.Header.Get(ControlTokenHeader) != "" || req.Header.Get("Authorization") != "" || req.Header.Get(ActorContextHeader) != "" {
		t.Fatalf("original request headers were mutated: %#v", req.Header)
	}

})

func controlServerRequest(t projectdaemonTestT, handler http.Handler, method string, path controlPath, token string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()

	if body == nil {
		body = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path.String(), body)
	if token != "" {
		req.Header.Set(ControlTokenHeader, token)
	}
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func assertControlStructuredError(t projectdaemonTestT, resp *httptest.ResponseRecorder, code string, messageID string) {
	t.Helper()
	var body struct {
		Error struct {
			Code      string `json:"code"`
			MessageID string `json:"messageId"`
			Message   string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode control error: %v; body=%s", err, resp.Body.String())
	}
	if body.Error.Code != code || body.Error.MessageID != messageID || body.Error.Message == "" {
		t.Fatalf("control error = %#v, want code=%q messageId=%q with message", body.Error, code, messageID)
	}
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
