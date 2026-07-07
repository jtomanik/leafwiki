package frontd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = Describe("frontd path and session primitives", Label("unit"), func() {
	DescribeTable("path family classification",
		func(path string, want []frontdIngressPathFamily) {
			Expect(frontdIngressFamiliesFor(path)).To(Equal(want))
		},
		Entry("classifies workspace list requests", "/api/workspaces", []frontdIngressPathFamily{frontdIngressWorkspaces}),
		Entry("classifies workspace ensure requests", "/api/workspaces/home/ensure", []frontdIngressPathFamily{frontdIngressWorkspaces, frontdIngressWorkspace}),
		Entry("classifies MCP root transport", "/mcp", []frontdIngressPathFamily{frontdIngressMCP}),
		Entry("classifies workspace MCP transport", "/mcp/workspaces/home", []frontdIngressPathFamily{frontdIngressMCP}),
		Entry("classifies public workspace assets", PublicWorkspacesPrefix+"/home/assets/logo.png", []frontdIngressPathFamily{frontdIngressWorkspace}),
		Entry("classifies workspace API routes", "/api/tree/home", []frontdIngressPathFamily{frontdIngressWorkspace}),
		Entry("classifies control-plane API routes", "/api/auth/login", []frontdIngressPathFamily{frontdIngressControlPlane}),
		Entry("classifies root well-known metadata", "/.well-known/oauth-authorization-server", []frontdIngressPathFamily{frontdIngressControlPlane, frontdIngressWellKnown}),
		Entry("classifies public frontend routes", "/about", []frontdIngressPathFamily{frontdIngressPublic}),
	)

	It("rejects paths outside the configured public base path", func() {
		Expect(basePathResult("/public/api/tree", "/wiki")).To(Equal(frontdBasePathResult{
			Outcome: frontdPathRejected,
			Path:    "/public/api/tree",
		}))
	})

	DescribeTable("public base path stripping",
		func(path string, basePath string, want frontdBasePathResult) {
			Expect(basePathResult(path, basePath)).To(Equal(want))
		},
		Entry("treats an empty base as the public root", "", "", frontdBasePathResult{
			Outcome: frontdPathAccepted,
			Path:    "/",
		}),
		Entry("maps the configured base path to the public root", "/wiki", " /wiki/ ", frontdBasePathResult{
			Outcome: frontdPathAccepted,
			Path:    "/",
		}),
		Entry("strips the configured base path from nested routes", "/wiki/api/tree", "/wiki", frontdBasePathResult{
			Outcome: frontdPathAccepted,
			Path:    "/api/tree",
		}),
		Entry("rejects sibling prefixes that only share text", "/wikid/api/tree", "/wiki", frontdBasePathResult{
			Outcome: frontdPathRejected,
			Path:    "/wikid/api/tree",
		}),
	)

	DescribeTable("public workspace API route translation",
		func(method string, path string, want frontdWorkspaceAPIRoute) {
			Expect(workspaceAPIRouteFor(method, path)).To(Equal(want))
		},
		Entry("maps workspace listing to the private control route", http.MethodGet, PublicWorkspacesPrefix, frontdWorkspaceAPIRoute{
			Outcome:     frontdWorkspaceAPIRouteAccepted,
			PrivatePath: "/__leafwiki/workspaces",
		}),
		Entry("maps workspace status lookups to the private control route", http.MethodGet, PublicWorkspacesPrefix+"/home/status", frontdWorkspaceAPIRoute{
			Outcome:     frontdWorkspaceAPIRouteAccepted,
			PrivatePath: "/__leafwiki/workspaces/home/status",
		}),
		Entry("maps workspace ensure requests to the private control route", http.MethodPost, PublicWorkspacesPrefix+"/home/ensure", frontdWorkspaceAPIRoute{
			Outcome:     frontdWorkspaceAPIRouteAccepted,
			PrivatePath: "/__leafwiki/workspaces/home/ensure",
		}),
		Entry("rejects unsupported workspace methods", http.MethodDelete, PublicWorkspacesPrefix+"/home/status", frontdWorkspaceAPIRoute{
			Outcome: frontdWorkspaceAPIRouteRejected,
		}),
		Entry("rejects malformed workspace paths", http.MethodGet, PublicWorkspacesPrefix+"/home/status/extra", frontdWorkspaceAPIRoute{
			Outcome: frontdWorkspaceAPIRouteRejected,
		}),
	)

	It("clones request paths without changing the original body or headers", func() {
		body := io.NopCloser(strings.NewReader("request body"))
		req, err := http.NewRequest(http.MethodPost, "http://frontd.local/base/tree", body)
		Expect(err).To(Succeed())
		req.URL.RawPath = "/base/%74ree"
		req.Header.Set("X-Original", "present")

		clone := cloneRequestPath(req, "/api/tree")

		Expect(requestPathObservationFor(clone)).To(Equal(requestPathObservation{
			Path:    "/api/tree",
			RawPath: "",
			Header:  "present",
		}))
		Expect(clone.Body).To(Equal(req.Body))
		Expect(requestPathObservationFor(req)).To(Equal(requestPathObservation{
			Path:    "/base/tree",
			RawPath: "/base/%74ree",
			Header:  "present",
		}))
	})

	It("looks up and clears MCP session workspace bindings", func() {
		bindings := NewMCPSessionBindings()
		sessionID := MCPSessionIDFromHeader(" session-1 ")
		workspaceID := mustDecodeWorkspaceID("home")
		var emptyWorkspaceID workspaceid.WorkspaceID

		Expect(bindings.Bind(MCPSessionID{}, workspaceID)).To(Succeed())
		Expect(bindings.Bind(sessionID, emptyWorkspaceID)).To(Succeed())
		Expect(bindings.Bind(sessionID, workspaceID)).To(Succeed())
		Expect(mcpSessionLookupFor(bindings, sessionID)).To(Equal(mcpSessionLookup{
			Outcome:     mcpSessionBound,
			WorkspaceID: workspaceID,
		}))

		bindings.Unbind(MCPSessionID{})
		Expect(mcpSessionLookupFor(bindings, sessionID)).To(Equal(mcpSessionLookup{
			Outcome:     mcpSessionBound,
			WorkspaceID: workspaceID,
		}))
		bindings.Unbind(sessionID)
		Expect(mcpSessionLookupFor(bindings, sessionID)).To(Equal(mcpSessionLookup{
			Outcome: mcpSessionUnbound,
		}))
	})

	It("parses explicit workspace MCP paths into typed workspace IDs", func() {
		Expect(workspaceMCPPathResult("/mcp/workspaces/home/messages")).To(ResolveWorkspaceMCPPath(mustDecodeWorkspaceID("home")))
	})

	It("classifies upstream constructor failures without losing sentinel identity", func() {
		_, wikidErr := NewControlPlaneProxy("http://", "control-token")
		_, workspacedErr := NewWorkspaceProxy(WorkspaceProxyOptions{
			Upstream:    "http://",
			DaemonToken: "control-token",
			Actor: func(*http.Request) (projectdaemon.ActorContext, error) {
				return projectdaemon.ActorContext{}, nil
			},
		})
		_, tokenErr := NewMCPProxy("http://127.0.0.1:43111", " ")
		_, actorErr := NewWorkspaceProxy(WorkspaceProxyOptions{
			Upstream:    "http://127.0.0.1:43111",
			DaemonToken: "control-token",
		})

		Expect(frontdUpstreamErrorFor(wikidErr)).To(Equal(frontdWikidUpstreamRejected))
		Expect(frontdUpstreamErrorFor(workspacedErr)).To(Equal(frontdWorkspacedUpstreamRejected))
		Expect(tokenErr).To(MatchError(errDaemonTokenRequired))
		Expect(actorErr).To(MatchError(errActorContextResolverRequired))
	})

	It("writes retryable structured errors for unavailable private upstreams", func() {
		rec := httptest.NewRecorder()

		retryableUnavailable(rec, httptest.NewRequest(http.MethodGet, "/api/tree", nil), io.ErrClosedPipe)

		Expect(rec).To(SatisfyAll(
			matchStructuredFrontdError(
				http.StatusServiceUnavailable,
				errCodeWorkspacedUnavailable,
				sharederrors.MessageIDForCode(errCodeWorkspacedUnavailable),
			),
			HaveHTTPHeaderWithValue("Retry-After", "1"),
		))
	})

	DescribeTable("leading slash normalization",
		func(path string, want string) {
			Expect(ensureLeadingSlash(path)).To(Equal(want))
		},
		Entry("preserves rooted paths", "/api/tree", "/api/tree"),
		Entry("roots relative paths", "api/tree", "/api/tree"),
	)
})

