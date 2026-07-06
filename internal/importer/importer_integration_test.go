package importer_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/importer"
	"github.com/perber/wiki/internal/properties"
	"github.com/perber/wiki/internal/tags"
	"github.com/perber/wiki/internal/wiki"
	"github.com/perber/wiki/internal/workspaceid"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func expectedAssetPath(pageID tree.PageID, filename tree.AssetName) string {
	return fmt.Sprintf("/assets/%s/%s", pageID, filename.Filename())
}

var errIntegrationFrontmatterMissing = errors.New("importer integration frontmatter missing")

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Importer distinguishes folder README section from README child page

func integFixtureUserID[T ~string](raw T) tree.UserID {
	return tree.UserIDFromString(raw)
}

func integFixtureAssetName[T ~string](raw T) tree.AssetName {
	return tree.AssetNameFromString(raw)
}

func integFixtureWorkspaceID[T ~string](raw T) workspaceid.WorkspaceID {
	ginkgo.GinkgoHelper()

	id, err := workspaceid.ParseWorkspaceID(string(raw))
	Expect(err).To(Succeed())
	return id
}

func integMustWrite(base, rel, content string) string {
	ginkgo.GinkgoHelper()

	abs := filepath.Join(base, filepath.FromSlash(rel))
	Expect(os.MkdirAll(filepath.Dir(abs), 0o755)).To(Succeed())
	Expect(os.WriteFile(abs, []byte(content), 0o644)).To(Succeed())
	return abs
}

func integFixturePath(rel string) string {
	ginkgo.GinkgoHelper()

	return integFixturePathForT(rel, "fixtures", "internal/importer/fixtures")
}

func integCopyFixtureToTemp(rel string) string {
	ginkgo.GinkgoHelper()

	sourceRoot := integFixturePath(rel)
	destRoot := filepath.Join(integTempDir(), rel)

	err := filepath.Walk(sourceRoot, func(sourcePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relativePath, err := filepath.Rel(sourceRoot, sourcePath)
		if err != nil {
			return err
		}
		if relativePath == "." {
			return os.MkdirAll(destRoot, 0o755)
		}
		destPath := filepath.Join(destRoot, relativePath)
		if info.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}
		raw, err := os.ReadFile(sourcePath)
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, raw, 0o644)
	})
	Expect(err).To(Succeed())
	return destRoot
}

