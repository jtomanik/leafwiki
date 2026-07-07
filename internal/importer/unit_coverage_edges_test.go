package importer

import (
	"errors"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("importer source discovery", ginkgo.Label("unit"), func() {
	ginkgo.It("ignores directory entries while looking for source index files", func() {
		base := importerTempDir()
		Expect(os.Mkdir(filepath.Join(base, "docs"), 0o755)).To(Succeed())
		Expect(os.Mkdir(filepath.Join(base, "docs", "index.md"), 0o755)).To(Succeed())

		Expect(observeSourceIndexLookup(base, "docs")).To(Equal(sourceIndexLookup{
			Outcome: sourceIndexAbsent,
		}))
	})

	ginkgo.It("uses README files as section suffix keys", func() {
		Expect(buildSourcePathSuffixKeys("docs/README.md", tree.NodeKindSection)).To(Equal([]string{"docs"}))
	})

	ginkgo.It("drops root index files from source suffix keys", func() {
		Expect(buildSourcePathSuffixKeys("index.md", tree.NodeKindPage)).To(BeNil())
	})

	ginkgo.It("prioritizes discovered index pages before shallower markdown files", func() {
		base := importerTempDir()
		importerWriteFile(base, "a.md", "# A")
		importerWriteFile(base, "docs/index.md", "# Docs")

		entries, err := FindMarkdownEntries(base)

		Expect(err).To(Succeed())
		Expect(entries).To(HaveExactElements(
			HaveField("SourcePath", newFixtureWorkspaceSourcePath("docs/index.md")),
			HaveField("SourcePath", newFixtureWorkspaceSourcePath("a.md")),
		))
	})
})

var _ = ginkgo.Describe("import plan errors", ginkgo.Label("unit"), func() {
	ginkgo.It("preserves wrapped planner errors and semantic error codes", func() {
		cause := errors.New("plan failed")
		err := newImportPlanError(ImportErrorCodeLookupPathFailed, cause)

		Expect(err).To(MatchError(cause))
		Expect(planErrorObservationFor(err, cause)).To(Equal(planErrorObservation{
			WrapsCause: true,
			Code:       ImportErrorCodeLookupPathFailed,
		}))
	})

	ginkgo.It("keeps nil and unknown plan errors without semantic codes", func() {
		Expect(newImportPlanError(ImportErrorCodeLookupPathFailed, nil)).To(Succeed())
		Expect((importPlanError{}).Error()).To(BeEmpty())
		Expect(importPlanErrorCode(errors.New("plain error"))).To(BeEmpty())
	})
})

type sourceIndexLookup struct {
	Name    string
	Outcome sourceIndexLookupOutcome
}

type sourceIndexLookupOutcome string

const (
	sourceIndexAbsent sourceIndexLookupOutcome = "absent"
	sourceIndexFound  sourceIndexLookupOutcome = "found"
)

func observeSourceIndexLookup(base string, sourceDir string) sourceIndexLookup {
	name, found := sourceDirIndexFile(base, sourceDir)
	if !found {
		return sourceIndexLookup{Outcome: sourceIndexAbsent}
	}
	return sourceIndexLookup{Name: name, Outcome: sourceIndexFound}
}

type planErrorObservation struct {
	WrapsCause bool
	Code       ImportErrorCode
}

func planErrorObservationFor(err error, cause error) planErrorObservation {
	return planErrorObservation{
		WrapsCause: errors.Is(err, cause),
		Code:       importPlanErrorCode(err),
	}
}
