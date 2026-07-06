package importer

import (
	"errors"
	"log/slog"
	"mime/multipart"
	"strings"

	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Importer migrates old route-style page link to .md

type fakeExecWiki struct {
	hash string

	ensureCalls int
	updateCalls int

	ensureFn func(userID tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error)
	updateFn func(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error)
	uploadFn func(userID tree.UserID, pageID tree.PageID, file multipart.File, filename tree.AssetName, byteCap shared.MaxBytes) (string, error)

	lastUpdatedContent *string
	ensureTargets      []tree.RoutePath
	ensureKinds        []tree.NodeKind
	updateTitles       []string
	uploadCalls        int
	uploadedAssets     []string
	lastUploadByteCap  shared.MaxBytes
}

func (f *fakeExecWiki) TreeHash() string { return f.hash }

func (f *fakeExecWiki) LookupPagePath(path tree.RoutePath) (*tree.PathLookup, error) {
	panic("not used by Executor")
}

func (f *fakeExecWiki) LookupPagePathForKind(path tree.RoutePath, kind tree.NodeKind) (*tree.PathLookup, error) {
	panic("not used by Executor")
}

func (f *fakeExecWiki) EnsurePath(userID tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error) {
	f.ensureCalls++
	f.ensureTargets = append(f.ensureTargets, targetPath)
	if kind != nil {
		f.ensureKinds = append(f.ensureKinds, *kind)
	}
	if f.ensureFn != nil {
		return f.ensureFn(userID, targetPath, title, kind)
	}
	return &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("p1"), Title: title, Slug: newFixtureSlug("slug"), Kind: *kind}}, nil
}

func (f *fakeExecWiki) UpdatePage(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
	f.updateCalls++
	f.lastUpdatedContent = content
	f.updateTitles = append(f.updateTitles, title)
	if f.updateFn != nil {
		return f.updateFn(userID, id, title, slug, content, kind)
	}
	// simulate tree change
	f.hash = f.hash + "-changed"
	return &tree.Page{PageNode: &tree.PageNode{ID: id, Title: title, Slug: slug, Kind: *kind}}, nil
}

func (f *fakeExecWiki) UploadAsset(userID tree.UserID, pageID tree.PageID, file multipart.File, filename tree.AssetName, byteCap shared.MaxBytes) (string, error) {
	f.uploadCalls++
	f.uploadedAssets = append(f.uploadedAssets, filename.Filename())
	f.lastUploadByteCap = byteCap
	if f.uploadFn != nil {
		return f.uploadFn(userID, pageID, file, filename, byteCap)
	}
	return "/assets/" + pageID.MetadataValue() + "/" + filename.Filename(), nil
}

func writeTmp(dir, rel, content string) {
	ginkgo.GinkgoHelper()

	importerWriteFile(dir, rel, content)
}

var _ = ginkgo.Describe("stale import execution", ginkgo.Label("unit"), func() {
	ginkgo.It("returns a stale-plan error without an execution result", func() {
		w := &fakeExecWiki{hash: "new"}
		plan := &PlanResult{TreeHash: "old"}
		opts := &PlanOptions{SourceBasePath: importerTempDir()}
		ex := NewExecutor(plan, opts, 0, w, slog.Default())

		got, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(MatchError(ErrImportPlanStale))
		Expect(got).To(BeNil())

	})
})

var _ = ginkgo.Describe("created page execution with source frontmatter", ginkgo.Label("unit"), func() {
	ginkgo.It("writes canonical metadata and drops importer-owned legacy fields", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "a.md", "---\naliases:\n  - x\ncustom_key: keep-me\nleafwiki_id: source-id\nleafwiki_title: Source Title\ntitle: X\n---\n\n# Heading\nBody")

		w := &fakeExecWiki{hash: "h1"}
		updatedContentByTitle := map[string]string{}
		w.updateFn = func(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
			w.lastUpdatedContent = content
			w.updateTitles = append(w.updateTitles, title)
			if content != nil {
				updatedContentByTitle[title] = *content
			}
			w.hash = w.hash + "-changed"
			return &tree.Page{PageNode: &tree.PageNode{ID: id, Title: title, Slug: slug, Kind: *kind}}, nil
		}
		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("a.md"), TargetPath: newFixtureRoutePath("docs/a"), Title: "A", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())

		res, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())

		Expect(res).To(SatisfyAll(
			MatchExecutionResultCounts(1, 0, ConsistOf(HaveField("Action", Equal(ExecutionActionCreated)))),
			HaveField("TreeHashBefore", Equal("h1")),
			HaveField("TreeHash", Not(Equal("h1"))),
		))
		Expect(w).To(MatchFakeExecWikiState(SatisfyAll(
			HaveField("EnsureCalls", Equal(1)),
			HaveField("UpdateCalls", Equal(1)),
			HaveField("LastUpdatedContent", Not(BeNil())),
		)))
		raw := *w.lastUpdatedContent
		Expect(raw).To(HavePrefix("<!-- leafwiki\n"))
		Expect(raw).NotTo(HavePrefix("---\n"))
		doc, err := importedPageDocumentResult(raw)
		Expect(err).To(Succeed())
		Expect(doc).To(HaveImportedPageDocument(
			Equal("\n# Heading\nBody"),
			HaveKeyWithValue("custom_key", "keep-me"),
			SatisfyAll(
				Not(HaveKey("title")),
				HaveKeyWithValue("aliases", ConsistOf("x")),
			),
		))
		Expect(raw).NotTo(ContainSubstring("leafwiki_id: source-id"))
		Expect(raw).NotTo(ContainSubstring("leafwiki_title: Source Title"))

	})
})

