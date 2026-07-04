package importer

import (
	"mime/multipart"
	"os"
	"path/filepath"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - README.md as normal page keeps its filesystem casing in generated links

type fakeWiki struct {
	treeHash string

	// planner part
	lookups          map[string]*tree.PathLookup
	lookupsForKind   map[fakeLookupForKindKey]*tree.PathLookup
	lookupErr        error
	lookupForKindErr error

	// executor part
	ensureCalls        int
	updateCalls        int
	lastUpdatedContent *string

	ensureFn      func(userID tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error)
	updateFn      func(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error)
	ensureErr     error
	ensureNilPage bool
	updateErr     error
}

type fakeLookupForKindKey struct {
	path tree.RoutePath
	kind tree.NodeKind
}

func (f *fakeWiki) TreeHash() string { return f.treeHash }

func (f *fakeWiki) LookupPagePath(p tree.RoutePath) (*tree.PathLookup, error) {
	if f.lookupErr != nil {
		return nil, f.lookupErr
	}
	key := p.FilesystemPath()
	if v, ok := f.lookups[key]; ok {
		return v, nil
	}
	return &tree.PathLookup{Path: p, Exists: false, Segments: []tree.PathSegment{}}, nil
}

func (f *fakeWiki) LookupPagePathForKind(p tree.RoutePath, kind tree.NodeKind) (*tree.PathLookup, error) {
	if f.lookupForKindErr != nil {
		return nil, f.lookupForKindErr
	}
	key := fakeLookupForKindKey{path: p, kind: kind}
	if v, ok := f.lookupsForKind[key]; ok {
		return v, nil
	}
	return f.LookupPagePath(p)
}

func (f *fakeWiki) EnsurePath(userID tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error) {
	f.ensureCalls++
	if f.ensureFn != nil {
		return f.ensureFn(userID, targetPath, title, kind)
	}
	if f.ensureErr != nil {
		return nil, f.ensureErr
	}
	if f.ensureNilPage {
		return nil, nil
	}
	k := tree.NodeKindPage
	if kind != nil {
		k = *kind
	}
	// create minimal page object
	return &tree.Page{PageNode: &tree.PageNode{
		ID:    "p1",
		Title: title,
		Slug:  newFixtureSlug("slug"),
		Kind:  k,
	}}, nil
}

func (f *fakeWiki) UpdatePage(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error) {
	f.updateCalls++
	f.lastUpdatedContent = content
	if f.updateFn != nil {
		return f.updateFn(userID, id, title, slug, content, kind)
	}
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	// simulate tree change after update
	f.treeHash = f.treeHash + "-changed"
	k := tree.NodeKindPage
	if kind != nil {
		k = *kind
	}
	return &tree.Page{PageNode: &tree.PageNode{
		ID:    tree.PageIDFromString(id),
		Title: title,
		Slug:  slug,
		Kind:  k,
	}}, nil
}

func (f *fakeWiki) UploadAsset(userID tree.UserID, pageID tree.PageID, file multipart.File, filename tree.AssetName, byteCap shared.MaxBytes) (string, error) {
	return "/assets/" + pageID.MetadataValue() + "/" + filename.Filename(), nil
}

func newPlannerWithFake(w *fakeWiki) *Planner {
	return NewPlanner(w, tree.NewSlugService())
}

func fakePathSegment(slug string, kind tree.NodeKind, id string, title string, exists bool) tree.PathSegment {
	pageID := tree.PageIDFromString(id)
	return tree.PathSegment{
		Slug:   tree.SlugFromString(slug),
		Kind:   &kind,
		ID:     &pageID,
		Title:  &title,
		Exists: exists,
	}
}

func fakeMissingPathSegment(slug string, kind tree.NodeKind) tree.PathSegment {
	return tree.PathSegment{
		Slug:   tree.SlugFromString(slug),
		Kind:   &kind,
		Exists: false,
	}
}

var _ = ginkgo.Describe("import plan creation for markdown pages", ginkgo.Label("unit"), func() {
	ginkgo.It("creates page plan items from markdown headings", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "My Page.md", "# Hello\n\nbody")

		wiki := &fakeWiki{
			treeHash: "h1",
			lookups:  map[string]*tree.PathLookup{},
		}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "My Page.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "/docs",
		})
		Expect(err).To(Succeed())
		Expect(res).To(SatisfyAll(
			HaveField("TreeHash", Equal("h1")),
			HaveImportPlanResult(ConsistOf(SatisfyAll(
				HaveField("Action", Equal(PlanActionCreate)),
				HaveField("Kind", Equal(tree.NodeKindPage)),
				HaveField("Title", Equal("Hello")),
				HaveField("TargetPath", Equal(tree.RoutePath("docs/my-page"))),
				HaveField("DesiredSlug", Equal(tree.Slug("my-page"))),
			))),
		))

	})
})