type frontdIngressPathFamily uint8

const (
	frontdIngressPublic frontdIngressPathFamily = iota + 1
	frontdIngressWorkspaces
	frontdIngressMCP
	frontdIngressWorkspace
	frontdIngressControlPlane
	frontdIngressWellKnown
)

func frontdIngressFamiliesFor(path string) []frontdIngressPathFamily {
	var families []frontdIngressPathFamily
	if isWorkspacesPath(path) {
		families = append(families, frontdIngressWorkspaces)
	}
	if isMCPPath(path) {
		families = append(families, frontdIngressMCP)
	}
	if isWorkspacePath(path) {
		families = append(families, frontdIngressWorkspace)
	}
	if isControlPlanePath(path) {
		families = append(families, frontdIngressControlPlane)
	}
	if isWellKnownPath(path) {
		families = append(families, frontdIngressWellKnown)
	}
	if len(families) == 0 {
		families = append(families, frontdIngressPublic)
	}
	return families
}

type frontdWorkspaceAPIRouteOutcome uint8

const (
	frontdWorkspaceAPIRouteRejected frontdWorkspaceAPIRouteOutcome = iota
	frontdWorkspaceAPIRouteAccepted
)

type frontdWorkspaceAPIRoute struct {
	Outcome     frontdWorkspaceAPIRouteOutcome
	PrivatePath string
}

