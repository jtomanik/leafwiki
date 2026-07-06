package tree

import (
	"errors"
	"fmt"
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/perber/wiki/internal/core/treemigration"
)

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node updates path lookup", func() {
		svc, _ := newLoadedService()

		docsID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Docs", newFixtureSlug("docs"), ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode docs failed: %v",

			err)

		archiveID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Archive", newFixtureSlug("archive"), ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode archive failed: %v",

			err,
		)

		guideID, err := svc.CreateNode(newFixtureUserID("system"), docsID, "Guide", newFixtureSlug("guide"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode guide failed: %v",

			err)
		{

			err := svc.MoveNode(newFixtureUserID("system"), *guideID, *archiveID, pageVersionUnchecked)
			Expect(err).To(Succeed(), "MoveNode failed: %v",

				err)
		}

		oldLookup, err := svc.LookupPagePath(newFixtureRoutePath("docs/guide"))
		Expect(err).To(Succeed(), "LookupPagePath old path failed: %v",

			err)
		Expect(oldLookup).To(matchMissingPathLookup(HaveExactElements(
			matchExistingPathSegment(*docsID),
			matchMissingPathSegment(),
		)), "expected old path to stop resolving after move")

		newLookup, err := svc.LookupPagePath(newFixtureRoutePath("archive/guide"))
		Expect(err).To(Succeed(), "LookupPagePath new path failed: %v",

			err)
		Expect(newLookup).To(matchExistingPathLookup(HaveExactElements(
			matchExistingPathSegment(*archiveID),
			matchExistingPathSegment(*guideID),
		)), "expected moved path to resolve at destination")

	})
})