var _ = ginkgo.Describe("import plan creation for folder index sections", ginkgo.Label("unit"), func() {
	ginkgo.It("creates section plan items for folder indexes", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "Guides/index.md", "---\ntitle: Guides\n---\n\n# Ignored")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "Guides/index.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		Expect(err).To(Succeed())
		Expect(res.Items).To(ConsistOf(SatisfyAll(
			HaveField("Kind", Equal(tree.NodeKindSection)),
			HaveField("Action", Equal(PlanActionCreate)),
			HaveField("TargetPath", Equal(tree.RoutePath("docs/guides"))),
			HaveField("DesiredSlug", Equal(tree.Slug("guides"))),
			HaveField("Title", Equal("Guides")),
		)))

	})
})

var _ = ginkgo.Describe("import plan creation for README folder fallbacks", ginkgo.Label("unit"), func() {
	ginkgo.It("creates section plan items for README folder fallbacks", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "Guides/README.md", "# Guides")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "Guides/README.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		Expect(err).To(Succeed())
		Expect(res.Items).To(ConsistOf(SatisfyAll(
			HaveField("Kind", Equal(tree.NodeKindSection)),
			HaveField("TargetPath", Equal(tree.RoutePath("docs/guides"))),
			HaveField("DesiredSlug", Equal(tree.Slug("guides"))),
		)))

	})
})

type nonExactReadmeCase struct {
	sourcePath tree.WorkspaceSourcePath
	wantPath   string
}

var _ = ginkgo.DescribeTable("import plan creation for non-exact README filenames",
	ginkgo.Label("unit"),
	func(tt nonExactReadmeCase) {
		tmp := importerTempDir()
		importerWriteFile(tmp, tt.sourcePath.FilesystemPath(), "# Readme Page")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: tt.sourcePath}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		Expect(err).To(Succeed())
		Expect(res.Items).To(ConsistOf(SatisfyAll(
			HaveField("Kind", Equal(tree.NodeKindPage)),
			HaveField("TargetPath", Equal(tree.RoutePathFromString(tt.wantPath))),
		)))
	},
	ginkgo.Entry("Guides/readme.md", nonExactReadmeCase{sourcePath: newFixtureWorkspaceSourcePath("Guides/readme.md"), wantPath: "docs/guides/readme"}),
	ginkgo.Entry("Guides/Readme.md", nonExactReadmeCase{sourcePath: newFixtureWorkspaceSourcePath("Guides/Readme.md"), wantPath: "docs/guides/readme"}),
)