func newTestWiki() *wiki.Wiki {
	ginkgo.GinkgoHelper()

	dataDir := filepath.Join(integTempDir(), "data")
	rootDir := filepath.Join(integTempDir(), "content")
	w, err := wiki.NewWiki(&wiki.WikiOptions{
		Workspace:           wiki.Workspace{ID: integFixtureWorkspaceID("default"), DataDir: dataDir, RootDir: rootDir},
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	Expect(err).To(Succeed())
	return w
}

func newTestImporterService(w *wiki.Wiki) *importer.ImporterService {
	ginkgo.GinkgoHelper()

	planner := importer.NewPlanner(wiki.NewWikiImportAdapter(w), tree.NewSlugService())
	importerDir := filepath.Join(w.GetStorageDir(), ".importer")
	return importer.NewImporterService(
		planner,
		importer.NewPlanStore(filepath.Join(importerDir, "current-plan.json")),
		filepath.Join(importerDir, "workspaces"),
		0,
	)
}

func newImporterProbe(w *wiki.Wiki) *wiki.WikiImportAdapter {
	return wiki.NewWikiImportAdapter(w)
}

func integImportedPageDocumentResult(raw string) (markdown.PageDocument, error) {
	ginkgo.GinkgoHelper()

	doc, _, err := markdown.ParsePageDocument(raw)
	return doc, err
}

func integImportedFrontmatterResult(raw string) (markdown.Frontmatter, string, error) {
	ginkgo.GinkgoHelper()

	fm, body, has, err := markdown.ParseFrontmatter(raw)
	if err != nil {
		return markdown.Frontmatter{}, "", err
	}
	if !has {
		return markdown.Frontmatter{}, body, errIntegrationFrontmatterMissing
	}
	return fm, body, nil
}

var _ = ginkgo.Describe("import execution writes migrated frontmatter", ginkgo.Label("integration"), func() {
	ginkgo.It("imports markdown with canonical metadata and preserved custom fields", func() {
		ws := integTempDir()
		integMustWrite(ws, "Imported.md", "---\naliases:\n  - alpha\ncustom_key: keep-me\nleafwiki_id: source-id\ntitle: Imported Title\n---\n\n# Imported Title\nBody")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		Expect(plan.Items).To(HaveLen(1))

		res, err := is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(Succeed())
		Expect(res).To(SatisfyAll(
			HaveField("ImportedCount", Equal(1)),
			HaveField("SkippedCount", BeZero()),
		))

		rawBytes, err := os.ReadFile(filepath.Join(w.GetRootDir(), "imported.md"))
		Expect(err).To(Succeed())
		raw := string(rawBytes)

		Expect(raw).To(HavePrefix("<!-- leafwiki\n"))
		Expect(raw).NotTo(HavePrefix("---\n"))
		doc, err := integImportedPageDocumentResult(raw)
		Expect(err).To(Succeed())
		Expect(doc).To(SatisfyAll(
			HaveField("Body", Equal("\n# Imported Title\nBody")),
			HaveField("Metadata.Fields", HaveKeyWithValue("custom_key", "keep-me")),
			HaveField("Metadata.Extra", SatisfyAll(
				Not(HaveKey("title")),
				HaveKeyWithValue("aliases", ConsistOf("alpha")),
			)),
			HaveField("Metadata.Page.ID", Not(BeEmpty())),
			HaveField("Metadata.Page.Title", Equal("Imported Title")),
		))
		Expect(raw).NotTo(ContainSubstring("leafwiki_id: source-id"))

	})
})

var _ = ginkgo.Describe("import execution indexes metadata", ginkgo.Label("integration"), func() {
	ginkgo.It("indexes imported tags and properties", func() {
		ws := integTempDir()
		integMustWrite(ws, "Imported.md", "---\ntags:\n  - React\n  - docs\nstatus: published\nowner: alice\npriority: 3\nowners:\n  - alice\n---\n\n# Imported Title\nBody")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)

		_, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())

		_, err = is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(Succeed())

		tagsStore, err := tags.NewTagsStore(w.GetStorageDir())
		Expect(err).To(Succeed())
		integWrapCloseWithErrorCheck(tagsStore.Close)

		allTags, err := tagsStore.GetAllTags("", 20)
		Expect(err).To(Succeed())
		Expect(allTags).To(HaveLen(2))

		reactPageIDs, err := tagsStore.GetPageIDsByTags([]string{"react"})
		Expect(err).To(Succeed())
		Expect(reactPageIDs).To(HaveLen(1))

		propsStore, err := properties.NewPropertiesStore(w.GetStorageDir())
		Expect(err).To(Succeed())
		integWrapCloseWithErrorCheck(propsStore.Close)

		keys, err := propsStore.GetAllPropertyKeys("", 20)
		Expect(err).To(Succeed())
		Expect(keys).To(HaveLen(2))

		statusPageIDs, err := propsStore.GetPageIDsByProperty("status", "published")
		Expect(err).To(Succeed())
		Expect(statusPageIDs).To(HaveLen(1))

		ownerPageIDs, err := propsStore.GetPageIDsByProperty("owner", "alice")
		Expect(err).To(Succeed())
		Expect(ownerPageIDs).To(HaveLen(1))

		priorityPageIDs, err := propsStore.GetPageIDsByProperty("priority", "3")
		Expect(err).To(Succeed())
		Expect(priorityPageIDs).To(BeEmpty())

	})
})

