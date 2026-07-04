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
	"github.com/onsi/gomega/gstruct"
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
	return &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("p1"), Title: title, Slug: "slug", Kind: *kind}}, nil
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
				{SourcePath: "a.md", TargetPath: "docs/a", Title: "A", Kind: tree.NodeKindPage, Action: PlanActionCreate},
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
				{SourcePath: "a.md", TargetPath: "docs/a", Title: "A", Kind: tree.NodeKindPage, Action: PlanActionCreate},
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
				{SourcePath: "a.md", TargetPath: "docs/a", Action: PlanActionSkip},
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
				{SourcePath: "a.md", TargetPath: "docs/a", Title: "A", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		res, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())
		Expect(res).To(MatchExecutionResultCounts(0, 1, ConsistOf(HaveField("Error", gstruct.PointTo(Not(BeEmpty()))))))
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
				{SourcePath: "a.md", TargetPath: "docs/a", Action: PlanActionUpdate}, // not handled in switch
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		res, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())
		Expect(res).To(MatchExecutionResultCounts(0, 1, ConsistOf(HaveField("Error", gstruct.PointTo(Equal("unknown action"))))))

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
				{SourcePath: "Ordner/index.md", TargetPath: "ordner", Title: "Ordner", Kind: tree.NodeKindSection, Action: PlanActionCreate},
				{SourcePath: "Ordner/Ordner.md", TargetPath: "ordner/ordner", Title: "Unterseite", Kind: tree.NodeKindPage, Action: PlanActionCreate},
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
			HaveField("EnsureTargets", Equal([]tree.RoutePath{"ordner", "ordner/ordner"})),
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
				{SourcePath: "Guides/index.md", TargetPath: "guides", Title: "Guides", Kind: tree.NodeKindSection, Action: PlanActionCreate},
				{SourcePath: "Guides/Setup.md", TargetPath: "guides/setup", Title: "Setup", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				{SourcePath: "Reference/Endpoints.md", TargetPath: "reference/endpoints", Title: "Endpoints", Kind: tree.NodeKindPage, Action: PlanActionCreate},
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
				{SourcePath: "Guides/Setup.md", TargetPath: "guides/setup", Title: "Setup", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				{SourcePath: "Reference/Endpoints.md", TargetPath: "reference/endpoints", Title: "Endpoints", Kind: tree.NodeKindPage, Action: PlanActionCreate},
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

var _ = ginkgo.Describe("import execution asset uploads", ginkgo.Label("unit"), func() {
	ginkgo.It("uploads referenced assets and rewrites hrefs to asset URLs", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "Guides/Setup.md", strings.Join([]string{
			"# Setup",
			"",
			"![Relative](./images/logo.png)",
			"[Asset](/shared/manual.pdf)",
			"![[./images/logo.png]]",
		}, "\n"))
		writeTmp(tmp, "Guides/images/logo.png", "png-bytes")
		writeTmp(tmp, "shared/manual.pdf", "pdf-bytes")

		w := &fakeExecWiki{hash: "h1"}
		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: "Guides/Setup.md", TargetPath: "guides/setup", Title: "Setup", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 1234, w, slog.Default())
		_, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())

		Expect(w).To(MatchFakeExecWikiState(SatisfyAll(
			HaveField("UploadCalls", Equal(2)),
			HaveField("LastUpdatedContent", Not(BeNil())),
			HaveField("LastUploadByteCap", Equal(shared.MaxBytes(1234))),
		)))
		Expect(*w.lastUpdatedContent).To(ContainSubstring("![Relative](/assets/p1/logo.png)"))
		Expect(*w.lastUpdatedContent).To(ContainSubstring("[Asset](/assets/p1/manual.pdf)"))
		Expect(*w.lastUpdatedContent).To(ContainSubstring("![logo.png](/assets/p1/logo.png)"))

	})
})

var _ = ginkgo.Describe("import execution non-image asset wiki links", ginkgo.Label("unit"), func() {
	ginkgo.It("rewrites non-image wiki asset links with file labels", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "Guides/Setup.md", strings.Join([]string{
			"# Setup",
			"",
			"[[../shared/manual.pdf]]",
			"![[../shared/manual.pdf]]",
		}, "\n"))
		writeTmp(tmp, "shared/manual.pdf", "pdf-bytes")

		w := &fakeExecWiki{hash: "h1"}
		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: "Guides/Setup.md", TargetPath: "guides/setup", Title: "Setup", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		_, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())

		Expect(w.lastUpdatedContent).NotTo(BeNil())
		Expect(*w.lastUpdatedContent).To(ContainSubstring("[manual.pdf](/assets/p1/manual.pdf)"))
		Expect(*w.lastUpdatedContent).To(ContainSubstring("![manual.pdf](/assets/p1/manual.pdf)"))

	})
})