var _ = ginkgo.Describe("import plan creation beside existing sections", ginkgo.Label("unit"), func() {
	ginkgo.It("creates page items beside existing sections", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "sync.md", "# Sync Page")

		wiki := &fakeWiki{
			treeHash: "h",
			lookups: map[string]*tree.PathLookup{
				"docs/sync": {
					Path:   "docs/sync",
					Exists: true,
					Segments: []tree.PathSegment{
						fakePathSegment("docs", tree.NodeKindSection, "docs-section", "Docs", true),
						fakePathSegment("sync", tree.NodeKindSection, "sync-section", "Sync Section", true),
					},
				},
			},
			lookupsForKind: map[fakeLookupForKindKey]*tree.PathLookup{
				{path: newFixtureRoutePath("docs/sync"), kind: tree.NodeKindPage}: {
					Path: "docs/sync",
					Segments: []tree.PathSegment{
						fakePathSegment("docs", tree.NodeKindSection, "docs-section", "Docs", true),
						fakeMissingPathSegment("sync", tree.NodeKindPage),
					},
					Exists: false,
				},
			},
		}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "sync.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		Expect(err).To(Succeed())
		Expect(res.Items).To(ConsistOf(SatisfyAll(
			HaveField("Action", Equal(PlanActionCreate)),
			HaveField("Kind", Equal(tree.NodeKindPage)),
			HaveField("TargetPath", Equal(tree.RoutePath("docs/sync"))),
		)))

	})
})

// - README.md as normal page keeps its filesystem casing in generated links
var _ = ginkgo.Describe("import plan creation with mixed-case index files", ginkgo.Label("unit"), func() {
	ginkgo.It("preserves mixed-case README child pages beside index sections", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "Guides/index.MD", "# Guides")
		importerWriteFile(tmp, "Guides/README.md", "# Readme Page")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{
			{SourcePath: "Guides/index.MD"},
			{SourcePath: "Guides/README.md"},
		}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		Expect(err).To(Succeed())
		Expect(res.Items).To(HaveLen(2))
		Expect(res.Items).To(ContainElement(SatisfyAll(
			HaveField("SourcePath", Equal(tree.WorkspaceSourcePath("Guides/README.md"))),
			HaveField("Kind", Equal(tree.NodeKindPage)),
			HaveField("TargetPath", Equal(tree.RoutePath("docs/guides/README"))),
		)))

	})
})

var _ = ginkgo.Describe("import plan title selection", ginkgo.Label("unit"), func() {
	ginkgo.It("prefers source leafwiki titles over fallback titles", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "Guide.md", "---\nleafwiki_title: Preferred Title\ntitle: Fallback Title\n---\n\n# Heading")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "Guide.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "",
		})
		Expect(err).To(Succeed())
		Expect(res.Items).To(ConsistOf(HaveField("Title", Equal("Preferred Title"))))

	})
})

type titleFallbackCase struct {
	content string
	want    string
}

var _ = ginkgo.DescribeTable("import title fallback selection",
	ginkgo.Label("unit"),
	func(tt titleFallbackCase) {
		mdFile, err := markdown.NewMarkdownFileFromRaw(`C:\Users\johnjkr\AppData\Local\Temp\import-1280817455\1999-07-23 - Memo to Staff.md`, tt.content)
		Expect(err).To(Succeed())

		title, err := mdFile.GetTitle()
		Expect(err).To(Succeed())
		Expect(title).To(Equal(tt.want))
	},
	ginkgo.Entry("frontmatter wins", titleFallbackCase{
		content: "---\nleafwiki_title: Frontmatter Title\n---\n\n# Heading Title",
		want:    "Frontmatter Title",
	}),
	ginkgo.Entry("first heading wins when frontmatter missing", titleFallbackCase{
		content: "Intro text\n\n# Heading Title\nBody",
		want:    "Heading Title",
	}),
	ginkgo.Entry("filename fallback strips windows path", titleFallbackCase{
		content: "Body without title markers",
		want:    "1999-07-23 - Memo to Staff",
	}),
)

var _ = ginkgo.Describe("import plan existing page detection", ginkgo.Label("unit"), func() {
	ginkgo.It("skips existing pages with their existing IDs", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "a.md", "# A")

		existingID := "id123"
		existingKind := tree.NodeKindPage
		existingTitle := "Existing A"

		wiki := &fakeWiki{
			treeHash: "h",
			lookups: map[string]*tree.PathLookup{
				"docs/a": {
					Path:   "docs/a",
					Exists: true,
					Segments: []tree.PathSegment{
						{Slug: "docs", Exists: true},
						{Slug: "a", Exists: true, ID: func() *tree.PageID { id := tree.PageIDFromString(existingID); return &id }(), Kind: &existingKind, Title: &existingTitle},
					},
				},
			},
		}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "a.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		Expect(err).To(Succeed())
		Expect(res).To(HaveImportPlanResult(ConsistOf(HaveSkippedExistingPage(
			tree.PageIDFromString(existingID),
			tree.Slug("a"),
		))))

	})
})

