package projectdaemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		Expect(counts).To(Equal([]int{1}))

		Expect(performControlRequest(handler, http.MethodPost, sessionHeartbeatPath(handle.ID), "control-token", nil)).To(HaveHTTPStatus(http.StatusOK))
		resp = performControlRequest(handler, http.MethodPost, "/sessions/missing/heartbeat", "control-token", nil)
		Expect(resp).To(testmatchers.HaveHTTPStructuredError(http.StatusNotFound, errCodeDaemonSessionNotFound, sharederrors.MessageIDForCode(errCodeDaemonSessionNotFound)))

		Expect(performControlRequest(handler, http.MethodDelete, sessionPath(handle.ID), "control-token", nil)).To(HaveHTTPStatus(http.StatusOK))
		Expect(sessions.Count()).To(BeZero())
		Expect(counts).To(Equal([]int{1, 0}))

		Expect(performControlRequest(handler, http.MethodDelete, "/sessions/missing", "control-token", nil)).To(HaveHTTPStatus(http.StatusOK))
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
		Expect(err).NotTo(HaveOccurred())

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

	ginkgo.It("lets the client call every control API and propagates structured errors", func() {
		var verifiedKey string
		expectedHealth := DaemonHealth{
			OK:            true,
			SchemaVersion: DescriptorSchemaVersion,
			PID:           123,
			DataDir:       "/data",
			RootDir:       "/root",
			ConfigHash:    "hash",
		}
		handler := NewControlServer(ControlServerOptions{
			Token:         "control-token",
			Sessions:      NewSessionRegistry(time.Minute, nil),
			AgentPresence: NewAgentPresenceRegistry(time.Minute, nil),
			Health:        expectedHealth,
			VerifyAPIKey: func(key string) error {
				verifiedKey = key
				return nil
			},
		})
		server := httptest.NewServer(handler)
		ginkgo.DeferCleanup(server.Close)
		client := NewClient(server.URL, "control-token")
		ctx := context.Background()

		health, err := client.Health(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(health).To(matchDaemonHealth(expectedHealth))
		handle, err := client.RegisterSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(handle.ID).NotTo(BeEmpty())
		Expect(client.HeartbeatSession(ctx, handle.ID)).To(Succeed())
		Expect(client.ReleaseSession(ctx, handle.ID)).To(Succeed())
		Expect(client.VerifyStdioAuth(ctx, "lwk_valid")).To(Succeed())
		cursorEvent := normalizedPresenceEvent(agenthooks.ProviderCursor, `{"hook_event_name":"sessionStart","session_id":"cursor-session"}`)
		Expect(client.RecordAgentPresence(ctx, cursorEvent)).To(Succeed())
		presenceSessions, err := client.ListAgentPresence(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(presenceSessions).To(ConsistOf(matchAgentPresenceSession(gstruct.Fields{
			"SessionIDHash": Equal(cursorEvent.SessionIDHash),
		})))
		Expect(verifiedKey).To(Equal("lwk_valid"))

		Expect(client.HeartbeatSession(ctx, newFixtureSessionID("missing"))).To(matchControlHTTPError(http.StatusNotFound, errCodeDaemonSessionNotFound))
		Expect(NewClient(server.URL, "wrong-token").Ping(ctx)).To(matchControlHTTPError(http.StatusUnauthorized, errCodeDaemonControlUnauthorized))
	})

	ginkgo.It("exposes structured control error text and status matching", func() {
		conflictCode := mustDecodeErrorCode("daemon_control_conflict")
			err := &ControlHTTPError{
				StatusCode: http.StatusConflict,
				Code:       conflictCode,
				MessageID:  sharederrors.MessageIDForCode(conflictCode),
				Message:    conflictCode.String(),
			}

		Expect(err).To(matchControlHTTPError(http.StatusConflict, conflictCode))
		Expect(err).To(haveControlStatus(http.StatusConflict))
		Expect(fmt.Errorf("wrapped: %w", err)).To(haveControlStatus(http.StatusConflict))
		Expect(err).NotTo(haveControlStatus(http.StatusUnauthorized))
		Expect(errors.New("plain")).NotTo(haveControlStatus(http.StatusConflict))
		Expect((*ControlHTTPError)(nil).Error()).To(BeEmpty())
	})

	ginkgo.It("returns decode errors for malformed JSON control responses", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Header.Get(ControlTokenHeader) != "control-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{"))
		}))
		ginkgo.DeferCleanup(server.Close)

		_, err := NewClient(server.URL, "control-token").Health(context.Background())

		Expect(err).To(matchProjectdaemonJSONSyntaxError())
	})

	ginkgo.It("adds control, bearer, and actor-context headers without mutating the original request", func() {
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
		Expect(err).NotTo(HaveOccurred())
		resp, err := client.Do(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Body.Close()).To(Succeed())

		Expect(forwardedControlHeaders{
			ControlToken: seenControlToken,
			BearerToken:  seenBearerToken,
			ActorContext: seenActorContext,
		}).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ControlToken": Equal("control-token"),
			"BearerToken":  Equal("Bearer stdio-api-key"),
			"ActorContext": Equal("encoded-actor"),
		}))
		Expect(req.Header).NotTo(SatisfyAny(
			HaveKey(ControlTokenHeader),
			HaveKey("Authorization"),
			HaveKey(ActorContextHeader),
		))
	})
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
	return WithTransform(func(err error) *ControlHTTPError {
		ginkgo.GinkgoHelper()
		var controlErr *ControlHTTPError
		if errors.As(err, &controlErr) {
			return controlErr
		}
		return nil
	}, gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"StatusCode": Equal(status),
	})))
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