var _ = ginkgo.Describe("import execution rewrites links and uploads assets", ginkgo.Label("integration"), func() {
	ginkgo.It("rewrites imported links and uploads referenced assets", func() {
		ws := integTempDir()
		integMustWrite(ws, "Guides/index.md", "# Guides")
		integMustWrite(ws, "Guides/Setup.md", strings.Join([]string{
			"# Setup",
			"",
			"[Guide Home](/Guides/)",
			"[API](../Reference/Endpoints.md#intro)",
			"![[./images/logo.png]]",
			"[Manual](/shared/manual.pdf)",
			"[[Reference/Endpoints|API Alias]]",
		}, "\n"))
		integMustWrite(ws, "Reference/Endpoints.md", "# Endpoints")
		integMustWrite(ws, "Guides/images/logo.png", "png-bytes")
		integMustWrite(ws, "shared/manual.pdf", "pdf-bytes")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)
		probe := newImporterProbe(w)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		Expect(plan.Items).To(HaveLen(3))

		_, err = is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(Succeed())

		setupPage, err := probe.FindByPath("guides/setup")
		Expect(err).To(Succeed())

		for _, expected := range []string{
			"[Guide Home](/guides)",
			"[API](/reference/endpoints.md#intro)",
			"[API Alias](/reference/endpoints.md)",
			expectedAssetPath(setupPage.ID, integFixtureAssetName("logo.png")),
			expectedAssetPath(setupPage.ID, integFixtureAssetName("manual.pdf")),
		} {
			Expect(setupPage.Content).To(ContainSubstring(expected))
		}

		assets, err := probe.ListAssets(setupPage.ID)
		Expect(err).To(Succeed())
		Expect(assets).To(HaveLen(2))

	})
})

var _ = ginkgo.Describe("import execution for link asset fixture packages", ginkgo.Label("integration"), func() {
	ginkgo.It("imports the link-asset fixture package with expected routes and assets", func() {
		ws := integCopyFixtureToTemp("link-assets-package")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)
		probe := newImporterProbe(w)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		Expect(plan.Items).To(HaveLen(5))

		_, err = is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(Succeed())

		setupPage, err := probe.FindByPath("guides/setup")
		Expect(err).To(Succeed())

		for _, expected := range []string{
			"[Relative MD](/reference/endpoints.md)",
			"[Absolute MD](/reference/endpoints.md)",
			"[Container](/guides)",
			"[Endpoints](/reference/endpoints.md)",
			"[API Alias](/reference/endpoints.md)",
			fmt.Sprintf("![Relative Image](%s)", expectedAssetPath(setupPage.ID, integFixtureAssetName("logo.png"))),
			fmt.Sprintf("[Manual](%s)", expectedAssetPath(setupPage.ID, integFixtureAssetName("manual.pdf"))),
			fmt.Sprintf("![logo.png](%s)", expectedAssetPath(setupPage.ID, integFixtureAssetName("logo.png"))),
			"`[Inline](../Reference/Endpoints.md)`",
			"`[[Reference/Endpoints|Inline Alias]]`",
			"[Fenced](../Reference/Endpoints.md)",
			"[[Reference/Endpoints|Fence Alias]]",
			"![[./images/logo.png]]",
		} {
			Expect(setupPage.Content).To(ContainSubstring(expected))
		}

		assets, err := probe.ListAssets(setupPage.ID)
		Expect(err).To(Succeed())
		Expect(assets).To(HaveLen(2))

		_, err = probe.FindByPath("reference/endpoints")
		Expect(err).To(Succeed())
		_, err = probe.FindByPath("reference/api-1")
		Expect(err).To(Succeed())
		_, err = probe.FindByPath("guides")
		Expect(err).To(Succeed())
		_, err = probe.FindByPath("readme")
		Expect(err).To(MatchError(tree.ErrPageNotFound))

	})
})

