package mcp

import (
	"context"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corerevision "github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	wikirevisions "github.com/perber/wiki/internal/wiki/revisions"
	"github.com/perber/wiki/internal/workspacesync"
)

func (r *Routes) registerRevisionTools(server *sdkmcp.Server, _ httpinternal.RouterOptions) {
	addTypedTool[listRevisionsInput, listRevisionsOutput](server, toolListRevisions, func(ctx context.Context, in listRevisionsInput) (listRevisionsOutput, error) {
		if r.listWorkspaceRevisions == nil {
			return listRevisionsOutput{}, unavailableWorkspaceRevisionBackend()
		}
		pageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
		if err != nil {
			return listRevisionsOutput{}, err
		}
		limit, err := wikirevisions.NormalizeRevisionListLimit(in.Limit, pageID)
		if err != nil {
			return listRevisionsOutput{}, err
		}
		page, err := r.workspaceRevisionPage(ctx, pageID)
		if err != nil {
			return listRevisionsOutput{}, err
		}
		out, err := r.listWorkspaceRevisions(ctx, page, strings.TrimSpace(in.Cursor), limit)
		if err != nil {
			return listRevisionsOutput{}, err
		}
		revisions := make([]*wikirevisions.RevisionResponse, 0, len(out.Revisions))
		for _, rev := range out.Revisions {
			revisions = append(revisions, wikirevisions.ToRevisionResponse(rev, r.userResolver))
		}
		return listRevisionsOutput{Revisions: revisions, NextCursor: out.NextCursor}, nil
	})

	addTypedTool[pageIDInput, revisionOutput](server, toolGetLatestRevision, func(ctx context.Context, in pageIDInput) (revisionOutput, error) {
		if r.listWorkspaceRevisions == nil {
			return revisionOutput{}, unavailableWorkspaceRevisionBackend()
		}
		pageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
		if err != nil {
			return revisionOutput{}, err
		}
		page, err := r.workspaceRevisionPage(ctx, pageID)
		if err != nil {
			return revisionOutput{}, err
		}
		out, err := r.listWorkspaceRevisions(ctx, page, "", 1)
		if err != nil || len(out.Revisions) == 0 {
			return revisionOutput{}, wikirevisions.NewRevisionNotFoundError("Revision not found", "revision for page %s not found", pageID)
		}
		return revisionOutput{Revision: wikirevisions.ToRevisionResponse(out.Revisions[0], r.userResolver)}, nil
	})

	addTypedTool[revisionIDInput, *wikirevisions.RevisionSnapshotResponse](server, toolGetRevision, func(ctx context.Context, in revisionIDInput) (*wikirevisions.RevisionSnapshotResponse, error) {
		if r.getWorkspaceRevision == nil {
			return nil, unavailableWorkspaceRevisionBackend()
		}
		rawPageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
		if err != nil {
			return nil, err
		}
		pageID, revisionID, err := wikirevisions.ValidateRevisionLookupInput(rawPageID, in.RevisionID)
		if err != nil {
			return nil, err
		}
		page, err := r.workspaceRevisionPage(ctx, pageID)
		if err != nil {
			return nil, err
		}
		snapshot, err := r.getWorkspaceRevision(ctx, page, revisionID)
		if err != nil {
			return nil, wikirevisions.NewRevisionNotFoundError("Revision not found", "revision %s for page %s not found", revisionID, pageID)
		}
		return wikirevisions.ToSnapshotResponse(snapshot, r.userResolver), nil
	})

	addTypedTool[compareRevisionsInput, *wikirevisions.RevisionComparisonResponse](server, toolCompareRevisions, func(ctx context.Context, in compareRevisionsInput) (*wikirevisions.RevisionComparisonResponse, error) {
		if r.getWorkspaceRevision == nil {
			return nil, unavailableWorkspaceRevisionBackend()
		}
		rawPageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
		if err != nil {
			return nil, err
		}
		pageID, baseRevisionID, targetRevisionID, err := wikirevisions.ValidateRevisionCompareInput(rawPageID, in.BaseRevisionID, in.TargetRevisionID)
		if err != nil {
			return nil, err
		}
		page, err := r.workspaceRevisionPage(ctx, pageID)
		if err != nil {
			return nil, err
		}
		base, baseErr := r.getWorkspaceRevision(ctx, page, baseRevisionID)
		target, targetErr := r.getWorkspaceRevision(ctx, page, targetRevisionID)
		if baseErr != nil || targetErr != nil {
			return nil, wikirevisions.NewRevisionNotFoundError("Revision not found", "revision compare resource for page %s not found", pageID)
		}
		return wikirevisions.ToComparisonResponse(&corerevision.RevisionComparison{
			Base:           base,
			Target:         target,
			ContentChanged: base.Content != target.Content,
			AssetChanges:   nil,
		}, r.userResolver), nil
	})

	addTypedTool[revisionAssetInput, assetOutput](server, toolGetRevisionAsset, func(_ context.Context, in revisionAssetInput) (assetOutput, error) {
		rawPageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
		if err != nil {
			return assetOutput{}, err
		}
		pageID, revisionID, assetName, err := wikirevisions.ValidateRevisionAssetInput(rawPageID, in.RevisionID, in.AssetName)
		if err != nil {
			return assetOutput{}, err
		}
		return assetOutput{}, wikirevisions.NewRevisionNotFoundError("Revision asset not found", "workspace sync revisions do not track assets for %s at %s in %s", assetName, pageID, revisionID)
	})

	addEditorTool[revisionIDInput, pageOutput](r, server, toolRestoreRevision, func(ctx context.Context, actor toolActor, in revisionIDInput) (pageOutput, error) {
		if r.restoreWorkspaceRevision == nil {
			return pageOutput{}, unavailableWorkspaceRevisionBackend()
		}
		rawPageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
		if err != nil {
			return pageOutput{}, err
		}
		pageID, revisionID, err := wikirevisions.ValidateRevisionLookupInput(rawPageID, in.RevisionID)
		if err != nil {
			return pageOutput{}, err
		}
		page, err := r.workspaceRevisionPage(ctx, pageID)
		if err != nil {
			return pageOutput{}, err
		}
		restored, err := r.restoreWorkspaceRevision(ctx, page, revisionID, workspacesync.Actor{
			ID:    actor.ID,
			Name:  actor.User.Username,
			Email: actor.User.Email,
		}, workspacesync.SourceMCP)
		if err != nil {
			return pageOutput{}, err
		}
		return pageOutput{Page: r.apiPage(restored, 0)}, nil
	})
}

func (r *Routes) workspaceRevisionPage(ctx context.Context, pageID string) (*tree.Page, error) {
	out, err := r.getPage.Execute(ctx, wikipages.GetPageInput{ID: strings.TrimSpace(pageID)})
	if err != nil {
		return nil, err
	}
	return out.Page, nil
}

func unavailableWorkspaceRevisionBackend() error {
	return wikirevisions.NewRevisionNotFoundError("Revision not found", "workspace revision backend is unavailable")
}
