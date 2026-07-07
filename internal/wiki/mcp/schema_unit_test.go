package mcp

import (
	"sort"

	"github.com/google/jsonschema-go/jsonschema"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

var _ = ginkgo.Describe("MCP schema contracts", func() {
	ginkgo.Describe("tool protocol names", ginkgo.Label("unit"), func() {
		ginkgo.It("round-trips tool IDs through their wire protocol names", func() {
			Expect(ToolRefresh.ProtocolName()).To(Equal(ToolProtocolNameFromToolID(ToolRefresh)))
			Expect(ToolProtocolNameFromWireName("wiki_refresh")).To(Equal(ToolRefresh.ProtocolName()))
			Expect(ToolDescriptionIDForTool(ToolMovePage)).To(Equal(ToolDescriptionMovePage))
		})

		ginkgo.It("publishes optional tool groups from their descriptor registries", func() {
			Expect(WorkspaceSyncToolNames()).To(Equal([]string{ToolRefresh.ProtocolName().WireName()}))
			Expect(RevisionToolNames()).To(Equal([]string{
				ToolListRevisions.ProtocolName().WireName(),
				ToolGetLatestRevision.ProtocolName().WireName(),
				ToolGetRevision.ProtocolName().WireName(),
				ToolCompareRevisions.ProtocolName().WireName(),
				ToolGetRevisionAsset.ProtocolName().WireName(),
				ToolRestoreRevision.ProtocolName().WireName(),
			}))
		})
	})

	ginkgo.Describe("tool input schemas", ginkgo.Label("unit"), func() {
		ginkgo.DescribeTable("advertises request fields and required wire keys",
			func(row toolSchemaContractRow) {
				Expect(toolInputSchema(row.Tool)).To(matchSchemaContract(row.Contract))
			},
			ginkgo.Entry("context requests expose refresh and pagination controls", toolSchemaContractRow{
				Tool: ToolGetContext,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"recentChangesLimit", "sinceToken", "syncMode", "treeDepth"},
					Required:   []string{},
				},
			}),
			ginkgo.Entry("refresh requests expose validation and source controls", toolSchemaContractRow{
				Tool: ToolRefresh,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"source", "validate"},
					Required:   []string{},
				},
			}),
			ginkgo.Entry("subtree requests expose lookup and rendering controls", toolSchemaContractRow{
				Tool: ToolGetSubtree,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"depth", "includeContentPreview", "includeLinkCounts", "includeMetadata", "pageId", "path"},
					Required:   []string{},
				},
			}),
			ginkgo.Entry("path lookup requests require a route path", toolSchemaContractRow{
				Tool: ToolLookupPath,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"kind", "path"},
					Required:   []string{"path"},
				},
			}),
			ginkgo.Entry("page validation requests accept page identity or path identity", toolSchemaContractRow{
				Tool: ToolValidatePage,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"kind", "pageId", "path"},
					Required:   []string{},
				},
			}),
			ginkgo.Entry("content validation requests require markdown path and content", toolSchemaContractRow{
				Tool: ToolValidateContent,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"content", "existingPageId", "kind", "path"},
					Required:   []string{"content", "path"},
				},
			}),
			ginkgo.Entry("wiki validation requests expose warning inclusion", toolSchemaContractRow{
				Tool: ToolValidateWiki,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"includeWarnings"},
					Required:   []string{},
				},
			}),
			ginkgo.Entry("partial metadata updates require a page version", toolSchemaContractRow{
				Tool: ToolUpdatePageMetadata,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"addTags", "includeLinkStatus", "includePage", "includeValidation", "pageId", "path", "removeProperties", "removeTags", "setProperties", "setTags", "version"},
					Required:   []string{"version"},
				},
			}),
			ginkgo.Entry("section replacements require page version, heading path, and content", toolSchemaContractRow{
				Tool: ToolReplacePageSection,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"content", "headingPath", "includeLinkStatus", "includePage", "includeValidation", "occurrence", "pageId", "path", "version"},
					Required:   []string{"content", "headingPath", "version"},
				},
			}),
			ginkgo.Entry("page creation accepts nullable parent and kind inputs", toolSchemaContractRow{
				Tool: ToolCreatePage,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"kind", "parentId", "slug", "title"},
					Required:   []string{"slug", "title"},
				},
			}),
			ginkgo.Entry("ensure page requests require a path and title", toolSchemaContractRow{
				Tool: ToolEnsurePage,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"kind", "path", "title"},
					Required:   []string{"path", "title"},
				},
			}),
			ginkgo.Entry("conversion requests require page identity version and target kind", toolSchemaContractRow{
				Tool: ToolConvertPage,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"id", "targetKind", "version"},
					Required:   []string{"id", "targetKind", "version"},
				},
			}),
			ginkgo.Entry("page lookup requests accept both legacy and semantic page identity keys", toolSchemaContractRow{
				Tool: ToolGetPage,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"id", "pageId"},
					Required:   []string{},
				},
			}),
			ginkgo.Entry("revision restoration requests require revision identity", toolSchemaContractRow{
				Tool: ToolRestoreRevision,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"id", "pageId", "revisionId"},
					Required:   []string{"revisionId"},
				},
			}),
			ginkgo.Entry("revision list requests accept cursor and limit pagination", toolSchemaContractRow{
				Tool: ToolListRevisions,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"cursor", "id", "limit", "pageId"},
					Required:   []string{},
				},
			}),
			ginkgo.Entry("revision lookup requests require revision identity", toolSchemaContractRow{
				Tool: ToolGetRevision,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"id", "pageId", "revisionId"},
					Required:   []string{"revisionId"},
				},
			}),
			ginkgo.Entry("revision comparison requests require both revision identities", toolSchemaContractRow{
				Tool: ToolCompareRevisions,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"baseRevisionId", "id", "pageId", "targetRevisionId"},
					Required:   []string{"baseRevisionId", "targetRevisionId"},
				},
			}),
			ginkgo.Entry("revision asset lookups require revision and asset identity", toolSchemaContractRow{
				Tool: ToolGetRevisionAsset,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"assetName", "id", "pageId", "revisionId"},
					Required:   []string{"revisionId", "assetName"},
				},
			}),
			ginkgo.Entry("refactor previews require explicit refactor kind", toolSchemaContractRow{
				Tool: ToolPreviewRefactor,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"content", "id", "kind", "pageId", "parentId", "slug", "title"},
					Required:   []string{"kind"},
				},
			}),
			ginkgo.Entry("refactor application requests require version and refactor kind", toolSchemaContractRow{
				Tool: ToolApplyRefactor,
				Contract: schemaContract{
					Type:       newFixtureSchemaType("object"),
					Properties: []string{"content", "id", "kind", "pageId", "parentId", "rewriteLinks", "slug", "title", "version"},
					Required:   []string{"kind", "version"},
				},
			}),
		)

		ginkgo.It("omits input schemas for tool names without request bodies", func() {
			Expect(toolInputSchema(newFixtureToolID("wiki_tool_without_request_body"))).To(BeNil())
		})
	})
})

