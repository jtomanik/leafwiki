package projectdaemon

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/agenthooks"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.Describe("project daemon control server", ginkgo.Label("integration"), func() {
	ginkgo.It("rejects unauthorized requests before routing to sessions or private MCP", func() {
		sessions := NewSessionRegistry(time.Minute, nil)
		handler := NewControlServer(ControlServerOptions{
			Token:    "control-token",
			Sessions: sessions,
			PrivateMCP: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte("private-mcp-forwarded"))
			}),
			AuthDisabled: true,
		})

		resp := performControlRequest(handler, http.MethodPost, "/sessions", "wrong-token", nil)
		Expect(resp).To(testmatchers.HaveHTTPStructuredError(http.StatusUnauthorized, errCodeDaemonControlUnauthorized, sharederrors.MessageIDForCode(errCodeDaemonControlUnauthorized)))
		Expect(sessions.Count()).To(BeZero())

		resp = performControlRequest(handler, http.MethodPost, "/mcp", "", strings.NewReader("{}"))
		Expect(resp).To(testmatchers.HaveHTTPStructuredError(http.StatusUnauthorized, errCodeDaemonControlUnauthorized, sharederrors.MessageIDForCode(errCodeDaemonControlUnauthorized)))
	})

	ginkgo.It("registers, heartbeats, and releases control sessions", func() {
		var counts []int
		sessions := NewSessionRegistry(time.Minute, func(count int) {
			counts = append(counts, count)
		})
		handler := NewControlServer(ControlServerOptions{
			Token:        "control-token",
			Sessions:     sessions,
			AuthDisabled: true,
		})

		resp := performControlRequest(handler, http.MethodPost, "/sessions", "control-token", nil)
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		handle := decodeControlResponse[SessionHandle](resp)
		Expect(handle.ID).NotTo(BeEmpty())
		Expect(sessions.Count()).To(Equal(1))
		Expect(sessions).To(reportSessionSeen(1))
		Expect(counts).To(Equal([]int{1}))

		Expect(performControlRequest(handler, http.MethodPost, sessionHeartbeatPath(handle.ID), "control-token", nil)).To(HaveHTTPStatus(http.StatusOK))
		resp = performControlRequest(handler, http.MethodPost, "/sessions/missing/heartbeat", "control-token", nil)
		Expect(resp).To(testmatchers.HaveHTTPStructuredError(http.StatusNotFound, errCodeDaemonSessionNotFound, sharederrors.MessageIDForCode(errCodeDaemonSessionNotFound)))

		Expect(performControlRequest(handler, http.MethodDelete, sessionPath(handle.ID), "control-token", nil)).To(HaveHTTPStatus(http.StatusOK))
		Expect(sessions.Count()).To(BeZero())
		Expect(sessions).To(reportSessionSeen(0))
		Expect(counts).To(Equal([]int{1, 0}))

		Expect(performControlRequest(handler, http.MethodDelete, "/sessions/missing", "control-token", nil)).To(HaveHTTPStatus(http.StatusOK))
	})

	ginkgo.It("expires sessions that were registered through the control API", func() {
		now := time.Date(2026, 7, 7, 9, 0, 0, 0, time.UTC)
		var counts []int
		sessions := NewSessionRegistry(time.Minute, func(count int) {
			counts = append(counts, count)
		})
		sessions.now = func() time.Time {
			return now
		}
		handler := NewControlServer(ControlServerOptions{
			Token:        "control-token",
			Sessions:     sessions,
			AuthDisabled: true,
		})

		resp := performControlRequest(handler, http.MethodPost, "/sessions", "control-token", nil)
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		Expect(sessions).To(reportSessionSeen(1))

		now = now.Add(2 * time.Minute)

		Expect(sessions.PruneExpired()).To(BeZero())
		Expect(sessions).To(reportSessionSeen(0))
		Expect(counts).To(Equal([]int{1, 0}))
	})

	ginkgo.It("expires registered sessions from the background control registry loop", func() {
		var counts []int
		sessions := NewSessionRegistry(10*time.Millisecond, func(count int) {
			counts = append(counts, count)
		})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			sessions.RunExpiryLoop(ctx, 5*time.Millisecond)
		}()
		handler := NewControlServer(ControlServerOptions{
			Token:        "control-token",
			Sessions:     sessions,
			AuthDisabled: true,
		})

		resp := performControlRequest(handler, http.MethodPost, "/sessions", "control-token", nil)
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		Expect(sessions).To(reportSessionSeen(1))

		Eventually(sessions.Count).
			WithTimeout(500 * time.Millisecond).
			WithPolling(5 * time.Millisecond).
			Should(BeZero())
		Expect(sessions).To(reportSessionSeen(0))
		Expect(counts).To(Equal([]int{1, 0}))

		cancel()
		Eventually(done).WithTimeout(250 * time.Millisecond).Should(BeClosed())
	})

	ginkgo.It("requires tokens for agent presence and returns sanitized accepted sessions", func() {
		presence := NewAgentPresenceRegistry(time.Minute, nil)
		handler := NewControlServer(ControlServerOptions{
			Token:         "control-token",
			Sessions:      NewSessionRegistry(time.Minute, nil),
			AgentPresence: presence,
			AuthDisabled:  true,
		})

		rawEvent := normalizedPresenceEvent(agenthooks.ProviderCodex, `{"hook_event_name":"SessionStart","session_id":"codex-session","model":"gpt-5.4"}`)
		body, err := json.Marshal(rawEvent)
		Expect(err).To(Succeed())

		resp := performControlRequest(handler, http.MethodPost, "/agent-presence/events", "", bytes.NewReader(body))
		Expect(resp).To(testmatchers.HaveHTTPStructuredError(http.StatusUnauthorized, errCodeDaemonControlUnauthorized, sharederrors.MessageIDForCode(errCodeDaemonControlUnauthorized)))
		Expect(presence.Count()).To(BeZero())
		resp = performControlRequest(handler, http.MethodGet, "/agent-presence", "wrong-token", nil)
		Expect(resp).To(testmatchers.HaveHTTPStructuredError(http.StatusUnauthorized, errCodeDaemonControlUnauthorized, sharederrors.MessageIDForCode(errCodeDaemonControlUnauthorized)))

		Expect(performControlRequest(handler, http.MethodPost, "/agent-presence/events", "control-token", bytes.NewReader(body))).To(HaveHTTPStatus(http.StatusOK))
		Expect(presence.Count()).To(Equal(1))

		resp = performControlRequest(handler, http.MethodGet, "/agent-presence", "control-token", nil)
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		sessions := decodeControlResponse[[]AgentPresenceSession](resp)
		Expect(sessions).To(ConsistOf(matchAgentPresenceSession(gstruct.Fields{
			"Provider":      Equal(agenthooks.ProviderCodex),
			"SessionIDHash": Equal(rawEvent.SessionIDHash),
		})))
		var presenceWire []map[string]any
		Expect(json.Unmarshal(resp.Body.Bytes(), &presenceWire)).To(Succeed())
		Expect(presenceWire).To(ConsistOf(SatisfyAll(
			Not(HaveKey("session_id")),
			Not(HaveKey("raw")),
		)))
	})

	ginkgo.It("expires agent sessions that were recorded through the control API", func() {
		now := time.Date(2026, 7, 7, 9, 15, 0, 0, time.UTC)
		var counts []int
		presence := NewAgentPresenceRegistry(time.Minute, func(count int) {
			counts = append(counts, count)
		})
		presence.now = func() time.Time {
			return now
		}
		handler := NewControlServer(ControlServerOptions{
			Token:         "control-token",
			Sessions:      NewSessionRegistry(time.Minute, nil),
			AgentPresence: presence,
			AuthDisabled:  true,
		})
		rawEvent := normalizedPresenceEvent(agenthooks.ProviderCodex, `{"hook_event_name":"SessionStart","session_id":"codex-session","source":"cli","tool_name":"mcp__leafwiki__wiki_get_page"}`)
		rawEvent.SeenAt = now
		body, err := json.Marshal(rawEvent)
		Expect(err).To(Succeed())

		resp := performControlRequest(handler, http.MethodPost, "/agent-presence/events", "control-token", bytes.NewReader(body))
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		Expect(presence).To(reportAgentPresenceSeen(1))

		resp = performControlRequest(handler, http.MethodGet, "/agent-presence", "control-token", nil)
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		sessions := decodeControlResponse[[]AgentPresenceSession](resp)
		Expect(sessions).To(ConsistOf(SatisfyAll(
			matchAgentPresenceSession(gstruct.Fields{
				"Source":   Equal(agenthooks.AgentSourceCLI),
				"ToolName": Equal(mustDecodeAgentToolName("mcp__leafwiki__wiki_get_page")),
			}),
			matchMCPToolPresenceSession(),
		)))

		now = now.Add(2 * time.Minute)

		Expect(presence.PruneExpired()).To(BeZero())
		Expect(presence).To(reportAgentPresenceSeen(0))
		Expect(counts).To(Equal([]int{1, 0}))
	})

	ginkgo.It("expires recorded agent sessions from the background control registry loop", func() {
		var counts []int
		presence := NewAgentPresenceRegistry(10*time.Millisecond, func(count int) {
			counts = append(counts, count)
		})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			presence.RunExpiryLoop(ctx, 5*time.Millisecond)
		}()
		handler := NewControlServer(ControlServerOptions{
			Token:         "control-token",
			Sessions:      NewSessionRegistry(time.Minute, nil),
			AgentPresence: presence,
			AuthDisabled:  true,
		})
		rawEvent := normalizedPresenceEvent(agenthooks.ProviderCodex, `{"hook_event_name":"SessionStart","session_id":"codex-session","source":"cli"}`)
		body, err := json.Marshal(rawEvent)
		Expect(err).To(Succeed())

		resp := performControlRequest(handler, http.MethodPost, "/agent-presence/events", "control-token", bytes.NewReader(body))
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		Expect(presence).To(reportAgentPresenceSeen(1))

		Eventually(presence.Count).
			WithTimeout(500 * time.Millisecond).
			WithPolling(5 * time.Millisecond).
			Should(BeZero())
		Expect(presence).To(reportAgentPresenceSeen(0))
		Expect(counts).To(Equal([]int{1, 0}))

		cancel()
		Eventually(done).WithTimeout(250 * time.Millisecond).Should(BeClosed())
	})

	ginkgo.It("forwards private MCP requests after control-token authorization", func() {
		seen := forwardedMCPRequest{}
		handler := NewControlServer(ControlServerOptions{
			Token:    "control-token",
			Sessions: NewSessionRegistry(time.Minute, nil),
			PrivateMCP: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				seen.Path = req.URL.Path
				seen.Authorization = req.Header.Get("Authorization")
				raw, _ := io.ReadAll(req.Body)
				seen.Body = string(raw)
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

		Expect(resp).To(SatisfyAll(HaveHTTPStatus(http.StatusAccepted), HaveHTTPBody("mcp-ok")))
		Expect(seen).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Path":          Equal("/mcp"),
			"Authorization": Equal("Bearer session-api-key"),
		}))
		var forwardedBody map[string]any
		Expect(json.Unmarshal([]byte(seen.Body), &forwardedBody)).To(Succeed())
		Expect(forwardedBody).To(HaveKeyWithValue("jsonrpc", "2.0"))
	})

	ginkgo.DescribeTable("stdio auth verification",
		func(tc stdioAuthBoundaryCase) {
			handler := NewControlServer(ControlServerOptions{
				Token:        "control-token",
				Sessions:     NewSessionRegistry(time.Minute, nil),
				AuthDisabled: tc.authDisabled,
				VerifyAPIKey: tc.verify,
			})

			resp := performControlRequest(handler, http.MethodPost, "/stdio-auth/verify", "control-token", strings.NewReader(tc.body))

			if tc.wantCode == "" {
				Expect(resp).To(HaveHTTPStatus(tc.wantStatus))
				return
			}
			Expect(resp).To(testmatchers.HaveHTTPStructuredError(tc.wantStatus, tc.wantCode, tc.wantMessageID))
		},
		ginkgo.Entry("rejects malformed JSON", stdioAuthBoundaryCase{authDisabled: true, body: "{", wantStatus: http.StatusBadRequest, wantCode: errCodeStdioAuthInvalidRequest, wantMessageID: sharederrors.MessageIDForCode(errCodeStdioAuthInvalidRequest)}),
		ginkgo.Entry("accepts an empty key when auth is disabled", stdioAuthBoundaryCase{authDisabled: true, body: `{}`, wantStatus: http.StatusOK}),
		ginkgo.Entry("rejects API keys when auth is disabled", stdioAuthBoundaryCase{authDisabled: true, body: `{"apiKey":"lwk_key"}`, wantStatus: http.StatusConflict, wantCode: errCodeStdioAuthAPIKeyRejected, wantMessageID: sharederrors.MessageIDForCode(errCodeStdioAuthAPIKeyRejected)}),
		ginkgo.Entry("requires an API key when auth is enabled", stdioAuthBoundaryCase{body: `{}`, wantStatus: http.StatusUnauthorized, wantCode: errCodeStdioAuthAPIKeyRequired, wantMessageID: sharederrors.MessageIDForCode(errCodeStdioAuthAPIKeyRequired)}),
		ginkgo.Entry("requires a verifier when auth is enabled", stdioAuthBoundaryCase{body: `{"apiKey":"lwk_key"}`, wantStatus: http.StatusInternalServerError, wantCode: errCodeStdioAuthAPIKeyVerifierUnavailable, wantMessageID: sharederrors.MessageIDForCode(errCodeStdioAuthAPIKeyVerifierUnavailable)}),
		ginkgo.Entry("rejects invalid API keys", stdioAuthBoundaryCase{body: `{"apiKey":"lwk_key"}`, verify: func(string) error { return ErrInvalidAPIKey }, wantStatus: http.StatusUnauthorized, wantCode: errCodeStdioAuthAPIKeyInvalid, wantMessageID: sharederrors.MessageIDForCode(errCodeStdioAuthAPIKeyInvalid)}),
		ginkgo.Entry("surfaces verifier storage failures", stdioAuthBoundaryCase{body: `{"apiKey":"lwk_key"}`, verify: func(string) error { return errors.New("database is locked") }, wantStatus: http.StatusServiceUnavailable, wantCode: errCodeStdioAuthAPIKeyVerifierFailed, wantMessageID: sharederrors.MessageIDForCode(errCodeStdioAuthAPIKeyVerifierFailed)}),
		ginkgo.Entry("accepts valid API keys", stdioAuthBoundaryCase{body: `{"apiKey":"lwk_key"}`, verify: func(key string) error {
			if key != "lwk_key" {
				return errors.New("wrong key")
			}
			return nil
		}, wantStatus: http.StatusOK}),
	)

})

