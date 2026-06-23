package revisions

import (
	"context"
	"testing"

	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync"
)

func TestRoutesConfigUsesSemanticRevisionIDs(t *testing.T) {
	_ = RoutesConfig{
		GetWorkspaceRevision: func(context.Context, *tree.Page, revision.RevisionID) (*revision.RevisionSnapshot, error) {
			return nil, nil
		},
		RestoreWorkspaceRevision: func(context.Context, *tree.Page, revision.RevisionID, workspacesync.Actor, workspacesync.Source) (*tree.Page, error) {
			return nil, nil
		},
	}
}
