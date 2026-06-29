package importer

import (
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"

	ginkgo "github.com/onsi/ginkgo/v2"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - README.md as normal page keeps its filesystem casing in generated links

type fakeWiki struct {
	treeHash string

	// planner part
	lookups          map[string]*tree.PathLookup
	lookupsForKind   map[string]*tree.PathLookup
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
	key := string(kind) + ":" + p.FilesystemPath()
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
		ID:    newFixturePageID(id),
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
	pageID := newFixturePageID(id)
	return tree.PathSegment{
		Slug:   newFixtureSlug(slug),
		Kind:   &kind,
		ID:     &pageID,
		Title:  &title,
		Exists: exists,
	}
}

func fakeMissingPathSegment(slug string, kind tree.NodeKind) tree.PathSegment {
	return tree.PathSegment{
		Slug:   newFixtureSlug(slug),
		Kind:   &kind,
		Exists: false,
	}
}

var _ = ginkgo.Describe("TestPlanner_CreatePlan_CreateNewPage_NonIndex", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		importerWriteFile(t, tmp, "My Page.md", "# Hello\n\nbody")

		wiki := &fakeWiki{
			treeHash: "h1",
			lookups:  map[string]*tree.PathLookup{},
		}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "My Page.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "/docs",
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		if res.TreeHash != "h1" {
			t.Fatalf("TreeHash = %q", res.TreeHash)
		}
		if len(res.Errors) != 0 {
			t.Fatalf("Errors = %#v", res.Errors)
		}
		if len(res.Items) != 1 {
			t.Fatalf("Items len = %d", len(res.Items))
		}

		it := res.Items[0]
		if it.Action != PlanActionCreate {
			t.Fatalf("Action = %q", it.Action)
		}
		if it.Kind != tree.NodeKindPage {
			t.Fatalf("Kind = %v", it.Kind)
		}
		if it.Title != "Hello" {
			t.Fatalf("Title = %q", it.Title)
		}
		if it.TargetPath != "docs/my-page" {
			t.Fatalf("TargetPath = %q (want docs/my-page)", it.TargetPath)
		}
		if it.DesiredSlug != "my-page" {
			t.Fatalf("DesiredSlug = %q (want my-page)", it.DesiredSlug)
		}

	})
})

var _ = ginkgo.Describe("TestPlanner_CreatePlan_CreateNewSection_IndexMd", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		importerWriteFile(t, tmp, "Guides/index.md", "---\ntitle: Guides\n---\n\n# Ignored")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "Guides/index.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		it := res.Items[0]

		if it.Kind != tree.NodeKindSection {
			t.Fatalf("Kind = %v", it.Kind)
		}
		if it.Action != PlanActionCreate {
			t.Fatalf("Action = %q", it.Action)
		}
		if it.TargetPath != "docs/guides" {
			t.Fatalf("TargetPath = %q (want docs/guides)", it.TargetPath)
		}
		if it.DesiredSlug != "guides" {
			t.Fatalf("DesiredSlug = %q (want guides)", it.DesiredSlug)
		}
		if it.Title != "Guides" {
			t.Fatalf("Title = %q", it.Title)
		}

	})
})

var _ = ginkgo.Describe("TestPlanner_CreatePlan_ReadmeMdFallbackSectionWhenNoIndex", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		importerWriteFile(t, tmp, "Guides/README.md", "# Guides")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "Guides/README.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		it := res.Items[0]

		if it.Kind != tree.NodeKindSection {
			t.Fatalf("Kind = %v, want section", it.Kind)
		}
		if it.TargetPath != "docs/guides" {
			t.Fatalf("TargetPath = %q, want docs/guides", it.TargetPath)
		}
		if it.DesiredSlug != "guides" {
			t.Fatalf("DesiredSlug = %q, want guides", it.DesiredSlug)
		}

	})
})

type nonExactReadmeCase struct {
	sourcePath string
	wantPath   string
}

var _ = ginkgo.DescribeTable("TestPlanner_CreatePlan_NonExactReadmeMdImportsAsPage",
	func(tt nonExactReadmeCase) {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		importerWriteFile(t, tmp, tt.sourcePath, "# Readme Page")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: newFixtureWorkspaceSourcePath(tt.sourcePath)}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		it := res.Items[0]
		if it.Kind != tree.NodeKindPage {
			t.Fatalf("Kind = %v, want page", it.Kind)
		}
		if it.TargetPath != newFixtureRoutePath(tt.wantPath) {
			t.Fatalf("TargetPath = %q, want %s", it.TargetPath, tt.wantPath)
		}
	},
	ginkgo.Entry("Guides/readme.md", nonExactReadmeCase{sourcePath: "Guides/readme.md", wantPath: "docs/guides/readme"}),
	ginkgo.Entry("Guides/Readme.md", nonExactReadmeCase{sourcePath: "Guides/Readme.md", wantPath: "docs/guides/readme"}),
)