func workspaceAPIRouteFor(method string, path string) frontdWorkspaceAPIRoute {
	if !isWorkspacesAPIPath(method, path) {
		return frontdWorkspaceAPIRoute{Outcome: frontdWorkspaceAPIRouteRejected}
	}
	return frontdWorkspaceAPIRoute{
		Outcome:     frontdWorkspaceAPIRouteAccepted,
		PrivatePath: privateWorkspaceAPIPath(path),
	}
}

type requestPathObservation struct {
	Path    string
	RawPath string
	Header  string
}

func requestPathObservationFor(req *http.Request) requestPathObservation {
	return requestPathObservation{
		Path:    req.URL.Path,
		RawPath: req.URL.RawPath,
		Header:  req.Header.Get("X-Original"),
	}
}

type mcpSessionLookupOutcome uint8

const (
	mcpSessionUnbound mcpSessionLookupOutcome = iota
	mcpSessionBound
)

type mcpSessionLookup struct {
	Outcome     mcpSessionLookupOutcome
	WorkspaceID workspaceid.WorkspaceID
}

func mcpSessionLookupFor(bindings *MCPSessionBindings, sessionID MCPSessionID) mcpSessionLookup {
	workspaceID, ok := bindings.Workspace(sessionID)
	if ok {
		return mcpSessionLookup{Outcome: mcpSessionBound, WorkspaceID: workspaceID}
	}
	return mcpSessionLookup{Outcome: mcpSessionUnbound}
}

type frontdUpstreamError uint8

const (
	frontdUpstreamAccepted frontdUpstreamError = iota
	frontdWikidUpstreamRejected
	frontdWorkspacedUpstreamRejected
)

func frontdUpstreamErrorFor(err error) frontdUpstreamError {
	switch {
	case IsInvalidWikidUpstream(err):
		return frontdWikidUpstreamRejected
	case IsInvalidWorkspacedUpstream(err):
		return frontdWorkspacedUpstreamRejected
	default:
		return frontdUpstreamAccepted
	}
}
