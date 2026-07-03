package projectdaemon

import (
	"context"
	"sync"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

var _ = ginkgo.Describe("session registry", func() {
	ginkgo.It("notifies only when the active session count changes", func() {
		now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
		var counts []int
		registry := NewSessionRegistry(time.Second, func(count int) {
			counts = append(counts, count)
		})
		registry.now = func() time.Time {
			return now
		}

		id, err := registry.Register()
		Expect(err).NotTo(HaveOccurred())
		registry.Release("missing-session")
		Expect(counts).To(Equal([]int{1}))

		Expect(registry.Heartbeat(id)).To(BeTrue())
		now = now.Add(2 * time.Second)
		Expect(registry.PruneExpired()).To(BeZero())
		Expect(registry.PruneExpired()).To(BeZero())
		registry.Release(id)

		Expect(counts).To(Equal([]int{1, 0}))
	})

	ginkgo.It("accepts semantic session identifiers for heartbeat and release", func() {
		registry := NewSessionRegistry(time.Second, nil)

		id, err := registry.Register()
		Expect(err).NotTo(HaveOccurred())
		var typed SessionID = id

		Expect(registry.Heartbeat(typed)).To(BeTrue())
		registry.Release(typed)
	})

	ginkgo.It("reports whether any session has been seen and how many are active", func() {
		registry := NewSessionRegistry(time.Second, nil)

		Expect(registry.SeenSession()).To(BeFalse())
		Expect(registry).To(reportSeenSessionState(false, 0))

		id, err := registry.Register()
		Expect(err).NotTo(HaveOccurred())
		Expect(registry).To(reportSeenSessionState(true, 1))

		registry.Release(id)
		Expect(registry).To(reportSeenSessionState(true, 0))
	})

	ginkgo.It("prunes expired sessions from the background expiry loop", func() {
		var counts []int
		var countsMu sync.Mutex
		registry := NewSessionRegistry(10*time.Millisecond, func(count int) {
			countsMu.Lock()
			defer countsMu.Unlock()
			counts = append(counts, count)
		})
		id, err := registry.Register()
		Expect(err).NotTo(HaveOccurred())

		ctx, cancel := context.WithCancel(context.Background())
		ginkgo.DeferCleanup(cancel)
		go registry.RunExpiryLoop(ctx, 5*time.Millisecond)

		Eventually(registry.Count).
			WithTimeout(500 * time.Millisecond).
			WithPolling(5 * time.Millisecond).
			Should(BeZero())
		Expect(registry.Heartbeat(id)).To(BeFalse())
		Eventually(func() []int {
			countsMu.Lock()
			defer countsMu.Unlock()
			return append([]int(nil), counts...)
		}).
			WithTimeout(500 * time.Millisecond).
			WithPolling(time.Millisecond).
			Should(Equal([]int{1, 0}))
	})
})

func reportSeenSessionState(seen bool, count int) types.GomegaMatcher {
	return WithTransform(func(registry *SessionRegistry) sessionRegistrySeenState {
		ginkgo.GinkgoHelper()
		gotSeen, gotCount := registry.SeenSessionCount()
		return sessionRegistrySeenState{seen: gotSeen, count: gotCount}
	}, Equal(sessionRegistrySeenState{seen: seen, count: count}))
}

type sessionRegistrySeenState struct {
	seen  bool
	count int
}

func lockedJoinCounts(mu *sync.Mutex, counts *[]int) string {
	mu.Lock()
	defer mu.Unlock()
	return joinTestCounts(*counts)
}

func matchSessionHandle(id SessionID) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ID": Equal(id),
	})
}