var _ = ginkgo.Describe("TestPlanner_CreatePlan_CreatesPageTwinWhenExistingSameRouteSectionExists", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		importerWriteFile(t, tmp, "sync.md", "# Sync Page")

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
			lookupsForKind: map[string]*tree.PathLookup{
				string(tree.NodeKindPage) + ":docs/sync": {
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
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		if len(res.Items) != 1 {
			t.Fatalf("Items len = %d, want 1", len(res.Items))
		}
		it := res.Items[0]
		if it.Action != PlanActionCreate {
			t.Fatalf("Action = %q, want create", it.Action)
		}
		if it.Kind != tree.NodeKindPage {
			t.Fatalf("Kind = %v, want page", it.Kind)
		}
		if it.TargetPath != "docs/sync" {
			t.Fatalf("TargetPath = %q, want docs/sync", it.TargetPath)
		}

	})
})

// - README.md as normal page keeps its filesystem casing in generated links
var _ = ginkgo.Describe("TestPlanner_CreatePlan_IndexMdCaseInsensitiveBeatsReadmeFallback", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		importerWriteFile(t, tmp, "Guides/index.MD", "# Guides")
		importerWriteFile(t, tmp, "Guides/README.md", "# Readme Page")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{
			{SourcePath: "Guides/index.MD"},
			{SourcePath: "Guides/README.md"},
		}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		if len(res.Items) != 2 {
			t.Fatalf("Items len = %d, want 2", len(res.Items))
		}

		var readme PlanItem
		for _, item := range res.Items {
			if item.SourcePath == "Guides/README.md" {
				readme = item
			}
		}
		if readme.SourcePath == "" {
			t.Fatalf("README item not found: %#v", res.Items)
		}
		if readme.Kind != tree.NodeKindPage {
			t.Fatalf("README Kind = %v, want page", readme.Kind)
		}
		if readme.TargetPath != "docs/guides/README" {
			t.Fatalf("README TargetPath = %q, want docs/guides/README", readme.TargetPath)
		}

	})
})

var _ = ginkgo.Describe("TestPlanner_CreatePlan_PrefersLeafWikiTitleOverTitle", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		importerWriteFile(t, tmp, "Guide.md", "---\nleafwiki_title: Preferred Title\ntitle: Fallback Title\n---\n\n# Heading")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "Guide.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "",
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		if len(res.Items) != 1 {
			t.Fatalf("Items len = %d", len(res.Items))
		}

		if got := res.Items[0].Title; got != "Preferred Title" {
			t.Fatalf("Title = %q (want Preferred Title)", got)
		}

	})
})

type titleFallbackCase struct {
	content string
	want    string
}

var _ = ginkgo.DescribeTable("TestPlanner_CreatePlan_TitleFallbackPriority_WindowsPathFilenameFallback",
	func(tt titleFallbackCase) {
		t := ginkgo.GinkgoT()
		mdFile, err := markdown.NewMarkdownFileFromRaw(`C:\Users\johnjkr\AppData\Local\Temp\import-1280817455\1999-07-23 - Memo to Staff.md`, tt.content)
		if err != nil {
			t.Fatalf("err: %v", err)
		}

		title, err := mdFile.GetTitle()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if title != tt.want {
			t.Fatalf("title = %q (want %q)", title, tt.want)
		}
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

var _ = ginkgo.Describe("TestPlanner_CreatePlan_SkipExisting_UsesLookupLastSegment", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		importerWriteFile(t, tmp, "a.md", "# A")

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
						{Slug: "a", Exists: true, ID: func() *tree.PageID { id := newFixturePageID(existingID); return &id }(), Kind: &existingKind, Title: &existingTitle},
					},
				},
			},
		}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "a.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		if len(res.Errors) != 0 {
			t.Fatalf("Errors = %#v", res.Errors)
		}

		it := res.Items[0]
		if it.Action != PlanActionSkip {
			t.Fatalf("Action = %q", it.Action)
		}
		if !it.Exists {
			t.Fatalf("Exists = false")
		}
		if it.ExistingID == nil || *it.ExistingID != newFixturePageID(existingID) {
			t.Fatalf("ExistingID = %#v (want %q)", it.ExistingID, existingID)
		}
		if it.DesiredSlug != "a" {
			t.Fatalf("DesiredSlug = %q (want a)", it.DesiredSlug)
		}

	})
})