var _ = ginkgo.Describe("import execution for nested LeafWiki fixture packages", ginkgo.Label("integration"), func() {
	ginkgo.It("imports nested LeafWiki fixture pages with metadata and links", func() {
		ws := integCopyFixtureToTemp("leafwiki-nested-package")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)
		probe := newImporterProbe(w)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		Expect(plan.Items).To(HaveLen(5))

		_, err = is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(Succeed())

		introPage, err := probe.FindByPath("intro")
		Expect(err).To(Succeed())
		gettingStartedPage, err := probe.FindByPath("docs/getting-started")
		Expect(err).To(Succeed())
		basicGuidePage, err := probe.FindByPath("docs/guides/basic-guide")
		Expect(err).To(Succeed())
		_, err = probe.FindByPath("docs")
		Expect(err).To(Succeed())
		_, err = probe.FindByPath("docs/guides")
		Expect(err).To(Succeed())

		for _, expected := range []string{
			"[Getting Started](/docs/getting-started.md)",
			"[Basic Guide](/docs/guides/basic-guide.md)",
		} {
			Expect(introPage.Content).To(ContainSubstring(expected))
		}

		for _, expected := range []string{
			"[Intro](/intro.md)",
			"[Basic Guide](/docs/guides/basic-guide.md)",
		} {
			Expect(gettingStartedPage.Content).To(ContainSubstring(expected))
		}

		for _, expected := range []string{
			"[Introduction](/intro.md)",
			"[Documentation](/docs)",
		} {
			Expect(basicGuidePage.Content).To(ContainSubstring(expected))
		}

		rawIntroBytes, err := os.ReadFile(filepath.Join(w.GetRootDir(), "intro.md"))
		Expect(err).To(Succeed())
		rawIntro := string(rawIntroBytes)

		fm, body, err := integImportedFrontmatterResult(rawIntro)
		Expect(err).To(Succeed())
		Expect(rawIntro).NotTo(ContainSubstring("leafwiki_id: intro-source"))
		Expect(fm).To(SatisfyAll(
			HaveField("LeafWikiID", Not(BeEmpty())),
			HaveField("LeafWikiTitle", Equal("Introduction")),
			HaveField("LeafWikiCreatorID", Equal(integFixtureUserID("system").MetadataValue())),
			HaveField("LeafWikiLastAuthorID", Equal(integFixtureUserID("system").MetadataValue())),
			HaveField("LeafWikiCreatedAt", Not(BeEmpty())),
			HaveField("LeafWikiUpdatedAt", Not(BeEmpty())),
			HaveField("ExtraFields", SatisfyAll(
				HaveKeyWithValue("category", "onboarding"),
				HaveKeyWithValue("aliases", ConsistOf("start")),
			)),
		))
		Expect(body).To(ContainSubstring("[Getting Started](/docs/getting-started.md)"))

	})
})

var _ = ginkgo.Describe("import execution for Obsidian wiki-link fixture packages", ginkgo.Label("integration"), func() {
	ginkgo.It("imports Obsidian wiki-link fixtures with expected canonical links", func() {
		ws := integCopyFixtureToTemp("obsidian-wikilinks-package")

		w := newTestWiki()
		integWrapCloseWithErrorCheck(w.Close)
		is := newTestImporterService(w)
		probe := newImporterProbe(w)

		plan, err := is.CreateImportPlanFromFolder(ws, "")
		Expect(err).To(Succeed())
		Expect(plan.Items).To(HaveLen(5))

		_, err = is.ExecuteCurrentPlan(integFixtureUserID("system"))
		Expect(err).To(Succeed())

		homePage, err := probe.FindByPath("home")
		Expect(err).To(Succeed())
		projectPlanPage, err := probe.FindByPath("project-plan")
		Expect(err).To(Succeed())
		brainstormPage, err := probe.FindByPath("daily/brainstorm")
		Expect(err).To(Succeed())
		meetingNotesPage, err := probe.FindByPath("daily/meeting-notes")
		Expect(err).To(Succeed())
		_, err = probe.FindByPath("archive/meeting-notes")
		Expect(err).To(Succeed())

		for _, expected := range []string{
			"[Project Plan](/project-plan.md)",
			"[Brainstorm](/daily/brainstorm.md)",
			"[[Meeting Notes]]",
			"[Meeting Alias](/daily/meeting-notes.md)",
			fmt.Sprintf("![diagram.png](%s)", expectedAssetPath(homePage.ID, integFixtureAssetName("diagram.png"))),
			"`[[Project Plan]]`",
			"[[Daily/Meeting Notes]]",
			"![[Attachments/diagram.png]]",
		} {
			Expect(homePage.Content).To(ContainSubstring(expected))
		}

		for _, expected := range []string{
			"[Meeting Notes](/daily/meeting-notes.md)",
			"[Home](/home.md)",
		} {
			Expect(projectPlanPage.Content).To(ContainSubstring(expected))
		}

		Expect(meetingNotesPage.Content).To(ContainSubstring("[Home](/home.md)"))
		Expect(brainstormPage.Content).To(ContainSubstring("[Home](/home.md)"))

		assets, err := probe.ListAssets(homePage.ID)
		Expect(err).To(Succeed())
		Expect(assets).To(HaveLen(1))

	})
})
