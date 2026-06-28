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
		return r.listRevisionsTool(ctx, in)
	})

	addTypedTool[pageIDInput, revisionOutput](server, toolGetLatestRevision, func(ctx context.Context, in pageIDInput) (revisionOutput, error) {
		return r.latestRevisionTool(ctx, in)
	})

	addTypedTool[revisionIDInput, *wikirevisions.RevisionSnapshotResponse](server, toolGetRevision, func(ctx context.Context, in revisionIDInput) (*wikirevisions.RevisionSnapshotResponse, error) {
		return r.getRevisionTool(ctx, in)
	})

	addTypedTool[compareRevisionsInput, *wikirevisions.RevisionComparisonResponse](server, toolCompareRevisions, func(ctx context.Context, in compareRevisionsInput) (*wikirevisions.RevisionComparisonResponse, error) {
		return r.compareRevisionsTool(ctx, in)
	})

	addTypedTool[revisionAssetInput, assetOutput](server, toolGetRevisionAsset, func(_ context.Context, in revisionAssetInput) (assetOutput, error) {
		return revisionAssetTool(in)
	})

	addEditorTool[revisionIDInput, pageOutput](r, server, toolRestoreRevision, func(ctx context.Context, actor toolActor, in revisionIDInput) (pageOutput, error) {
		return r.restoreRevisionTool(ctx, actor, in)
	})
}

func (r *Routes) listRevisionsTool(ctx context.Context, in listRevisionsInput) (listRevisionsOutput, error) {
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
	out, err := r.listWorkspaceRevisions(ctx, page, strings.TrimSpace(in.Cursor), workspacesync.PageRevisionLimit(limit))
	if err != nil {
		return listRevisionsOutput{}, err
	}
	revisions := make([]*wikirevisions.RevisionResponse, 0, len(out.Revisions))
	for _, rev := range out.Revisions {
		revisions = append(revisions, wikirevisions.ToRevisionResponse(rev, r.userResolver))
	}
	return listRevisionsOutput{Revisions: revisions, NextCursor: out.NextCursor}, nil
}

func (r *Routes) latestRevisionTool(ctx context.Context, in pageIDInput) (revisionOutput, error) {
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
		return revisionOutput{}, wikirevisions.NewRevisionNotFoundError("Revision not found", "revision for page %s not found", pageID.MetadataValue())
	}
	return revisionOutput{Revision: wikirevisions.ToRevisionResponse(out.Revisions[0], r.userResolver)}, nil
}

func (r *Routes) getRevisionTool(ctx context.Context, in revisionIDInput) (*wikirevisions.RevisionSnapshotResponse, error) {
	if r.getWorkspaceRevision == nil {
		return nil, unavailableWorkspaceRevisionBackend()
	}
	rawPageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
	if err != nil {
		return nil, err
	}
	pageID, revisionID, err := wikirevisions.ValidateRevisionLookup(rawPageID, in.RevisionID)
	if err != nil {
		return nil, err
	}
	page, err := r.workspaceRevisionPage(ctx, pageID)
	if err != nil {
		return nil, err
	}
	snapshot, err := r.getWorkspaceRevision(ctx, page, revisionID)
	if err != nil {
		return nil, wikirevisions.NewRevisionNotFoundError("Revision not found", "revision %s for page %s not found", revisionID.CommitID(), pageID.MetadataValue())
	}
	return wikirevisions.ToSnapshotResponse(snapshot, r.userResolver), nil
}

func (r *Routes) compareRevisionsTool(ctx context.Context, in compareRevisionsInput) (*wikirevisions.RevisionComparisonResponse, error) {
	if r.getWorkspaceRevision == nil {
		return nil, unavailableWorkspaceRevisionBackend()
	}
	rawPageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
	if err != nil {
		return nil, err
	}
	pageID, baseRevisionID, targetRevisionID, err := wikirevisions.ValidateRevisionCompare(rawPageID, in.BaseRevisionID, in.TargetRevisionID)
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
		return nil, wikirevisions.NewRevisionNotFoundError("Revision not found", "revision compare resource for page %s not found", pageID.MetadataValue())
	}
	return wikirevisions.ToComparisonResponse(&corerevision.RevisionComparison{
		Base:           base,
		Target:         target,
		ContentChanged: base.Content != target.Content,
		AssetChanges:   nil,
	}, r.userResolver), nil
}

func revisionAssetTool(in revisionAssetInput) (assetOutput, error) {
	rawPageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
	if err != nil {
		return assetOutput{}, err
	}
	pageID, revisionID, assetName, err := wikirevisions.ValidateRevisionAsset(rawPageID, in.RevisionID, in.AssetName)
	if err != nil {
		return assetOutput{}, err
	}
	return assetOutput{}, wikirevisions.NewRevisionNotFoundError(
		"Revision asset not found",
		"workspace sync revisions do not track assets for %s at %s in %s",
		assetName.Filename(),
		pageID.MetadataValue(),
		revisionID.CommitID(),
	)
}

func (r *Routes) restoreRevisionTool(ctx context.Context, actor toolActor, in revisionIDInput) (pageOutput, error) {
	if r.restoreWorkspaceRevision == nil {
		return pageOutput{}, unavailableWorkspaceRevisionBackend()
	}
	rawPageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
	if err != nil {
		return pageOutput{}, err
	}
	pageID, revisionID, err := wikirevisions.ValidateRevisionLookup(rawPageID, in.RevisionID)
	if err != nil {
		return pageOutput{}, err
	}
	page, err := r.workspaceRevisionPage(ctx, pageID)
	if err != nil {
		return pageOutput{}, err
	}
	restored, err := r.restoreWorkspaceRevision(ctx, page, revisionID, workspacesync.Actor{
		ID:    workspacesync.NewActorIDUnchecked(actor.ID),
		Name:  actor.User.Username,
		Email: actor.User.Email,
	}, workspacesync.SourceMCP)
	if err != nil {
		return pageOutput{}, err
	}
	return pageOutput{Page: r.apiPage(restored, 0)}, nil
}

func (r *Routes) workspaceRevisionPage(ctx context.Context, pageID tree.PageID) (*tree.Page, error) {
	out, err := r.getPage.Execute(ctx, wikipages.GetPageInput{ID: pageID})
	if err != nil {
		return nil, err
	}
	return out.Page, nil
}

func unavailableWorkspaceRevisionBackend() error {
	return wikirevisions.NewRevisionNotFoundError("Revision not found", "workspace revision backend is unavailable")
}
