package revision

import (
	"testing"

	"github.com/perber/wiki/internal/core/tree"
)

func TestServiceRevisionMethodsUseSemanticIDs(t *testing.T) {
	var _ RevisionID = Revision{}.ID
	var _ func(*Service, tree.PageID, tree.UserID, string) (*Revision, bool, error) = (*Service).RecordContentUpdate
	var _ func(*Service, []*tree.Page, tree.UserID, string) []error = (*Service).RecordContentUpdates
	var _ func(*Service, tree.PageID, tree.UserID, string) (*Revision, bool, error) = (*Service).RecordAssetChange
	var _ func(*Service, tree.PageID, tree.UserID, string) (*Revision, bool, error) = (*Service).RecordStructureChange
	var _ func(*Service, tree.PageID, RevisionID) (*RevisionSnapshot, error) = (*Service).GetRevisionSnapshot
	var _ func(*Service, tree.PageID, RevisionID, RevisionID) (*RevisionComparison, error) = (*Service).CompareRevisionSnapshots
	var _ func(*Service, tree.PageID, RevisionID, tree.AssetName) (*RevisionAssetContent, error) = (*Service).GetRevisionAsset
	var _ func(*Service, tree.PageID, RevisionID, tree.UserID) error = (*Service).RestoreRevision
}
