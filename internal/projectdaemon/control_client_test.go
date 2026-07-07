package projectdaemon

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/perber/wiki/internal/agenthooks"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

var _ = ginkgo.Describe("project daemon control client", ginkgo.Label("unit"), func() {
	ginkgo.It("builds authenticated JSON requests through the configured transport", func() {
		var requests []controlClientRequest
		client := NewClient("http://daemon-control.local/", "control-token")
		client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests = append(requests, controlClientRequest{
				Method:       req.Method,
				Path:         req.URL.EscapedPath(),
				ControlToken: req.Header.Get(ControlTokenHeader),
				ContentType:  req.Header.Get("Content-Type"),
			})
			switch req.URL.EscapedPath() {
			case "/health":
				return jsonResponse(http.StatusOK, `{"ok":true,"schemaVersion":1,"pid":1234,"dataDir":"/data","rootDir":"/root","configHash":"hash"}`), nil
			case "/sessions":
				return jsonResponse(http.StatusOK, `{"id":"session-1"}`), nil
			case "/sessions/session-1/heartbeat", "/sessions/session-1":
				return jsonResponse(http.StatusNoContent, ``), nil
			case "/stdio-auth/verify", "/agent-presence/events":
				return jsonResponse(http.StatusNoContent, ``), nil
			case "/agent-presence":
				return jsonResponse(http.StatusOK, `[{"provider":"codex","sessionIdHash":"hash"}]`), nil
			default:
				return jsonResponse(http.StatusNotFound, ``), nil
			}
		})}

		health, err := client.Health(context.Background())
		Expect(err).To(Succeed())
		session, err := client.RegisterSession(context.Background())
		Expect(err).To(Succeed())
		Expect(client.HeartbeatSession(context.Background(), session.ID)).To(Succeed())
		Expect(client.ReleaseSession(context.Background(), session.ID)).To(Succeed())
		Expect(client.VerifyStdioAuth(context.Background(), "api-key")).To(Succeed())
		Expect(client.RecordAgentPresence(context.Background(), agenthooks.Event{
			Provider:      agenthooks.ProviderCodex,
			SessionIDHash: "hash",
		})).To(Succeed())
		presence, err := client.ListAgentPresence(context.Background())
		Expect(err).To(Succeed())

		Expect(health).To(SatisfyAll(
			matchDaemonHealthAvailability(daemonHealthAvailable),
			gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ConfigHash": Equal("hash"),
			})),
		))
		Expect(session).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ID": Equal(newFixtureSessionID("session-1")),
		})))
		Expect(presence).To(ConsistOf(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Provider":      Equal(agenthooks.ProviderCodex),
			"SessionIDHash": Equal("hash"),
		})))
		Expect(requests).To(ContainElements(
			Equal(controlClientRequest{Method: http.MethodGet, Path: "/health", ControlToken: "control-token"}),
			Equal(controlClientRequest{Method: http.MethodPost, Path: "/sessions", ControlToken: "control-token", ContentType: "application/json"}),
			Equal(controlClientRequest{Method: http.MethodPost, Path: "/sessions/session-1/heartbeat", ControlToken: "control-token", ContentType: "application/json"}),
			Equal(controlClientRequest{Method: http.MethodDelete, Path: "/sessions/session-1", ControlToken: "control-token"}),
			Equal(controlClientRequest{Method: http.MethodPost, Path: "/stdio-auth/verify", ControlToken: "control-token", ContentType: "application/json"}),
			Equal(controlClientRequest{Method: http.MethodPost, Path: "/agent-presence/events", ControlToken: "control-token", ContentType: "application/json"}),
			Equal(controlClientRequest{Method: http.MethodGet, Path: "/agent-presence", ControlToken: "control-token"}),
		))
	})

	ginkgo.It("reports empty session handles and structured control errors", func() {
		client := NewClient("http://daemon-control.local", "control-token")
		responses := []controlClientResponse{
			{Status: http.StatusOK, Body: `{"id":""}`},
			{Status: http.StatusUnauthorized, Body: `{"error":{"code":"daemon_control_unauthorized","messageId":"errors.daemon.control_unauthorized","message":"unauthorized"}}`},
			{Status: http.StatusTeapot, Body: `plain status`},
		}
		client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			next := responses[0]
			responses = responses[1:]
			return jsonResponse(next.Status, next.Body), nil
		})}

		_, err := client.RegisterSession(context.Background())
		Expect(err).To(MatchError(errDaemonEmptySessionID))

		err = client.VerifyStdioAuth(context.Background(), "api-key")
		Expect(err).To(SatisfyAll(
			matchControlHTTPError(http.StatusUnauthorized, errCodeDaemonControlUnauthorized),
			withControlErrorMessageID(sharederrors.MessageIDForCode(errCodeDaemonControlUnauthorized)),
		))
		Expect(IsControlStatus(err, http.StatusUnauthorized)).To(matchControlStatus(controlStatusMatched))
		Expect(IsControlStatus(err, http.StatusForbidden)).To(matchControlStatus(controlStatusUnmatched))

		err = client.VerifyStdioAuth(context.Background(), "api-key")
		Expect(err).To(matchPlainControlHTTPStatus(http.StatusTeapot))
	})

	ginkgo.It("adds authentication headers without mutating the original request", func() {
		actorContext := "actor-context"
		req, err := http.NewRequest(http.MethodGet, "http://workspace.local/mcp", nil)
		Expect(err).To(Succeed())
		req.Header.Set("Authorization", "Bearer original")

		var forwarded http.Header
		transport := AuthRoundTripper{
			Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				forwarded = req.Header.Clone()
				return jsonResponse(http.StatusNoContent, ``), nil
			}),
			ControlToken: "control-token",
			BearerToken:  " bearer-token ",
			ActorContext: " " + actorContext + " ",
		}
		resp, err := transport.RoundTrip(req)
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(resp.Body.Close()).To(Succeed())
		})

		Expect(controlForwardingHeadersFor(forwarded)).To(Equal(controlForwardingHeaders{
			ControlToken: "control-token",
			BearerToken:  "Bearer bearer-token",
			ActorContext: actorContext,
		}))
		Expect(req).To(HaveField("Header", HaveKeyWithValue("Authorization", ConsistOf("Bearer original"))))
	})

	ginkgo.It("preserves control path and session identifier semantics", func() {
		sessionID := newFixtureSessionID("session-1")

		Expect(sessionID).To(Equal(newFixtureSessionID("session-1")))
		Expect(sessionPath(sessionID)).To(Equal(controlPath("/sessions/session-1")))
		Expect(sessionHeartbeatPath(sessionID)).To(Equal(controlPath("/sessions/session-1/heartbeat")))
		Expect(controlPath("/health").String()).To(Equal("/health"))
		Expect(controlClientObservationFor(NewClient("http://daemon-control.local/", "token"))).To(Equal(controlClientObservation{
			BaseURL:       "http://daemon-control.local",
			Token:         "token",
			ClientTimeout: 5 * time.Second,
		}))
	})
})