type forwardedMCPRequest struct {
	Path          string
	Authorization string
	Body          string
}

type forwardedControlHeaders struct {
	ControlToken string
	BearerToken  string
	ActorContext string
}

type stdioAuthBoundaryCase struct {
	authDisabled  bool
	verify        func(string) error
	body          string
	wantStatus    int
	wantCode      sharederrors.ErrorCode
	wantMessageID sharederrors.MessageID
}

func performControlRequest(handler http.Handler, method string, path controlPath, token string, body io.Reader) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()

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

func decodeControlResponse[T any](resp *httptest.ResponseRecorder) T {
	ginkgo.GinkgoHelper()

	var out T
	Expect(json.Unmarshal(resp.Body.Bytes(), &out)).To(Succeed())
	return out
}

func matchDaemonHealth(expected DaemonHealth) types.GomegaMatcher {
	return gstruct.PointTo(Equal(expected))
}

func matchControlHTTPError(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	wantMessageID := sharederrors.MessageIDForCode(code)
	return WithTransform(func(err error) *ControlHTTPError {
		ginkgo.GinkgoHelper()
		var controlErr *ControlHTTPError
		if errors.As(err, &controlErr) {
			return controlErr
		}
		return nil
	}, gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"StatusCode": Equal(status),
		"Code":       Equal(code),
		"MessageID":  Equal(wantMessageID),
		"Message":    Not(BeEmpty()),
	})))
}

