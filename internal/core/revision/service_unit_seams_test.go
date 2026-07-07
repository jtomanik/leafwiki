package revision

import (
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/tree"
)

type revisionServiceRecordContract struct {
	Type    RevisionType
	PageID  tree.PageID
	Summary string
}

type revisionRestoredRawMode uint8

const (
	revisionRestoredRawBodyOnly revisionRestoredRawMode = iota
	revisionRestoredRawReplacingMetadata
)

type revisionRestoredRawObservation struct {
	Mode revisionRestoredRawMode
	Raw  string
	Err  error
}

func unitRevisionPage(pageID tree.PageID, content string) *tree.Page {
	createdAt := time.Unix(1700000000, 0).UTC()
	return &tree.Page{
		PageNode: &tree.PageNode{
			ID:    pageID,
			Title: "Guide",
			Slug:  newFixtureSlug("guide"),
			Kind:  tree.NodeKindPage,
			Metadata: tree.PageMetadata{
				CreatedAt:    createdAt,
				UpdatedAt:    createdAt.Add(time.Minute),
				CreatorID:    newFixtureUserID("creator-user"),
				LastAuthorID: newFixtureUserID("author-user"),
			},
		},
		Content: content,
	}
}

func writeUnitRevisionLiveAsset(storageDir string, pageID tree.PageID, name, content string) {
	ginkgo.GinkgoHelper()

	dir := filepath.Join(storageDir, "assets", pageID.MetadataValue())
	Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)).To(Succeed())
}

func restoredRawContentObservation(raw string, replaceMetadata bool, err error) revisionRestoredRawObservation {
	mode := revisionRestoredRawBodyOnly
	if replaceMetadata {
		mode = revisionRestoredRawReplacingMetadata
	}
	return revisionRestoredRawObservation{Mode: mode, Raw: raw, Err: err}
}

func matchRestoredRawContent(mode revisionRestoredRawMode, raw types.GomegaMatcher) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Mode": Equal(mode),
		"Raw":  raw,
		"Err":  Succeed(),
	})
}

func matchRevisionServiceRecord(expected revisionServiceRecordContract) types.GomegaMatcher {
	return WithTransform(func(rev *Revision) revisionServiceRecordContract {
		if rev == nil {
			return revisionServiceRecordContract{}
		}
		return revisionServiceRecordContract{
			Type:    rev.Type,
			PageID:  rev.PageID,
			Summary: rev.Summary,
		}
	}, gstruct.MatchAllFields(gstruct.Fields{
		"Type":    Equal(expected.Type),
		"PageID":  Equal(expected.PageID),
		"Summary": Equal(expected.Summary),
	}))
}

func matchRevisionCommitIdentity(expected tree.RevisionID) types.GomegaMatcher {
	return WithTransform(func(actual tree.RevisionID) tree.RevisionID {
		return tree.RevisionIDFromString(actual.CommitID())
	}, Equal(expected))
}

func sRevisionState(pageID tree.PageID) *RevisionState {
	return &RevisionState{
		PageID:      pageID,
		Title:       "Guide",
		Slug:        newFixtureSlug("guide"),
		Kind:        tree.NodeKindPage,
		Content:     "Body",
		ContentHash: sha256HexBytes([]byte("Body")),
	}
}
