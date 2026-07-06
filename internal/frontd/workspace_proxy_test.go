package frontd

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

type observedWorkspaceRouterRequest struct {
	Path         string
	Query        string
	Token        string
	ActorContext string
	Body         string
}

type workspaceRouterProxyResolverErrorCase struct {
	err           error
	wantStatus    int
	wantCode      sharederrors.ErrorCode
	wantMessageID sharederrors.MessageID
}

type workspaceRouterProxyDependencyErrorCase struct {
	opts          WorkspaceRouterProxyOptions
	wantCode      sharederrors.ErrorCode
	wantMessageID sharederrors.MessageID
}

var _ = Describe("workspace router proxy", Label("integration"), func() {
	It("resolves a workspace route and rewrites the public API path", func() {
		now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
		var seen observedWorkspaceRouterRequest
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seen.Path = req.URL.Path
			seen.Query = req.URL.RawQuery
			seen.Token = req.Header.Get(projectdaemon.ControlTokenHeader)
			seen.ActorContext = req.Header.Get(projectdaemon.ActorContextHeader)
			raw, _ := io.ReadAll(req.Body)
			seen.Body = string(raw)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("proxied"))
		}))
		DeferCleanup(upstream.Close)

		proxy := NewWorkspaceRouterProxy(WorkspaceRouterProxyOptions{
			Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{WorkspaceID: mustDecodeWorkspaceID("home"), Upstream: upstream.URL, DaemonToken: "private-token"}, nil
			},
			Actor: func(_ *http.Request, workspaceID workspaceid.WorkspaceID) (projectdaemon.ActorContext, error) {
				return projectdaemon.ActorContext{
					Version:     1,
					Issuer:      projectdaemon.ActorContextIssuerWikid,
					Subject:     "user:admin",
					Role:        "admin",
					WorkspaceID: workspaceID,
					AuthMethod:  "cookie",
					IssuedAt:    now,
					ExpiresAt:   now.Add(5 * time.Minute),
				}, nil
			},
		})

		req := httptest.NewRequest(http.MethodPost, "/api/workspaces/home/tree?depth=1", strings.NewReader("body"))
		req.Header.Set("Authorization", "Bearer public")
		req.Header.Set("Cookie", "leafwiki_at=public")
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, req)

		Expect(rec).To(HaveHTTPStatus(http.StatusCreated))
		Expect(rec).To(HaveHTTPBody("proxied"))
		Expect(seen).To(SatisfyAll(
			HaveField("Path", Equal("/api/tree")),
			HaveField("Query", Equal("depth=1")),
			HaveField("Body", Equal("body")),
			HaveField("Token", Equal("private-token")),
		))
		decoded, err := projectdaemon.DecodeActorContext(seen.ActorContext, projectdaemon.ActorContextValidation{Now: now.Add(time.Minute), WorkspaceID: mustDecodeWorkspaceID("home")})
		Expect(err).To(Succeed())
		Expect(decoded).To(matchFrontdActorSubjectID("admin"))
	})

	It("rewrites workspace asset paths to the static asset route", func() {
		var seenPath string
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seenPath = req.URL.Path
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("asset"))
		}))
		DeferCleanup(upstream.Close)

		proxy := NewWorkspaceRouterProxy(WorkspaceRouterProxyOptions{
			Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{WorkspaceID: mustDecodeWorkspaceID("docs"), Upstream: upstream.URL, DaemonToken: "private-token"}, nil
			},
			Actor: func(_ *http.Request, workspaceID workspaceid.WorkspaceID) (projectdaemon.ActorContext, error) {
				now := time.Now().UTC()
				return projectdaemon.ActorContext{
					Version:     1,
					Issuer:      projectdaemon.ActorContextIssuerWikid,
					Subject:     "user:admin",
					Role:        "admin",
					WorkspaceID: workspaceID,
					AuthMethod:  "cookie",
					IssuedAt:    now,
					ExpiresAt:   now.Add(5 * time.Minute),
				}, nil
			},
		})

		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspaces/docs/assets/page-id/upload-test.png", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(seenPath).To(Equal("/assets/page-id/upload-test.png"))
	})

	DescribeTable("resolver errors",
		func(tt workspaceRouterProxyResolverErrorCase) {
			proxy := NewWorkspaceRouterProxy(WorkspaceRouterProxyOptions{
				Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
					return WorkspaceRoute{}, tt.err
				},
				Actor: func(*http.Request, workspaceid.WorkspaceID) (projectdaemon.ActorContext, error) {
					return projectdaemon.ActorContext{}, nil
				},
			})
			rec := httptest.NewRecorder()
			proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspaces/home/tree", nil))

			Expect(rec).To(matchStructuredFrontdError(tt.wantStatus, tt.wantCode, tt.wantMessageID))
		},
		Entry("maps not-found errors", workspaceRouterProxyResolverErrorCase{err: ErrWorkspaceNotFound, wantStatus: http.StatusNotFound, wantCode: errCodeWorkspaceNotFound, wantMessageID: sharederrors.MessageIDForCode(errCodeWorkspaceNotFound)}),
		Entry("maps forbidden errors", workspaceRouterProxyResolverErrorCase{err: ErrWorkspaceForbidden, wantStatus: http.StatusForbidden, wantCode: errCodeWorkspaceForbidden, wantMessageID: sharederrors.MessageIDForCode(errCodeWorkspaceForbidden)}),
	)

	It("maps unexpected resolver errors to workspace-unavailable responses", func() {
		proxy := NewWorkspaceRouterProxy(WorkspaceRouterProxyOptions{
			Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return WorkspaceRoute{}, errors.New("boom")
			},
			Actor: func(*http.Request, workspaceid.WorkspaceID) (projectdaemon.ActorContext, error) {
				return projectdaemon.ActorContext{}, nil
			},
		})
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspaces/home/tree", nil))

		Expect(rec).To(matchStructuredFrontdError(
			http.StatusServiceUnavailable,
			errCodeWorkspaceUnavailable,
			sharederrors.MessageIDForCode(errCodeWorkspaceUnavailable),
		))
	})

	DescribeTable("dependency errors",
		func(tt workspaceRouterProxyDependencyErrorCase) {
			proxy := NewWorkspaceRouterProxy(tt.opts)
			rec := httptest.NewRecorder()
			proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workspaces/home/tree", nil))

			Expect(rec).To(matchStructuredFrontdError(http.StatusServiceUnavailable, tt.wantCode, tt.wantMessageID))
		},
		Entry("reports missing resolver dependency", workspaceRouterProxyDependencyErrorCase{
			opts:          WorkspaceRouterProxyOptions{},
			wantCode:      errCodeWorkspaceResolverUnavailable,
			wantMessageID: sharederrors.MessageIDForCode(errCodeWorkspaceResolverUnavailable),
		}),
		Entry("reports missing actor resolver dependency", workspaceRouterProxyDependencyErrorCase{
			opts: WorkspaceRouterProxyOptions{
				Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
					return WorkspaceRoute{WorkspaceID: mustDecodeWorkspaceID("home"), Upstream: "http://127.0.0.1:1", DaemonToken: "token"}, nil
				},
			},
			wantCode:      errCodeWorkspaceActorContextUnavailable,
			wantMessageID: sharederrors.MessageIDForCode(errCodeWorkspaceActorContextUnavailable),
		}),
	)
})
