package http_test

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	httpinternal "github.com/perber/wiki/internal/http"
)

var _ = Describe("HTTP frontend helper routing", Label("unit"), func() {
	DescribeTable("base path request resolution",
		func(requestPath string, basePath string, want frontendRequestPathResult) {
			Expect(frontendRequestPathFor(requestPath, basePath)).To(Equal(want))
		},
		Entry("keeps root paths when no base path is configured", "/docs", "", frontendRequestPathResult{
			Outcome: frontendRequestPathAccepted,
			Path:    "/docs",
		}),
		Entry("normalizes the base path itself to the SPA root", "/wiki", "/wiki", frontendRequestPathResult{
			Outcome: frontendRequestPathAccepted,
			Path:    "/",
		}),
		Entry("strips a configured base path prefix", "/wiki/docs", "/wiki", frontendRequestPathResult{
			Outcome: frontendRequestPathAccepted,
			Path:    "/docs",
		}),
		Entry("rejects requests outside the configured base path", "/outside", "/wiki", frontendRequestPathResult{
			Outcome: frontendRequestPathRejected,
		}),
	)

	DescribeTable("SPA fallback eligibility",
		func(method string, path string, want frontendSPARouteDecision) {
			Expect(frontendSPARouteFor(method, path)).To(Equal(want))
		},
		Entry("accepts document GET requests", http.MethodGet, "/docs", frontendSPARouteAccepted),
		Entry("rejects mutating methods", http.MethodPost, "/docs", frontendSPARouteRejected),
		Entry("rejects exact MCP transport routes", http.MethodGet, "/mcp", frontendSPARouteRejected),
		Entry("rejects API routes", http.MethodGet, "/api/pages/home", frontendSPARouteRejected),
		Entry("rejects static asset routes", http.MethodGet, "/static/index.js", frontendSPARouteRejected),
	)

	It("rewrites frontend placeholders and static paths into deterministic HTML", func() {
		html := `<html><head></head><body><img src="/static/logo.svg"><span>{{__SITE_NAME__}}</span><span>{{__BASE_PATH__}}</span><link href="{{__FAVICON_HREF__}}"></body></html>`
		got := httpinternal.FrontendIndexHTML(html, httpinternal.FrontendConfig{
			GetSiteName: func() string {
				return "Docs"
			},
			GetFaviconFile: func() string {
				return "favicon.ico"
			},
		}, httpinternal.RouterOptions{
			BasePath:           "/wiki",
			InjectCodeInHeader: `<meta name="x-test" content="ok">`,
		}, "/tmp/custom.css")

		Expect(got).To(Equal(`<html><head>  <link rel="stylesheet" href="/wiki/custom.css">
    <meta name="x-test" content="ok">
  </head><body><img src="/wiki/static/logo.svg"><span>Docs</span><span>/wiki</span><link href="/wiki/branding/favicon.ico"></body></html>`))
	})

	It("uses default frontend metadata when callbacks are missing or blank", func() {
		html := `<html><head></head><body>{{__SITE_NAME__}} {{__BASE_PATH__}} {{__FAVICON_HREF__}}</body></html>`
		got := httpinternal.FrontendIndexHTML(html, httpinternal.FrontendConfig{
			GetSiteName: func() string {
				return ""
			},
		}, httpinternal.RouterOptions{}, "")

		Expect(got).To(Equal(`<html><head></head><body>LeafWiki  /favicon.svg</body></html>`))
	})
})

type frontendRequestPathOutcome uint8

const (
	frontendRequestPathRejected frontendRequestPathOutcome = iota
	frontendRequestPathAccepted
)

type frontendRequestPathResult struct {
	Outcome frontendRequestPathOutcome
	Path    string
}

func frontendRequestPathFor(requestPath string, basePath string) frontendRequestPathResult {
	path, ok := httpinternal.FrontendRequestPath(requestPath, basePath)
	if ok {
		return frontendRequestPathResult{Outcome: frontendRequestPathAccepted, Path: path}
	}
	return frontendRequestPathResult{Outcome: frontendRequestPathRejected}
}

type frontendSPARouteDecision uint8

const (
	frontendSPARouteRejected frontendSPARouteDecision = iota
	frontendSPARouteAccepted
)

func frontendSPARouteFor(method string, path string) frontendSPARouteDecision {
	if httpinternal.IsFrontendSPARoute(method, path) {
		return frontendSPARouteAccepted
	}
	return frontendSPARouteRejected
}