var _ = ginkgo.Describe("TestPlanner_CreatePlan_Error_SourceMissing_IsCollected", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "missing.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		if len(res.Items) != 0 {
			t.Fatalf("Items len = %d (want 0)", len(res.Items))
		}
		if len(res.Errors) != 1 {
			t.Fatalf("Errors len = %d (want 1)", len(res.Errors))
		}

	})
})

var _ = ginkgo.Describe("TestPlanner_CreatePlan_Error_SourceIsDirectory_IsCollected", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		if err := os.MkdirAll(filepath.Join(tmp, "dir"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "dir"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		if len(res.Items) != 0 {
			t.Fatalf("Items len = %d (want 0)", len(res.Items))
		}
		if len(res.Errors) != 1 {
			t.Fatalf("Errors len = %d (want 1)", len(res.Errors))
		}

	})
})

var _ = ginkgo.Describe("TestPlanner_CreatePlan_Error_ExistingZeroSegments_IsCollected", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		importerWriteFile(t, tmp, "x.md", "# X")

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
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		if len(res.Items) != 0 {
			t.Fatalf("Items len = %d (want 0)", len(res.Items))
		}
		if len(res.Errors) != 1 {
			t.Fatalf("Errors len = %d (want 1)", len(res.Errors))
		}

	})
})

// ---- Title extraction -------------------------------------------------------

var _ = ginkgo.Describe("TestPlanner_CreatePlan_TitleExtractionError_AddsNote", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		abs := importerWriteFile(t, tmp, "unreadable.md", "# Title")

		// Make file unreadable to trigger extraction error
		if err := os.Chmod(abs, 0o000); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		ginkgo.DeferCleanup(os.Chmod, abs, os.FileMode(0o644))

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "unreadable.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		if len(res.Errors) != 0 {
			t.Fatalf("Errors = %#v", res.Errors)
		}
		if len(res.Items) != 1 {
			t.Fatalf("Items len = %d (want 1)", len(res.Items))
		}

		it := res.Items[0]
		if len(it.Notes) != 1 {
			t.Fatalf("Notes len = %d (want 1)", len(it.Notes))
		}
		if !strings.Contains(it.Notes[0], "Failed to load markdown file for title extraction") {
			t.Fatalf("Note = %q (should contain 'Failed to load markdown file for title extraction')", it.Notes[0])
		}
		// Title should still be set (fallback to filename)
		if it.Title != "unreadable" {
			t.Fatalf("Title = %q (want unreadable)", it.Title)
		}

	})
})

var _ = ginkgo.Describe("TestPlanner_analyzeEntry_NormalizesSourceDirSegments", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		// "My Guides/Intro.md" -> "my-guides/intro" via centralized SlugService creation normalization.
		tmp := t.TempDir()
		importerWriteFile(t, tmp, "My Guides/Intro.md", "# Intro")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "My Guides/Intro.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		if len(res.Errors) != 0 {
			t.Fatalf("Errors = %#v", res.Errors)
		}
		if res.Items[0].TargetPath != "docs/my-guides/intro" {
			t.Fatalf("TargetPath = %q (want docs/my-guides/intro)", res.Items[0].TargetPath)
		}

	})
})

var _ = ginkgo.Describe("TestPlanner_analyzeEntry_ReservedSlugSegmentsUseCentralizedSafeNormalization", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		importerWriteFile(t, tmp, "Reference/API.md", "# API")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "Reference/API.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		if len(res.Errors) != 0 {
			t.Fatalf("Errors = %#v", res.Errors)
		}
		if got := res.Items[0].TargetPath; got != "docs/reference/api-1" {
			t.Fatalf("TargetPath = %q (want docs/reference/api-1)", got)
		}
		if got := res.Items[0].DesiredSlug; got != "api-1" {
			t.Fatalf("DesiredSlug = %q (want api-1)", got)
		}

	})
})

var _ = ginkgo.Describe("TestPlanner_analyzeEntry_InvalidSourceDirSegment_ReturnsError", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		// Import path normalization still rejects segments that collapse to an empty slug.
		// A segment like "!!!" normalizes to "", so planning should report an error.
		tmp := t.TempDir()
		importerWriteFile(t, tmp, "!!!/a.md", "# A")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "!!!/a.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		if len(res.Items) != 0 {
			t.Fatalf("Items len = %d (want 0)", len(res.Items))
		}
		if len(res.Errors) != 1 {
			t.Fatalf("Errors len = %d (want 1)", len(res.Errors))
		}
		// optional: grobe Assertion, dass es ein Validate-Fehler ist
		if res.Errors[0] == "" {
			t.Fatalf("unexpected error: %v", res.Errors[0])
		}

	})
})