var _ = ginkgo.Describe("import execution wiki-link basename resolution", ginkgo.Label("unit"), func() {
	ginkgo.It("resolves unique basename wiki links and leaves ambiguous names unchanged", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "Home.md", strings.Join([]string{
			"# Home",
			"",
			"[[Brainstorm]]",
			"[[Meeting Notes]]",
		}, "\n"))
		writeTmp(tmp, "Daily/Brainstorm.md", "# Brainstorm")
		writeTmp(tmp, "Daily/Meeting Notes.md", "# Daily Meeting Notes")
		writeTmp(tmp, "Archive/Meeting Notes.md", "# Archived Meeting Notes")

		w := &fakeExecWiki{hash: "h1"}
		updatedContentByTitle := map[string]string{}
		w.updateFn = func(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
			w.lastUpdatedContent = content
			if content != nil {
				updatedContentByTitle[title] = *content
			}
			w.hash = w.hash + "-changed"
			return &tree.Page{PageNode: &tree.PageNode{ID: id, Title: title, Slug: slug, Kind: *kind}}, nil
		}

		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: "Home.md", TargetPath: "home", Title: "Home", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				{SourcePath: "Daily/Brainstorm.md", TargetPath: "daily/brainstorm", Title: "Brainstorm", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				{SourcePath: "Daily/Meeting Notes.md", TargetPath: "daily/meeting-notes", Title: "Meeting Notes", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				{SourcePath: "Archive/Meeting Notes.md", TargetPath: "archive/meeting-notes", Title: "Meeting Notes", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		_, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())

		homeContent, err := updatedContentByTitleResult(updatedContentByTitle, "Home")
		Expect(err).To(Succeed())
		Expect(homeContent).To(ContainSubstring("[Brainstorm](/daily/brainstorm.md)"))
		Expect(homeContent).To(ContainSubstring("[[Meeting Notes]]"))

	})
})

var _ = ginkgo.Describe("import execution wiki-link path suffix resolution", ginkgo.Label("unit"), func() {
	ginkgo.It("resolves wiki-link path suffixes to imported pages", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "knowledge-main/tools/kubernetes/resources/StatefulSet.md", strings.Join([]string{
			"# StatefulSet",
			"",
			"[[tools/kubernetes/resources/Deployment|Deployment]]",
		}, "\n"))
		writeTmp(tmp, "knowledge-main/tools/kubernetes/resources/Deployment.md", "# Deployment")

		w := &fakeExecWiki{hash: "h1"}
		updatedContentByTitle := map[string]string{}
		w.updateFn = func(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
			w.lastUpdatedContent = content
			if content != nil {
				updatedContentByTitle[title] = *content
			}
			w.hash = w.hash + "-changed"
			return &tree.Page{PageNode: &tree.PageNode{ID: id, Title: title, Slug: slug, Kind: *kind}}, nil
		}

		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: "knowledge-main/tools/kubernetes/resources/StatefulSet.md", TargetPath: "knowledge-main/tools/kubernetes/resources/statefulset", Title: "StatefulSet", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				{SourcePath: "knowledge-main/tools/kubernetes/resources/Deployment.md", TargetPath: "knowledge-main/tools/kubernetes/resources/deployment", Title: "Deployment", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		_, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())

		statefulSetContent, err := updatedContentByTitleResult(updatedContentByTitle, "StatefulSet")
		Expect(err).To(Succeed())
		Expect(statefulSetContent).To(ContainSubstring("[Deployment](/knowledge-main/tools/kubernetes/resources/deployment.md)"))

	})
})

var _ = ginkgo.Describe("import execution unresolved wiki links", ginkgo.Label("unit"), func() {
	ginkgo.It("generates fallback hrefs for unresolved wiki links", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "Home.md", strings.Join([]string{
			"# Home",
			"",
			"[[Missing Note]]",
		}, "\n"))

		w := &fakeExecWiki{hash: "h1"}
		updatedContentByTitle := map[string]string{}
		w.updateFn = func(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
			w.lastUpdatedContent = content
			if content != nil {
				updatedContentByTitle[title] = *content
			}
			w.hash = w.hash + "-changed"
			return &tree.Page{PageNode: &tree.PageNode{ID: id, Title: title, Slug: slug, Kind: *kind}}, nil
		}

		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: "Home.md", TargetPath: "home", Title: "Home", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		_, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())

		homeContent, err := updatedContentByTitleResult(updatedContentByTitle, "Home")
		Expect(err).To(Succeed())
		Expect(homeContent).To(ContainSubstring("[Missing Note](/missing-note)"))

	})
})

