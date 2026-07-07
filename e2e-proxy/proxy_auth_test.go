package e2eproxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

type proxyErrorCode string

const proxyErrorCodeAuthInvalidRefreshToken proxyErrorCode = "auth_invalid_refresh_token"

func (code proxyErrorCode) String() string {
	return string(code)
}

// doProxy makes a GET request through the reverse proxy.
// Set testUser to non-empty to populate X-Test-User, which the proxy converts
// to Remote-User.
func doProxy(path, testUser string, extraHeaders map[string]string) *http.Response {
	GinkgoHelper()
	return doProxyRequest(http.MethodGet, path, testUser, nil, extraHeaders)
}

func doProxyRequest(method, path, testUser string, body io.Reader, extraHeaders map[string]string) *http.Response {
	GinkgoHelper()
	req, err := http.NewRequest(method, proxyURL+path, body)
	Expect(err).NotTo(HaveOccurred())
	if testUser != "" {
		req.Header.Set("X-Test-User", testUser)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	return resp
}

func readBody(r *http.Response) string {
	GinkgoHelper()
	defer func() {
		Expect(r.Body.Close()).To(Succeed())
	}()
	b, err := io.ReadAll(r.Body)
	Expect(err).NotTo(HaveOccurred())
	return strings.TrimSpace(string(b))
}

func haveProxyHTTPStatus(want int) types.GomegaMatcher {
	GinkgoHelper()
	return HaveHTTPStatus(want)
}

// adminAccessToken obtains an access-token cookie by logging in as admin directly
// via the proxy. The login endpoint is public and unaffected by proxy auth.
func adminAccessToken() string {
	GinkgoHelper()
	payload := `{"identifier":"admin","password":"admin"}`
	req, err := http.NewRequest(http.MethodPost, proxyURL+"/api/auth/login", strings.NewReader(payload))
	Expect(err).NotTo(HaveOccurred())
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	defer func() {
		Expect(resp.Body.Close()).To(Succeed())
	}()
	rawBody, err := io.ReadAll(resp.Body)
	Expect(err).NotTo(HaveOccurred())
	body := strings.TrimSpace(string(rawBody))
	Expect(resp).To(haveProxyHTTPStatus(http.StatusOK), "body: %s", body)

	var accessCookie *http.Cookie
	Expect(resp.Cookies()).To(ContainElement(HaveField("Name", "leafwiki_at"), &accessCookie))
	return accessCookie.Value
}

var _ = Describe("proxy authentication", Label("e2e"), func() {
	It("allows the trusted proxy admin user to reach protected user routes", func() {
		resp := doProxy("/api/users", "admin", nil)
		body := readBody(resp)
		Expect(resp).To(haveProxyHTTPStatus(http.StatusOK), "body: %s", body)
	})

	It("rejects a trusted proxy user that does not exist in LeafWiki", func() {
		resp := doProxy("/api/users", "no-such-user-xyz", nil)
		body := readBody(resp)
		Expect(resp).To(haveProxyHTTPStatus(http.StatusUnauthorized), "body: %s", body)
	})

	It("rejects protected user routes when the proxy provides no user", func() {
		resp := doProxy("/api/users", "", nil)
		body := readBody(resp)
		Expect(resp).To(haveProxyHTTPStatus(http.StatusUnauthorized), "body: %s", body)
	})

	It("serves public configuration when the proxy provides no user", func() {
		resp := doProxy("/api/config", "", nil)
		body := readBody(resp)
		Expect(resp).To(haveProxyHTTPStatus(http.StatusOK), "body: %s", body)
	})

	It("serves public configuration when the proxy provides an admin user", func() {
		resp := doProxy("/api/config", "admin", nil)
		body := readBody(resp)
		Expect(resp).To(haveProxyHTTPStatus(http.StatusOK), "body: %s", body)
	})

	It("returns configuration JSON through the proxy without an authenticated user", func() {
		resp := doProxy("/api/config", "", nil)
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		defer func() {
			Expect(resp.Body.Close()).To(Succeed())
		}()
		var body map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
		Expect(body).To(HaveKey("authDisabled"))
	})

	It("falls back to a LeafWiki access-token cookie when no proxy user is provided", func() {
		token := adminAccessToken()

		req, err := http.NewRequest(http.MethodGet, proxyURL+"/api/users", nil)
		Expect(err).NotTo(HaveOccurred())
		req.AddCookie(&http.Cookie{Name: "leafwiki_at", Value: token})

		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		body := readBody(resp)
		Expect(resp).To(haveProxyHTTPStatus(http.StatusOK), "body: %s", body)
	})

	It("rejects refresh-token requests without a backing session", func() {
		resp := refreshTokenWithoutSessionResponse("", nil)
		body := readBody(resp)
		Expect(resp).To(haveProxyHTTPStatus(http.StatusUnprocessableEntity), "body: %s", body)
		Expect(body).To(haveProxyErrorCode(proxyErrorCodeAuthInvalidRefreshToken))
	})

	It("ignores client-supplied Remote-User headers when the proxy provides no user", func() {
		resp := doProxy("/api/users", "", map[string]string{
			"Remote-User": "admin",
		})
		body := readBody(resp)
		Expect(resp).To(haveProxyHTTPStatus(http.StatusUnauthorized), "body: %s", body)
	})

	It("keeps the trusted proxy admin user when clients inject another Remote-User", func() {
		resp := doProxy("/api/users", "admin", map[string]string{
			"Remote-User": "no-such-user-xyz",
		})
		body := readBody(resp)
		Expect(resp).To(haveProxyHTTPStatus(http.StatusOK), "body: %s", body)
	})

	It("rejects an unknown trusted proxy user even when clients inject admin as Remote-User", func() {
		resp := doProxy("/api/users", "no-such-user-xyz", map[string]string{
			"Remote-User": "admin",
		})
		body := readBody(resp)
		Expect(resp).To(haveProxyHTTPStatus(http.StatusUnauthorized), "body: %s", body)
	})

	It("rejects refresh-token requests without a session even when the proxy provides an admin user", func() {
		resp := refreshTokenWithoutSessionResponse("admin", nil)
		body := readBody(resp)
		Expect(resp).To(haveProxyHTTPStatus(http.StatusUnprocessableEntity), "body: %s", body)
		Expect(body).To(haveProxyErrorCode(proxyErrorCodeAuthInvalidRefreshToken))
	})

	It("returns configuration JSON through the proxy with an admin user", func() {
		resp := doProxy("/api/config", "admin", nil)
		Expect(resp).To(HaveHTTPStatus(http.StatusOK))
		defer func() {
			Expect(resp.Body.Close()).To(Succeed())
		}()
		var body map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
		Expect(body).To(HaveKey("authDisabled"))
	})
})

func refreshTokenWithoutSessionResponse(testUser string, extraHeaders map[string]string) *http.Response {
	GinkgoHelper()
	return doProxyRequest(http.MethodPost, "/api/auth/refresh-token", testUser, nil, extraHeaders)
}

func haveProxyErrorCode(code proxyErrorCode) types.GomegaMatcher {
	GinkgoHelper()
	return WithTransform(func(body string) (proxyErrorCode, error) {
		var parsed struct {
			Error struct {
				Code proxyErrorCode `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(body), &parsed); err != nil {
			return "", fmt.Errorf("decode proxy error response: %w", err)
		}
		return parsed.Error.Code, nil
	}, Equal(code))
}
