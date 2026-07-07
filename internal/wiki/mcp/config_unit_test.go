package mcp

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/shared"
	httpinternal "github.com/perber/wiki/internal/http"
)

var _ = ginkgo.Describe("MCP configuration payload", ginkgo.Label("unit"), func() {
	ginkgo.It("publishes router feature flags and frontend URL settings", func() {
		opts := httpinternal.RouterOptions{
			PublicAccess:            true,
			HideLinkMetadataSection: true,
			AuthDisabled:            true,
			BasePath:                "/wiki",
			MarkdownLinkRootPrefix:  "/docs",
			MaxAssetUploadSizeBytes: shared.MaxBytes(4096),
			EnableWorkspaceSync:     true,
			EnableLinkRefactor:      true,
			HTTPRemoteUser: httpinternal.HTTPRemoteUserConfig{
				Enabled:   true,
				LogoutURL: "https://logout.example.test",
			},
		}

		Expect(configOutputForOptions(opts)).To(matchMCPConfigOutput(mcpConfigContract{
			PublicAccess:            mcpConfigEnabled,
			LinkMetadata:            mcpConfigHidden,
			Auth:                    mcpConfigDisabled,
			BasePath:                "/wiki",
			MarkdownLinkRootPrefix:  "/docs",
			MaxAssetUploadSizeBytes: 4096,
			WorkspaceSync:           mcpConfigEnabled,
			LinkRefactor:            mcpConfigEnabled,
			HTTPRemoteUser:          mcpConfigEnabled,
			HTTPRemoteUserLogoutURL: "https://logout.example.test",
		}))
	})
})

type mcpConfigFlag uint8

const (
	mcpConfigDisabled mcpConfigFlag = iota
	mcpConfigEnabled
	mcpConfigHidden
	mcpConfigVisible
)

type mcpConfigContract struct {
	PublicAccess            mcpConfigFlag
	LinkMetadata            mcpConfigFlag
	Auth                    mcpConfigFlag
	BasePath                string
	MarkdownLinkRootPrefix  string
	MaxAssetUploadSizeBytes int64
	WorkspaceSync           mcpConfigFlag
	LinkRefactor            mcpConfigFlag
	HTTPRemoteUser          mcpConfigFlag
	HTTPRemoteUserLogoutURL string
}

func matchMCPConfigOutput(want mcpConfigContract) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(mcpConfigContractFor, Equal(want))
}

func mcpConfigContractFor(output configOutput) mcpConfigContract {
	return mcpConfigContract{
		PublicAccess:            mcpConfigFlagFor(output.PublicAccess),
		LinkMetadata:            mcpConfigLinkMetadataFlagFor(output.HideLinkMetadataSection),
		Auth:                    mcpConfigAuthFlagFor(output.AuthDisabled),
		BasePath:                output.BasePath,
		MarkdownLinkRootPrefix:  output.MarkdownLinkRootPrefix,
		MaxAssetUploadSizeBytes: output.MaxAssetUploadSizeBytes,
		WorkspaceSync:           mcpConfigFlagFor(output.EnableWorkspaceSync),
		LinkRefactor:            mcpConfigFlagFor(output.EnableLinkRefactor),
		HTTPRemoteUser:          mcpConfigFlagFor(output.HTTPRemoteUserEnabled),
		HTTPRemoteUserLogoutURL: output.HTTPRemoteUserLogoutURL,
	}
}

func mcpConfigFlagFor(enabled bool) mcpConfigFlag {
	if enabled {
		return mcpConfigEnabled
	}
	return mcpConfigDisabled
}

func mcpConfigLinkMetadataFlagFor(hidden bool) mcpConfigFlag {
	if hidden {
		return mcpConfigHidden
	}
	return mcpConfigVisible
}

func mcpConfigAuthFlagFor(disabled bool) mcpConfigFlag {
	if disabled {
		return mcpConfigDisabled
	}
	return mcpConfigEnabled
}
