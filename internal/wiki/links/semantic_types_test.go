package links

import (
	"testing"

	"github.com/perber/wiki/internal/core/tree"
)

func TestLinkUseCaseInputsUseSemanticPageIDs(t *testing.T) {
	t.Parallel()

	status := GetLinkStatusInput{PageID: tree.NewPageIDUnchecked("page-1")}
	var _ tree.PageID = status.PageID

	backlinks := GetBacklinksInput{PageID: tree.NewPageIDUnchecked("page-1")}
	var _ tree.PageID = backlinks.PageID

	outgoing := GetOutgoingLinksInput{PageID: tree.NewPageIDUnchecked("page-1")}
	var _ tree.PageID = outgoing.PageID
}
