package mcp

import (
	"context"
	"encoding/base64"
	"os"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corerevision "github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	wikirevisions "github.com/perber/wiki/internal/wiki/revisions"
	"github.com/perber/wiki/internal/workspacesync"
)

func (r *Routes) registerRevisionTools(server *sdkmcp.Server, opts httpinternal.RouterOptions) {
	addTypedTool[listRevisionsInput, listRevisionsOutput](server, toolListRevisions, func(ctx context.Context, in listRevisionsInput) (listRevisionsOutput, error) {
		pageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
		if err != nil {
			return listRevisionsOutput{}, err
		}
		limit, err := wikirevisions.NormalizeRevisionListLimit(in.Limit, pageID)
		if err != nil {
			return listRevisionsOutput{}, err
		}
		if opts.EnableWorkspaceSync {
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
		}
		out, err := r.listRevs.Execute(ctx, wikirevisions.ListRevisionsInput{
			PageID: pageID,
			Cursor: strings.TrimSpace(in.Cursor),
			Limit:  limit,
		})
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
		pageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
		if err != nil {
			return revisionOutput{}, err
		}
		if opts.EnableWorkspaceSync {
			page, err := r.workspaceRevisionPage(ctx, pageID)
			if err != nil {
				return revisionOutput{}, err
			}
			out, err := r.listWorkspaceRevisions(ctx, page, "", 1)
			if err != nil || len(out.Revisions) == 0 {
				return revisionOutput{}, wikirevisions.NewRevisionNotFoundError("Revision not found", "revision for page %s not found", pageID)
			}
			return revisionOutput{Revision: wikirevisions.ToRevisionResponse(out.Revisions[0], r.userResolver)}, nil
		}
		out, err := r.getLatestRev.Execute(ctx, wikirevisions.GetLatestRevisionInput{PageID: pageID})
		if err != nil {
			return revisionOutput{}, err
		}
		if out.Revision == nil {
			return revisionOutput{}, wikirevisions.NewRevisionNotFoundError("Revision not found", "revision for page %s not found", pageID)
		}
		return revisionOutput{Revision: wikirevisions.ToRevisionResponse(out.Revision, r.userResolver)}, nil
	})

	addTypedTool[revisionIDInput, *wikirevisions.RevisionSnapshotResponse](server, toolGetRevision, func(ctx context.Context, in revisionIDInput) (*wikirevisions.RevisionSnapshotResponse, error) {
		rawPageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
		if err != nil {
			return nil, err
		}
		pageID, revisionID, err := wikirevisions.ValidateRevisionLookupInput(rawPageID, in.RevisionID)
		if err != nil {
			return nil, err
		}
		if opts.EnableWorkspaceSync {
			page, err := r.workspaceRevisionPage(ctx, pageID)
			if err != nil {
				return nil, err
			}
			snapshot, err := r.getWorkspaceRevision(ctx, page, revisionID)
			if err != nil {
				return nil, wikirevisions.NewRevisionNotFoundError("Revision not found", "revision %s for page %s not found", revisionID, pageID)
			}
			return wikirevisions.ToSnapshotResponse(snapshot, r.userResolver), nil
		}
		out, err := r.getRev.Execute(ctx, wikirevisions.GetRevisionInput{
			PageID:     pageID,
			RevisionID: revisionID,
		})
		if err != nil {
			return nil, err
		}
		if out.Snapshot == nil {
			return nil, wikirevisions.NewRevisionNotFoundError("Revision not found", "revision %s for page %s not found", revisionID, pageID)
		}
		return wikirevisions.ToSnapshotResponse(out.Snapshot, r.userResolver), nil
	})

	addTypedTool[compareRevisionsInput, *wikirevisions.RevisionComparisonResponse](server, toolCompareRevisions, func(ctx context.Context, in compareRevisionsInput) (*wikirevisions.RevisionComparisonResponse, error) {
		rawPageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
		if err != nil {
			return nil, err
		}
		pageID, baseRevisionID, targetRevisionID, err := wikirevisions.ValidateRevisionCompareInput(rawPageID, in.BaseRevisionID, in.TargetRevisionID)
		if err != nil {
			return nil, err
		}
		if opts.EnableWorkspaceSync {
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
		}
		out, err := r.compareRevs.Execute(ctx, wikirevisions.CompareRevisionsInput{
			PageID:           pageID,
			BaseRevisionID:   baseRevisionID,
			TargetRevisionID: targetRevisionID,
		})
		if err != nil {
			return nil, err
		}
		if out.Comparison == nil {
			return nil, wikirevisions.NewRevisionNotFoundError("Revision not found", "revision compare resource for page %s not found", pageID)
		}
		return wikirevisions.ToComparisonResponse(out.Comparison, r.userResolver), nil
	})

	addTypedTool[revisionAssetInput, assetOutput](server, toolGetRevisionAsset, func(ctx context.Context, in revisionAssetInput) (assetOutput, error) {
		rawPageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
		if err != nil {
			return assetOutput{}, err
		}
		pageID, revisionID, assetName, err := wikirevisions.ValidateRevisionAssetInput(rawPageID, in.RevisionID, in.AssetName)
		if err != nil {
			return assetOutput{}, err
		}
		if opts.EnableWorkspaceSync {
			return assetOutput{}, wikirevisions.NewRevisionNotFoundError("Revision asset not found", "workspace sync revisions do not track assets")
		}
		out, err := r.getRevAsset.Execute(ctx, wikirevisions.GetRevisionAssetInput{
			PageID:     pageID,
			RevisionID: revisionID,
			AssetName:  assetName,
		})
		if err != nil {
			return assetOutput{}, err
		}
		if out.Asset == nil {
			return assetOutput{}, wikirevisions.NewRevisionNotFoundError("Revision asset not found", "revision asset %s for page %s revision %s not found", assetName, pageID, revisionID)
		}
		data, err := os.ReadFile(out.Asset.Path)
		if err != nil {
			return assetOutput{}, wikirevisions.NewRevisionAssetBlobUnavailableError(assetName, pageID, revisionID, err)
		}
		mimeType := wikirevisions.DetectRevisionAssetMIMEType(out.Asset.Asset.Name, out.Asset.Asset.MIMEType)
		return assetOutput{
			Filename:      out.Asset.Asset.Name,
			MimeType:      mimeType,
			ContentBase64: base64.StdEncoding.EncodeToString(data),
		}, nil
	})

	addEditorTool[revisionIDInput, pageOutput](r, server, toolRestoreRevision, func(ctx context.Context, actor toolActor, in revisionIDInput) (pageOutput, error) {
		rawPageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
		if err != nil {
			return pageOutput{}, err
		}
		if opts.EnableWorkspaceSync {
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
		}
		out, err := r.restoreRev.Execute(ctx, wikirevisions.RestoreRevisionInput{
			UserID:     actor.ID,
			PageID:     rawPageID,
			RevisionID: strings.TrimSpace(in.RevisionID),
		})
		if err != nil {
			return pageOutput{}, err
		}
		return pageOutput{Page: r.apiPage(out.Page, 0)}, nil
	})
}

func (r *Routes) workspaceRevisionPage(ctx context.Context, pageID string) (*tree.Page, error) {
	out, err := r.getPage.Execute(ctx, wikipages.GetPageInput{ID: strings.TrimSpace(pageID)})
	if err != nil {
		return nil, err
	}
	return out.Page, nil
}
