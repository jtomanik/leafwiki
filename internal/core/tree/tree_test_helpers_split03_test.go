package tree

import (
	. "github.com/onsi/gomega"
	"os"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega/types"
)

func haveChildPageIDs(ids ...PageID) types.GomegaMatcher {
	return HaveField("Children", WithTransform(func(children []*PageNode) []PageID {
		out := make([]PageID, 0, len(children))
		for _, child := range children {
			out = append(out, child.ID)
		}
		return out
	}, Equal(ids)))
}

func haveChildPositions(positions ...int) types.GomegaMatcher {
	return HaveField("Children", WithTransform(func(children []*PageNode) []int {
		out := make([]int, 0, len(children))
		for _, child := range children {
			out = append(out, child.Position)
		}
		return out
	}, Equal(positions)))
}

func matchExistingPathSegment(id PageID) types.GomegaMatcher {
	return WithTransform(pathSegmentObservationFrom, Equal(pathSegmentObservation{
		State: pathSegmentExisting,
		ID:    id,
	}))
}

func matchMissingPathSegment() types.GomegaMatcher {
	return WithTransform(pathSegmentObservationFrom, Equal(pathSegmentObservation{
		State: pathSegmentMissing,
	}))
}

func matchRootSection() types.GomegaMatcher {
	return SatisfyAll(
		Not(BeNil()),
		HaveField("ID", Equal(RootPageID)),
		HaveField("Slug", Equal(newFixtureSlug("root"))),
		HaveField("Title", Equal("root")),
		HaveField("Kind", Equal(NodeKindSection)),
		HaveField("Parent", BeNil()),
		HaveField("Children", BeEmpty()),
	)
}

func tempTreeDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-tree-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}
