package search

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("IndexingStatus", func() {
	ginkgo.It("starts with an inactive empty status", func() {
		status := NewIndexingStatus()

		Expect(status.IsActive()).To(BeFalse())
		Expect(status.IsFailed()).To(BeFalse())
		Expect(status.IsReady()).To(BeFalse())
		snapshot := status.Snapshot()
		Expect(snapshot.Active).To(BeFalse())
		Expect(snapshot.Indexed).To(Equal(0))
		Expect(snapshot.Failed).To(Equal(0))
		Expect(snapshot.FinishedAt.IsZero()).To(BeTrue())
	})

	ginkgo.It("tracks successful indexing runs and returns independent snapshots", func() {
		status := NewIndexingStatus()

		status.Start()
		Expect(status.IsActive()).To(BeTrue())
		status.Success()
		status.Success()
		status.Finish()

		Expect(status.IsActive()).To(BeFalse())
		Expect(status.IsReady()).To(BeTrue())
		Expect(status.IsFailed()).To(BeFalse())
		snapshot := status.Snapshot()
		Expect(snapshot.Indexed).To(Equal(2))
		Expect(snapshot.Failed).To(Equal(0))
		Expect(snapshot.FinishedAt.IsZero()).To(BeFalse())

		snapshot.Indexed = 99
		Expect(status.Snapshot().Indexed).To(Equal(2))
	})

	ginkgo.It("tracks failed indexing runs and resets counters on restart", func() {
		status := NewIndexingStatus()

		status.Start()
		status.Success()
		status.Fail()
		status.Finish()

		Expect(status.IsReady()).To(BeFalse())
		Expect(status.IsFailed()).To(BeTrue())
		Expect(status.Snapshot().Indexed).To(Equal(1))
		Expect(status.Snapshot().Failed).To(Equal(1))

		status.Start()
		Expect(status.IsActive()).To(BeTrue())
		Expect(status.IsReady()).To(BeFalse())
		Expect(status.IsFailed()).To(BeFalse())
		snapshot := status.Snapshot()
		Expect(snapshot.Indexed).To(Equal(0))
		Expect(snapshot.Failed).To(Equal(0))
		Expect(snapshot.FinishedAt.IsZero()).To(BeTrue())
	})
})
