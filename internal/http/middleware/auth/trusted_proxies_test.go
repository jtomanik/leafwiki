package auth_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	authmw "github.com/perber/wiki/internal/http/middleware/auth"
)

type trustedProxyHeaderPolicyObservation struct {
	HonoredRemoteAddresses []string
	IgnoredRemoteAddresses []string
}

var _ = Describe("trusted proxy parsing", Label("unit"), func() {
	It("ignores proxy headers when the configured proxy list is empty", func() {
		tp, err := authmw.ParseTrustedProxies("")
		Expect(err).To(Succeed())

		Expect(observeTrustedProxyHeaderPolicy(tp, "127.0.0.1")).To(matchTrustedProxyHeaderPolicy(
			BeEmpty(),
			ConsistOf("127.0.0.1"),
		))
	})

	It("honors a configured single IP and ignores other addresses", func() {
		tp, err := authmw.ParseTrustedProxies("127.0.0.1")
		Expect(err).To(Succeed())

		Expect(observeTrustedProxyHeaderPolicy(tp, "127.0.0.1", "192.168.1.1")).To(matchTrustedProxyHeaderPolicy(
			ConsistOf("127.0.0.1"),
			ConsistOf("192.168.1.1"),
		))
	})

	It("honors every configured IP in a comma-separated list", func() {
		tp, err := authmw.ParseTrustedProxies("127.0.0.1, ::1, 10.0.0.1")
		Expect(err).To(Succeed())

		Expect(observeTrustedProxyHeaderPolicy(tp, "127.0.0.1", "::1", "10.0.0.1", "10.0.0.2")).To(matchTrustedProxyHeaderPolicy(
			ConsistOf("127.0.0.1", "::1", "10.0.0.1"),
			ConsistOf("10.0.0.2"),
		))
	})

	It("honors addresses inside configured CIDR ranges only", func() {
		tp, err := authmw.ParseTrustedProxies("172.18.0.0/16")
		Expect(err).To(Succeed())

		Expect(observeTrustedProxyHeaderPolicy(tp, "172.18.0.1", "172.18.255.254", "172.19.0.1")).To(matchTrustedProxyHeaderPolicy(
			ConsistOf("172.18.0.1", "172.18.255.254"),
			ConsistOf("172.19.0.1"),
		))
	})

	It("honors exact IPs and CIDR ranges from the same trusted set", func() {
		tp, err := authmw.ParseTrustedProxies("127.0.0.1, ::1, 172.18.0.0/16")
		Expect(err).To(Succeed())

		Expect(observeTrustedProxyHeaderPolicy(tp, "127.0.0.1", "::1", "172.18.42.10")).To(matchTrustedProxyHeaderPolicy(
			ConsistOf("127.0.0.1", "::1", "172.18.42.10"),
			BeEmpty(),
		))
	})

	It("rejects invalid IP entries", func() {
		_, err := authmw.ParseTrustedProxies("not-an-ip")
		Expect(err).To(MatchError(authmw.ErrInvalidTrustedProxy))
	})

	It("rejects invalid CIDR entries", func() {
		_, err := authmw.ParseTrustedProxies("999.0.0.0/8")
		Expect(err).To(MatchError(authmw.ErrInvalidTrustedProxy))
	})

	It("honors trusted remote addresses after removing the port", func() {
		tp, err := authmw.ParseTrustedProxies("127.0.0.1, ::1")
		Expect(err).To(Succeed())

		Expect(observeTrustedProxyHeaderPolicy(tp, "127.0.0.1:54321", "[::1]:54321")).To(matchTrustedProxyHeaderPolicy(
			ConsistOf("127.0.0.1:54321", "[::1]:54321"),
			BeEmpty(),
		))
	})

	It("ignores malformed remote addresses", func() {
		tp, err := authmw.ParseTrustedProxies("127.0.0.1")
		Expect(err).To(Succeed())

		Expect(observeTrustedProxyHeaderPolicy(tp, "not-valid")).To(matchTrustedProxyHeaderPolicy(
			BeEmpty(),
			ConsistOf("not-valid"),
		))
	})
})

func observeTrustedProxyHeaderPolicy(tp *authmw.TrustedProxies, remoteAddresses ...string) trustedProxyHeaderPolicyObservation {
	GinkgoHelper()

	observation := trustedProxyHeaderPolicyObservation{}
	for _, remoteAddress := range remoteAddresses {
		if tp.IsTrusted(remoteAddress) {
			observation.HonoredRemoteAddresses = append(observation.HonoredRemoteAddresses, remoteAddress)
			continue
		}
		observation.IgnoredRemoteAddresses = append(observation.IgnoredRemoteAddresses, remoteAddress)
	}
	return observation
}

func matchTrustedProxyHeaderPolicy(honoredRemoteAddresses, ignoredRemoteAddresses types.GomegaMatcher) types.GomegaMatcher {
	GinkgoHelper()

	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"HonoredRemoteAddresses": honoredRemoteAddresses,
		"IgnoredRemoteAddresses": ignoredRemoteAddresses,
	})
}
