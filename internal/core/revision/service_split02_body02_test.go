package revision

import (
	"os"

	. "github.com/onsi/gomega"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("service", func() {

	ginkgo.It("restores historical custom frontmatter while keeping managed fields current", ginkgo.Label("integration"), func() {
		service, treeService, _ := newRevisionTestService()

		pageKind := tree.NodeKindPage
		pageIDPtr, err := treeService.CreateNode("creator", nil, "Page", "page", &pageKind)
		Expect(err).NotTo(HaveOccurred())
		pageID := *pageIDPtr

		firstRaw := renderRevisionTestMarkdown(pageID, "Page",
			map[string]interface{}{"customKey": "first"},
			map[string]interface{}{"aliases": []interface{}{"one"}},
			"Body",
		)
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("creator"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &firstRaw, true)).To(Succeed())
		firstRecord := createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "creator", "first"))
		Expect(firstRecord).To(haveRecordedRevision(Not(BeNil())))
		firstRev := firstRecord.Revision

		secondRaw := renderRevisionTestMarkdown(pageID, "Changed",
			map[string]interface{}{"customKey": "second"},
			map[string]interface{}{"aliases": []interface{}{"two"}},
			"Body changed",
		)
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("editor"), tree.PageIDFromString(pageID), "Changed", newFixtureSlug("page"), &secondRaw, true)).To(Succeed())
		Expect(createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "editor", "second"))).
			To(haveRecordedRevision(Not(BeNil())))

		beforeRestore, err := treeService.GetPage(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		managedID := beforeRestore.ID
		managedCreatedAt := beforeRestore.Metadata.CreatedAt
		managedCreatorID := beforeRestore.Metadata.CreatorID
		beforeUpdatedAt := beforeRestore.Metadata.UpdatedAt

		Expect(service.RestoreRevision(tree.PageIDFromString(pageID), RevisionIDFromString(firstRev.ID), newFixtureUserID("restorer"))).To(Succeed())

		page, err := treeService.GetPage(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(page).To(SatisfyAll(
			HaveField("ID", managedID),
			HaveField("Title", "Page"),
			HaveField("Metadata.CreatedAt", managedCreatedAt),
			HaveField("Metadata.CreatorID", managedCreatorID),
			HaveField("Metadata.LastAuthorID", revisionTestUserID("restorer")),
		))
		Expect(page.Metadata.UpdatedAt).To(BeTemporally(">", beforeUpdatedAt))

		raw, err := treeService.ReadPageRaw(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(raw).To(haveCanonicalRevisionRawStorage())
		Expect(parsedRevisionFrontmatter(markdown.ParseFrontmatter(raw))).To(haveParsedRevisionFrontmatter(
			SatisfyAll(
				HaveField("LeafWikiID", managedID.MetadataValue()),
				HaveField("LeafWikiTitle", page.Title),
				HaveField("LeafWikiCreatorID", managedCreatorID.MetadataValue()),
				HaveField("LeafWikiLastAuthorID", "restorer"),
				HaveField("LeafWikiUpdatedAt", Not(BeEmpty())),
				HaveField("ExtraFields", SatisfyAll(
					HaveKeyWithValue("customKey", "first"),
					HaveKeyWithValue("aliases", HaveExactElements("one")),
				)),
			),
			Equal("Body"),
		))
	})

	ginkgo.It("restores canonical fields without moving extras into managed fields", ginkgo.Label("integration"), func() {
		service, treeService, _ := newRevisionTestService()

		pageKind := tree.NodeKindPage
		pageIDPtr, err := treeService.CreateNode("creator", nil, "Page", "page", &pageKind)
		Expect(err).NotTo(HaveOccurred())
		pageID := *pageIDPtr

		firstRaw, err := markdown.RenderPageDocument(markdown.PageDocument{
			Body: "First body",
			Metadata: markdown.PageMetadata{
				Version: 1,
				Page:    markdown.PageMetadataPage{ID: pageID.MetadataValue(), Title: "Page"},
				Fields:  map[string]interface{}{"status": "draft", "priority": 2},
				Extra:   map[string]interface{}{"source": "imported"},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("creator"), pageID, "Page", newFixtureSlug("page"), &firstRaw, true)).To(Succeed())
		firstRecord := createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "creator", "first"))
		Expect(firstRecord).To(haveRecordedRevision(Not(BeNil())))
		firstRev := firstRecord.Revision

		secondRaw, err := markdown.RenderPageDocument(markdown.PageDocument{
			Body: "Second body",
			Metadata: markdown.PageMetadata{
				Version: 1,
				Page:    markdown.PageMetadataPage{ID: pageID.MetadataValue(), Title: "Page"},
				Fields:  map[string]interface{}{"status": "ready"},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("editor"), pageID, "Page", newFixtureSlug("page"), &secondRaw, true)).To(Succeed())
		Expect(createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "editor", "second"))).
			To(haveRecordedRevision(Not(BeNil())))

		Expect(service.RestoreRevision(tree.PageIDFromString(pageID), RevisionIDFromString(firstRev.ID), newFixtureUserID("restorer"))).To(Succeed())
		raw, err := treeService.ReadPageRaw(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(raw).To(haveCanonicalRevisionRawStorage())
		doc, _, err := markdown.ParsePageDocument(raw)
		Expect(err).NotTo(HaveOccurred())
		Expect(doc).To(SatisfyAll(
			HaveField("Metadata.Fields", SatisfyAll(
				HaveKeyWithValue("priority", 2),
				Not(HaveKey("source")),
			)),
			HaveField("Metadata.Extra", HaveKeyWithValue("source", "imported")),
			HaveField("Body", "First body"),
		))
	})

	ginkgo.It("restores an explicitly empty metadata snapshot", ginkgo.Label("integration"), func() {
		service, treeService, _ := newRevisionTestService()

		pageKind := tree.NodeKindPage
		pageIDPtr, err := treeService.CreateNode("creator", nil, "Page", "page", &pageKind)
		Expect(err).NotTo(HaveOccurred())
		pageID := *pageIDPtr

		firstRaw := renderRevisionTestMarkdown(pageID, "Page", nil, nil, "Empty metadata body")
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("creator"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &firstRaw, true)).To(Succeed())
		firstRecord := createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "creator", "empty metadata"))
		Expect(firstRecord).To(haveRecordedRevision(Not(BeNil())))
		firstRev := firstRecord.Revision
		Expect(firstRev.PageMetadata).NotTo(BeNil())

		secondRaw := renderRevisionTestMarkdown(pageID, "Page",
			map[string]interface{}{"status": "ready"},
			map[string]interface{}{"source": "imported"},
			"Non-empty metadata body",
		)
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("editor"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &secondRaw, true)).To(Succeed())
		Expect(createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "editor", "non-empty metadata"))).
			To(haveRecordedRevision(Not(BeNil())))

		Expect(service.RestoreRevision(tree.PageIDFromString(pageID), RevisionIDFromString(firstRev.ID), newFixtureUserID("restorer"))).To(Succeed())
		raw, err := treeService.ReadPageRaw(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(raw).To(haveCanonicalRevisionRawStorage())
		doc, _, err := markdown.ParsePageDocument(raw)
		Expect(err).NotTo(HaveOccurred())
		Expect(doc).To(SatisfyAll(
			HaveField("Metadata.Tags", BeEmpty()),
			HaveField("Metadata.Fields", BeEmpty()),
			HaveField("Metadata.Extra", BeEmpty()),
			HaveField("Body", "Empty metadata body"),
		))
	})

	ginkgo.It("preserves current custom frontmatter for legacy revisions without metadata snapshots", ginkgo.Label("integration"), func() {
		service, treeService, _ := newRevisionTestService()

		pageKind := tree.NodeKindPage
		pageIDPtr, err := treeService.CreateNode("creator", nil, "Page", "page", &pageKind)
		Expect(err).NotTo(HaveOccurred())
		pageID := *pageIDPtr

		initialRaw := renderRevisionTestMarkdown(pageID, "Page",
			map[string]interface{}{"customKey": "current"},
			nil,
			"Current body",
		)
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("creator"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &initialRaw, true)).To(Succeed())

		page, err := treeService.GetPage(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())

		state := service.revisionStateFromPage(page)
		contentHash, err := service.store.SaveContentBlob([]byte("Legacy body"))
		Expect(err).NotTo(HaveOccurred())
		legacyRevision, err := service.newRevision(RevisionTypeContentUpdate, state, "legacy-author", "legacy", "")
		Expect(err).NotTo(HaveOccurred())
		legacyRevision.ContentHash = contentHash
		legacyRevision.ExtraFrontmatter = nil
		legacyRevision.ExtraFrontmatterHash = ""
		Expect(service.store.SaveRevision(legacyRevision)).To(Succeed())

		Expect(service.RestoreRevision(tree.PageIDFromString(pageID), RevisionIDFromString(legacyRevision.ID), newFixtureUserID("restorer"))).To(Succeed())

		raw, err := treeService.ReadPageRaw(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(raw).To(haveCanonicalRevisionRawStorage())
		Expect(parsedRevisionFrontmatter(markdown.ParseFrontmatter(raw))).To(haveParsedRevisionFrontmatter(
			HaveField("ExtraFields", HaveKeyWithValue("customKey", "current")),
			Equal("Legacy body"),
		))
	})

	ginkgo.It("writes canonical metadata from legacy revision extra frontmatter", ginkgo.Label("integration"), func() {
		service, treeService, _ := newRevisionTestService()

		pageKind := tree.NodeKindPage
		pageIDPtr, err := treeService.CreateNode("creator", nil, "Page", "page", &pageKind)
		Expect(err).NotTo(HaveOccurred())
		pageID := *pageIDPtr

		initialContent := "Current body"
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("creator"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &initialContent, false)).To(Succeed())

		page, err := treeService.GetPage(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())

		state := service.revisionStateFromPage(page)
		contentHash, err := service.store.SaveContentBlob([]byte("Legacy body with extra"))
		Expect(err).NotTo(HaveOccurred())
		legacyRevision, err := service.newRevision(RevisionTypeContentUpdate, state, "legacy-author", "legacy extra", "")
		Expect(err).NotTo(HaveOccurred())
		legacyRevision.ContentHash = contentHash
		legacyRevision.PageMetadata = nil
		legacyRevision.PageMetadataHash = ""
		legacyRevision.ExtraFrontmatter = map[string]interface{}{
			"status":  "legacy",
			"aliases": []interface{}{"old"},
		}
		legacyRevision.ExtraFrontmatterHash, err = hashExtraFrontmatter(legacyRevision.ExtraFrontmatter)
		Expect(err).NotTo(HaveOccurred())
		Expect(service.store.SaveRevision(legacyRevision)).To(Succeed())

		Expect(service.RestoreRevision(tree.PageIDFromString(pageID), RevisionIDFromString(legacyRevision.ID), newFixtureUserID("restorer"))).To(Succeed())

		raw, err := treeService.ReadPageRaw(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(raw).To(haveCanonicalRevisionRawStorage())
		doc, _, err := markdown.ParsePageDocument(raw)
		Expect(err).NotTo(HaveOccurred())
		Expect(doc).To(SatisfyAll(
			HaveField("Metadata", SatisfyAll(
				HaveField("Fields", HaveKeyWithValue("status", "legacy")),
				HaveField("Extra", HaveKeyWithValue("aliases", HaveExactElements("old"))),
			)),
			HaveField("Body", Equal("Legacy body with extra")),
		))
	})

	ginkgo.It("keeps legacy YAML-looking content in the page body", ginkgo.Label("integration"), func() {
		service, treeService, _ := newRevisionTestService()

		pageKind := tree.NodeKindPage
		pageIDPtr, err := treeService.CreateNode("creator", nil, "Page", "page", &pageKind)
		Expect(err).NotTo(HaveOccurred())
		pageID := *pageIDPtr

		initialContent := "Current body"
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("creator"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &initialContent, false)).To(Succeed())

		page, err := treeService.GetPage(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())

		legacyBody := "---\ntitle: not frontmatter\n---\nBody content"
		state := service.revisionStateFromPage(page)
		contentHash, err := service.store.SaveContentBlob([]byte(legacyBody))
		Expect(err).NotTo(HaveOccurred())
		legacyRevision, err := service.newRevision(RevisionTypeContentUpdate, state, "legacy-author", "legacy body-only", "")
		Expect(err).NotTo(HaveOccurred())
		legacyRevision.ContentHash = contentHash
		legacyRevision.ExtraFrontmatter = nil
		legacyRevision.ExtraFrontmatterHash = ""
		Expect(service.store.SaveRevision(legacyRevision)).To(Succeed())

		Expect(service.RestoreRevision(tree.PageIDFromString(pageID), RevisionIDFromString(legacyRevision.ID), newFixtureUserID("restorer"))).To(Succeed())

		restoredPage, err := treeService.GetPage(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(restoredPage.Content).To(Equal(legacyBody))

		raw, err := treeService.ReadPageRaw(tree.PageIDFromString(pageID))
		Expect(err).NotTo(HaveOccurred())
		Expect(raw).To(haveCanonicalRevisionRawStorage())
		fm, body, hasFrontmatter, err := markdown.ParseFrontmatter(raw)
		Expect(parsedRevisionFrontmatter(fm, body, hasFrontmatter, err)).
			To(haveParsedRevisionFrontmatter(HaveField("ExtraFields", BeEmpty()), Equal(legacyBody)))
	})

	ginkgo.It("rebuilds missing previous manifests for content and structure revisions", ginkgo.Label("integration"), func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "hello")
		writeLiveAsset(storageDir, pageID, "a.txt", "asset-a")

		firstRecord := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), "tester", "asset"))
		Expect(firstRecord).To(haveRecordedRevision(Not(BeNil())))
		firstRev := firstRecord.Revision
		missingManifestPath := service.store.assetManifestPath(firstRev.AssetManifestHash)
		Expect(os.Remove(missingManifestPath)).To(Succeed())

		content := "hello-updated"
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &content, false)).To(Succeed())
		contentRecord := createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID), "tester", "content"))
		Expect(contentRecord).To(haveRecordedRevision(HaveField("AssetManifestHash", firstRev.AssetManifestHash)))
		contentRev := contentRecord.Revision
		_, err := service.store.LoadAssetManifest(contentRev.AssetManifestHash)
		Expect(err).NotTo(HaveOccurred())

		Expect(os.Remove(service.store.assetManifestPath(contentRev.AssetManifestHash))).To(Succeed())
		Expect(createdRevisionRecord(service.RecordStructureChange(revisionTestPageID(pageID), "tester", "structure"))).
			To(haveRecordedRevision(HaveField("AssetManifestHash", firstRev.AssetManifestHash)))
	})

	ginkgo.It("reports missing and tampered revision artifacts", ginkgo.Label("integration"), func() {
		service, treeService, storageDir := newRevisionTestService()

		pageID1 := createRevisionTestPage(treeService, "Page1", "page1", "hello")
		contentRecord := createdRevisionRecord(service.RecordContentUpdate(revisionTestPageID(pageID1), "tester", "content"))
		Expect(contentRecord).To(haveRecordedRevision(Not(BeNil())))
		Expect(os.Remove(service.store.contentBlobPath(contentRecord.Revision.ContentHash))).To(Succeed())
		issues1, err := service.CheckRevisionIntegrity(revisionTestPageID(pageID1))
		Expect(err).NotTo(HaveOccurred())
		Expect(issues1).To(HaveExactElements(matchRevisionIntegrityIssue(errCodeRevisionIntegrityMissingContent)))

		pageID2 := createRevisionTestPage(treeService, "Page2", "page2", "hello")
		writeLiveAsset(storageDir, pageID2, "a.txt", "asset-a")
		assetRecord := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID2), "tester", "asset"))
		Expect(assetRecord).To(haveRecordedRevision(Not(BeNil())))
		assetRev := assetRecord.Revision
		Expect(os.Remove(service.store.assetManifestPath(assetRev.AssetManifestHash))).To(Succeed())
		issues2, err := service.CheckRevisionIntegrity(revisionTestPageID(pageID2))
		Expect(err).NotTo(HaveOccurred())
		Expect(issues2).To(HaveExactElements(matchRevisionIntegrityIssue(errCodeRevisionIntegrityMissingManifest)))

		pageID3 := createRevisionTestPage(treeService, "Page3", "page3", "hello")
		writeLiveAsset(storageDir, pageID3, "a.txt", "asset-a")
		assetRecord3 := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID3), "tester", "asset"))
		Expect(assetRecord3).To(haveRecordedRevision(Not(BeNil())))
		assetRev3 := assetRecord3.Revision
		refs, err := service.store.LoadAssetManifest(assetRev3.AssetManifestHash)
		Expect(err).NotTo(HaveOccurred())
		Expect(refs).To(HaveLen(1))
		Expect(os.WriteFile(service.store.AssetBlobPath(refs[0].SHA256), []byte("tampered"), 0o644)).To(Succeed())
		issues3, err := service.CheckRevisionIntegrity(revisionTestPageID(pageID3))
		Expect(err).NotTo(HaveOccurred())
		Expect(issues3).To(HaveExactElements(matchRevisionIntegrityIssue(errCodeRevisionIntegrityHashMismatch)))
	})

	ginkgo.It("compares revision snapshots with content and asset deltas", ginkgo.Label("integration"), func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "one")
		writeLiveAsset(storageDir, pageID, "a.txt", "asset-a")

		baseRecord := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), "tester", "base"))
		Expect(baseRecord).To(haveRecordedRevision(Not(BeNil())))
		baseRev := baseRecord.Revision

		content := "two"
		Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), tree.PageIDFromString(pageID), "Page", newFixtureSlug("page"), &content, false)).To(Succeed())
		writeLiveAsset(storageDir, pageID, "b.txt", "asset-b")
		targetRecord := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), "tester", "target"))
		Expect(targetRecord).To(haveRecordedRevision(Not(BeNil())))
		targetRev := targetRecord.Revision

		comparison, err := service.CompareRevisionSnapshots(tree.PageIDFromString(pageID), RevisionIDFromString(baseRev.ID), RevisionIDFromString(targetRev.ID))
		Expect(err).NotTo(HaveOccurred())
		Expect(comparison).To(SatisfyAll(
			matchRevisionContentDelta(Equal("one"), Equal("two")),
			HaveField("AssetChanges", HaveExactElements(SatisfyAll(
				HaveField("Name", "b.txt"),
				HaveField("Status", "added"),
			))),
		))
	})

	ginkgo.It("returns stored revision asset blobs after live assets are deleted", ginkgo.Label("integration"), func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "one")
		writeLiveAsset(storageDir, pageID, "image.png", "asset-image")

		record := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), "tester", "with asset"))
		Expect(record).To(haveRecordedRevision(Not(BeNil())))
		rev := record.Revision

		Expect(os.Remove(revisionAssetPath(storageDir, pageID, "image.png"))).To(Succeed())

		asset, err := service.GetRevisionAsset(tree.PageIDFromString(pageID), RevisionIDFromString(rev.ID), tree.AssetName("image.png"))
		Expect(err).NotTo(HaveOccurred())
		Expect(asset).NotTo(BeNil())
		Expect(asset.Asset.Name).To(Equal("image.png"))
		content, err := os.ReadFile(asset.Path)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("asset-image"))
	})

	ginkgo.It("returns a localized asset-not-found error for missing manifest entries", ginkgo.Label("integration"), func() {
		service, treeService, storageDir := newRevisionTestService()
		pageID := createRevisionTestPage(treeService, "Page", "page", "one")
		writeLiveAsset(storageDir, pageID, "image.png", "asset-image")

		record := createdRevisionRecord(service.RecordAssetChange(revisionTestPageID(pageID), "tester", "with asset"))
		Expect(record).To(haveRecordedRevision(Not(BeNil())))
		rev := record.Revision

		_, err := service.GetRevisionAsset(tree.PageIDFromString(pageID), RevisionIDFromString(rev.ID), tree.AssetName("missing.png"))
		Expect(err).To(MatchLocalizedRevisionErrorCode(errCodeRevisionPreviewAssetNotFound))
	})
})
