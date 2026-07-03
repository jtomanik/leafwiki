package tree

import (
	"errors"
	"os"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

func matchErrorAs(target any) types.GomegaMatcher {
	return WithTransform(func(err error) bool {
		return errors.As(err, target)
	}, BeTrue())
}

func matchTreeNode(kind NodeKind, id PageID, title string) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Kind", Equal(kind)),
		HaveField("ID", Equal(id)),
		HaveField("Title", Equal(title)),
	)
}

func matchTreeNodePointer(kind NodeKind, id types.GomegaMatcher) types.GomegaMatcher {
	return SatisfyAll(
		Not(BeNil()),
		WithTransform(func(actual *PageNode) PageNode {
			if actual == nil {
				return PageNode{}
			}
			return *actual
		}, SatisfyAll(
			HaveField("Kind", Equal(kind)),
			HaveField("ID", id),
		)),
	)
}

func matchExistingPathSegment(id PageID) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Exists", BeTrue()),
		HaveField("ID", WithTransform(func(actual *PageID) PageID {
			if actual == nil {
				return ""
			}
			return *actual
		}, Equal(id))),
	)
}

func matchMissingPathSegment() types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Exists", BeFalse()),
		HaveField("ID", BeNil()),
	)
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
