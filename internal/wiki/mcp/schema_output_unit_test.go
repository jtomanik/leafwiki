package mcp

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("MCP output schema contracts", ginkgo.Label("unit"), func() {
	ginkgo.DescribeTable("advertises result fields for tool families",
		func(row toolSchemaContractRow) {
			Expect(toolOutputSchema(row.Tool)).To(matchSchemaContract(row.Contract))
		},
		ginkgo.Entry("context responses preserve the required workspace summary contract", toolSchemaContractRow{
			Tool: ToolGetContext,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"activeSessions", "canonicalLinkExamples", "changesSincePreviousContext", "config", "contextHistory", "contextToken", "presenceStatus", "previousContextToken", "recentChanges", "recommendedTools", "server", "syncStatus", "tree", "user", "validation", "warnings"},
				Required:   []string{"activeSessions", "canonicalLinkExamples", "changesSincePreviousContext", "config", "contextHistory", "contextToken", "presenceStatus", "previousContextToken", "recentChanges", "recommendedTools", "server", "syncStatus", "tree", "user", "validation"},
			},
		}),
		ginkgo.Entry("refresh responses expose sync status and changed paths", toolSchemaContractRow{
			Tool: ToolRefresh,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"lastCommitHash", "recentChangedPaths", "syncStatus", "validation"},
				Required:   []string{"lastCommitHash", "recentChangedPaths", "syncStatus"},
			},
		}),
		ginkgo.Entry("subtree responses expose root breadcrumbs depth and truncation", toolSchemaContractRow{
			Tool: ToolGetSubtree,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"breadcrumbs", "depth", "root", "truncated"},
				Required:   []string{"breadcrumbs", "depth", "root", "truncated"},
			},
		}),
		ginkgo.Entry("validation responses expose summary and issue details", toolSchemaContractRow{
			Tool: ToolValidateWiki,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"issues", "ok", "summary"},
				Required:   []string{"issues", "ok", "summary"},
			},
		}),
		ginkgo.Entry("current user responses expose the public user payload", toolSchemaContractRow{
			Tool: ToolGetCurrentUser,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"user"},
				Required:   []string{"user"},
			},
		}),
		ginkgo.Entry("configuration responses expose server feature toggles", toolSchemaContractRow{
			Tool: ToolGetConfig,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"authDisabled", "basePath", "enableLinkRefactor", "enableWorkspaceSync", "hideLinkMetadataSection", "httpRemoteUserEnabled", "httpRemoteUserLogoutUrl", "markdownLinkRootPrefix", "maxAssetUploadSizeBytes", "publicAccess"},
				Required:   []string{"authDisabled", "basePath", "enableLinkRefactor", "enableWorkspaceSync", "hideLinkMetadataSection", "httpRemoteUserEnabled", "httpRemoteUserLogoutUrl", "markdownLinkRootPrefix", "maxAssetUploadSizeBytes", "publicAccess"},
			},
		}),
		ginkgo.Entry("partial metadata responses expose validation page and link status", toolSchemaContractRow{
			Tool: ToolUpdatePageMetadata,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"linkStatus", "page", "pageId", "path", "title", "validation", "version"},
				Required:   []string{"pageId", "path", "title", "version"},
			},
		}),
		ginkgo.Entry("tree responses expose the tree payload", toolSchemaContractRow{
			Tool: ToolGetTree,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"tree"},
				Required:   []string{"tree"},
			},
		}),
		ginkgo.Entry("page responses expose page and link status", toolSchemaContractRow{
			Tool: ToolGetPage,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"linkStatus", "page"},
				Required:   []string{"linkStatus", "page"},
			},
		}),
		ginkgo.Entry("path-addressed page responses expose page and link status", toolSchemaContractRow{
			Tool: ToolGetPageByPath,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"linkStatus", "page"},
				Required:   []string{"linkStatus", "page"},
			},
		}),
		ginkgo.Entry("mutation responses expose the saved page", toolSchemaContractRow{
			Tool: ToolCreatePage,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"page"},
				Required:   []string{"page"},
			},
		}),
		ginkgo.Entry("copy responses expose the saved page", toolSchemaContractRow{
			Tool: ToolCopyPage,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"page"},
				Required:   []string{"page"},
			},
		}),
		ginkgo.Entry("ensure responses expose the saved page", toolSchemaContractRow{
			Tool: ToolEnsurePage,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"page"},
				Required:   []string{"page"},
			},
		}),
		ginkgo.Entry("restore responses expose the saved page", toolSchemaContractRow{
			Tool: ToolRestoreRevision,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"page"},
				Required:   []string{"page"},
			},
		}),
		ginkgo.Entry("lookup responses expose path lookup details", toolSchemaContractRow{
			Tool: ToolLookupPath,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"lookup"},
				Required:   []string{"lookup"},
			},
		}),
		ginkgo.Entry("permalink responses expose resolved targets", toolSchemaContractRow{
			Tool: ToolResolvePermalink,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"target"},
				Required:   []string{"target"},
			},
		}),
		ginkgo.Entry("slug suggestion responses expose the slug", toolSchemaContractRow{
			Tool: ToolSuggestSlug,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"slug"},
				Required:   []string{"slug"},
			},
		}),
		ginkgo.Entry("message-only responses expose localized message identity", toolSchemaContractRow{
			Tool: ToolDeletePage,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"message", "messageId"},
				Required:   []string{"message", "messageId"},
			},
		}),
		ginkgo.Entry("move responses expose localized message identity", toolSchemaContractRow{
			Tool: ToolMovePage,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"message", "messageId"},
				Required:   []string{"message", "messageId"},
			},
		}),
		ginkgo.Entry("sort responses expose localized message identity", toolSchemaContractRow{
			Tool: ToolSortPages,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"message", "messageId"},
				Required:   []string{"message", "messageId"},
			},
		}),
		ginkgo.Entry("conversion responses expose localized message identity", toolSchemaContractRow{
			Tool: ToolConvertPage,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"message", "messageId"},
				Required:   []string{"message", "messageId"},
			},
		}),
		ginkgo.Entry("search responses expose pagination and facet metadata", toolSchemaContractRow{
			Tool: ToolSearchPages,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"count", "hasMore", "items", "limit", "offset", "tagFacets"},
				Required:   []string{"count", "hasMore", "items", "limit", "offset", "tagFacets"},
			},
		}),
		ginkgo.Entry("search status responses expose nullable status details", toolSchemaContractRow{
			Tool: ToolGetSearchStatus,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"status"},
				Required:   []string{"status"},
			},
		}),
		ginkgo.Entry("tag list responses expose tag arrays", toolSchemaContractRow{
			Tool: ToolListTags,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"tags"},
				Required:   []string{"tags"},
			},
		}),
		ginkgo.Entry("tag and property lookup responses expose page arrays", toolSchemaContractRow{
			Tool: ToolGetPagesByTags,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"pages"},
				Required:   []string{"pages"},
			},
		}),
		ginkgo.Entry("property-filtered page responses expose page arrays", toolSchemaContractRow{
			Tool: ToolGetPagesByProperty,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"pages"},
				Required:   []string{"pages"},
			},
		}),
		ginkgo.Entry("property key responses expose key arrays", toolSchemaContractRow{
			Tool: ToolListPropertyKeys,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"keys"},
				Required:   []string{"keys"},
			},
		}),
		ginkgo.Entry("link status responses expose link status details", toolSchemaContractRow{
			Tool: ToolGetLinkStatus,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"status"},
				Required:   []string{"status"},
			},
		}),
		ginkgo.Entry("asset upload responses expose uploaded file URLs", toolSchemaContractRow{
			Tool: ToolUploadAsset,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"file"},
				Required:   []string{"file"},
			},
		}),
		ginkgo.Entry("asset binary responses expose encoded content metadata", toolSchemaContractRow{
			Tool: ToolGetAsset,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"contentBase64", "filename", "mimeType"},
				Required:   []string{"contentBase64", "filename", "mimeType"},
			},
		}),
		ginkgo.Entry("revision asset binary responses expose encoded content metadata", toolSchemaContractRow{
			Tool: ToolGetRevisionAsset,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"contentBase64", "filename", "mimeType"},
				Required:   []string{"contentBase64", "filename", "mimeType"},
			},
		}),
		ginkgo.Entry("asset listing responses expose asset file arrays", toolSchemaContractRow{
			Tool: ToolListAssets,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"files"},
				Required:   []string{"files"},
			},
		}),
		ginkgo.Entry("asset rename responses expose the new URL", toolSchemaContractRow{
			Tool: ToolRenameAsset,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"url"},
				Required:   []string{"url"},
			},
		}),
		ginkgo.Entry("asset delete responses expose localized message identity", toolSchemaContractRow{
			Tool: ToolDeleteAsset,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"message", "messageId"},
				Required:   []string{"message", "messageId"},
			},
		}),
		ginkgo.Entry("revision list responses expose pagination and revisions", toolSchemaContractRow{
			Tool: ToolListRevisions,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"nextCursor", "revisions"},
				Required:   []string{"nextCursor", "revisions"},
			},
		}),
		ginkgo.Entry("latest revision responses expose revision details", toolSchemaContractRow{
			Tool: ToolGetLatestRevision,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"revision"},
				Required:   []string{"revision"},
			},
		}),
		ginkgo.Entry("revision responses expose content and assets", toolSchemaContractRow{
			Tool: ToolGetRevision,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"assets", "content", "revision"},
				Required:   []string{"assets", "content", "revision"},
			},
		}),
		ginkgo.Entry("revision comparisons expose both snapshots and changed-asset metadata", toolSchemaContractRow{
			Tool: ToolCompareRevisions,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"assetChanges", "base", "contentChanged", "target"},
				Required:   []string{"assetChanges", "base", "contentChanged", "target"},
			},
		}),
		ginkgo.Entry("refactor previews expose path movement and warning metadata", toolSchemaContractRow{
			Tool: ToolPreviewRefactor,
			Contract: schemaContract{
				Type:       newFixtureSchemaType("object"),
				Properties: []string{"affectedPages", "counts", "kind", "newPath", "oldPath", "pageId", "warnings"},
				Required:   []string{"affectedPages", "counts", "kind", "newPath", "oldPath", "pageId", "warnings"},
			},
		}),
	)

	ginkgo.Describe("primitive schema vocabulary", ginkgo.Label("unit"), func() {
		ginkgo.It("uses JSON schema primitives for nullable, list, and map values", func() {
			Expect(nullableStringSchema()).To(matchSchemaContract(schemaContract{Types: []schemaTypeFixture{newFixtureSchemaType("string"), newFixtureSchemaType("null")}}))
			Expect(integerSchema()).To(matchSchemaContract(schemaContract{Type: newFixtureSchemaType("integer")}))
			Expect(booleanSchema()).To(matchSchemaContract(schemaContract{Type: newFixtureSchemaType("boolean")}))
			Expect(objectValueSchema()).To(matchSchemaContract(schemaContract{Type: newFixtureSchemaType("object")}))
			Expect(nullableObjectSchema()).To(matchSchemaContract(schemaContract{Types: []schemaTypeFixture{newFixtureSchemaType("object"), newFixtureSchemaType("null")}}))
			Expect(arrayValueSchema()).To(matchSchemaContract(schemaContract{Type: newFixtureSchemaType("array")}))
			Expect(stringArraySchema()).To(matchSchemaContract(schemaContract{
				Type:     newFixtureSchemaType("array"),
				ItemType: newFixtureSchemaType("string"),
			}))
			Expect(stringMapSchema()).To(matchSchemaContract(schemaContract{
				Type:                     newFixtureSchemaType("object"),
				AdditionalPropertiesType: newFixtureSchemaType("string"),
			}))
		})
	})
})