func haveControlStatus(status int) types.GomegaMatcher {
	return WithTransform(func(err error) controlStatusObservation {
		ginkgo.GinkgoHelper()
		if IsControlStatus(err, status) {
			return controlStatusMatched
		}
		return controlStatusUnmatched
	}, Equal(controlStatusMatched))
}

type controlStatusObservation uint8

const (
	controlStatusUnmatched controlStatusObservation = iota
	controlStatusMatched
)

type privateMCPActorContextCase struct {
	actorContextHeader func(time.Time) string
	want               privateMCPActorContextBoundary
}

type privateMCPActorContextBoundary uint8

const (
	privateMCPActorContextMissing privateMCPActorContextBoundary = iota + 1
	privateMCPActorContextMalformed
	privateMCPActorContextMalformedJSON
	privateMCPActorContextInvalidWorkspace
	privateMCPActorContextExpired
	privateMCPActorContextValidationRejected
)

func matchPrivateMCPActorContextBoundary(want privateMCPActorContextBoundary) types.GomegaMatcher {
	return WithTransform(classifyPrivateMCPActorContextBoundary, Equal(want))
}

func classifyPrivateMCPActorContextBoundary(err error) privateMCPActorContextBoundary {
	switch {
	case errors.Is(err, errActorContextRequired):
		return privateMCPActorContextMissing
	case errors.Is(err, errDecodeActorContext):
		return privateMCPActorContextMalformed
	case errors.Is(err, errDecodeActorContextJSON):
		return privateMCPActorContextMalformedJSON
	case actorContextWorkspaceIDError(err):
		return privateMCPActorContextInvalidWorkspace
	case errors.Is(err, errActorContextExpired):
		return privateMCPActorContextExpired
	case err != nil:
		return privateMCPActorContextValidationRejected
	default:
		return 0
	}
}

func actorContextWorkspaceIDError(err error) bool {
	var validationErr *workspaceid.ValidationError
	return errors.As(err, &validationErr)
}

func encodedPrivateActorContext(ctx ActorContext) string {
	ginkgo.GinkgoHelper()
	encoded, err := EncodeActorContext(ctx)
	Expect(err).To(Succeed())
	return encoded
}

func encodedActorContextWire(wire actorContextWire) string {
	ginkgo.GinkgoHelper()
	raw, err := json.Marshal(wire)
	Expect(err).To(Succeed())
	return base64.RawURLEncoding.EncodeToString(raw)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
