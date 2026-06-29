package e2eproxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	Expect(err).NotTo(HaveOccurred())
	return strings.TrimSpace(string(b))
}

func assertStatus(resp *http.Response, want int) string {
	GinkgoHelper()
	body := readBody(resp)
	Expect(resp.StatusCode).To(Equal(want), "body: %s", body)
	return body
}

// loginAdmin obtains an access-token cookie by logging in as admin directly
// via the proxy. The login endpoint is public and unaffected by proxy auth.
func loginAdmin() string {
	GinkgoHelper()
	payload := `{"identifier":"admin","password":"admin"}`
	req, err := http.NewRequest(http.MethodPost, proxyURL+"/api/auth/login", strings.NewReader(payload))
	Expect(err).NotTo(HaveOccurred())
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		Fail("login failed " + resp.Status + ": " + string(b))
	}
	for _, c := range resp.Cookies() {
		if c.Name == "leafwiki_at" {
			return c.Value
		}
	}
	Fail("no leafwiki_at cookie in login response")
	return ""
}

var _ = Describe("proxy authentication", func() {
	It("ProxyAuth_ValidUser_Admin", func() {
		resp := doProxy("/api/users", "admin", nil)
		assertStatus(resp, http.StatusOK)
	})

	It("ProxyAuth_UnknownUser", func() {
		resp := doProxy("/api/users", "no-such-user-xyz", nil)
		assertStatus(resp, http.StatusUnauthorized)
	})

	It("ProxyAuth_NoHeader_ProtectedRoute", func() {
		resp := doProxy("/api/users", "", nil)
		assertStatus(resp, http.StatusUnauthorized)
	})

	It("ProxyAuth_PublicRoute_NoHeader", func() {
		resp := doProxy("/api/config", "", nil)
		assertStatus(resp, http.StatusOK)
	})

	It("ProxyAuth_PublicRoute_WithUser", func() {
		resp := doProxy("/api/config", "admin", nil)
		assertStatus(resp, http.StatusOK)
	})

	It("ProxyAuth_ConfigResponse_Roundtrip", func() {
		resp := doProxy("/api/config", "", nil)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		defer resp.Body.Close()
		var body map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
		Expect(body).To(HaveKey("authDisabled"))
	})

	It("ProxyAuth_FallbackToJWT", func() {
		token := loginAdmin()

		req, err := http.NewRequest(http.MethodGet, proxyURL+"/api/users", nil)
		Expect(err).NotTo(HaveOccurred())
		req.AddCookie(&http.Cookie{Name: "leafwiki_at", Value: token})

		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		assertStatus(resp, http.StatusOK)
	})

	It("RefreshToken_NoSession_Returns422", func() {
		body := assertRefreshTokenWithoutSession("", nil)
		assertInvalidRefreshTokenBody(body)
	})

	It("ProxyAuth_DirectRemoteUserInjection", func() {
		resp := doProxy("/api/users", "", map[string]string{
			"Remote-User": "admin",
		})
		assertStatus(resp, http.StatusUnauthorized)
	})

	It("ProxyAuth_RemoteUserInjectionIgnoredWhenProxyUserIsAdmin", func() {
		resp := doProxy("/api/users", "admin", map[string]string{
			"Remote-User": "no-such-user-xyz",
		})
		assertStatus(resp, http.StatusOK)
	})

	It("ProxyAuth_RemoteUserInjectionDoesNotBypassUnknownProxyUser", func() {
		resp := doProxy("/api/users", "no-such-user-xyz", map[string]string{
			"Remote-User": "admin",
		})
		assertStatus(resp, http.StatusUnauthorized)
	})

	It("RefreshToken_NoSession_WithProxyUserStillReturns422", func() {
		body := assertRefreshTokenWithoutSession("admin", nil)
		assertInvalidRefreshTokenBody(body)
	})

	It("ProxyAuth_ConfigResponse_WithUserRoundtrip", func() {
		resp := doProxy("/api/config", "admin", nil)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		defer resp.Body.Close()
		var body map[string]any
		Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
		Expect(body).To(HaveKey("authDisabled"))
	})
})

func assertRefreshTokenWithoutSession(testUser string, extraHeaders map[string]string) string {
	GinkgoHelper()
	resp := doProxyRequest(http.MethodPost, "/api/auth/refresh-token", testUser, nil, extraHeaders)
	return assertStatus(resp, http.StatusUnprocessableEntity)
}

func assertInvalidRefreshTokenBody(body string) {
	GinkgoHelper()
	var parsed struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	Expect(json.Unmarshal([]byte(body), &parsed)).To(Succeed(), "body: %s", body)
	Expect(parsed.Error.Code).To(Equal("auth_invalid_refresh_token"))
}
