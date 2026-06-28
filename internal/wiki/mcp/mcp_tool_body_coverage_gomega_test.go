package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coreauth "github.com/perber/wiki/internal/core/auth"
	corerevision "github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/http/dto"
	corelinks "github.com/perber/wiki/internal/links"
	coreprop "github.com/perber/wiki/internal/properties"
	coresearch "github.com/perber/wiki/internal/search"
	coretags "github.com/perber/wiki/internal/tags"
	wikilinks "github.com/perber/wiki/internal/wiki/links"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
	wikiproperties "github.com/perber/wiki/internal/wiki/properties"
	wikisearch "github.com/perber/wiki/internal/wiki/search"
	wikitags "github.com/perber/wiki/internal/wiki/tags"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = Describe("MCP extracted tool bodies", func() {
	It("covers actor, context, page, partial-edit, and validation tool body branches", func() {
		backendErr := errors.New("tool failed")
		_, err := callActorTool[emptyInput, currentUserOutput](&Routes{}, context.Background(), nil, emptyInput{}, func(context.Context, toolActor, emptyInput) (currentUserOutput, error) {
			return currentUserOutput{}, nil
		})
		Expect(err).To(HaveOccurred())
		actorOut, err := callActorTool[emptyInput, currentUserOutput](&Routes{authDisabled: true}, context.Background(), nil, emptyInput{}, func(_ context.Context, actor toolActor, _ emptyInput) (currentUserOutput, error) {
			return currentUserOutput{User: actor.User.ToPublicUser()}, nil
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(actorOut.User.ID).NotTo(BeEmpty())

		_, err = (&Routes{}).getContextTool(context.Background(), nil, httpinternal.RouterOptions{}, getContextInput{})
		Expect(err).To(HaveOccurred())

		unloadedTree := tree.NewTreeServiceWithOptions(tree.TreeOptions{DataDir: GinkgoT().TempDir(), RootDir: GinkgoT().TempDir()})
		Expect((&Routes{treeService: unloadedTree}).getTreeTool(getTreeInput{}).Tree).To(BeNil())
		Expect(os.WriteFile(filepath.Join(unloadedTree.RootDir(), "README.md"), []byte("# Root\n"), 0o644)).To(Succeed())
		_, err = (&Routes{treeService: unloadedTree}).findToolPageByInputPath(context.Background(), "README.md", string(tree.NodeKindSection))
		Expect(err).To(HaveOccurred())

		routes := newContextToolTestRoutes(GinkgoT())
		page, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		routes.lookupPath = fakeMCPLookupPathUseCase{err: backendErr}
		_, err = routes.lookupPathTool(context.Background(), pathInput{Path: "home"})
		Expect(err).To(MatchError(backendErr))
		routes.lookupPath = fakeMCPLookupPathUseCase{out: &wikipages.LookupPagePathOutput{}}
		_, err = routes.lookupPathTool(context.Background(), pathInput{Path: "home"})
		Expect(err).NotTo(HaveOccurred())

		routes.ensurePath = fakeMCPEnsurePathUseCase{err: backendErr}
		pageKind := string(tree.NodeKindPage)
		_, err = routes.ensurePageTool(context.Background(), toolActor{ID: "user-1"}, ensurePageInput{Path: "new-page", Title: "New", Kind: &pageKind})
		Expect(err).To(MatchError(backendErr))
		routes.ensurePath = fakeMCPEnsurePathUseCase{out: &wikipages.EnsurePathOutput{Page: page}}
		_, err = routes.ensurePageTool(context.Background(), toolActor{ID: "user-1"}, ensurePageInput{Path: "new-page", Title: "New", Kind: &pageKind})
		Expect(err).NotTo(HaveOccurred())

		routes.updatePage = fakeMCPUpdatePageUseCase{err: backendErr}
		_, err = routes.updatePageTool(context.Background(), toolActor{ID: "user-1"}, updatePageInput{ID: page.ID.String(), TagsPresent: true, Tags: []string{""}})
		Expect(err).To(HaveOccurred())
		content := "updated"
		_, err = routes.updatePageTool(context.Background(), toolActor{ID: "user-1"}, updatePageInput{ID: "missing", Content: &content})
		Expect(err).To(HaveOccurred())

		relPath, err := routes.treeService.ContentPathForNode(page.PageNode)
		Expect(err).NotTo(HaveOccurred())
		absPath := filepath.Join(routes.treeService.RootDir(), filepath.FromSlash(relPath))
		originalRaw, err := os.ReadFile(absPath)
		Expect(err).NotTo(HaveOccurred())
		routes.findByPath = fakeMCPFindByPathUseCase{out: &wikipages.FindByPathOutput{Page: page}}
		Expect(os.Remove(absPath)).To(Succeed())
		_, err = routes.updatePageMetadataTool(context.Background(), toolActor{ID: "user-1"}, updatePageMetadataInput{Path: "home", Version: page.Version().String()})
		Expect(err).To(HaveOccurred())
		Expect(os.WriteFile(absPath, originalRaw, 0o644)).To(Succeed())
		Expect(os.WriteFile(absPath, []byte("<!-- leafwiki\n: bad\n-->\nBody"), 0o644)).To(Succeed())
		_, err = routes.updatePageMetadataTool(context.Background(), toolActor{ID: "user-1"}, updatePageMetadataInput{Path: "home", Version: page.Version().String()})
		Expect(err).To(HaveOccurred())
		Expect(os.WriteFile(absPath, originalRaw, 0o644)).To(Succeed())
		_, err = routes.updatePageMetadataTool(context.Background(), toolActor{ID: "user-1"}, updatePageMetadataInput{Path: "home", Version: page.Version().String(), AddTags: []string{""}})
		Expect(err).To(HaveOccurred())
		originalBuildMarkdownWithPublicMetadataPatch := buildMarkdownWithPublicMetadataPatch
		buildMarkdownWithPublicMetadataPatch = func(string, tree.PageID, string, wikipages.PublicMetadataPatch, string) (string, error) {
			return "", backendErr
		}
		DeferCleanup(func() {
			buildMarkdownWithPublicMetadataPatch = originalBuildMarkdownWithPublicMetadataPatch
		})
		builderContent := "Body"
		_, err = routes.updatePageTool(context.Background(), toolActor{ID: "user-1"}, updatePageInput{ID: page.ID.String(), Title: page.Title, Slug: page.Slug.String(), Content: &builderContent})
		Expect(err).To(MatchError(backendErr))
		_, err = routes.updatePageMetadataTool(context.Background(), toolActor{ID: "user-1"}, updatePageMetadataInput{Path: "home", Version: page.Version().String(), AddTags: []string{"tag"}})
		Expect(err).To(MatchError(backendErr))
		buildMarkdownWithPublicMetadataPatch = originalBuildMarkdownWithPublicMetadataPatch
		routes.findByPath = fakeMCPFindByPathUseCase{out: &wikipages.FindByPathOutput{Page: page}}
		Expect(os.WriteFile(absPath, []byte("<!-- leafwiki extra\nversion: 1\npage:\n  id: page-123\n-->\nBody"), 0o644)).To(Succeed())
		_, err = routes.updatePageTool(context.Background(), toolActor{ID: "user-1"}, updatePageInput{ID: page.ID.String(), TagsPresent: true, Tags: []string{"tag"}})
		Expect(err).To(HaveOccurred())
		Expect(os.WriteFile(absPath, originalRaw, 0o644)).To(Succeed())

		routes.updatePage = fakeMCPUpdatePageUseCase{out: &wikipages.UpdatePageOutput{Page: page}}
		_, err = routes.updatePageMetadataTool(context.Background(), toolActor{ID: "user-1"}, updatePageMetadataInput{})
		Expect(err).To(HaveOccurred())
		_, err = routes.updatePageMetadataTool(context.Background(), toolActor{ID: "user-1"}, updatePageMetadataInput{PageID: page.ID.String(), Version: "stale"})
		Expect(err).To(HaveOccurred())
		_, err = routes.updatePageMetadataTool(context.Background(), toolActor{ID: "user-1"}, updatePageMetadataInput{PageID: page.ID.String(), Version: page.Version().String(), AddTags: []string{"tag"}})
		Expect(err).NotTo(HaveOccurred())

		routes.updatePage = fakeMCPUpdatePageUseCase{err: backendErr}
		_, err = routes.updatePageMetadataTool(context.Background(), toolActor{ID: "user-1"}, updatePageMetadataInput{PageID: page.ID.String(), Version: page.Version().String(), AddTags: []string{"tag"}})
		Expect(err).To(MatchError(backendErr))
		_, err = routes.replacePageSectionTool(context.Background(), toolActor{ID: "user-1"}, replacePageSectionInput{})
		Expect(err).To(HaveOccurred())
		_, err = routes.replacePageSectionTool(context.Background(), toolActor{ID: "user-1"}, replacePageSectionInput{PageID: page.ID.String(), Version: page.Version().String(), HeadingPath: []string{"Missing"}, Content: "replacement"})
		Expect(err).To(HaveOccurred())
		sectionContent := "# Heading\nold"
		Expect(routes.treeService.UpdateNodeUncheckedVersion("system", page.ID, page.Title, page.Slug, &sectionContent, false)).To(Succeed())
		page, err = routes.treeService.GetPage(page.ID)
		Expect(err).NotTo(HaveOccurred())
		routes.updatePage = fakeMCPUpdatePageUseCase{out: &wikipages.UpdatePageOutput{Page: page}}
		_, err = routes.replacePageSectionTool(context.Background(), toolActor{ID: "user-1"}, replacePageSectionInput{PageID: page.ID.String(), Version: page.Version().String(), HeadingPath: []string{"Heading"}, Content: "replacement"})
		Expect(err).NotTo(HaveOccurred())
		routes.updatePage = fakeMCPUpdatePageUseCase{err: backendErr}
		_, err = routes.replacePageSectionTool(context.Background(), toolActor{ID: "user-1"}, replacePageSectionInput{PageID: page.ID.String(), Version: page.Version().String(), HeadingPath: []string{"Heading"}, Content: "replacement"})
		Expect(err).To(MatchError(backendErr))

		_, err = routes.validatePageTool(context.Background(), validatePageInput{})
		Expect(err).To(HaveOccurred())
		Expect(os.Remove(absPath)).To(Succeed())
		_ = routes.validateLoadedTree(context.Background())
		routes.findByPath = fakeMCPFindByPathUseCase{out: &wikipages.FindByPathOutput{Page: page}}
		_, err = routes.validatePageTool(context.Background(), validatePageInput{Path: "home"})
		Expect(err).To(HaveOccurred())
		Expect(os.WriteFile(absPath, originalRaw, 0o644)).To(Succeed())
		out, err := routes.validatePageTool(context.Background(), validatePageInput{PageID: page.ID.String()})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Summary.Errors).To(BeZero())

		routes.workspaceRootDir = ""
		routes.workspaceSyncStatus = func() workspacesync.SyncStatus {
			return workspacesync.SyncStatus{ValidationErrors: []workspacesync.ValidationError{{Path: "home.md", Message: "broken", Severity: "error"}}}
		}
		wikiOut, err := routes.validateWikiTool(context.Background(), validateWikiInput{})
		Expect(err).NotTo(HaveOccurred())
		Expect(wikiOut.OK).To(BeFalse())
	})

	It("covers tag and property tool body success and error branches", func() {
		backendErr := errors.New("backend failed")
		routes := &Routes{
			getTags:      fakeMCPGetTagsUseCase{err: backendErr},
			pagesByTags:  fakeMCPPagesByTagsUseCase{err: backendErr},
			propertyKeys: fakeMCPPropertyKeysUseCase{err: backendErr},
			pagesByProp:  fakeMCPPagesByPropertyUseCase{err: backendErr},
		}

		_, err := routes.listTagsTool(context.Background(), listTagsInput{Query: "tag"})
		Expect(err).To(MatchError(backendErr))
		_, err = routes.pagesByTagsTool(context.Background(), pagesByTagsInput{})
		Expect(err).To(HaveOccurred())
		_, err = routes.pagesByTagsTool(context.Background(), pagesByTagsInput{Tags: []string{"tag"}})
		Expect(err).To(MatchError(backendErr))
		_, err = routes.listPropertyKeysTool(context.Background(), listPropertyKeysInput{Query: "status"})
		Expect(err).To(MatchError(backendErr))
		_, err = routes.pagesByPropertyTool(context.Background(), pagesByPropertyInput{Key: "status", Value: "draft"})
		Expect(err).To(MatchError(backendErr))

		routes.getTags = fakeMCPGetTagsUseCase{out: &wikitags.GetTagsOutput{Tags: []coretags.TagCount{{Tag: "tag", Count: 2}}}}
		routes.pagesByTags = fakeMCPPagesByTagsUseCase{out: &wikitags.GetPagesByTagsOutput{Pages: []*dto.TaggedPage{{ID: "page-1"}}}}
		routes.propertyKeys = fakeMCPPropertyKeysUseCase{out: &wikiproperties.GetPropertyKeysOutput{Keys: []coreprop.PropertyKeyCount{{Key: "status", Count: 1}}}}
		routes.pagesByProp = fakeMCPPagesByPropertyUseCase{out: &wikiproperties.GetPagesByPropertyOutput{Pages: []*dto.PropertyPage{{ID: "page-1"}}}}

		tags, err := routes.listTagsTool(context.Background(), listTagsInput{Query: "tag"})
		Expect(err).NotTo(HaveOccurred())
		Expect(tags.Tags).To(HaveLen(1))
		pages, err := routes.pagesByTagsTool(context.Background(), pagesByTagsInput{Tags: []string{"tag"}})
		Expect(err).NotTo(HaveOccurred())
		Expect(pages.Pages).To(HaveLen(1))
		keys, err := routes.listPropertyKeysTool(context.Background(), listPropertyKeysInput{Query: "status"})
		Expect(err).NotTo(HaveOccurred())
		Expect(keys.Keys).To(HaveLen(1))
		pages, err = routes.pagesByPropertyTool(context.Background(), pagesByPropertyInput{Key: "status", Value: "draft"})
		Expect(err).NotTo(HaveOccurred())
		Expect(pages.Pages).To(HaveLen(1))
	})

	It("covers search tool body validation, backend, and pagination branches", func() {
		backendErr := errors.New("search failed")
		routes := &Routes{search: fakeMCPSearchUseCase{err: backendErr}}

		_, err := routes.searchPagesTool(context.Background(), searchPagesInput{})
		Expect(err).To(HaveOccurred())
		_, err = routes.searchPagesTool(context.Background(), searchPagesInput{Query: "needle"})
		Expect(err).To(MatchError(backendErr))

		routes.search = fakeMCPSearchUseCase{out: &wikisearch.SearchOutput{Result: &coresearch.SearchResult{
			Count:    2,
			Items:    []coresearch.SearchResultItem{{PageID: "page-1"}},
			StartAt:  0,
			PageSize: 20,
		}}}
		out, err := routes.searchPagesTool(context.Background(), searchPagesInput{Query: "needle"})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Limit).To(Equal(20))
		Expect(out.HasMore).To(BeTrue())
	})

	It("covers refactor tool body validation, backend, and success branches", func() {
		backendErr := errors.New("refactor failed")
		routes := &Routes{
			previewRef: fakeMCPPreviewRefactorUseCase{err: backendErr},
			applyRef:   fakeMCPApplyRefactorUseCase{err: backendErr},
		}

		_, err := routes.previewRefactorTool(context.Background(), previewRefactorInput{})
		Expect(err).To(HaveOccurred())
		_, err = routes.previewRefactorTool(context.Background(), previewRefactorInput{PageID: "page-1"})
		Expect(err).To(MatchError(backendErr))
		_, err = routes.applyRefactorTool(context.Background(), toolActor{ID: "user-1"}, applyRefactorInput{})
		Expect(err).To(HaveOccurred())
		_, err = routes.applyRefactorTool(context.Background(), toolActor{ID: "user-1"}, applyRefactorInput{PageID: "page-1"})
		Expect(err).To(MatchError(backendErr))

		routes = newContextToolTestRoutes(GinkgoT())
		page, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		routes.previewRef = fakeMCPPreviewRefactorUseCase{out: &wikipages.RefactorPreview{}}
		routes.applyRef = fakeMCPApplyRefactorUseCase{out: page}
		preview, err := routes.previewRefactorTool(context.Background(), previewRefactorInput{PageID: page.ID.String()})
		Expect(err).NotTo(HaveOccurred())
		Expect(preview).NotTo(BeNil())
		applied, err := routes.applyRefactorTool(context.Background(), toolActor{ID: "user-1"}, applyRefactorInput{PageID: page.ID.String()})
		Expect(err).NotTo(HaveOccurred())
		Expect(applied.Page.ID).To(Equal(page.ID.String()))
	})

	It("covers revision tool body validation, backend, and success branches", func() {
		backendErr := errors.New("revision failed")
		actor := toolActor{ID: "user-1", User: &coreauth.User{ID: "user-1", Username: "user", Email: "user@example.com"}}
		routes := &Routes{}
		_, err := routes.listRevisionsTool(context.Background(), listRevisionsInput{PageID: "page-1"})
		Expect(err).To(HaveOccurred())
		_, err = routes.latestRevisionTool(context.Background(), pageIDInput{PageID: "page-1"})
		Expect(err).To(HaveOccurred())
		_, err = routes.getRevisionTool(context.Background(), revisionIDInput{PageID: "page-1", RevisionID: "rev-1"})
		Expect(err).To(HaveOccurred())
		_, err = routes.compareRevisionsTool(context.Background(), compareRevisionsInput{PageID: "page-1", BaseRevisionID: "base", TargetRevisionID: "target"})
		Expect(err).To(HaveOccurred())
		_, err = routes.restoreRevisionTool(context.Background(), actor, revisionIDInput{PageID: "page-1", RevisionID: "rev-1"})
		Expect(err).To(HaveOccurred())

		routes.listWorkspaceRevisions = func(context.Context, *tree.Page, string, workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
			return workspacesync.PageRevisionList{}, backendErr
		}
		routes.getWorkspaceRevision = func(context.Context, *tree.Page, tree.RevisionID) (*corerevision.RevisionSnapshot, error) {
			return nil, backendErr
		}
		routes.restoreWorkspaceRevision = func(context.Context, *tree.Page, tree.RevisionID, workspacesync.Actor, workspacesync.Source) (*tree.Page, error) {
			return nil, backendErr
		}
		routes.getPage = fakeMCPGetPageUseCase{err: backendErr}

		_, err = routes.listRevisionsTool(context.Background(), listRevisionsInput{})
		Expect(err).To(HaveOccurred())
		_, err = routes.listRevisionsTool(context.Background(), listRevisionsInput{PageID: "page-1"})
		Expect(err).To(MatchError(backendErr))
		_, err = routes.latestRevisionTool(context.Background(), pageIDInput{})
		Expect(err).To(HaveOccurred())
		_, err = routes.latestRevisionTool(context.Background(), pageIDInput{PageID: "page-1"})
		Expect(err).To(MatchError(backendErr))
		_, err = routes.getRevisionTool(context.Background(), revisionIDInput{})
		Expect(err).To(HaveOccurred())
		_, err = routes.getRevisionTool(context.Background(), revisionIDInput{PageID: "page-1", RevisionID: ""})
		Expect(err).To(HaveOccurred())
		_, err = routes.getRevisionTool(context.Background(), revisionIDInput{PageID: "page-1", RevisionID: "rev-1"})
		Expect(err).To(MatchError(backendErr))
		_, err = routes.compareRevisionsTool(context.Background(), compareRevisionsInput{})
		Expect(err).To(HaveOccurred())
		_, err = routes.compareRevisionsTool(context.Background(), compareRevisionsInput{PageID: "page-1", BaseRevisionID: "", TargetRevisionID: "target"})
		Expect(err).To(HaveOccurred())
		_, err = routes.compareRevisionsTool(context.Background(), compareRevisionsInput{PageID: "page-1", BaseRevisionID: "base", TargetRevisionID: "target"})
		Expect(err).To(MatchError(backendErr))
		_, err = revisionAssetTool(revisionAssetInput{})
		Expect(err).To(HaveOccurred())
		_, err = revisionAssetTool(revisionAssetInput{PageID: "page-1", RevisionID: "", AssetName: "logo.png"})
		Expect(err).To(HaveOccurred())
		_, err = revisionAssetTool(revisionAssetInput{PageID: "page-1", RevisionID: "rev-1", AssetName: "logo.png"})
		Expect(err).To(HaveOccurred())
		_, err = routes.restoreRevisionTool(context.Background(), actor, revisionIDInput{})
		Expect(err).To(HaveOccurred())
		_, err = routes.restoreRevisionTool(context.Background(), actor, revisionIDInput{PageID: "page-1", RevisionID: ""})
		Expect(err).To(HaveOccurred())
		_, err = routes.restoreRevisionTool(context.Background(), actor, revisionIDInput{PageID: "page-1", RevisionID: "rev-1"})
		Expect(err).To(MatchError(backendErr))

		pageForBackendErrors := &tree.Page{PageNode: &tree.PageNode{ID: "page-1", Title: "Page", Slug: "page", Kind: tree.NodeKindPage}}
		routes.getPage = fakeMCPGetPageUseCase{out: &wikipages.GetPageOutput{Page: pageForBackendErrors}}
		_, err = routes.listRevisionsTool(context.Background(), listRevisionsInput{PageID: "page-1"})
		Expect(err).To(MatchError(backendErr))
		_, err = routes.latestRevisionTool(context.Background(), pageIDInput{PageID: "page-1"})
		Expect(err).To(HaveOccurred())
		_, err = routes.getRevisionTool(context.Background(), revisionIDInput{PageID: "page-1", RevisionID: "rev-1"})
		Expect(err).To(HaveOccurred())
		_, err = routes.compareRevisionsTool(context.Background(), compareRevisionsInput{PageID: "page-1", BaseRevisionID: "base", TargetRevisionID: "target"})
		Expect(err).To(HaveOccurred())
		_, err = routes.restoreRevisionTool(context.Background(), actor, revisionIDInput{PageID: "page-1", RevisionID: "rev-1"})
		Expect(err).To(MatchError(backendErr))

		routes = newContextToolTestRoutes(GinkgoT())
		page, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		rev := mcpTestRevision(page.ID, "rev-1")
		routes.getPage = fakeMCPGetPageUseCase{out: &wikipages.GetPageOutput{Page: page}}
		routes.listWorkspaceRevisions = func(context.Context, *tree.Page, string, workspacesync.PageRevisionLimit) (workspacesync.PageRevisionList, error) {
			return workspacesync.PageRevisionList{Revisions: []*corerevision.Revision{rev}, NextCursor: "next"}, nil
		}
		snapshots := map[string]*corerevision.RevisionSnapshot{
			"base":   {Revision: mcpTestRevision(page.ID, "base"), Content: "base"},
			"target": {Revision: mcpTestRevision(page.ID, "target"), Content: "target"},
			"rev-1":  {Revision: rev, Content: "content"},
		}
		routes.getWorkspaceRevision = func(_ context.Context, _ *tree.Page, id tree.RevisionID) (*corerevision.RevisionSnapshot, error) {
			if snapshot := snapshots[id.String()]; snapshot != nil {
				return snapshot, nil
			}
			return nil, backendErr
		}
		routes.restoreWorkspaceRevision = func(context.Context, *tree.Page, tree.RevisionID, workspacesync.Actor, workspacesync.Source) (*tree.Page, error) {
			return page, nil
		}

		listed, err := routes.listRevisionsTool(context.Background(), listRevisionsInput{PageID: page.ID.String(), Cursor: " cursor "})
		Expect(err).NotTo(HaveOccurred())
		Expect(listed.Revisions).To(HaveLen(1))
		Expect(listed.NextCursor).To(Equal("next"))
		latest, err := routes.latestRevisionTool(context.Background(), pageIDInput{PageID: page.ID.String()})
		Expect(err).NotTo(HaveOccurred())
		Expect(latest.Revision).NotTo(BeNil())
		gotRevision, err := routes.getRevisionTool(context.Background(), revisionIDInput{PageID: page.ID.String(), RevisionID: "rev-1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(gotRevision).NotTo(BeNil())
		compared, err := routes.compareRevisionsTool(context.Background(), compareRevisionsInput{PageID: page.ID.String(), BaseRevisionID: "base", TargetRevisionID: "target"})
		Expect(err).NotTo(HaveOccurred())
		Expect(compared).NotTo(BeNil())
		restored, err := routes.restoreRevisionTool(context.Background(), actor, revisionIDInput{PageID: page.ID.String(), RevisionID: "rev-1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(restored.Page.ID).To(Equal(page.ID.String()))
	})

	It("covers link-status dependent helper branches", func() {
		backendErr := errors.New("links failed")
		routes := newContextToolTestRoutes(GinkgoT())
		page, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		routes.linkStatus = fakeMCPLinkStatusUseCase{err: backendErr}

		Expect(routes.subtreeLinkCounts(context.Background(), page.PageNode)).To(BeNil())
		_, err = routes.pageOutputWithLinkStatus(context.Background(), page, 0)
		Expect(err).To(MatchError(backendErr))
		includeValidation := false
		_, err = routes.partialEditOutput(context.Background(), page, false, &includeValidation, true)
		Expect(err).To(MatchError(backendErr))

		routes.linkStatus = fakeMCPLinkStatusUseCase{out: &wikilinks.GetLinkStatusOutput{Status: &corelinks.LinkStatusResult{Counts: corelinks.LinkStatusCounts{Outgoings: 1}}}}
		Expect(routes.subtreeLinkCounts(context.Background(), page.PageNode)).To(Equal(corelinks.LinkStatusCounts{Outgoings: 1}))
		withLinks, err := routes.partialEditOutput(context.Background(), page, true, &includeValidation, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(withLinks.Page).NotTo(BeNil())
		Expect(withLinks.LinkStatus).NotTo(BeNil())
	})
})

func mcpTestRevision(pageID tree.PageID, id string) *corerevision.Revision {
	GinkgoHelper()
	return &corerevision.Revision{
		ID:     tree.RevisionIDFromString(id),
		PageID: pageID,
		Type:   corerevision.RevisionTypeContentUpdate,
		Title:  "Home",
		Slug:   "home",
		Kind:   string(tree.NodeKindPage),
		Path:   "home",
	}
}

type fakeMCPGetTagsUseCase struct {
	out *wikitags.GetTagsOutput
	err error
}

func (f fakeMCPGetTagsUseCase) Execute(context.Context, wikitags.GetTagsInput) (*wikitags.GetTagsOutput, error) {
	return f.out, f.err
}

type fakeMCPPagesByTagsUseCase struct {
	out *wikitags.GetPagesByTagsOutput
	err error
}

func (f fakeMCPPagesByTagsUseCase) Execute(context.Context, wikitags.GetPagesByTagsInput) (*wikitags.GetPagesByTagsOutput, error) {
	return f.out, f.err
}

type fakeMCPPropertyKeysUseCase struct {
	out *wikiproperties.GetPropertyKeysOutput
	err error
}

func (f fakeMCPPropertyKeysUseCase) Execute(context.Context, wikiproperties.GetPropertyKeysInput) (*wikiproperties.GetPropertyKeysOutput, error) {
	return f.out, f.err
}

type fakeMCPPagesByPropertyUseCase struct {
	out *wikiproperties.GetPagesByPropertyOutput
	err error
}

func (f fakeMCPPagesByPropertyUseCase) Execute(context.Context, wikiproperties.GetPagesByPropertyInput) (*wikiproperties.GetPagesByPropertyOutput, error) {
	return f.out, f.err
}

type fakeMCPSearchUseCase struct {
	out *wikisearch.SearchOutput
	err error
}

func (f fakeMCPSearchUseCase) Execute(context.Context, wikisearch.SearchInput) (*wikisearch.SearchOutput, error) {
	return f.out, f.err
}

type fakeMCPPreviewRefactorUseCase struct {
	out *wikipages.RefactorPreview
	err error
}

func (f fakeMCPPreviewRefactorUseCase) Execute(context.Context, wikipages.RefactorPreviewInput) (*wikipages.RefactorPreview, error) {
	return f.out, f.err
}

type fakeMCPApplyRefactorUseCase struct {
	out *tree.Page
	err error
}

func (f fakeMCPApplyRefactorUseCase) Execute(context.Context, wikipages.RefactorApplyInput) (*tree.Page, error) {
	return f.out, f.err
}

type fakeMCPGetPageUseCase struct {
	out *wikipages.GetPageOutput
	err error
}

func (f fakeMCPGetPageUseCase) Execute(context.Context, wikipages.GetPageInput) (*wikipages.GetPageOutput, error) {
	return f.out, f.err
}

type fakeMCPFindByPathUseCase struct {
	out *wikipages.FindByPathOutput
	err error
}

func (f fakeMCPFindByPathUseCase) Execute(context.Context, wikipages.FindByPathInput) (*wikipages.FindByPathOutput, error) {
	return f.out, f.err
}

type fakeMCPLookupPathUseCase struct {
	out *wikipages.LookupPagePathOutput
	err error
}

func (f fakeMCPLookupPathUseCase) Execute(context.Context, wikipages.LookupPagePathInput) (*wikipages.LookupPagePathOutput, error) {
	return f.out, f.err
}

type fakeMCPEnsurePathUseCase struct {
	out *wikipages.EnsurePathOutput
	err error
}

func (f fakeMCPEnsurePathUseCase) Execute(context.Context, wikipages.EnsurePathInput) (*wikipages.EnsurePathOutput, error) {
	return f.out, f.err
}

type fakeMCPUpdatePageUseCase struct {
	out *wikipages.UpdatePageOutput
	err error
}

func (f fakeMCPUpdatePageUseCase) Execute(context.Context, wikipages.UpdatePageInput) (*wikipages.UpdatePageOutput, error) {
	return f.out, f.err
}

type fakeMCPLinkStatusUseCase struct {
	out *wikilinks.GetLinkStatusOutput
	err error
}

func (f fakeMCPLinkStatusUseCase) Execute(context.Context, wikilinks.GetLinkStatusInput) (*wikilinks.GetLinkStatusOutput, error) {
	return f.out, f.err
}
