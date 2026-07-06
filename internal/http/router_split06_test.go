package http_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/importer"
)

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects asset uploads that exceed the configured size limit", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)

		router := httpinternal.NewRouter(w.Registrars(), w.FrontendConfig(), httpinternal.RouterOptions{
			PublicAccess:            false,
			InjectCodeInHeader:      "",
			AllowInsecure:           true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			HideLinkMetadataSection: false,
			MaxAssetUploadSizeBytes: 32,
		})

		page := createPageViaAPI(router, "Asset Limit Test", newFixtureSlug("asset-limit-test"), nil, pageNodeKind())

		loginBody := `{"identifier": "admin", "password": "admin"}`
		loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		router.ServeHTTP(loginRec, loginReq)
		Expect(loginRec).To(HaveHTTPStatus(http.StatusOK), "Failed to login: %d - %s", loginRec.Code, loginRec.Body.String())

		cookies := loginRec.Result().Cookies()
		csrfToken := loginRec.Header().Get("X-CSRF-Token")
		if csrfToken == "" {
			for _, c := range cookies {
				if c.Name == "leafwiki_csrf" || c.Name == "__Host-leafwiki_csrf" {
					csrfToken = c.Value
					break
				}
			}
		}

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", "large.txt")
		Expect(err).NotTo(HaveOccurred(), "Failed to create form file: %v", err)
		{

			_, err := part.Write([]byte(strings.Repeat("a", 128)))
			Expect(err).NotTo(HaveOccurred(), "Failed to write file content: %v", err)
		}
		{

			err := writer.Close()
			Expect(err).NotTo(HaveOccurred(), "Failed to close multipart writer: %v", err)
		}

		uploadReq := httptest.NewRequest(http.MethodPost, "/api/pages/"+page.ID+"/assets", body)
		uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
		uploadReq.Header.Set("X-CSRF-Token", csrfToken)
		for _, cookie := range cookies {
			uploadReq.AddCookie(cookie)
		}

		uploadRec := httptest.NewRecorder()
		router.ServeHTTP(uploadRec, uploadReq)
		Expect(uploadRec).To(HaveHTTPStatus(http.StatusRequestEntityTooLarge), "Expected 413 Request Entity Too Large, got %d - %s", uploadRec.Code, uploadRec.Body.String())

		assetDir := filepath.Join(w.GetStorageDir(), "assets", page.ID)
		entries, err := os.ReadDir(assetDir)
		if err != nil {
			if os.IsNotExist(err) {
				return
			}
			Expect(err).NotTo(HaveOccurred(), "Failed to read asset directory: %v", err)
		}
		Expect(entries).To(HaveLen(0), "Expected no files after rejected upload, got %d", len(entries))

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("suggests a slug for a page title", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstanceWithRevision(w)

		rec := authenticatedRequest(router, http.MethodGet, "/api/pages/slug-suggestion?title=NewPage", nil)
		Expect(rec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200, got %d", rec.Code)

		var resp map[string]interface{}
		{
			err := json.Unmarshal(rec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid JSON response: %v", err)
		}
		Expect(resp).To(HaveKeyWithValue("slug", "newpage"), "slug suggestion response = %#v", resp)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("cancels the current import plan", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		var body bytes.Buffer
		writer := multipart.NewWriter(&body)

		fileWriter, err := writer.CreateFormFile("file", "fixture-1.zip")
		Expect(err).NotTo(HaveOccurred(), "CreateFormFile failed: %v", err)

		zipFile, err := os.Open("../importer/fixtures/fixture-1.zip")
		Expect(err).NotTo(HaveOccurred(), "Open fixture zip failed: %v", err)

		wrapCloseWithErrorCheck(zipFile.Close)
		{

			_, err := io.Copy(fileWriter, zipFile)
			Expect(err).NotTo(HaveOccurred(), "Copy zip fixture failed: %v", err)
		}
		{

			err := writer.Close()
			Expect(err).NotTo(HaveOccurred(), "Close multipart writer failed: %v", err)
		}

		loginBody := `{"identifier": "admin", "password": "admin"}`
		loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		router.ServeHTTP(loginRec, loginReq)
		Expect(loginRec).To(HaveHTTPStatus(http.StatusOK), "Failed to login: %d - %s", loginRec.Code, loginRec.Body.String())

		credentials := readAuthenticatedSessionCredentials(loginRec)
		Expect(credentials).To(haveAuthenticatedSessionCredentials())

		createReq := httptest.NewRequest(http.MethodPost, "/api/import/plan", &body)
		createReq.Header.Set("Content-Type", writer.FormDataContentType())
		createReq.Header.Set("X-CSRF-Token", credentials.CSRFToken)
		for _, cookie := range credentials.Cookies {
			createReq.AddCookie(cookie)
		}

		createRec := httptest.NewRecorder()
		router.ServeHTTP(createRec, createReq)
		Expect(createRec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200 when creating import plan, got %d: %s", createRec.Code, createRec.Body.String())

		cancelReq := httptest.NewRequest(http.MethodDelete, "/api/import/plan", nil)
		cancelReq.Header.Set("Content-Type", "application/json")
		cancelReq.Header.Set("X-CSRF-Token", credentials.CSRFToken)
		for _, cookie := range credentials.Cookies {
			cancelReq.AddCookie(cookie)
		}

		cancelRec := httptest.NewRecorder()
		router.ServeHTTP(cancelRec, cancelReq)
		Expect(cancelRec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200 when canceling import plan, got %d: %s", cancelRec.Code, cancelRec.Body.String())
		{

			got := strings.TrimSpace(cancelRec.Body.String())
			Expect(got).To(Equal("null"), "Expected null response body when clearing import plan, got %q", got)
		}

		getRec := authenticatedRequest(router, http.MethodGet, "/api/import/plan", nil)
		Expect(getRec).To(HaveHTTPStatus(http.StatusNotFound), "Expected status 404 when fetching canceled import plan, got %d: %s", getRec.Code, getRec.Body.String())

		var resp map[string]interface{}
		{
			err := json.Unmarshal(getRec.Body.Bytes(), &resp)
			Expect(err).NotTo(HaveOccurred(), "Invalid JSON response: %v", err)
		}

		Expect(resp).To(HaveKeyWithValue("error", HaveKey("code")), "Expected structured error response after canceling import plan, got: %v", resp)

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("imports pages links and assets from an uploaded zip", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstance(w)

		fixtureDir := importerFixturePathForHTTPTests("link-assets-package")
		zipBytes := createZipFromDir(fixtureDir)

		loginBody := `{"identifier": "admin", "password": "admin"}`
		loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		router.ServeHTTP(loginRec, loginReq)
		Expect(loginRec).To(HaveHTTPStatus(http.StatusOK), "Failed to login: %d - %s", loginRec.Code, loginRec.Body.String())

		credentials := readAuthenticatedSessionCredentials(loginRec)
		Expect(credentials).To(haveAuthenticatedSessionCredentials())

		var planBody bytes.Buffer
		planWriter := multipart.NewWriter(&planBody)
		fileWriter, err := planWriter.CreateFormFile("file", "link-assets-package.zip")
		Expect(err).NotTo(HaveOccurred(), "CreateFormFile failed: %v", err)
		{

			_, err := fileWriter.Write(zipBytes)
			Expect(err).NotTo(HaveOccurred(), "Write zip bytes failed: %v", err)
		}
		{

			err := planWriter.Close()
			Expect(err).NotTo(HaveOccurred(), "Close multipart writer failed: %v", err)
		}

		planReq := httptest.NewRequest(http.MethodPost, "/api/import/plan", &planBody)
		planReq.Header.Set("Content-Type", planWriter.FormDataContentType())
		planReq.Header.Set("X-CSRF-Token", credentials.CSRFToken)
		for _, cookie := range credentials.Cookies {
			planReq.AddCookie(cookie)
		}

		planRec := httptest.NewRecorder()
		router.ServeHTTP(planRec, planReq)
		Expect(planRec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200 when creating import plan, got %d: %s", planRec.Code, planRec.Body.String())

		var planResp struct {
			Items []map[string]any `json:"items"`
		}
		{
			err := json.Unmarshal(planRec.Body.Bytes(), &planResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid import plan response JSON: %v", err)
		}
		Expect(planResp.Items).To(HaveLen(5), "expected 5 plan items, got %d", len(planResp.Items))

		execReq := httptest.NewRequest(http.MethodPost, "/api/import/execute", strings.NewReader(""))
		execReq.Header.Set("Content-Type", "application/json")
		execReq.Header.Set("X-CSRF-Token", credentials.CSRFToken)
		for _, cookie := range credentials.Cookies {
			execReq.AddCookie(cookie)
		}

		execRec := httptest.NewRecorder()
		router.ServeHTTP(execRec, execReq)
		Expect(execRec).To(HaveHTTPStatus(http.StatusAccepted), "Expected status 202 when starting import, got %d: %s", execRec.Code, execRec.Body.String())

		var execResp struct {
			ImportedCount   int    `json:"imported_count"`
			SkippedCount    int    `json:"skipped_count"`
			ExecutionStatus string `json:"execution_status"`
			ExecutionResult *struct {
				ImportedCount int `json:"imported_count"`
				SkippedCount  int `json:"skipped_count"`
			} `json:"execution_result"`
		}
		{
			err := json.Unmarshal(execRec.Body.Bytes(), &execResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid import execute response JSON: %v", err)
		}
		Expect(execResp.ExecutionStatus).To(Equal("running"), "expected running execution status, got %q", execResp.ExecutionStatus)

		var completedResp struct {
			ExecutionStatus string `json:"execution_status"`
			ExecutionResult *struct {
				ImportedCount int `json:"imported_count"`
				SkippedCount  int `json:"skipped_count"`
			} `json:"execution_result"`
		}

		Eventually(func(g Gomega) {
			statusReq := httptest.NewRequest(http.MethodGet, "/api/import/plan", nil)
			for _, cookie := range credentials.Cookies {
				statusReq.AddCookie(cookie)
			}

			statusRec := httptest.NewRecorder()
			router.ServeHTTP(statusRec, statusReq)

			g.Expect(statusRec).To(HaveHTTPStatus(http.StatusOK), statusRec.Body.String())
			g.Expect(json.Unmarshal(statusRec.Body.Bytes(), &completedResp)).To(Succeed(), statusRec.Body.String())
			g.Expect(completedResp.ExecutionStatus).To(Equal("completed"))
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		Expect(completedResp).To(SatisfyAll(
			HaveField("ExecutionStatus", "completed"),
			HaveField("ExecutionResult", Not(BeNil())),
		), "expected completed execution result, got %#v", completedResp)
		Expect(completedResp.ExecutionResult).To(HaveValue(SatisfyAll(
			HaveField("ImportedCount", 4),
			HaveField("SkippedCount", 1),
		)), "unexpected execution result: %#v", completedResp.ExecutionResult)

		setupPage := getPageByPathViaAPI(router, "guides/setup")
		for _, expected := range []string{
			"[Relative MD](/reference/endpoints.md)",
			"[Absolute MD](/reference/endpoints.md)",
			"[Container](/guides)",
			"[Endpoints](/reference/endpoints.md)",
			"[API Alias](/reference/endpoints.md)",
			"![Relative Image](/assets/" + setupPage.ID + "/logo.png)",
			"[Manual](/assets/" + setupPage.ID + "/manual.pdf)",
		} {
			Expect(setupPage.Content).To(ContainSubstring(expected), "expected setup content to contain %q, got:\n%s", expected, setupPage.Content)

		}

		assets := listAssetsViaAPI(router, apiPageDTOID(setupPage))
		Expect(assets).To(HaveLen(2), "expected 2 uploaded assets, got %#v", assets)

		_ = getPageByPathViaAPI(router, "reference/api-1")

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("applies the configured asset upload limit during import execution", func() {

		w := createWikiTestInstance()
		wrapCloseWithErrorCheck(w.Close)
		router := createRouterTestInstanceWithMaxAssetUploadSize(w, 1024)

		fixtureDir := httpTestTempDir()
		{
			err := os.MkdirAll(filepath.Join(fixtureDir, "docs"), 0o755)
			Expect(err).NotTo(HaveOccurred(), "mkdir fixture dir: %v", err)
		}
		{

			err := os.WriteFile(filepath.Join(fixtureDir, "docs", "setup.md"), []byte("# Setup\n\n[Manual](./manual.pdf)\n"), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write markdown fixture: %v", err)
		}
		{

			err := os.WriteFile(filepath.Join(fixtureDir, "docs", "manual.pdf"), bytes.Repeat([]byte("a"), 2048), 0o644)
			Expect(err).NotTo(HaveOccurred(), "write oversized asset fixture: %v", err)
		}

		zipBytes := createZipFromDir(fixtureDir)

		loginBody := `{"identifier": "admin", "password": "admin"}`
		loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(loginBody))
		loginReq.Header.Set("Content-Type", "application/json")
		loginRec := httptest.NewRecorder()
		router.ServeHTTP(loginRec, loginReq)
		Expect(loginRec).To(HaveHTTPStatus(http.StatusOK), "Failed to login: %d - %s", loginRec.Code, loginRec.Body.String())

		credentials := readAuthenticatedSessionCredentials(loginRec)
		Expect(credentials).To(haveAuthenticatedSessionCredentials())

		var planBody bytes.Buffer
		planWriter := multipart.NewWriter(&planBody)
		fileWriter, err := planWriter.CreateFormFile("file", "oversized-assets.zip")
		Expect(err).NotTo(HaveOccurred(), "CreateFormFile failed: %v", err)
		{

			_, err := fileWriter.Write(zipBytes)
			Expect(err).NotTo(HaveOccurred(), "Write zip bytes failed: %v", err)
		}
		{

			err := planWriter.Close()
			Expect(err).NotTo(HaveOccurred(), "Close multipart writer failed: %v", err)
		}

		planReq := httptest.NewRequest(http.MethodPost, "/api/import/plan", &planBody)
		planReq.Header.Set("Content-Type", planWriter.FormDataContentType())
		planReq.Header.Set("X-CSRF-Token", credentials.CSRFToken)
		for _, cookie := range credentials.Cookies {
			planReq.AddCookie(cookie)
		}

		planRec := httptest.NewRecorder()
		router.ServeHTTP(planRec, planReq)
		Expect(planRec).To(HaveHTTPStatus(http.StatusOK), "Expected status 200 when creating import plan, got %d: %s", planRec.Code, planRec.Body.String())

		execReq := httptest.NewRequest(http.MethodPost, "/api/import/execute", strings.NewReader(""))
		execReq.Header.Set("Content-Type", "application/json")
		execReq.Header.Set("X-CSRF-Token", credentials.CSRFToken)
		for _, cookie := range credentials.Cookies {
			execReq.AddCookie(cookie)
		}

		execRec := httptest.NewRecorder()
		router.ServeHTTP(execRec, execReq)
		Expect(execRec).To(HaveHTTPStatus(http.StatusAccepted), "Expected status 202 when starting import, got %d: %s", execRec.Code, execRec.Body.String())

		var execResp struct {
			ExecutionStatus string `json:"execution_status"`
		}
		{
			err := json.Unmarshal(execRec.Body.Bytes(), &execResp)
			Expect(err).NotTo(HaveOccurred(), "Invalid import execute response JSON: %v", err)
		}
		Expect(execResp.ExecutionStatus).To(Equal("running"), "expected running execution status, got %q", execResp.ExecutionStatus)

		var completedResp struct {
			ExecutionStatus string `json:"execution_status"`
			ExecutionResult *struct {
				ImportedCount int `json:"imported_count"`
				SkippedCount  int `json:"skipped_count"`
				Items         []struct {
					Action    importer.ExecutionAction `json:"action"`
					ErrorCode importer.ImportErrorCode `json:"error_code"`
				} `json:"items"`
			} `json:"execution_result"`
		}

		Eventually(func(g Gomega) {
			statusReq := httptest.NewRequest(http.MethodGet, "/api/import/plan", nil)
			for _, cookie := range credentials.Cookies {
				statusReq.AddCookie(cookie)
			}

			statusRec := httptest.NewRecorder()
			router.ServeHTTP(statusRec, statusReq)

			g.Expect(statusRec).To(HaveHTTPStatus(http.StatusOK), statusRec.Body.String())
			g.Expect(json.Unmarshal(statusRec.Body.Bytes(), &completedResp)).To(Succeed(), statusRec.Body.String())
			g.Expect(completedResp.ExecutionStatus).To(Equal("completed"))
		}).WithTimeout(5 * time.Second).WithPolling(50 * time.Millisecond).Should(Succeed())

		Expect(completedResp).To(SatisfyAll(
			HaveField("ExecutionStatus", "completed"),
			HaveField("ExecutionResult", Not(BeNil())),
		), "expected completed execution result, got %#v", completedResp)
		Expect(completedResp.ExecutionResult).To(HaveValue(SatisfyAll(
			HaveField("ImportedCount", BeZero()),
			HaveField("SkippedCount", 1),
			HaveField("Items", HaveExactElements(SatisfyAll(
				HaveField("Action", Equal(importer.ExecutionActionSkipped)),
				HaveField("ErrorCode", Equal(importer.ImportErrorCodeTransformContentFailed)),
			))),
		)), "unexpected execution result: %#v", completedResp.ExecutionResult)

	})
})
