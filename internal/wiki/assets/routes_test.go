package assets

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	httpinternal "github.com/perber/wiki/internal/http"
)

var _ = ginkgo.Describe("asset routes", ginkgo.Label("integration"), func() {
	ginkgo.It("serves static asset files when public access is enabled", func() {
		assetsDir := assetTempDir()
		pageDir := filepath.Join(assetsDir, "page-1")
		Expect(os.MkdirAll(pageDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(pageDir, "note.txt"), []byte("hello"), 0o644)).To(Succeed())

		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{AssetsDir: assetsDir})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{PublicAccess: true, DisableFrontendRoutes: true},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/page-1/note.txt", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
		Expect(rec).To(HaveHTTPBody("hello"))
	})

	ginkgo.It("requires authentication before serving private static asset files", func() {
		assetsDir := assetTempDir()
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{AssetsDir: assetsDir})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{DisableFrontendRoutes: true},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/page-1/note.txt", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized), rec.Body.String())
	})

	ginkgo.It("requires CSRF protection for asset mutations", func() {
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{
				AuthDisabled:          true,
				DisableFrontendRoutes: true,
			},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/pages/page-1/assets", nil))

		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden), rec.Body.String())
	})
})

func assetTempDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-assets-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}
