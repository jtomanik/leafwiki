package workspaced

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/assets"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/wiki"
)

var _ = ginkgo.Describe("workspaced routers", func() {
	ginkgo.DescribeTable("public identity and global routes",
		func(method string, path string) {
			w := newTestWiki()
			ginkgo.DeferCleanup(func() {
				Expect(w.Close()).To(Succeed())
			})
			router := NewRouter(w, httpinternal.RouterOptions{
				PublicAccess:            true,
				AllowInsecure:           true,
				AuthDisabled:            true,
				AccessTokenTimeout:      15 * time.Minute,
				RefreshTokenTimeout:     7 * 24 * time.Hour,
				MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			})

			rec := request(router, method, path)
			Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), rec.Body.String())
		},
		ginkgo.Entry("hide the config endpoint", http.MethodGet, "/api/config"),
		ginkgo.Entry("hide the current-user endpoint", http.MethodGet, "/api/auth/me"),
		ginkgo.Entry("hide the login endpoint", http.MethodPost, "/api/auth/login"),
		ginkgo.Entry("hide user administration endpoints", http.MethodGet, "/api/users"),
		ginkgo.Entry("hide branding endpoints", http.MethodGet, "/api/branding"),
		ginkgo.Entry("hide OAuth token endpoints", http.MethodPost, "/oauth/token"),
		ginkgo.Entry("hide OAuth metadata endpoints", http.MethodGet, "/.well-known/oauth-protected-resource/mcp"),
	)

	ginkgo.It("keeps workspace routes available", func() {
		w := newTestWiki()
		ginkgo.DeferCleanup(func() {
			Expect(w.Close()).To(Succeed())
		})
		router := NewRouter(w, httpinternal.RouterOptions{
			PublicAccess:            true,
			AllowInsecure:           true,
			AuthDisabled:            true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		})

		rec := request(router, http.MethodGet, "/api/tree")
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	})

	ginkgo.DescribeTable("embedded frontend routes",
		func(routerFactory func(*wiki.Wiki) http.Handler, path string) {
			embedFrontendOrig := httpinternal.EmbedFrontend
			httpinternal.EmbedFrontend = "true"
			ginkgo.DeferCleanup(func() {
				httpinternal.EmbedFrontend = embedFrontendOrig
			})
			stylesheetDir := workspacedTempDir()
			Expect(os.WriteFile(filepath.Join(stylesheetDir, "custom.css"), []byte("body { color: red; }\n"), 0o644)).To(Succeed())
			workspacedChdir(stylesheetDir)

			w := newTestWiki()
			ginkgo.DeferCleanup(func() {
				Expect(w.Close()).To(Succeed())
			})
			router := routerFactory(w)

			rec := request(router, http.MethodGet, path)
			Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), rec.Body.String())
		},
		ginkgo.Entry("do not expose custom stylesheets from the workspaced router", workspacedRouterFactory, "/custom.css"),
		ginkgo.Entry("do not expose favicons from the workspaced router", workspacedRouterFactory, "/favicon.svg"),
		ginkgo.Entry("do not expose static assets from the workspaced router", workspacedRouterFactory, "/static/index-DYW7NERi.js"),
		ginkgo.Entry("do not expose SPA fallbacks from the workspaced router", workspacedRouterFactory, "/workspace-spa-route"),
		ginkgo.Entry("do not expose custom stylesheets from the authenticated workspaced router", authenticatedWorkspacedRouterFactory, "/custom.css"),
		ginkgo.Entry("do not expose favicons from the authenticated workspaced router", authenticatedWorkspacedRouterFactory, "/favicon.svg"),
		ginkgo.Entry("do not expose static assets from the authenticated workspaced router", authenticatedWorkspacedRouterFactory, "/static/index-DYW7NERi.js"),
		ginkgo.Entry("do not expose SPA fallbacks from the authenticated workspaced router", authenticatedWorkspacedRouterFactory, "/workspace-spa-route"),
	)
})

func newTestWiki() *wiki.Wiki {
	ginkgo.GinkgoHelper()
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		StorageDir:          workspacedTempDir(),
		AdminPassword:       "admin",
		JWTSecret:           "secret",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
		AuthDisabled:        true,
	})
	Expect(err).NotTo(HaveOccurred())
	return w
}

func workspacedRouterOptions() httpinternal.RouterOptions {
	ginkgo.GinkgoHelper()
	return httpinternal.RouterOptions{
		PublicAccess:            true,
		AllowInsecure:           true,
		AuthDisabled:            true,
		AccessTokenTimeout:      15 * time.Minute,
		RefreshTokenTimeout:     7 * 24 * time.Hour,
		MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
	}
}

func workspacedRouterFactory(w *wiki.Wiki) http.Handler {
	ginkgo.GinkgoHelper()
	opts := workspacedRouterOptions()
	opts.CustomStylesheet = "custom.css"
	return NewRouter(w, opts)
}

func authenticatedWorkspacedRouterFactory(w *wiki.Wiki) http.Handler {
	ginkgo.GinkgoHelper()
	opts := workspacedRouterOptions()
	opts.CustomStylesheet = "custom.css"
	return NewAuthenticatedRouter(w, opts, PrivateAuthOptions{
		DaemonToken: "private-token",
		WorkspaceID: "current",
		Now:         func() time.Time { return time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC) },
	})
}

func request(router http.Handler, method, path string) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	req := httptest.NewRequest(method, path, strings.NewReader(""))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func workspacedTempDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-workspaced-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func workspacedChdir(dir string) {
	ginkgo.GinkgoHelper()
	previous, err := os.Getwd()
	Expect(err).NotTo(HaveOccurred())
	Expect(os.Chdir(dir)).To(Succeed())
	ginkgo.DeferCleanup(func() {
		Expect(os.Chdir(previous)).To(Succeed())
	})
}
