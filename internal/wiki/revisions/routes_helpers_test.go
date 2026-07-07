package revisions

import (
	"net/http/httptest"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/revision"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

type revisionRouteFixture struct {
	treeService *tree.TreeService
	pageID      tree.PageID
}

func newRevisionRouteFixture() revisionRouteFixture {
	ginkgo.GinkgoHelper()
	gin.SetMode(gin.TestMode)

	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{
		DataDir: newRevisionTempDir(),
		RootDir: newRevisionTempDir(),
	})
	Expect(treeService.LoadTree()).To(Succeed())
	kind := tree.NodeKindPage
	pageID, err := treeService.CreateNode(newFixtureUserID("alice"), nil, "Page A", newFixtureSlug("page-a"), &kind)
	Expect(err).NotTo(HaveOccurred())

	return revisionRouteFixture{
		treeService: treeService,
		pageID:      *pageID,
	}
}

func newRevisionTempDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-revisions-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func performRevisionHandlerRequest(handler gin.HandlerFunc, method, target string, params gin.Params, user *coreauth.User) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, nil)
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Params = params
	if user != nil {
		c.Set("user", user)
	}

	handler(c)
	return rec
}

func HaveRevisionRouteError(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.HaveHTTPStructuredError(status, code, sharederrors.MessageIDForCode(code))
}

type revisionComparisonContentState string

const revisionComparisonContentChanged revisionComparisonContentState = "changed"

type revisionComparisonObservation struct {
	ContentState  revisionComparisonContentState
	BaseContent   string
	TargetContent string
	AssetChanges  []RevisionAssetDeltaResponse
}

func observeRevisionComparison(response RevisionComparisonResponse) revisionComparisonObservation {
	observation := revisionComparisonObservation{
		AssetChanges: response.AssetChanges,
	}
	if response.ContentChanged {
		observation.ContentState = revisionComparisonContentChanged
	}
	if response.Base != nil {
		observation.BaseContent = response.Base.Content
	}
	if response.Target != nil {
		observation.TargetContent = response.Target.Content
	}
	return observation
}

func matchContentChangedRevisionComparison(baseContent string, targetContent string, assetChanges types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	fields := gstruct.Fields{
		"ContentState":  Equal(revisionComparisonContentChanged),
		"BaseContent":   Equal(baseContent),
		"TargetContent": Equal(targetContent),
	}
	if assetChanges != nil {
		fields["AssetChanges"] = assetChanges
	}
	return WithTransform(observeRevisionComparison, gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

func matchRevisionWireID(want revision.RevisionID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(got string) revision.RevisionID {
		return revision.RevisionIDFromString(got)
	}, Equal(want))
}

func matchRevisionPageWireID(want tree.PageID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(got string) tree.PageID {
		return tree.PageIDFromString(got)
	}, Equal(want))
}

func MatchRevisionErrorCode(code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.MatchLocalizedError(code, sharederrors.MessageIDForCode(code))
}

func revisionFor(pageID tree.PageID, revisionID revision.RevisionID) *revision.Revision {
	ginkgo.GinkgoHelper()

	return &revision.Revision{
		ID:        revisionID,
		PageID:    pageID,
		Type:      revision.RevisionTypeContentUpdate,
		AuthorID:  "alice",
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Title:     "Page A",
		Slug:      newFixtureSlug("page-a"),
		Kind:      tree.NodeKindPage,
		Path:      "page-a",
	}
}

func revisionSnapshotFor(pageID tree.PageID, revisionID revision.RevisionID, content string) *revision.RevisionSnapshot {
	ginkgo.GinkgoHelper()

	return &revision.RevisionSnapshot{
		Revision: revisionFor(pageID, revisionID),
		Content:  content,
	}
}
