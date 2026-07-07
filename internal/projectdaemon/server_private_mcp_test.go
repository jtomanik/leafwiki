package projectdaemon

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("project daemon private MCP actor context", ginkgo.Label("integration"), func() {
	ginkgo.It("preserves decodable actor-context headers for private MCP handlers", func() {
		now := time.Date(2026, 7, 7, 9, 30, 0, 0, time.UTC)
		encodedActor, err := EncodeActorContext(ActorContext{
			Version:     1,
			Issuer:      ActorContextIssuerWikid,
			Subject:     "user:editor-1",
			Username:    "editor",
			Email:       "editor@example.com",
			Role:        "editor",
			WorkspaceID: mustDecodeWorkspaceID("docs"),
			AuthMethod:  "cookie",
			SessionID:   newFixtureSessionID("session-1"),
			IssuedAt:    now,
			ExpiresAt:   now.Add(5 * time.Minute),
		})
		Expect(err).To(Succeed())
		var seen ActorContext
		handler := NewControlServer(ControlServerOptions{
			Token:    "control-token",
			Sessions: NewSessionRegistry(time.Minute, nil),
			PrivateMCP: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				decoded, decodeErr := DecodeActorContext(req.Header.Get(ActorContextHeader), ActorContextValidation{
					Now:         now,
					WorkspaceID: mustDecodeWorkspaceID("docs"),
				})
				Expect(decodeErr).To(Succeed())
				seen = decoded
				w.WriteHeader(http.StatusAccepted)
			}),
			AuthDisabled: true,
		})

		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0"}`))
		req.Header.Set(ControlTokenHeader, "control-token")
		req.Header.Set(ActorContextHeader, encodedActor)
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)

		Expect(resp).To(HaveHTTPStatus(http.StatusAccepted))
		Expect(seen).To(matchActorContextIdentity("editor-1", "editor", "cookie"))
	})

	ginkgo.DescribeTable("actor-context validation",
		func(tc privateMCPActorContextCase) {
			now := time.Date(2026, 7, 7, 9, 45, 0, 0, time.UTC)
			var seenErr error
			handler := NewControlServer(ControlServerOptions{
				Token:    "control-token",
				Sessions: NewSessionRegistry(time.Minute, nil),
				PrivateMCP: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					_, seenErr = DecodeActorContext(req.Header.Get(ActorContextHeader), ActorContextValidation{
						Now:         now,
						WorkspaceID: mustDecodeWorkspaceID("docs"),
					})
					if seenErr != nil {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					w.WriteHeader(http.StatusAccepted)
				}),
				AuthDisabled: true,
			})
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0"}`))
			req.Header.Set(ControlTokenHeader, "control-token")
			if tc.actorContextHeader != nil {
				req.Header.Set(ActorContextHeader, tc.actorContextHeader(now))
			}
			resp := httptest.NewRecorder()

			handler.ServeHTTP(resp, req)

			Expect(resp).To(HaveHTTPStatus(http.StatusUnauthorized))
			Expect(seenErr).To(matchPrivateMCPActorContextBoundary(tc.want))
		},
		ginkgo.Entry("rejects a missing actor context", privateMCPActorContextCase{
			want: privateMCPActorContextMissing,
		}),
		ginkgo.Entry("rejects a malformed actor context envelope", privateMCPActorContextCase{
			actorContextHeader: func(time.Time) string { return "%%%invalid-base64" },
			want:               privateMCPActorContextMalformed,
		}),
		ginkgo.Entry("rejects actor context JSON that cannot be decoded", privateMCPActorContextCase{
			actorContextHeader: func(time.Time) string {
				return base64.RawURLEncoding.EncodeToString([]byte("{"))
			},
			want: privateMCPActorContextMalformedJSON,
		}),
		ginkgo.Entry("rejects actor context with an invalid workspace identity", privateMCPActorContextCase{
			actorContextHeader: func(now time.Time) string {
				return encodedActorContextWire(actorContextWire{
					Version:     1,
					Issuer:      ActorContextIssuerWikid,
					Subject:     "user:editor",
					WorkspaceID: "not valid",
					ExpiresAt:   now.Add(time.Minute),
				})
			},
			want: privateMCPActorContextInvalidWorkspace,
		}),
		ginkgo.Entry("rejects actor context for another workspace", privateMCPActorContextCase{
			actorContextHeader: func(now time.Time) string {
				return encodedPrivateActorContext(ActorContext{
					Version:     1,
					Issuer:      ActorContextIssuerWikid,
					Subject:     "user:editor",
					WorkspaceID: mustDecodeWorkspaceID("other"),
					ExpiresAt:   now.Add(time.Minute),
				})
			},
			want: privateMCPActorContextValidationRejected,
		}),
		ginkgo.Entry("rejects stale actor context", privateMCPActorContextCase{
			actorContextHeader: func(now time.Time) string {
				return encodedPrivateActorContext(ActorContext{
					Version:     1,
					Issuer:      ActorContextIssuerWikid,
					Subject:     "user:editor",
					WorkspaceID: mustDecodeWorkspaceID("docs"),
					ExpiresAt:   now.Add(-time.Second),
				})
			},
			want: privateMCPActorContextExpired,
		}),
	)
})
