package projectdaemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/agenthooks"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

var _ = ginkgo.Describe("project daemon control client server boundary", ginkgo.Label("integration"), func() {
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
		Expect(err).To(Succeed())
		Expect(health).To(matchDaemonHealth(expectedHealth))
		handle, err := client.RegisterSession(ctx)
		Expect(err).To(Succeed())
		Expect(handle.ID).NotTo(BeEmpty())
		Expect(client.HeartbeatSession(ctx, handle.ID)).To(Succeed())
		Expect(client.ReleaseSession(ctx, handle.ID)).To(Succeed())
		Expect(client.VerifyStdioAuth(ctx, "lwk_valid")).To(Succeed())
		cursorEvent := normalizedPresenceEvent(agenthooks.ProviderCursor, `{"hook_event_name":"sessionStart","session_id":"cursor-session"}`)
		Expect(client.RecordAgentPresence(ctx, cursorEvent)).To(Succeed())
		presenceSessions, err := client.ListAgentPresence(ctx)
		Expect(err).To(Succeed())
		Expect(presenceSessions).To(ConsistOf(matchAgentPresenceSession(gstruct.Fields{
			"SessionIDHash": Equal(cursorEvent.SessionIDHash),
		})))
		Expect(verifiedKey).To(Equal("lwk_valid"))

		Expect(client.HeartbeatSession(ctx, newFixtureSessionID("missing"))).To(matchControlHTTPError(http.StatusNotFound, errCodeDaemonSessionNotFound))
		Expect(NewClient(server.URL, "wrong-token").Ping(ctx)).To(matchControlHTTPError(http.StatusUnauthorized, errCodeDaemonControlUnauthorized))
	})

	ginkgo.It("exposes structured control error text and status matching", func() {
		detail := sharederrors.NewLocalizedErrorDetailFromCode(errCodeDaemonSessionNotFound)
		err := &ControlHTTPError{
			StatusCode: http.StatusNotFound,
			Code:       detail.Code,
			MessageID:  detail.MessageID,
			Message:    detail.Message,
		}

		Expect(err).To(matchControlHTTPError(http.StatusNotFound, errCodeDaemonSessionNotFound))
		Expect(err).To(haveControlStatus(http.StatusNotFound))
		Expect(fmt.Errorf("wrapped: %w", err)).To(haveControlStatus(http.StatusNotFound))
		Expect(err).NotTo(haveControlStatus(http.StatusUnauthorized))
		Expect(errors.New("plain")).NotTo(haveControlStatus(http.StatusNotFound))
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
		Expect(err).To(Succeed())
		resp, err := client.Do(req)
		Expect(err).To(Succeed())
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