var _ = ginkgo.Describe("created page execution with distinct metadata fields", ginkgo.Label("unit"), func() {
	ginkgo.It("preserves distinct imported frontmatter fields", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "a.md", "---\nalpha: first\nbeta: second\nnested:\n  key: value\n---\n\nBody")

		w := &fakeExecWiki{hash: "h1"}
		updatedContentByTitle := map[string]string{}
		w.updateFn = func(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
			w.lastUpdatedContent = content
			w.updateTitles = append(w.updateTitles, title)
			if content != nil {
				updatedContentByTitle[title] = *content
			}
			w.hash = w.hash + "-changed"
			return &tree.Page{PageNode: &tree.PageNode{ID: id, Title: title, Slug: slug, Kind: *kind}}, nil
		}
		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("a.md"), TargetPath: newFixtureRoutePath("docs/a"), Title: "A", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		_, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())

		Expect(w.lastUpdatedContent).NotTo(BeNil())

		fm, _, err := importedFrontmatterResult(*w.lastUpdatedContent)
		Expect(err).To(Succeed())
		Expect(fm.ExtraFields).To(SatisfyAll(
			HaveKeyWithValue("alpha", "first"),
			HaveKeyWithValue("beta", "second"),
			HaveKeyWithValue("nested", HaveKeyWithValue("key", "value")),
		))

	})
})

var _ = ginkgo.Describe("skipped import items", ginkgo.Label("unit"), func() {
	ginkgo.It("records skipped items without touching wiki content", func() {
		tmp := importerTempDir()
		w := &fakeExecWiki{hash: "h1"}
		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("a.md"), TargetPath: newFixtureRoutePath("docs/a"), Action: PlanActionSkip},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		res, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())

		Expect(res).To(MatchExecutionResultCounts(0, 1, HaveLen(1)))
		Expect(w).To(MatchFakeExecWikiState(SatisfyAll(
			HaveField("EnsureCalls", BeZero()),
			HaveField("UpdateCalls", BeZero()),
		)))

	})
})

var _ = ginkgo.Describe("create execution when path creation fails", ginkgo.Label("unit"), func() {
	ginkgo.It("records create failures without updating page content", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "a.md", "Body")

		w := &fakeExecWiki{
			hash: "h1",
			ensureFn: func(userID tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error) {
				return nil, errors.New("boom")
			},
		}
		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("a.md"), TargetPath: newFixtureRoutePath("docs/a"), Title: "A", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		res, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())
		Expect(res).To(MatchExecutionResultCounts(0, 1, ConsistOf(HaveExecutionItemErrorCode(ImportErrorCodeEnsurePathFailed))))
		Expect(w.updateCalls).To(BeZero())

	})
})

var _ = ginkgo.Describe("execution of unsupported plan actions", ginkgo.Label("unit"), func() {
	ginkgo.It("records unsupported plan actions as skipped item errors", func() {
		tmp := importerTempDir()
		w := &fakeExecWiki{hash: "h1"}
		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("a.md"), TargetPath: newFixtureRoutePath("docs/a"), Action: PlanActionUpdate}, // not handled in switch
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		res, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())
		Expect(res).To(MatchExecutionResultCounts(0, 1, ConsistOf(HaveExecutionItemErrorCode(ImportErrorCodeUnknownAction))))

	})
})

var _ = ginkgo.Describe("folder index import ordering", ginkgo.Label("unit"), func() {
	ginkgo.It("creates folder indexes before child pages", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "Ordner/index.md", `---
title: Ordner
---

# Ordner`)
		writeTmp(tmp, "Ordner/Ordner.md", "# Unterseite")

		w := &fakeExecWiki{hash: "h1"}
		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("Ordner/index.md"), TargetPath: newFixtureRoutePath("ordner"), Title: "Ordner", Kind: tree.NodeKindSection, Action: PlanActionCreate},
				{SourcePath: newFixtureWorkspaceSourcePath("Ordner/Ordner.md"), TargetPath: newFixtureRoutePath("ordner/ordner"), Title: "Unterseite", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		res, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())
		Expect(res).To(MatchExecutionResultCounts(2, 0, HaveLen(2)))
		Expect(w).To(MatchFakeExecWikiState(SatisfyAll(
			HaveField("EnsureCalls", Equal(2)),
			HaveField("UpdateCalls", Equal(2)),
			HaveField("EnsureTargets", Equal([]tree.RoutePath{newFixtureRoutePath("ordner"), newFixtureRoutePath("ordner/ordner")})),
			HaveField("EnsureKinds", Equal([]tree.NodeKind{tree.NodeKindSection, tree.NodeKindPage})),
			HaveField("UpdateTitles", Equal([]string{"Ordner", "Unterseite"})),
		)))

	})
})