type controlClientRequest struct {
	Method       string
	Path         string
	ControlToken string
	ContentType  string
}

type controlClientResponse struct {
	Status int
	Body   string
}

type controlClientObservation struct {
	BaseURL       string
	Token         string
	ClientTimeout time.Duration
}

type controlForwardingHeaders struct {
	ControlToken string
	BearerToken  string
	ActorContext string
}

func controlForwardingHeadersFor(header http.Header) controlForwardingHeaders {
	return controlForwardingHeaders{
		ControlToken: header.Get(ControlTokenHeader),
		BearerToken:  header.Get("Authorization"),
		ActorContext: header.Get(ActorContextHeader),
	}
}

func controlClientObservationFor(client *Client) controlClientObservation {
	if client == nil || client.httpClient == nil {
		return controlClientObservation{}
	}
	return controlClientObservation{
		BaseURL:       client.baseURL,
		Token:         client.token,
		ClientTimeout: client.httpClient.Timeout,
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func withControlErrorMessageID(messageID sharederrors.MessageID) types.GomegaMatcher {
	return WithTransform(func(err error) sharederrors.MessageID {
		var controlErr *ControlHTTPError
		if errors.As(err, &controlErr) {
			return controlErr.MessageID
		}
		return ""
	}, Equal(messageID))
}

func matchPlainControlHTTPStatus(status int) types.GomegaMatcher {
	var noCode sharederrors.ErrorCode
	var noMessageID sharederrors.MessageID
	return WithTransform(func(err error) *ControlHTTPError {
		var controlErr *ControlHTTPError
		if errors.As(err, &controlErr) {
			return controlErr
		}
		return nil
	}, gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"StatusCode": Equal(status),
		"Code":       Equal(noCode),
		"MessageID":  Equal(noMessageID),
		"Message":    Not(BeEmpty()),
	})))
}

func matchControlStatus(want controlStatusObservation) types.GomegaMatcher {
	return WithTransform(func(matched bool) controlStatusObservation {
		if matched {
			return controlStatusMatched
		}
		return controlStatusUnmatched
	}, Equal(want))
}

type daemonHealthObservation uint8

const (
	daemonHealthUnavailable daemonHealthObservation = iota
	daemonHealthAvailable
)

func matchDaemonHealthAvailability(want daemonHealthObservation) types.GomegaMatcher {
	return WithTransform(func(health *DaemonHealth) daemonHealthObservation {
		if health != nil && health.OK {
			return daemonHealthAvailable
		}
		return daemonHealthUnavailable
	}, Equal(want))
}