// --- F) Migration V3 (metadata frontmatter backfill) ---
var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree migrates to V5 backfills child order files", func() {
		if CurrentSchemaVersion < 5 {
			ginkgo.Skip("requires schema v5+")
		}

		tmpDir := tempTreeDir()
		{

			err := saveSchema(tmpDir, CurrentSchemaVersion)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		docsID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Docs", newFixtureSlug("docs"), ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode docs failed: %v",

			err)

		alphaID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Alpha", newFixtureSlug("alpha"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode alpha failed: %v",

			err)

		betaID, err := svc.CreateNode(newFixtureUserID("system"), docsID, "Beta", newFixtureSlug("beta"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode beta failed: %v",

			err)

		root := svc.GetTree()
		root.Children = []*PageNode{root.Children[1], root.Children[0]}
		for i, child := range root.Children {
			child.Position = i
		}

		docsNode, err := svc.FindPageByID(*docsID)
		Expect(err).To(Succeed(), "FindPageByID docs failed: %v",

			err)
		Expect(docsNode).To(haveChildPageIDs(*betaID), "expected docs child beta before migration")
		{

			err := os.Remove(filepath.Join(tmpDir, "root", ".order.json"))
			Expect(err).To(SatisfyAny(Succeed(), matchErrorIs(os.ErrNotExist)), "remove root order file: %v", err)
		}
		{

			err := os.Remove(filepath.Join(tmpDir, "root", "docs", ".order.json"))
			Expect(err).To(SatisfyAny(Succeed(), matchErrorIs(os.ErrNotExist)), "remove docs order file: %v", err)
		}

		persistLegacyTreeSnapshot(tmpDir, svc.GetTree())
		{

			err := saveSchema(tmpDir, 4)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		loaded := NewTreeService(tmpDir)
		{
			err := loaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree (migrating) failed: %v",

				err,
			)
		}

		Expect(readOrderIDs(filepath.Join(tmpDir, "root"))).To(matchPersistedPageIDOrder(*alphaID, *docsID))

		Expect(readOrderIDs(filepath.Join(tmpDir, "root", "docs"))).To(matchPersistedPageIDOrder(*betaID))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree migrates to V4 materializes missing section index", func() {
		if CurrentSchemaVersion < 4 {
			ginkgo.Skip("requires schema v4+")
		}

		tmpDir := tempTreeDir()
		{

			err := saveSchema(tmpDir, 3)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		id, err := svc.CreateNode(newFixtureUserID("system"), nil, "Docs", newFixtureSlug("docs"), ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		node, err := svc.FindPageByID(*id)
		Expect(err).To(Succeed(), "FindPageByID failed: %v",

			err)

		node.Metadata = PageMetadata{
			CreatedAt:    time.Date(2026, time.March, 22, 10, 15, 30, 0, time.UTC),
			UpdatedAt:    time.Date(2026, time.March, 22, 11, 16, 31, 0, time.UTC),
			CreatorID:    newFixtureUserID("alice"),
			LastAuthorID: newFixtureUserID("bob"),
		}

		persistLegacyTreeSnapshot(tmpDir, svc.GetTree())

		indexPath := filepath.Join(tmpDir, "root", "docs", "index.md")
		{
			err := os.Remove(indexPath)
			Expect(err).To(Succeed(), "remove section index failed: %v",

				err,
			)
		}
		{

			err := saveSchema(tmpDir, 3)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		loaded := NewTreeService(tmpDir)
		{
			err := loaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree (migrating) failed: %v",

				err,
			)
		}

		raw, err := os.ReadFile(indexPath)
		Expect(err).To(Succeed(), "read migrated section index: %v",

			err,
		)

		frontmatter, body, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(SatisfyAll(
			matchManagedFrontmatter(*id, "Docs"),
			matchFrontmatterTimestamps("2026-03-22T10:15:30Z", "2026-03-22T11:16:31Z"),
			matchFrontmatterAuthors(newFixtureUserID("alice"), newFixtureUserID("bob")),
		), "expected section frontmatter to be materialized, got %#v", frontmatter)
		Expect(strings.TrimSpace(body)).To(BeEmpty(),

			"expected empty section body after migration, got %q",

			body)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree resumes interrupted migration with persisted legacy snapshot", func() {
		if CurrentSchemaVersion < 3 {
			ginkgo.Skip("requires schema v3+")
		}

		tmpDir := tempTreeDir()
		{
			err := saveSchema(tmpDir, CurrentSchemaVersion)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		id, err := svc.CreateNode(newFixtureUserID("system"), nil, "Page1", newFixtureSlug("page1"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		node, err := svc.FindPageByID(*id)
		Expect(err).To(Succeed(), "FindPageByID failed: %v",

			err)

		root := svc.GetTree()
		// Build a legacy snapshot with metadata stripped, without mutating the live tree.
		legacySnapshot := &PageNode{
			ID:    root.ID,
			Slug:  root.Slug,
			Title: root.Title,
			Kind:  root.Kind,
			Children: []*PageNode{{
				ID:       node.ID,
				Slug:     node.Slug,
				Title:    node.Title,
				Kind:     node.Kind,
				Position: node.Position,
			}},
		}
		persistLegacyTreeSnapshot(tmpDir, legacySnapshot)

		pagePath := filepath.Join(tmpDir, "root", "page1.md")
		legacyBody := "# Page 1 Content\nHello World\n"
		{
			err := os.WriteFile(pagePath, []byte(legacyBody), 0o644)
			Expect(err).To(Succeed(), "write legacy content failed: %v",

				err,
			)
		}

		originalModTime := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
		{
			err := os.Chtimes(pagePath, originalModTime, originalModTime)
			Expect(err).To(Succeed(), "Chtimes failed: %v",

				err)
		}
		{

			err := saveSchema(tmpDir, 0)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		interrupted := NewTreeService(tmpDir)
		legacyTree, err := interrupted.store.LoadTree(legacyTreeFilename)
		Expect(err).To(Succeed(), "LoadTree legacy snapshot failed: %v",

			err)

		interrupted.tree = legacyTree

		deps := interrupted.migrationDependencies()
		stopErr := errors.New("stop after v2")
		deps.SaveSchema = func(version int) error {
			if err := saveSchema(tmpDir, version); err != nil {
				return err
			}
			if version == 2 {
				return stopErr
			}
			return nil
		}

		err = treemigration.Run(0, deps)
		Expect(err).To(MatchError(stopErr), "expected interrupted migration error, got %v",

			err)

		reloaded := NewTreeService(tmpDir)
		{
			err := reloaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree after interrupted migration failed: %v",

				err)
		}

		raw, err := os.ReadFile(pagePath)
		Expect(err).To(Succeed(), "read resumed migration file: %v",

			err,
		)

		frontmatter, _, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(matchFrontmatterTimestamps(
			originalModTime.Format(time.RFC3339),
			originalModTime.Format(time.RFC3339),
		), "expected resumed migration to preserve v1 metadata via persisted legacy snapshot, got %#v", frontmatter)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("load tree migrates to V3 backfills metadata frontmatter", func() {
		if CurrentSchemaVersion < 3 {
			ginkgo.Skip("requires schema v3+")
		}

		tmpDir := tempTreeDir()
		{

			err := saveSchema(tmpDir, 2)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		id, err := svc.CreateNode(newFixtureUserID("system"), nil, "Page1", newFixtureSlug("page1"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		node, err := svc.FindPageByID(*id)
		Expect(err).To(Succeed(), "FindPageByID failed: %v",

			err)

		node.Metadata = PageMetadata{
			CreatedAt:    time.Date(2026, time.March, 21, 10, 15, 30, 0, time.UTC),
			UpdatedAt:    time.Date(2026, time.March, 21, 11, 16, 31, 0, time.UTC),
			CreatorID:    newFixtureUserID("alice"),
			LastAuthorID: newFixtureUserID("bob"),
		}

		persistLegacyTreeSnapshot(tmpDir, svc.GetTree())

		pagePath := filepath.Join(tmpDir, "root", "page1.md")
		legacyContent := fmt.Sprintf("---\nleafwiki_id: %s\nleafwiki_title: Page1\n---\n# Page 1 Content\nHello World\n", *id)
		{
			err := os.WriteFile(pagePath, []byte(legacyContent), 0o644)
			Expect(err).To(Succeed(), "write legacy content failed: %v",

				err,
			)
		}
		{

			err := saveSchema(tmpDir, 2)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		loaded := NewTreeService(tmpDir)
		{
			err := loaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree (migrating) failed: %v",

				err,
			)
		}

		raw, err := os.ReadFile(pagePath)
		Expect(err).To(Succeed(), "read migrated file: %v",

			err)

		frontmatter, migratedBody, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)
		Expect(frontmatter).To(SatisfyAll(
			matchFrontmatterTimestamps("2026-03-21T10:15:30Z", "2026-03-21T11:16:31Z"),
			matchFrontmatterAuthors(newFixtureUserID("alice"), newFixtureUserID("bob")),
		), "expected metadata to be backfilled, got %#v", frontmatter)

		wantBody := "# Page 1 Content\nHello World\n"
		Expect(migratedBody).
			To(Equal(
				wantBody,
			), "expected body preserved exactly.\nGot:\n%q\nWant:\n%q",

				migratedBody, wantBody)

	})
})
