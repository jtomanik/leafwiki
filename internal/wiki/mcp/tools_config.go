package mcp

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	httpinternal "github.com/perber/wiki/internal/http"
)

func (r *Routes) registerConfigTools(server *sdkmcp.Server, opts httpinternal.RouterOptions) {
	addActorTool[emptyInput, currentUserOutput](r, server, toolGetCurrentUser, func(_ context.Context, actor toolActor, _ emptyInput) (currentUserOutput, error) {
		return currentUserOutput{User: actor.User.ToPublicUser()}, nil
	})

	addTypedTool[emptyInput, configOutput](server, toolGetConfig, func(context.Context, emptyInput) (configOutput, error) {
		return configOutputForOptions(opts), nil
	})
}

func configOutputForOptions(opts httpinternal.RouterOptions) configOutput {
	return configOutput{
		PublicAccess:            opts.PublicAccess,
		HideLinkMetadataSection: opts.HideLinkMetadataSection,
		AuthDisabled:            opts.AuthDisabled,
		BasePath:                opts.BasePath,
		MarkdownLinkRootPrefix:  opts.MarkdownLinkRootPrefix,
		MaxAssetUploadSizeBytes: opts.MaxAssetUploadSizeBytes,
		EnableRevision:          opts.EnableRevision,
		EnableWorkspaceSync:     opts.EnableWorkspaceSync,
		EnableLinkRefactor:      opts.EnableLinkRefactor,
		HTTPRemoteUserEnabled:   opts.HTTPRemoteUser.Enabled,
		HTTPRemoteUserLogoutURL: opts.HTTPRemoteUser.LogoutURL,
	}
}