var _ = ginkgo.Describe("TestPlanner_CreatePlan_RootIndexMd_EmptyWikiPath_UsesFallbackTitle", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		// Test case for root-level index.md with empty TargetBasePath and markdown loading failure
		// When wikiPath is empty, path.Base("") returns ".", which is not meaningful.
		// The fix should use filename without extension as fallback.
		tmp := t.TempDir()
		abs := importerWriteFile(t, tmp, "index.md", "# Title")

		// Make file unreadable to trigger markdown loading failure
		if err := os.Chmod(abs, 0o000); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		ginkgo.DeferCleanup(os.Chmod, abs, os.FileMode(0o644))

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "index.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "", // empty target base path
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		if len(res.Errors) != 0 {
			t.Fatalf("Errors = %#v", res.Errors)
		}
		if len(res.Items) != 1 {
			t.Fatalf("Items len = %d (want 1)", len(res.Items))
		}

		it := res.Items[0]
		if it.TargetPath != "" {
			t.Fatalf("TargetPath = %q (want empty)", it.TargetPath)
		}
		if it.Kind != tree.NodeKindSection {
			t.Fatalf("Kind = %v (want Section)", it.Kind)
		}
		// The title should fallback to "index" (filename without .md), not "." from path.Base("")
		if it.Title != "index" {
			t.Fatalf("Title = %q (want index as fallback when wikiPath is empty and markdown fails)", it.Title)
		}
		// Should have a note about failed markdown loading
		if len(it.Notes) == 0 {
			t.Fatalf("Expected notes about failed markdown loading")
		}
		if !strings.Contains(it.Notes[0], "Failed to load markdown file for title extraction") {
			t.Fatalf("Note = %q (should contain 'Failed to load markdown file for title extraction')", it.Notes[0])
		}

	})
})

var _ = ginkgo.Describe("TestPlanner_CreatePlan_FolderIndexAndSiblingPage_MapToSectionAndNestedPage", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		importerWriteFile(t, tmp, "Ordner/index.md", `---
title: Ordner
---

# Ordner`)
		importerWriteFile(t, tmp, "Ordner/Ordner.md", "# Unterseite")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{
			{SourcePath: "Ordner/index.md"},
			{SourcePath: "Ordner/Ordner.md"},
		}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "",
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		if len(res.Errors) != 0 {
			t.Fatalf("Errors = %#v", res.Errors)
		}
		if len(res.Items) != 2 {
			t.Fatalf("Items len = %d (want 2)", len(res.Items))
		}

		section := res.Items[0]
		page := res.Items[1]

		if section.SourcePath != "Ordner/index.md" || section.Kind != tree.NodeKindSection || section.TargetPath != "ordner" {
			t.Fatalf("unexpected section item: %#v", section)
		}
		if page.SourcePath != "Ordner/Ordner.md" || page.Kind != tree.NodeKindPage || page.TargetPath != "ordner/ordner" {
			t.Fatalf("unexpected nested page item: %#v", page)
		}

	})
})

var _ = ginkgo.Describe("TestPlanner_CreatePlan_FolderMarkdownWithoutIndex_RemainsNestedPage", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		importerWriteFile(t, tmp, "Ordner/Ordner.md", "# Unterseite")

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "Ordner/Ordner.md"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "wiki",
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		if len(res.Errors) != 0 {
			t.Fatalf("Errors = %#v", res.Errors)
		}
		if len(res.Items) != 1 {
			t.Fatalf("Items len = %d (want 1)", len(res.Items))
		}

		it := res.Items[0]
		if it.Kind != tree.NodeKindPage {
			t.Fatalf("Kind = %v (want Page)", it.Kind)
		}
		if it.TargetPath != "wiki/ordner/ordner" {
			t.Fatalf("TargetPath = %q (want wiki/ordner/ordner)", it.TargetPath)
		}

	})
})

var _ = ginkgo.Describe("TestPlanner_CreatePlan_CreateNewSection_IndexUppercaseMD", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		importerWriteFile(t, tmp, "Guides/index.MD", `---
title: Guides
---

# Ignored`)

		wiki := &fakeWiki{treeHash: "h", lookups: map[string]*tree.PathLookup{}}
		p := newPlannerWithFake(wiki)

		res, err := p.CreatePlan([]ImportMDFile{{SourcePath: "Guides/index.MD"}}, PlanOptions{
			SourceBasePath: tmp,
			TargetBasePath: "docs",
		})
		if err != nil {
			t.Fatalf("CreatePlan err: %v", err)
		}
		it := res.Items[0]
		if it.Kind != tree.NodeKindSection {
			t.Fatalf("Kind = %v", it.Kind)
		}
		if it.TargetPath != "docs/guides" {
			t.Fatalf("TargetPath = %q (want docs/guides)", it.TargetPath)
		}

	})
})
