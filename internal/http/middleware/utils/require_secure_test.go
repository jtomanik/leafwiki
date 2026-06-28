package utils

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RequireSecure", func() {
	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
	})

	It("accepts directly secure requests", func() {
		ctx := requireSecureContext(nil)
		ctx.Request.TLS = &tls.ConnectionState{}

		secure, err := RequireSecure(ctx, false)

		Expect(err).NotTo(HaveOccurred())
		Expect(secure).To(BeTrue())
	})

	DescribeTable("accepts trusted HTTPS forwarding headers",
		func(headers http.Header) {
			ctx := requireSecureContext(headers)

			secure, err := RequireSecure(ctx, false)

			Expect(err).NotTo(HaveOccurred())
			Expect(secure).To(BeTrue())
		},
		Entry("X-Forwarded-Proto HTTPS", http.Header{"X-Forwarded-Proto": []string{"HTTPS"}}),
		Entry("X-Forwarded-Proto chain containing HTTPS", http.Header{"X-Forwarded-Proto": []string{"http, https"}}),
		Entry("X-Forwarded-Ssl on", http.Header{"X-Forwarded-Ssl": []string{"on"}}),
		Entry("Front-End-Https on", http.Header{"Front-End-Https": []string{"ON"}}),
	)

	It("allows insecure requests when configured", func() {
		ctx := requireSecureContext(nil)

		secure, err := RequireSecure(ctx, true)

		Expect(err).NotTo(HaveOccurred())
		Expect(secure).To(BeFalse())
	})

	It("rejects insecure requests when secure cookies are required", func() {
		ctx := requireSecureContext(nil)

		secure, err := RequireSecure(ctx, false)

		Expect(err).To(MatchError(ErrHTTPSRequired))
		Expect(secure).To(BeFalse())
	})
})

func requireSecureContext(headers http.Header) *gin.Context {
	GinkgoHelper()
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header = headers
	ctx.Request = req
	return ctx
}