var _ = ginkgo.Describe("import plan source file errors", ginkgo.Label("unit"), func() {
	ginkgo.It("records errors for missing source files", func() {
		tmp := importerTempDir()
		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "missing.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		Expect(err).To(Succeed())
		Expect(res).To(HaveImportPlanErrors(HaveLen(1)))

	})
})

var _ = ginkgo.Describe("import plan directory source errors", ginkgo.Label("unit"), func() {
	ginkgo.It("records errors when a source path is a directory", func() {
		tmp := importerTempDir()
		Expect(os.MkdirAll(filepath.Join(tmp, "dir"), 0o755)).To(Succeed())

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "dir"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		Expect(err).To(Succeed())
		Expect(res).To(HaveImportPlanErrors(HaveLen(1)))

	})
})

var _ = ginkgo.Describe("import plan malformed lookup errors", ginkgo.Label("unit"), func() {
	ginkgo.It("records malformed lookup responses as plan errors", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "x.md", "# X")

		wiki := &fakeWiki{
			treeHash: "h",
			lookups: map[string]*tree.PathLookup{
				"docs/x": {Path: "docs/x", Exists: true, Segments: []tree.PathSegment{}},
			},
		}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "x.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		Expect(err).To(Succeed())
		Expect(res).To(HaveImportPlanErrors(HaveLen(1)))

	})
})

// ---- Title extraction -------------------------------------------------------

var _ = ginkgo.Describe("import plan title extraction failures", ginkgo.Label("unit"), func() {
	ginkgo.It("falls back to filenames when title extraction fails", func() {
		tmp := importerTempDir()
		abs := importerWriteFile(tmp, "unreadable.md", "# Title")

		// Make file unreadable to trigger extraction error
		Expect(os.Chmod(abs, 0o000)).To(Succeed())
		ginkgo.DeferCleanup(os.Chmod, abs, os.FileMode(0o644))

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "unreadable.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		Expect(err).To(Succeed())
		Expect(res).To(HaveImportPlanResult(ConsistOf(SatisfyAll(
			HaveField("Notes", ConsistOf(ContainSubstring("Failed to load markdown file for title extraction"))),
			HaveField("Title", Equal("unreadable")),
		))))

	})
})

var _ = ginkgo.Describe("import plan source directory normalization", ginkgo.Label("unit"), func() {
	ginkgo.It("normalizes source directories into target route paths", func() {
		// "My Guides/Intro.md" -> "my-guides/intro" via centralized SlugService creation normalization.
		tmp := importerTempDir()
		importerWriteFile(tmp, "My Guides/Intro.md", "# Intro")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "My Guides/Intro.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		Expect(err).To(Succeed())
		Expect(res).To(HaveImportPlanResult(ConsistOf(HaveField("TargetPath", Equal(tree.RoutePath("docs/my-guides/intro"))))))

	})
})

var _ = ginkgo.Describe("import plan reserved slug normalization", ginkgo.Label("unit"), func() {
	ginkgo.It("avoids reserved target slugs by suffixing them", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "Reference/API.md", "# API")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "Reference/API.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		Expect(err).To(Succeed())
		Expect(res).To(HaveImportPlanResult(ConsistOf(SatisfyAll(
			HaveField("TargetPath", Equal(tree.RoutePath("docs/reference/api-1"))),
			HaveField("DesiredSlug", Equal(tree.Slug("api-1"))),
		))))

	})
})