type toolSchemaContractRow struct {
	Tool     ToolID
	Contract schemaContract
}

type schemaContract struct {
	Type                     schemaTypeFixture
	Types                    []schemaTypeFixture
	Properties               []string
	Required                 []string
	ItemType                 schemaTypeFixture
	AdditionalPropertiesType schemaTypeFixture
}

func matchSchemaContract(want schemaContract) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	want.Properties = sortedSchemaStrings(want.Properties)
	want.Required = sortedSchemaStrings(want.Required)
	want.Types = sortedSchemaTypes(want.Types)
	return WithTransform(schemaContractFromSchema, Equal(want))
}

func schemaContractFromSchema(schema *jsonschema.Schema) schemaContract {
	if schema == nil {
		return schemaContract{}
	}
	properties := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		properties = append(properties, name)
	}
	out := schemaContract{
		Type:       schemaTypeFixture(schema.Type),
		Types:      schemaTypesFromStrings(schema.Types),
		Properties: sortedSchemaStrings(properties),
		Required:   sortedSchemaStrings(schema.Required),
	}
	if schema.Items != nil {
		out.ItemType = schemaTypeFixture(schema.Items.Type)
	}
	if schema.AdditionalProperties != nil {
		out.AdditionalPropertiesType = schemaTypeFixture(schema.AdditionalProperties.Type)
	}
	return out
}

func schemaTypesFromStrings(values []string) []schemaTypeFixture {
	types := make([]schemaTypeFixture, 0, len(values))
	for _, value := range values {
		types = append(types, schemaTypeFixture(value))
	}
	return sortedSchemaTypes(types)
}

func sortedSchemaTypes(values []schemaTypeFixture) []schemaTypeFixture {
	out := append([]schemaTypeFixture{}, values...)
	sort.Slice(out, func(i, j int) bool {
		return out[i] < out[j]
	})
	return out
}

func sortedSchemaStrings(values []string) []string {
	out := append([]string{}, values...)
	sort.Strings(out)
	return out
}
