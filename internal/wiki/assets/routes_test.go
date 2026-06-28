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

var _ = ginkgo.Describe("asset routes", func() {
	ginkgo.It("TestRoutesServeStaticAssetsWhenPublicAccessEnabled", func() {
		assetsDir := ginkgo.GinkgoT().TempDir()
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

		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
		Expect(rec.Body.String()).To(Equal("hello"))
	})

	ginkgo.It("TestRoutesRequireAuthForPrivateStaticAssets", func() {
		assetsDir := ginkgo.GinkgoT().TempDir()
		router := httpinternal.NewRouter(
			[]httpinternal.RouteRegistrar{NewRoutes(RoutesConfig{AssetsDir: assetsDir})},
			httpinternal.FrontendConfig{},
			httpinternal.RouterOptions{DisableFrontendRoutes: true},
		)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/page-1/note.txt", nil))

		Expect(rec.Code).To(Equal(http.StatusUnauthorized), rec.Body.String())
	})

	ginkgo.It("TestRoutesRequireCSRFForAssetMutations", func() {
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

		Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())
	})
})
