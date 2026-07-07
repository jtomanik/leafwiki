package links

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type targetKindStorageObservation struct {
	Input  TargetKind
	Stored TargetKind
}

var _ = ginkgo.Describe("stored link target kinds", ginkgo.Label("unit"), func() {
	ginkgo.It("preserves explicit section and unknown target states", func() {
		Expect(observeStoredTargetKind(TargetKindSection)).To(Equal(targetKindStorageObservation{
			Input:  TargetKindSection,
			Stored: TargetKindSection,
		}))
		Expect(observeStoredTargetKind(TargetKindUnknown)).To(Equal(targetKindStorageObservation{
			Input:  TargetKindUnknown,
			Stored: TargetKindUnknown,
		}))
	})
})

func observeStoredTargetKind(kind TargetKind) targetKindStorageObservation {
	return targetKindStorageObservation{
		Input:  kind,
		Stored: kind.Stored(),
	}
}
