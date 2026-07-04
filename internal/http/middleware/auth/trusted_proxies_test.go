package auth_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	authmw "github.com/perber/wiki/internal/http/middleware/auth"
)

var _ = Describe("trusted proxy parsing", Label("unit"), func() {
	It("trusts nobody when the configured proxy list is empty", func() {
		tp, err := authmw.ParseTrustedProxies("")
		Expect(err).To(Succeed())
		Expect(tp.IsTrusted("127.0.0.1")).To(BeFalse())
	})

	It("trusts a configured single IP and rejects other addresses", func() {
		tp, err := authmw.ParseTrustedProxies("127.0.0.1")
		Expect(err).To(Succeed())
		Expect(tp.IsTrusted("127.0.0.1")).To(BeTrue())
		Expect(tp.IsTrusted("192.168.1.1")).To(BeFalse())
	})

	It("trusts every configured IP in a comma-separated list", func() {
		tp, err := authmw.ParseTrustedProxies("127.0.0.1, ::1, 10.0.0.1")
		Expect(err).To(Succeed())
		Expect(tp.IsTrusted("127.0.0.1")).To(BeTrue())
		Expect(tp.IsTrusted("::1")).To(BeTrue())
		Expect(tp.IsTrusted("10.0.0.1")).To(BeTrue())
		Expect(tp.IsTrusted("10.0.0.2")).To(BeFalse())
	})

	It("trusts addresses inside configured CIDR ranges only", func() {
		tp, err := authmw.ParseTrustedProxies("172.18.0.0/16")
		Expect(err).To(Succeed())
		Expect(tp.IsTrusted("172.18.0.1")).To(BeTrue())
		Expect(tp.IsTrusted("172.18.255.254")).To(BeTrue())
		Expect(tp.IsTrusted("172.19.0.1")).To(BeFalse())
	})

	It("combines exact IPs and CIDR ranges in the trusted set", func() {
		tp, err := authmw.ParseTrustedProxies("127.0.0.1, ::1, 172.18.0.0/16")
		Expect(err).To(Succeed())
		Expect(tp.IsTrusted("127.0.0.1")).To(BeTrue())
		Expect(tp.IsTrusted("::1")).To(BeTrue())
		Expect(tp.IsTrusted("172.18.42.10")).To(BeTrue())
	})

	It("rejects invalid IP entries", func() {
		_, err := authmw.ParseTrustedProxies("not-an-ip")
		Expect(err).To(MatchError(authmw.ErrInvalidTrustedProxy))
	})

	It("rejects invalid CIDR entries", func() {
		_, err := authmw.ParseTrustedProxies("999.0.0.0/8")
		Expect(err).To(MatchError(authmw.ErrInvalidTrustedProxy))
	})

	It("matches trusted remote addresses after removing the port", func() {
		tp, err := authmw.ParseTrustedProxies("127.0.0.1")
		Expect(err).To(Succeed())
		Expect(tp.IsTrusted("127.0.0.1:54321")).To(BeTrue())
	})

	It("rejects malformed remote addresses", func() {
		tp, err := authmw.ParseTrustedProxies("127.0.0.1")
		Expect(err).To(Succeed())
		Expect(tp.IsTrusted("not-valid")).To(BeFalse())
	})
})
