package security

import (
	"encoding/hex"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("security middleware support primitives", Label("unit"), func() {
	It("generates CSRF tokens from cryptographic entropy", func() {
		originalRandRead := csrfRandRead
		DeferCleanup(func() {
			csrfRandRead = originalRandRead
		})
		csrfRandRead = func(buf []byte) (int, error) {
			for i := range buf {
				buf[i] = byte(i + 1)
			}
			return len(buf), nil
		}

		token, err := NewCSRFCookie(false, time.Hour).generateToken()
		Expect(err).To(Succeed())
		decoded, err := hex.DecodeString(token)
		Expect(err).To(Succeed())

		Expect(decoded).To(Equal(sequentialEntropy(32)))
	})

	It("returns entropy failures before issuing a CSRF token", func() {
		entropyErr := errors.New("csrf entropy failed")
		originalRandRead := csrfRandRead
		DeferCleanup(func() {
			csrfRandRead = originalRandRead
		})
		csrfRandRead = func([]byte) (int, error) {
			return 0, entropyErr
		}

		token, err := NewCSRFCookie(false, time.Hour).generateToken()

		Expect(token).To(BeEmpty())
		Expect(err).To(MatchError(entropyErr))
	})

	It("keeps only active rate-limit events inside the configured window", func() {
		now := time.Now()
		limiter := &rateLimiter{
			hits: map[string][]time.Time{
				"active-client":  {now.Add(-time.Second), now.Add(-2 * time.Second)},
				"expired-client": {now.Add(-time.Minute)},
			},
			window: 10 * time.Second,
		}

		limiter.cleanup()

		Expect(rateLimiterWindowFor(limiter)).To(ConsistOf(
			Equal(rateLimiterWindowState{ClientKey: "active-client", ActiveEvents: 2}),
		))
	})
})

type rateLimiterWindowState struct {
	ClientKey    string
	ActiveEvents int
}

func rateLimiterWindowFor(limiter *rateLimiter) []rateLimiterWindowState {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	states := make([]rateLimiterWindowState, 0, len(limiter.hits))
	for key, events := range limiter.hits {
		states = append(states, rateLimiterWindowState{
			ClientKey:    key,
			ActiveEvents: len(events),
		})
	}
	return states
}

func sequentialEntropy(length int) []byte {
	out := make([]byte, length)
	for i := range out {
		out[i] = byte(i + 1)
	}
	return out
}
