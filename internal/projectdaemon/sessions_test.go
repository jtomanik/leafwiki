package projectdaemon

import (
	"context"
	"sync"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.It("TestSessionRegistryNotifiesOnlyOnCountTransitions", func() {
	t := ginkgo.GinkgoT()
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	var counts []int
	registry := NewSessionRegistry(time.Second, func(count int) {
		counts = append(counts, count)
	})
	registry.now = func() time.Time {
		return now
	}

	id, err := registry.Register()
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	registry.Release("missing-session")
	if got, want := joinCounts(counts), "1"; got != want {
		t.Fatalf("counts after missing release = %s, want %s", got, want)
	}

	registry.Heartbeat(id)
	now = now.Add(2 * time.Second)
	registry.PruneExpired()
	registry.PruneExpired()
	registry.Release(id)

	if got, want := joinCounts(counts), "1,0"; got != want {
		t.Fatalf("counts = %s, want %s", got, want)
	}

})

var _ = ginkgo.It("TestSessionRegistryUsesSemanticSessionIDs", func() {
	t := ginkgo.GinkgoT()
	registry := NewSessionRegistry(time.Second, nil)

	id, err := registry.Register()
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	var typed SessionID = id

	if !registry.Heartbeat(typed) {
		t.Fatalf("Heartbeat returned false for registered session")
	}
	registry.Release(typed)

})

var _ = ginkgo.It("SessionRegistry exposes seen state and active count", func() {
	t := ginkgo.GinkgoT()
	registry := NewSessionRegistry(time.Second, nil)
	if registry.SeenSession() {
		t.Fatalf("SeenSession = true before any registration")
	}
	if seen, count := registry.SeenSessionCount(); seen || count != 0 {
		t.Fatalf("SeenSessionCount = %v/%d, want false/0", seen, count)
	}

	id, err := registry.Register()
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if seen, count := registry.SeenSessionCount(); !seen || count != 1 {
		t.Fatalf("SeenSessionCount after register = %v/%d, want true/1", seen, count)
	}
	registry.Release(id)
	if seen, count := registry.SeenSessionCount(); !seen || count != 0 {
		t.Fatalf("SeenSessionCount after release = %v/%d, want true/0", seen, count)
	}
})

var _ = ginkgo.It("TestSessionRegistryRunExpiryLoopPrunesExpiredSessions", func() {
	t := ginkgo.GinkgoT()
	var counts []int
	var countsMu sync.Mutex
	registry := NewSessionRegistry(10*time.Millisecond, func(count int) {
		countsMu.Lock()
		defer countsMu.Unlock()
		counts = append(counts, count)
	})
	id, err := registry.Register()
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go registry.RunExpiryLoop(ctx, 5*time.Millisecond)

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if registry.Count() == 0 {
			if registry.Heartbeat(id) {
				t.Fatalf("expired session accepted heartbeat")
			}
			for time.Now().Before(deadline) {
				if got := lockedJoinCounts(&countsMu, &counts); got == "1,0" {
					return
				}
				time.Sleep(time.Millisecond)
			}
			t.Fatalf("counts = %s, want %s", lockedJoinCounts(&countsMu, &counts), "1,0")
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("session was not pruned before timeout; count=%d counts=%s", registry.Count(), lockedJoinCounts(&countsMu, &counts))

})

func joinCounts(counts []int) string {
	if len(counts) == 0 {
		return ""
	}
	out := ""
	for i, count := range counts {
		if i > 0 {
			out += ","
		}
		out += string(rune('0' + count))
	}
	return out
}

func lockedJoinCounts(mu *sync.Mutex, counts *[]int) string {
	mu.Lock()
	defer mu.Unlock()
	return joinCounts(*counts)
}
