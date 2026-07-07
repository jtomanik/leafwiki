package search

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"
)

var _ = ginkgo.Describe("IndexingStatus", ginkgo.Label("unit"), func() {
	ginkgo.It("starts with an inactive empty status", func() {
		status := NewIndexingStatus()

		Expect(status).To(matchIndexingNotStarted())
	})

	ginkgo.It("tracks successful indexing runs and returns independent snapshots", func() {
		status := NewIndexingStatus()

		status.Start()
		Expect(status).To(matchActiveIndexingRun(0, 0))
		status.Success()
		status.Success()
		status.Finish()

		Expect(status).To(matchReadyIndexingRun(2))
		snapshot := status.Snapshot()
		Expect(snapshot).To(matchReadyIndexingRun(2))

		snapshot.Indexed = 99
		Expect(status.Snapshot()).To(matchReadyIndexingRun(2))
	})

	ginkgo.It("tracks failed indexing runs and resets counters on restart", func() {
		status := NewIndexingStatus()

		status.Start()
		status.Success()
		status.Fail()
		status.Finish()

		Expect(status).To(matchFailedIndexingRun(1, 1))

		status.Start()
		Expect(status).To(matchActiveIndexingRun(0, 0))
		snapshot := status.Snapshot()
		Expect(snapshot).To(matchActiveIndexingRun(0, 0))
	})
})

type indexingStatusMatcher struct {
	state   string
	indexed int
	failed  int
}

func matchIndexingNotStarted() types.GomegaMatcher {
	return indexingStatusMatcher{state: "not started"}
}

func matchActiveIndexingRun(indexed int, failed int) types.GomegaMatcher {
	return indexingStatusMatcher{state: "active", indexed: indexed, failed: failed}
}

func matchReadyIndexingRun(indexed int) types.GomegaMatcher {
	return indexingStatusMatcher{state: "ready", indexed: indexed}
}

func matchFailedIndexingRun(indexed int, failed int) types.GomegaMatcher {
	return indexingStatusMatcher{state: "failed", indexed: indexed, failed: failed}
}

func (m indexingStatusMatcher) Match(actual interface{}) (bool, error) {
	status, ok := actual.(*IndexingStatus)
	if !ok || status == nil {
		return false, nil
	}
	snapshot := status.Snapshot()
	switch m.state {
	case "not started":
		return !status.IsActive() && !status.IsReady() && !status.IsFailed() &&
			snapshot.Indexed == 0 && snapshot.Failed == 0 && snapshot.FinishedAt.IsZero(), nil
	case "active":
		return status.IsActive() && !status.IsReady() && !status.IsFailed() &&
			snapshot.Indexed == m.indexed && snapshot.Failed == m.failed && snapshot.FinishedAt.IsZero(), nil
	case "ready":
		return !status.IsActive() && status.IsReady() && !status.IsFailed() &&
			snapshot.Indexed == m.indexed && snapshot.Failed == 0 && !snapshot.FinishedAt.IsZero(), nil
	case "failed":
		return !status.IsActive() && !status.IsReady() && status.IsFailed() &&
			snapshot.Indexed == m.indexed && snapshot.Failed == m.failed && !snapshot.FinishedAt.IsZero(), nil
	default:
		return false, nil
	}
}

func (m indexingStatusMatcher) FailureMessage(actual interface{}) string {
	return format.Message(actual, "to describe indexing status", m.state)
}

func (m indexingStatusMatcher) NegatedFailureMessage(actual interface{}) string {
	return format.Message(actual, "not to describe indexing status", m.state)
}
