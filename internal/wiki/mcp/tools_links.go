package mcp

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	wikilinks "github.com/perber/wiki/internal/wiki/links"
)

func (r *Routes) registerLinkTools(server *sdkmcp.Server) {
	addTypedTool[pageIDInput, linkStatusOutput](server, toolGetLinkStatus, func(ctx context.Context, in pageIDInput) (linkStatusOutput, error) {
		pageID, err := exactlyOneIDOrPageID(in.ID, in.PageID)
		if err != nil {
			return linkStatusOutput{}, err
		}
		out, err := r.linkStatus.Execute(ctx, wikilinks.GetLinkStatusInput{PageID: pageID})
		if err != nil {
			return linkStatusOutput{}, err
		}
		return linkStatusOutput{Status: out.Status}, nil
	})
}