// - Importer migrates old route-style page link to .md
var _ = ginkgo.Describe("import execution link rewriting", ginkgo.Label("unit"), func() {
	ginkgo.It("rewrites imported markdown links to created wiki routes", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "Guides/index.md", "# Guides")
		writeTmp(tmp, "Guides/Setup.md", strings.Join([]string{
			"# Setup",
			"",
			"[Relative](../Reference/Endpoints.md)",
			"[Absolute](/Guides/index.md)",
			"[RouteStyle](/Reference/Endpoints)",
			"[Container](/Guides/)",
			"[[Reference/Endpoints|API Alias]]",
		}, "\n"))
		writeTmp(tmp, "Reference/Endpoints.md", "# Endpoints")

		w := &fakeExecWiki{hash: "h1"}
		updatedContentByTitle := map[string]string{}
		w.updateFn = func(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
			w.lastUpdatedContent = content
			w.updateTitles = append(w.updateTitles, title)
			if content != nil {
				updatedContentByTitle[title] = *content
			}
			w.hash = w.hash + "-changed"
			return &tree.Page{PageNode: &tree.PageNode{ID: id, Title: title, Slug: slug, Kind: *kind}}, nil
		}
		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("Guides/index.md"), TargetPath: newFixtureRoutePath("guides"), Title: "Guides", Kind: tree.NodeKindSection, Action: PlanActionCreate},
				{SourcePath: newFixtureWorkspaceSourcePath("Guides/Setup.md"), TargetPath: newFixtureRoutePath("guides/setup"), Title: "Setup", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				{SourcePath: newFixtureWorkspaceSourcePath("Reference/Endpoints.md"), TargetPath: newFixtureRoutePath("reference/endpoints"), Title: "Endpoints", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		_, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())

		setupContent, err := updatedContentByTitleResult(updatedContentByTitle, "Setup")
		Expect(err).To(Succeed())

		for _, expected := range []string{
			"[Relative](/reference/endpoints.md)",
			"[Absolute](/guides)",
			"[RouteStyle](/reference/endpoints.md)",
			"[Container](/guides)",
			"[API Alias](/reference/endpoints.md)",
		} {
			Expect(setupContent).To(ContainSubstring(expected))
		}

	})
})

var _ = ginkgo.Describe("import execution metadata preservation", ginkgo.Label("unit"), func() {
	ginkgo.It("preserves metadata links while rewriting body links", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "Guides/Setup.md", strings.Join([]string{
			"---",
			`custom_link: "[Endpoint](/Reference/Endpoints)"`,
			"nested:",
			`  link: "[Endpoint](/Reference/Endpoints)"`,
			"---",
			"",
			"# Setup",
			"",
			"[RouteStyle](/Reference/Endpoints)",
		}, "\n"))
		writeTmp(tmp, "Reference/Endpoints.md", "# Endpoints")

		w := &fakeExecWiki{hash: "h1"}
		updatedContentByTitle := map[string]string{}
		w.updateFn = func(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
			w.lastUpdatedContent = content
			w.updateTitles = append(w.updateTitles, title)
			if content != nil {
				updatedContentByTitle[title] = *content
			}
			w.hash = w.hash + "-changed"
			return &tree.Page{PageNode: &tree.PageNode{ID: id, Title: title, Slug: slug, Kind: *kind}}, nil
		}
		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("Guides/Setup.md"), TargetPath: newFixtureRoutePath("guides/setup"), Title: "Setup", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				{SourcePath: newFixtureWorkspaceSourcePath("Reference/Endpoints.md"), TargetPath: newFixtureRoutePath("reference/endpoints"), Title: "Endpoints", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		_, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())
		setupContent, err := updatedContentByTitleResult(updatedContentByTitle, "Setup")
		Expect(err).To(Succeed())
		doc, err := importedPageDocumentResult(setupContent)
		Expect(err).To(Succeed())
		Expect(doc).To(HaveImportedPageDocument(
			ContainSubstring("[RouteStyle](/reference/endpoints.md)"),
			HaveKeyWithValue("custom_link", "[Endpoint](/Reference/Endpoints)"),
			HaveKeyWithValue("nested", HaveKeyWithValue("link", "[Endpoint](/Reference/Endpoints)")),
		))

	})
})
