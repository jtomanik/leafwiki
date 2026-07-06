package tree

import ginkgo "github.com/onsi/ginkgo/v2"

func newInMemoryService() *TreeService {
	ginkgo.GinkgoHelper()
	svc := NewTreeService(tempTreeDir())
	svc.tree = edgeSectionNode(RootPageID, newFixtureSlug("root"), "Root", nil)
	svc.rebuildIndexesLocked()
	return svc
}
