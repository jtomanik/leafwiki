package mcp_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/tree"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

func getHTTPPageByPath(router http.Handler, path string) map[string]any {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/by-path?path="+path, nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	var out map[string]any
	Expect(json.Unmarshal(rec.Body.Bytes(), &out)).To(Succeed(), "decode HTTP page by path %q", path)
	return out
}

func getHTTPPageByID(router http.Handler, pageID tree.PageID) map[string]any {
	GinkgoHelper()

	return getHTTPMap(router, "/api/pages/"+pageID.MetadataValue())
}

func getHTTPValue(router http.Handler, path string) any {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	return decodeJSONValue("GET "+path, rec.Body.Bytes())
}

func getHTTPStatus(router http.Handler, path string, wantStatus int) string {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	Expect(rec).To(HaveHTTPStatus(wantStatus), rec.Body.String())
	return rec.Body.String()
}

func getHTTPMap(router http.Handler, path string) map[string]any {
	GinkgoHelper()

	value := getHTTPValue(router, path)
	Expect(value).To(BeAssignableToTypeOf(map[string]any{}), "GET %s should decode as object", path)
	return value.(map[string]any)
}

func updateHTTPPage(router http.Handler, pageID tree.PageID, payload map[string]any) map[string]any {
	GinkgoHelper()

	body, err := json.Marshal(payload)
	Expect(err).NotTo(HaveOccurred())
	csrfToken, csrfCookies := issueHTTPCSRF(router)
	req := httptest.NewRequest(http.MethodPut, "/api/pages/"+pageID.MetadataValue(), strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range csrfCookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	var out map[string]any
	Expect(json.Unmarshal(rec.Body.Bytes(), &out)).To(Succeed(), "decode HTTP page update %q", pageID.MetadataValue())
	return out
}

func putHTTPJSON(router http.Handler, path string, payload map[string]any, wantStatus int) map[string]any {
	GinkgoHelper()

	return requestHTTPJSON(router, http.MethodPut, path, payload, wantStatus)
}

func putHTTPJSONBody(router http.Handler, path string, payload map[string]any, wantStatus int) string {
	GinkgoHelper()

	return requestHTTPJSONBody(router, http.MethodPut, path, payload, wantStatus)
}

func postHTTPJSON(router http.Handler, path string, payload map[string]any, wantStatus int) map[string]any {
	GinkgoHelper()

	return requestHTTPJSON(router, http.MethodPost, path, payload, wantStatus)
}

func postHTTPJSONBody(router http.Handler, path string, payload map[string]any, wantStatus int) string {
	GinkgoHelper()

	return requestHTTPJSONBody(router, http.MethodPost, path, payload, wantStatus)
}

func postHTTPJSONNoContent(router http.Handler, path string, payload map[string]any, wantStatus int) {
	GinkgoHelper()

	body := postHTTPJSONBody(router, path, payload, wantStatus)
	Expect(strings.TrimSpace(body)).To(BeEmpty())
}

func requestHTTPJSON(router http.Handler, method, path string, payload map[string]any, wantStatus int) map[string]any {
	GinkgoHelper()

	raw := requestHTTPJSONBody(router, method, path, payload, wantStatus)
	return decodeJSONMap(method+" "+path, []byte(raw))
}

func requestHTTPJSONBody(router http.Handler, method, path string, payload map[string]any, wantStatus int) string {
	GinkgoHelper()

	body, err := json.Marshal(payload)
	Expect(err).NotTo(HaveOccurred())
	csrfToken, csrfCookies := issueHTTPCSRF(router)
	req := httptest.NewRequest(method, path, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range csrfCookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	Expect(rec).To(HaveHTTPStatus(wantStatus), rec.Body.String())
	return rec.Body.String()
}

func decodeJSONMap(label string, raw []byte) map[string]any {
	GinkgoHelper()

	value := decodeJSONValue(label, raw)
	Expect(value).To(BeAssignableToTypeOf(map[string]any{}), "%s should decode to a JSON object", label)
	return value.(map[string]any)
}

func deleteHTTPStatus(router http.Handler, path string, wantStatus int) string {
	GinkgoHelper()

	csrfToken, csrfCookies := issueHTTPCSRF(router)
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range csrfCookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	Expect(rec).To(HaveHTTPStatus(wantStatus), rec.Body.String())
	return rec.Body.String()
}

func getHTTPSearch(router http.Handler, values url.Values) map[string]any {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?"+values.Encode(), nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	return decodeJSONMap("GET /api/search", rec.Body.Bytes())
}

func uploadHTTPAsset(router http.Handler, pageID tree.PageID, filename tree.AssetName, content []byte, wantStatus int) map[string]any {
	GinkgoHelper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename.Filename())
	Expect(err).NotTo(HaveOccurred())
	_, err = part.Write(content)
	Expect(err).NotTo(HaveOccurred())
	Expect(writer.Close()).To(Succeed())

	csrfToken, csrfCookies := issueHTTPCSRF(router)
	req := httptest.NewRequest(http.MethodPost, "/api/pages/"+pageID.MetadataValue()+"/assets", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range csrfCookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	Expect(rec).To(HaveHTTPStatus(wantStatus), rec.Body.String())
	return decodeJSONMap(httpAssetUploadLabel(pageID), rec.Body.Bytes())
}

func httpAssetUploadLabel(pageID tree.PageID) string {
	return "POST asset for page " + pageID.MetadataValue()
}

func getHTTPAssets(router http.Handler, pageID tree.PageID) map[string]any {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/"+pageID.MetadataValue()+"/assets", nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	return decodeJSONMap("GET asset list for page "+pageID.MetadataValue(), rec.Body.Bytes())
}

func getHTTPAsset(router http.Handler, pageID tree.PageID, filename tree.AssetName) string {
	GinkgoHelper()

	body, _ := getHTTPAssetWithContentType(router, pageID, filename)
	return body
}

func getHTTPAssetWithContentType(router http.Handler, pageID tree.PageID, filename tree.AssetName) (string, string) {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/"+pageID.MetadataValue()+"/"+url.PathEscape(filename.String()), nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	return rec.Body.String(), rec.Header().Get("Content-Type")
}

func getHTTPLatestRevision(router http.Handler, pageID tree.PageID) map[string]any {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/"+pageID.MetadataValue()+"/revisions/latest", nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	return decodeJSONMap("GET latest revision for page "+pageID.MetadataValue(), rec.Body.Bytes())
}

func getHTTPRevision(router http.Handler, pageID tree.PageID, revisionID tree.RevisionID) map[string]any {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/"+pageID.MetadataValue()+"/revisions/"+revisionID.String(), nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	return decodeJSONMap(httpRevisionLabel(pageID), rec.Body.Bytes())
}

func httpRevisionLabel(pageID tree.PageID) string {
	return "GET revision for page " + pageID.MetadataValue()
}