var _ = ginkgo.Describe("import execution code-block link preservation", ginkgo.Label("unit"), func() {
	ginkgo.It("leaves code block links unchanged while rewriting real links", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "Guides/Setup.md", strings.Join([]string{
			"# Setup",
			"",
			"`[Inline](../Reference/Endpoints.md)`",
			"",
			"```md",
			"[Fence](../Reference/Endpoints.md)",
			"[[Reference/Endpoints|Fence Alias]]",
			"```",
			"",
			"[Real](../Reference/Endpoints.md)",
			"[[Reference/Endpoints|Real Alias]]",
		}, "\n"))
		writeTmp(tmp, "Reference/Endpoints.md", "# Endpoints")

		w := &fakeExecWiki{hash: "h1"}
		updatedContentByTitle := map[string]string{}
		w.updateFn = func(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
			w.lastUpdatedContent = content
			if content != nil {
				updatedContentByTitle[title] = *content
			}
			w.hash = w.hash + "-changed"
			return &tree.Page{PageNode: &tree.PageNode{ID: id, Title: title, Slug: slug, Kind: *kind}}, nil
		}

		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: "Guides/Setup.md", TargetPath: "guides/setup", Title: "Setup", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				{SourcePath: "Reference/Endpoints.md", TargetPath: "reference/endpoints", Title: "Endpoints", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		_, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())

		setupContent, err := updatedContentByTitleResult(updatedContentByTitle, "Setup")
		Expect(err).To(Succeed())
		for _, expected := range []string{
			"`[Inline](../Reference/Endpoints.md)`",
			"[Fence](../Reference/Endpoints.md)",
			"[[Reference/Endpoints|Fence Alias]]",
			"[Real](/reference/endpoints.md)",
			"[Real Alias](/reference/endpoints.md)",
		} {
			Expect(setupContent).To(ContainSubstring(expected))
		}

	})
})

var _ = ginkgo.Describe("import execution Windows-style import paths", ginkgo.Label("unit"), func() {
	ginkgo.It("normalizes Windows-style import link separators", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "Guides/Setup.md", strings.Join([]string{
			"# Setup",
			"",
			"[Doc](..\\Reference\\Endpoints.md)",
			"![Diagram](images\\diagram.png)",
		}, "\n"))
		writeTmp(tmp, "Reference/Endpoints.md", "# Endpoints")
		writeTmp(tmp, "Guides/images/diagram.png", "png-bytes")

		w := &fakeExecWiki{hash: "h1"}
		updatedContentByTitle := map[string]string{}
		w.updateFn = func(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
			w.lastUpdatedContent = content
			if content != nil {
				updatedContentByTitle[title] = *content
			}
			w.hash = w.hash + "-changed"
			return &tree.Page{PageNode: &tree.PageNode{ID: id, Title: title, Slug: slug, Kind: *kind}}, nil
		}

		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: "Guides/Setup.md", TargetPath: "guides/setup", Title: "Setup", Kind: tree.NodeKindPage, Action: PlanActionCreate},
				{SourcePath: "Reference/Endpoints.md", TargetPath: "reference/endpoints", Title: "Endpoints", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		_, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())

		setupContent, err := updatedContentByTitleResult(updatedContentByTitle, "Setup")
		Expect(err).To(Succeed())
		for _, expected := range []string{
			"[Doc](/reference/endpoints.md)",
			"![Diagram](/assets/p1/diagram.png)",
		} {
			Expect(setupContent).To(ContainSubstring(expected))
		}

	})
})

var _ = ginkgo.Describe("import execution Windows drive-letter links", ginkgo.Label("unit"), func() {
	ginkgo.It("leaves Windows drive-letter links unchanged", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "Guides/Setup.md", strings.Join([]string{
			"# Setup",
			"",
			"[Windows File](C:\\Users\\John\\Notes\\Endpoints.md)",
			"![Windows Image](C:\\Users\\John\\Images\\diagram.png)",
		}, "\n"))

		w := &fakeExecWiki{hash: "h1"}
		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: "Guides/Setup.md", TargetPath: "guides/setup", Title: "Setup", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		_, err := ex.Execute(newFixtureUserID("user1"))
		Expect(err).To(Succeed())

		Expect(w.lastUpdatedContent).NotTo(BeNil())
		for _, expected := range []string{
			"[Windows File](C:\\Users\\John\\Notes\\Endpoints.md)",
			"![Windows Image](C:\\Users\\John\\Images\\diagram.png)",
		} {
			Expect(*w.lastUpdatedContent).To(ContainSubstring(expected))
		}

	})
})