var _ = ginkgo.Describe("import plan invalid source directory segments", ginkgo.Label("unit"), func() {
	ginkgo.It("reports invalid source directory segments as plan errors", func() {
		// Import path normalization still rejects segments that collapse to an empty slug.
		// A segment like "!!!" normalizes to "", so planning should report an error.
		tmp := importerTempDir()
		importerWriteFile(tmp, "!!!/a.md", "# A")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "!!!/a.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		Expect(err).To(Succeed())
		Expect(res).To(HaveImportPlanErrors(HaveLen(1)))
		// optional: grobe Assertion, dass es ein Validate-Fehler ist
		Expect(res.Errors).To(ContainElement(Not(BeEmpty())))

	})
})

var _ = ginkgo.Describe("import plan root index fallback titles", ginkgo.Label("unit"), func() {
	ginkgo.It("uses the root index filename when title extraction fails", func() {
		// Test case for root-level index.md with empty TargetBasePath and markdown loading failure
		// When wikiPath is empty, path.Base("") returns ".", which is not meaningful.
		// The fix should use filename without extension as fallback.
		tmp := importerTempDir()
		abs := importerWriteFile(tmp, "index.md", "# Title")

		// Make file unreadable to trigger markdown loading failure
		Expect(os.Chmod(abs, 0o000)).To(Succeed())
		ginkgo.DeferCleanup(os.Chmod, abs, os.FileMode(0o644))

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "index.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "", // empty target base path
		})
		Expect(err).To(Succeed())
		Expect(res).To(HaveImportPlanResult(ConsistOf(SatisfyAll(
			HaveField("TargetPath", Equal(tree.RoutePath(""))),
			HaveField("Kind", Equal(tree.NodeKindSection)),
			HaveField("Title", Equal("index")),
			HaveField("Notes", ContainElement(ContainSubstring("Failed to load markdown file for title extraction"))),
		))))

	})
})

var _ = ginkgo.Describe("import plan folder index and sibling pages", ginkgo.Label("unit"), func() {
	ginkgo.It("creates section and sibling page items for folder imports", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "Ordner/index.md", `---
title: Ordner
---

# Ordner`)
		importerWriteFile(tmp, "Ordner/Ordner.md", "# Unterseite")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{
			{SourcePath: "Ordner/index.md"},
			{SourcePath: "Ordner/Ordner.md"},
		}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "",
		})
		Expect(err).To(Succeed())
		Expect(res).To(HaveImportPlanResult(ConsistOf(
			SatisfyAll(
				HaveField("SourcePath", Equal(tree.WorkspaceSourcePath("Ordner/index.md"))),
				HaveField("Kind", Equal(tree.NodeKindSection)),
				HaveField("TargetPath", Equal(tree.RoutePath("ordner"))),
			),
			SatisfyAll(
				HaveField("SourcePath", Equal(tree.WorkspaceSourcePath("Ordner/Ordner.md"))),
				HaveField("Kind", Equal(tree.NodeKindPage)),
				HaveField("TargetPath", Equal(tree.RoutePath("ordner/ordner"))),
			),
		)))

	})
})

var _ = ginkgo.Describe("import plan folder markdown without index", ginkgo.Label("unit"), func() {
	ginkgo.It("creates folder markdown as pages when no index exists", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "Ordner/Ordner.md", "# Unterseite")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "Ordner/Ordner.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "wiki",
		})
		Expect(err).To(Succeed())
		Expect(res).To(HaveImportPlanResult(ConsistOf(SatisfyAll(
			HaveField("Kind", Equal(tree.NodeKindPage)),
			HaveField("TargetPath", Equal(tree.RoutePath("wiki/ordner/ordner"))),
		))))

	})
})

var _ = ginkgo.Describe("import plan uppercase index sections", ginkgo.Label("unit"), func() {
	ginkgo.It("treats uppercase index files as section indexes", func() {
		tmp := importerTempDir()
		importerWriteFile(tmp, "Guides/index.MD", `---
title: Guides
---

# Ignored`)

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "Guides/index.MD"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		Expect(err).To(Succeed())
		Expect(res.Items).To(ConsistOf(SatisfyAll(
			HaveField("Kind", Equal(tree.NodeKindSection)),
			HaveField("TargetPath", Equal(tree.RoutePath("docs/guides"))),
		)))

	})
})
