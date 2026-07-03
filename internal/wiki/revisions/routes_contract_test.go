package revisions

import (
	"context"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = ginkgo.Describe("routes contracts", func() {
	ginkgo.It("requires semantic revision IDs in route callbacks", func() {
		_ = RoutesConfig{
			GetWorkspaceRevision: func(context.Context, *tree.Page, revision.RevisionID) (*revision.RevisionSnapshot, error) {
				return nil, nil
			},
			RestoreWorkspaceRevision: func(context.Context, *tree.Page, revision.RevisionID, workspacesync.Actor, workspacesync.Source) (*tree.Page, error) {
				return nil, nil
			},
		}
	})
})
