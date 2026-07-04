package utils

import (
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type secureTransportOutcome string

const (
	secureTransportAccepted   secureTransportOutcome = "secure transport accepted"
	insecureTransportAllowed  secureTransportOutcome = "insecure transport allowed"
	insecureTransportRejected secureTransportOutcome = "insecure transport rejected"
)

var _ = Describe("RequireSecure", func() {
	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
	})

	It("accepts directly secure requests", func() {
		ctx := requireSecureContext(nil)
		ctx.Request.TLS = &tls.ConnectionState{}

		outcome, err := requireSecureTransportOutcome(ctx, false)

		Expect(err).NotTo(HaveOccurred())
		Expect(outcome).To(Equal(secureTransportAccepted))
	})

	DescribeTable("accepts trusted HTTPS forwarding headers",
		func(headers http.Header) {
			ctx := requireSecureContext(headers)

			outcome, err := requireSecureTransportOutcome(ctx, false)

			Expect(err).NotTo(HaveOccurred())
			Expect(outcome).To(Equal(secureTransportAccepted))
		},
		Entry("X-Forwarded-Proto HTTPS", http.Header{"X-Forwarded-Proto": []string{"HTTPS"}}),
		Entry("X-Forwarded-Proto chain containing HTTPS", http.Header{"X-Forwarded-Proto": []string{"http, https"}}),
		Entry("X-Forwarded-Ssl on", http.Header{"X-Forwarded-Ssl": []string{"on"}}),
		Entry("Front-End-Https on", http.Header{"Front-End-Https": []string{"ON"}}),
	)

	It("allows insecure requests when configured", func() {
		ctx := requireSecureContext(nil)

		outcome, err := requireSecureTransportOutcome(ctx, true)

		Expect(err).NotTo(HaveOccurred())
		Expect(outcome).To(Equal(insecureTransportAllowed))
	})

	It("rejects insecure requests when secure cookies are required", func() {
		ctx := requireSecureContext(nil)

		outcome, err := requireSecureTransportOutcome(ctx, false)

		Expect(err).To(MatchError(ErrHTTPSRequired))
		Expect(outcome).To(Equal(insecureTransportRejected))
	})
})

func requireSecureTransportOutcome(ctx *gin.Context, allowInsecure bool) (secureTransportOutcome, error) {
	GinkgoHelper()

	secure, err := RequireSecure(ctx, allowInsecure)
	if errors.Is(err, ErrHTTPSRequired) {
		return insecureTransportRejected, err
	}
	if err != nil {
		return "", err
	}
	if secure {
		return secureTransportAccepted, nil
	}
	return insecureTransportAllowed, nil
}

func requireSecureContext(headers http.Header) *gin.Context {
	GinkgoHelper()
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/secure", nil)
	req.Header = headers
	ctx.Request = req
	return ctx
}
