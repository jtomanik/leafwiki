package frontd

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

type observedWikidResolverRequest struct {
	Path         string
	Token        string
	Auth         string
	OriginalPath string
}

type observedSingleWorkspaceResolverRequest struct {
	Path         string
	OriginalPath string
}

type workspaceResolverAccessErrorCase struct {
	code int
	want error
}

var _ = Describe("wikid workspace resolver", func() {
	It("ensures the requested workspace and returns its workspaced route", func() {
		var seen observedWikidResolverRequest
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seen.Path = req.URL.Path
			seen.Token = req.Header.Get(projectdaemon.ControlTokenHeader)
			seen.Auth = req.Header.Get("Authorization")
			seen.OriginalPath = req.Header.Get("X-LeafWiki-Original-Path")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
			"workspace":{"id":"docs"},
			"status":{"workspaceId":"docs","state":"running","url":"http://127.0.0.1:49152"}
		}`))
		}))
		DeferCleanup(upstream.Close)

		resolve, err := NewWikidWorkspaceResolver(upstream.URL, "daemon-token")
		Expect(err).To(Succeed())
		req := httptest.NewRequest(http.MethodGet, "/api/workspaces/docs/tree", nil)
		req.Header.Set("Authorization", "Bearer public-token")

		route, err := resolve(req, workspaceid.WorkspaceID("docs"))
		Expect(err).To(Succeed())

		Expect(seen).To(SatisfyAll(
			HaveField("Path", Equal("/__leafwiki/workspaces/docs/ensure")),
			HaveField("Token", Equal("daemon-token")),
			HaveField("Auth", Equal("Bearer public-token")),
			HaveField("OriginalPath", Equal("/api/workspaces/docs/tree")),
		))
		Expect(route).To(SatisfyAll(
			HaveField("WorkspaceID", Equal(workspaceid.WorkspaceID("docs"))),
			HaveField("Upstream", Equal("http://127.0.0.1:49152")),
			HaveField("PrivateMCPURL", Equal("http://127.0.0.1:49152/mcp")),
		))
	})

	It("rejects invalid workspace IDs before ensuring them in wikid", func() {
		upstreamCalled := false
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			upstreamCalled = true
		}))
		DeferCleanup(upstream.Close)

		resolve, err := NewWikidWorkspaceResolver(upstream.URL, "daemon-token")
		Expect(err).To(Succeed())

		_, err = resolve(httptest.NewRequest(http.MethodGet, "/api/workspaces/%20docs/tree", nil), workspaceid.WorkspaceID(" docs"))
		Expect(err).To(MatchError(ErrWorkspaceNotFound))
		Expect(upstreamCalled).To(BeFalse())
	})

	It("preserves the original root MCP path when resolving a single workspace", func() {
		var seen observedSingleWorkspaceResolverRequest
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seen.Path = req.URL.Path
			seen.OriginalPath = req.Header.Get("X-LeafWiki-Original-Path")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"workspaces":[{"id":"home"}]}`))
		}))
		DeferCleanup(upstream.Close)

		resolve, err := NewWikidSingleWorkspaceResolver(upstream.URL, "daemon-token")
		Expect(err).To(Succeed())

		workspaceID, err := resolve(httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(err).To(Succeed())

		Expect(workspaceID).To(Equal(workspaceid.WorkspaceID("home")))
		Expect(seen).To(SatisfyAll(
			HaveField("Path", Equal("/__leafwiki/workspaces")),
			HaveField("OriginalPath", Equal("/mcp")),
		))
	})

	It("rejects ambiguous workspace lists for root MCP", func() {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"workspaces":[{"id":"home"},{"id":"docs"}]}`))
		}))
		DeferCleanup(upstream.Close)

		resolve, err := NewWikidSingleWorkspaceResolver(upstream.URL, "daemon-token")
		Expect(err).To(Succeed())

		_, err = resolve(httptest.NewRequest(http.MethodPost, "/mcp", nil))
		Expect(err).To(MatchError(ErrWorkspaceAmbiguous))
	})

	DescribeTable("access error mapping",
		func(tt workspaceResolverAccessErrorCase) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(tt.code)
			}))
			DeferCleanup(upstream.Close)
			resolve, err := NewWikidWorkspaceResolver(upstream.URL, "daemon-token")
			Expect(err).To(Succeed())

			_, err = resolve(httptest.NewRequest(http.MethodGet, "/api/workspaces/docs/tree", nil), workspaceid.WorkspaceID("docs"))
			Expect(err).To(MatchError(tt.want))
		},
		Entry("maps not-found responses", workspaceResolverAccessErrorCase{code: http.StatusNotFound, want: ErrWorkspaceNotFound}),
		Entry("maps forbidden responses", workspaceResolverAccessErrorCase{code: http.StatusForbidden, want: ErrWorkspaceForbidden}),
	)
})

var _ = Describe("wikid workspace resolver constructors", func() {
	It("reject invalid upstreams and missing daemon tokens", func() {
		_, err := NewWikidWorkspaceResolver("://bad", "token")
		Expect(err).To(MatchError(errInvalidWikidUpstream))
		_, err = NewWikidWorkspaceResolver("http://127.0.0.1:1", " ")
		Expect(err).To(MatchError(errDaemonTokenRequired))

		_, err = NewWikidSingleWorkspaceResolver("://bad", "token")
		Expect(err).To(MatchError(errInvalidWikidUpstream))
		_, err = NewWikidSingleWorkspaceResolver("http://127.0.0.1:1", " ")
		Expect(err).To(MatchError(errDaemonTokenRequired))
	})
})

var _ = Describe("wikid workspace resolver request construction", func() {
	It("returns request construction errors before contacting wikid", func() {
		originalNewRequest := newFrontdRequestWithContext
		requestErr := errors.New("frontd request failed")
		newFrontdRequestWithContext = func(_ context.Context, _ string, _ string, _ io.Reader) (*http.Request, error) {
			return nil, requestErr
		}
		DeferCleanup(func() {
			newFrontdRequestWithContext = originalNewRequest
		})

		resolver, err := NewWikidWorkspaceResolver("http://127.0.0.1:1", "token")
		Expect(err).To(Succeed())
		_, err = resolver(nil, "home")
		Expect(err).To(MatchError(requestErr))

		singleResolver, err := NewWikidSingleWorkspaceResolver("http://127.0.0.1:1", "token")
		Expect(err).To(Succeed())
		_, err = singleResolver(nil)
		Expect(err).To(MatchError(requestErr))
	})
})
