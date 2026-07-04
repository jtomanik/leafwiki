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

func getHTTPPageByID(router http.Handler, pageID string) map[string]any {
	GinkgoHelper()

	return getHTTPMap(router, "/api/pages/"+pageID)
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

func updateHTTPPage(router http.Handler, pageID string, payload map[string]any) map[string]any {
	GinkgoHelper()

	body, err := json.Marshal(payload)
	Expect(err).NotTo(HaveOccurred())
	csrfToken, csrfCookies := issueHTTPCSRF(router)
	req := httptest.NewRequest(http.MethodPut, "/api/pages/"+pageID, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range csrfCookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	var out map[string]any
	Expect(json.Unmarshal(rec.Body.Bytes(), &out)).To(Succeed(), "decode HTTP page update %q", pageID)
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

func uploadHTTPAsset(router http.Handler, pageID, filename string, content []byte, wantStatus int) map[string]any {
	GinkgoHelper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	Expect(err).NotTo(HaveOccurred())
	_, err = part.Write(content)
	Expect(err).NotTo(HaveOccurred())
	Expect(writer.Close()).To(Succeed())

	csrfToken, csrfCookies := issueHTTPCSRF(router)
	req := httptest.NewRequest(http.MethodPost, "/api/pages/"+pageID+"/assets", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range csrfCookies {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	Expect(rec).To(HaveHTTPStatus(wantStatus), rec.Body.String())
	return decodeJSONMap("POST asset "+pageID+"/"+filename, rec.Body.Bytes())
}

func getHTTPAssets(router http.Handler, pageID string) map[string]any {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/"+pageID+"/assets", nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	return decodeJSONMap("GET asset list for page "+pageID, rec.Body.Bytes())
}

func getHTTPAsset(router http.Handler, pageID, filename string) string {
	GinkgoHelper()

	body, _ := getHTTPAssetWithContentType(router, pageID, filename)
	return body
}

func getHTTPAssetWithContentType(router http.Handler, pageID, filename string) (string, string) {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/"+pageID+"/"+url.PathEscape(filename), nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	return rec.Body.String(), rec.Header().Get("Content-Type")
}

func getHTTPLatestRevision(router http.Handler, pageID string) map[string]any {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/"+pageID+"/revisions/latest", nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	return decodeJSONMap("GET latest revision for page "+pageID, rec.Body.Bytes())
}

func getHTTPRevision(router http.Handler, pageID, revisionID string) map[string]any {
	GinkgoHelper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/pages/"+pageID+"/revisions/"+revisionID, nil))
	Expect(rec).To(HaveHTTPStatus(http.StatusOK), rec.Body.String())
	return decodeJSONMap("GET revision "+pageID+"/"+revisionID, rec.Body.Bytes())
}
