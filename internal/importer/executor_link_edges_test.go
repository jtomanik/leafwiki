package importer

import (
	"log/slog"
	"strings"

	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Importer migrates old route-style page link to .md

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
