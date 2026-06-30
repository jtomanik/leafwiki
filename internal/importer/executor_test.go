package importer

import (
	"errors"
	"log/slog"
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

func writeTmp(t importerTestT, dir, rel, content string) {
	t.Helper()
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

var _ = ginkgo.Describe("TestExecutor_StalePlan", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		w := &fakeExecWiki{hash: "new"}
		plan := &PlanResult{TreeHash: "old"}
		opts := &PlanOptions{SourceBasePath: t.TempDir()}
		ex := NewExecutor(plan, opts, 0, w, slog.Default())

		got, err := ex.Execute(newFixtureUserID("user1"))
		if err == nil {
			t.Fatalf("expected stale plan error")
		}
		if got != nil {
			t.Fatalf("expected nil result on stale plan, got %#v", got)
		}
		if !errors.Is(err, ErrImportPlanStale) {
			t.Fatalf("unexpected stale plan error: %v", err)
		}

	})
})

var _ = ginkgo.Describe("TestExecutor_Create_HappyPath_PreservesNonInternalFrontmatter", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "a.md", "---\naliases:\n  - x\ncustom_key: keep-me\nleafwiki_id: source-id\nleafwiki_title: Source Title\ntitle: X\n---\n\n# Heading\nBody")

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
		if err != nil {
			t.Fatalf("Execute err: %v", err)
		}

		if res.ImportedCount != 1 || res.SkippedCount != 0 {
			t.Fatalf("counts imported=%d skipped=%d", res.ImportedCount, res.SkippedCount)
		}
		if len(res.Items) != 1 || res.Items[0].Action != ExecutionActionCreated {
			t.Fatalf("item result: %#v", res.Items)
		}
		if w.ensureCalls != 1 || w.updateCalls != 1 {
			t.Fatalf("calls ensure=%d update=%d", w.ensureCalls, w.updateCalls)
		}

		if w.lastUpdatedContent == nil {
			t.Fatalf("expected content to be passed to UpdatePage")
		}
		raw := *w.lastUpdatedContent
		if !strings.HasPrefix(raw, "<!-- leafwiki\n") {
			t.Fatalf("expected canonical LeafWiki metadata comment, got: %q", raw)
		}
		if strings.HasPrefix(raw, "---\n") {
			t.Fatalf("expected importer output not to use legacy YAML frontmatter, got: %q", raw)
		}
		doc, _, err := markdown.ParsePageDocument(raw)
		if err != nil {
			t.Fatalf("ParsePageDocument err: %v", err)
		}
		if doc.Body != "\n# Heading\nBody" {
			t.Fatalf("unexpected body: %q", doc.Body)
		}
		if got := doc.Metadata.Fields["custom_key"]; got != "keep-me" {
			t.Fatalf("expected custom_key to be preserved, got %#v", got)
		}
		if got := doc.Metadata.Extra["title"]; got != nil {
			t.Fatalf("expected title alias to be consumed during metadata migration, got %#v", got)
		}
		aliases, ok := doc.Metadata.Extra["aliases"].([]interface{})
		if !ok || len(aliases) != 1 || aliases[0] != "x" {
			t.Fatalf("expected aliases to be preserved, got %#v", doc.Metadata.Extra["aliases"])
		}
		if strings.Contains(raw, "leafwiki_id: source-id") {
			t.Fatalf("expected source leafwiki_id to be dropped, got: %q", raw)
		}
		if strings.Contains(raw, "leafwiki_title: Source Title") {
			t.Fatalf("expected source leafwiki_title to be dropped, got: %q", raw)
		}

		if res.TreeHashBefore != "h1" {
			t.Fatalf("TreeHashBefore = %q", res.TreeHashBefore)
		}
		if res.TreeHash == "h1" {
			t.Fatalf("expected TreeHash to change (fake changes it), got %q", res.TreeHash)
		}

	})
})

var _ = ginkgo.Describe("TestExecutor_Create_HappyPath_PreservesDistinctExtraFieldValues", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "a.md", "---\nalpha: first\nbeta: second\nnested:\n  key: value\n---\n\nBody")

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
		if _, err := ex.Execute(newFixtureUserID("user1")); err != nil {
			t.Fatalf("Execute err: %v", err)
		}

		if w.lastUpdatedContent == nil {
			t.Fatalf("expected content to be passed to UpdatePage")
		}

		fm, _, has, err := markdown.ParseFrontmatter(*w.lastUpdatedContent)
		if err != nil {
			t.Fatalf("ParseFrontmatter err: %v", err)
		}
		if !has {
			t.Fatalf("expected frontmatter, got %q", *w.lastUpdatedContent)
		}
		if got := fm.ExtraFields["alpha"]; got != "first" {
			t.Fatalf("expected alpha=first, got %#v", got)
		}
		if got := fm.ExtraFields["beta"]; got != "second" {
			t.Fatalf("expected beta=second, got %#v", got)
		}
		nested, ok := fm.ExtraFields["nested"].(map[string]interface{})
		if !ok || nested["key"] != "value" {
			t.Fatalf("expected nested map to be preserved, got %#v", fm.ExtraFields["nested"])
		}

	})
})

var _ = ginkgo.Describe("TestExecutor_Skip_DoesNotCallWiki", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
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
		if err != nil {
			t.Fatalf("Execute err: %v", err)
		}

		if res.SkippedCount != 1 || res.ImportedCount != 0 {
			t.Fatalf("counts imported=%d skipped=%d", res.ImportedCount, res.SkippedCount)
		}
		if w.ensureCalls != 0 || w.updateCalls != 0 {
			t.Fatalf("expected no wiki calls, got ensure=%d update=%d", w.ensureCalls, w.updateCalls)
		}

	})
})

var _ = ginkgo.Describe("TestExecutor_Create_EnsurePathError_SkipsItem", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "a.md", "Body")

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
		if err != nil {
			t.Fatalf("Execute err: %v", err)
		}
		if res.SkippedCount != 1 || res.ImportedCount != 0 {
			t.Fatalf("counts imported=%d skipped=%d", res.ImportedCount, res.SkippedCount)
		}
		if res.Items[0].Error == nil || *res.Items[0].Error == "" {
			t.Fatalf("expected error message")
		}
		if w.updateCalls != 0 {
			t.Fatalf("UpdatePage should not be called")
		}

	})
})

var _ = ginkgo.Describe("TestExecutor_UnknownAction_SkipsItem", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
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
		if err != nil {
			t.Fatalf("Execute err: %v", err)
		}
		if res.SkippedCount != 1 {
			t.Fatalf("SkippedCount=%d", res.SkippedCount)
		}
		if res.Items[0].Error == nil || *res.Items[0].Error != "unknown action" {
			t.Fatalf("Error=%#v", res.Items[0].Error)
		}

	})
})

var _ = ginkgo.Describe("TestExecutor_Create_FolderIndexAndSiblingPage_ImportsSectionThenNestedPage", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "Ordner/index.md", `---
title: Ordner
---

# Ordner`)
		writeTmp(t, tmp, "Ordner/Ordner.md", "# Unterseite")

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
		if err != nil {
			t.Fatalf("Execute err: %v", err)
		}
		if res.ImportedCount != 2 || res.SkippedCount != 0 {
			t.Fatalf("counts imported=%d skipped=%d", res.ImportedCount, res.SkippedCount)
		}
		if w.ensureCalls != 2 || w.updateCalls != 2 {
			t.Fatalf("calls ensure=%d update=%d", w.ensureCalls, w.updateCalls)
		}
		if len(w.ensureTargets) != 2 || w.ensureTargets[0] != "ordner" || w.ensureTargets[1] != "ordner/ordner" {
			t.Fatalf("unexpected ensure targets: %#v", w.ensureTargets)
		}
		if len(w.ensureKinds) != 2 || w.ensureKinds[0] != tree.NodeKindSection || w.ensureKinds[1] != tree.NodeKindPage {
			t.Fatalf("unexpected ensure kinds: %#v", w.ensureKinds)
		}
		if len(w.updateTitles) != 2 || w.updateTitles[0] != "Ordner" || w.updateTitles[1] != "Unterseite" {
			t.Fatalf("unexpected update titles: %#v", w.updateTitles)
		}

	})
})

// - Importer migrates old route-style page link to .md
var _ = ginkgo.Describe("TestExecutor_Create_RewritesMarkdownAndWikiLinksToImportedPages", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "Guides/index.md", "# Guides")
		writeTmp(t, tmp, "Guides/Setup.md", strings.Join([]string{
			"# Setup",
			"",
			"[Relative](../Reference/Endpoints.md)",
			"[Absolute](/Guides/index.md)",
			"[RouteStyle](/Reference/Endpoints)",
			"[Container](/Guides/)",
			"[[Reference/Endpoints|API Alias]]",
		}, "\n"))
		writeTmp(t, tmp, "Reference/Endpoints.md", "# Endpoints")

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
		if _, err := ex.Execute(newFixtureUserID("user1")); err != nil {
			t.Fatalf("Execute err: %v", err)
		}

		setupContent, ok := updatedContentByTitle["Setup"]
		if !ok {
			t.Fatalf("expected setup content to be updated")
		}

		for _, expected := range []string{
			"[Relative](/reference/endpoints.md)",
			"[Absolute](/guides)",
			"[RouteStyle](/reference/endpoints.md)",
			"[Container](/guides)",
			"[API Alias](/reference/endpoints.md)",
		} {
			if !strings.Contains(setupContent, expected) {
				t.Fatalf("expected rewritten content to contain %q, got:\n%s", expected, setupContent)
			}
		}

	})
})

var _ = ginkgo.Describe("TestExecutor_Create_RewritesBodyLinksWithoutTouchingMetadataValues", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "Guides/Setup.md", strings.Join([]string{
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
		writeTmp(t, tmp, "Reference/Endpoints.md", "# Endpoints")

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
		if _, err := ex.Execute(newFixtureUserID("user1")); err != nil {
			t.Fatalf("Execute err: %v", err)
		}
		setupContent, ok := updatedContentByTitle["Setup"]
		if !ok {
			t.Fatalf("expected setup content to be updated")
		}
		doc, _, err := markdown.ParsePageDocument(setupContent)
		if err != nil {
			t.Fatalf("ParsePageDocument err: %v", err)
		}
		if got := doc.Metadata.Fields["custom_link"]; got != "[Endpoint](/Reference/Endpoints)" {
			t.Fatalf("metadata field link was rewritten: %#v", got)
		}
		nested, ok := doc.Metadata.Extra["nested"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected nested extra metadata, got %#v", doc.Metadata.Extra["nested"])
		}
		if got := nested["link"]; got != "[Endpoint](/Reference/Endpoints)" {
			t.Fatalf("metadata extra link was rewritten: %#v", got)
		}
		if !strings.Contains(doc.Body, "[RouteStyle](/reference/endpoints.md)") {
			t.Fatalf("expected body link to be rewritten, got:\n%s", doc.Body)
		}

	})
})

var _ = ginkgo.Describe("TestExecutor_Create_UploadsRelativeAndRootAssets", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "Guides/Setup.md", strings.Join([]string{
			"# Setup",
			"",
			"![Relative](./images/logo.png)",
			"[Asset](/shared/manual.pdf)",
			"![[./images/logo.png]]",
		}, "\n"))
		writeTmp(t, tmp, "Guides/images/logo.png", "png-bytes")
		writeTmp(t, tmp, "shared/manual.pdf", "pdf-bytes")

		w := &fakeExecWiki{hash: "h1"}
		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: "Guides/Setup.md", TargetPath: "guides/setup", Title: "Setup", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 1234, w, slog.Default())
		if _, err := ex.Execute(newFixtureUserID("user1")); err != nil {
			t.Fatalf("Execute err: %v", err)
		}

		if w.uploadCalls != 2 {
			t.Fatalf("expected 2 asset uploads, got %d", w.uploadCalls)
		}
		if w.lastUpdatedContent == nil {
			t.Fatalf("expected content to be updated")
		}
		if !strings.Contains(*w.lastUpdatedContent, "![Relative](/assets/p1/logo.png)") {
			t.Fatalf("expected relative asset link rewrite, got:\n%s", *w.lastUpdatedContent)
		}
		if !strings.Contains(*w.lastUpdatedContent, "[Asset](/assets/p1/manual.pdf)") {
			t.Fatalf("expected root asset link rewrite, got:\n%s", *w.lastUpdatedContent)
		}
		if !strings.Contains(*w.lastUpdatedContent, "![logo.png](/assets/p1/logo.png)") {
			t.Fatalf("expected wiki asset link rewrite, got:\n%s", *w.lastUpdatedContent)
		}
		if w.lastUploadByteCap != 1234 {
			t.Fatalf("expected asset uploads to use configured max bytes, got %d", w.lastUploadByteCap)
		}

	})
})

var _ = ginkgo.Describe("TestExecutor_Create_WikiLinkToNonImageAssetStaysNormalLink", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "Guides/Setup.md", strings.Join([]string{
			"# Setup",
			"",
			"[[../shared/manual.pdf]]",
			"![[../shared/manual.pdf]]",
		}, "\n"))
		writeTmp(t, tmp, "shared/manual.pdf", "pdf-bytes")

		w := &fakeExecWiki{hash: "h1"}
		plan := &PlanResult{
			TreeHash: "h1",
			Items: []PlanItem{
				{SourcePath: "Guides/Setup.md", TargetPath: "guides/setup", Title: "Setup", Kind: tree.NodeKindPage, Action: PlanActionCreate},
			},
		}
		opts := &PlanOptions{SourceBasePath: tmp}

		ex := NewExecutor(plan, opts, 0, w, slog.Default())
		if _, err := ex.Execute(newFixtureUserID("user1")); err != nil {
			t.Fatalf("Execute err: %v", err)
		}

		if w.lastUpdatedContent == nil {
			t.Fatalf("expected content to be updated")
		}
		if !strings.Contains(*w.lastUpdatedContent, "[manual.pdf](/assets/p1/manual.pdf)") {
			t.Fatalf("expected non-embed wiki asset to stay a normal link, got:\n%s", *w.lastUpdatedContent)
		}
		if !strings.Contains(*w.lastUpdatedContent, "![manual.pdf](/assets/p1/manual.pdf)") {
			t.Fatalf("expected embed wiki asset to use embed syntax, got:\n%s", *w.lastUpdatedContent)
		}

	})
})

var _ = ginkgo.Describe("TestExecutor_Create_WikiLinkFallsBackToUniqueNestedBasenameOnly", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "Home.md", strings.Join([]string{
			"# Home",
			"",
			"[[Brainstorm]]",
			"[[Meeting Notes]]",
		}, "\n"))
		writeTmp(t, tmp, "Daily/Brainstorm.md", "# Brainstorm")
		writeTmp(t, tmp, "Daily/Meeting Notes.md", "# Daily Meeting Notes")
		writeTmp(t, tmp, "Archive/Meeting Notes.md", "# Archived Meeting Notes")

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
		if _, err := ex.Execute(newFixtureUserID("user1")); err != nil {
			t.Fatalf("Execute err: %v", err)
		}

		homeContent := updatedContentByTitle["Home"]
		if !strings.Contains(homeContent, "[Brainstorm](/daily/brainstorm.md)") {
			t.Fatalf("expected unique basename wiki link rewrite, got:\n%s", homeContent)
		}
		if !strings.Contains(homeContent, "[[Meeting Notes]]") {
			t.Fatalf("expected ambiguous basename wiki link to stay unchanged, got:\n%s", homeContent)
		}

	})
})

var _ = ginkgo.Describe("TestExecutor_Create_WikiLinkResolvesUniqueNestedPathSuffix", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "knowledge-main/tools/kubernetes/resources/StatefulSet.md", strings.Join([]string{
			"# StatefulSet",
			"",
			"[[tools/kubernetes/resources/Deployment|Deployment]]",
		}, "\n"))
		writeTmp(t, tmp, "knowledge-main/tools/kubernetes/resources/Deployment.md", "# Deployment")

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
		if _, err := ex.Execute(newFixtureUserID("user1")); err != nil {
			t.Fatalf("Execute err: %v", err)
		}

		statefulSetContent := updatedContentByTitle["StatefulSet"]
		if !strings.Contains(statefulSetContent, "[Deployment](/knowledge-main/tools/kubernetes/resources/deployment.md)") {
			t.Fatalf("expected unique nested path suffix wiki link rewrite, got:\n%s", statefulSetContent)
		}

	})
})

var _ = ginkgo.Describe("TestExecutor_Create_UnresolvedWikiLinkFallsBackToDeadMarkdownLink", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "Home.md", strings.Join([]string{
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
		if _, err := ex.Execute(newFixtureUserID("user1")); err != nil {
			t.Fatalf("Execute err: %v", err)
		}

		homeContent := updatedContentByTitle["Home"]
		if !strings.Contains(homeContent, "[Missing Note](/missing-note)") {
			t.Fatalf("expected unresolved wiki link fallback to dead markdown link, got:\n%s", homeContent)
		}

	})
})

var _ = ginkgo.Describe("TestExecutor_Create_DoesNotRewriteLinksInsideCode", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "Guides/Setup.md", strings.Join([]string{
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
		writeTmp(t, tmp, "Reference/Endpoints.md", "# Endpoints")

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
		if _, err := ex.Execute(newFixtureUserID("user1")); err != nil {
			t.Fatalf("Execute err: %v", err)
		}

		setupContent := updatedContentByTitle["Setup"]
		for _, expected := range []string{
			"`[Inline](../Reference/Endpoints.md)`",
			"[Fence](../Reference/Endpoints.md)",
			"[[Reference/Endpoints|Fence Alias]]",
			"[Real](/reference/endpoints.md)",
			"[Real Alias](/reference/endpoints.md)",
		} {
			if !strings.Contains(setupContent, expected) {
				t.Fatalf("expected content to contain %q, got:\n%s", expected, setupContent)
			}
		}

	})
})

var _ = ginkgo.Describe("TestExecutor_Create_RewritesWindowsStyleMarkdownAndAssetPaths", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "Guides/Setup.md", strings.Join([]string{
			"# Setup",
			"",
			"[Doc](..\\Reference\\Endpoints.md)",
			"![Diagram](images\\diagram.png)",
		}, "\n"))
		writeTmp(t, tmp, "Reference/Endpoints.md", "# Endpoints")
		writeTmp(t, tmp, "Guides/images/diagram.png", "png-bytes")

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
		if _, err := ex.Execute(newFixtureUserID("user1")); err != nil {
			t.Fatalf("Execute err: %v", err)
		}

		setupContent := updatedContentByTitle["Setup"]
		for _, expected := range []string{
			"[Doc](/reference/endpoints.md)",
			"![Diagram](/assets/p1/diagram.png)",
		} {
			if !strings.Contains(setupContent, expected) {
				t.Fatalf("expected content to contain %q, got:\n%s", expected, setupContent)
			}
		}

	})
})

var _ = ginkgo.Describe("TestExecutor_Create_LeavesWindowsDriveLetterPathsUntouched", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "Guides/Setup.md", strings.Join([]string{
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
		if _, err := ex.Execute(newFixtureUserID("user1")); err != nil {
			t.Fatalf("Execute err: %v", err)
		}

		if w.lastUpdatedContent == nil {
			t.Fatalf("expected updated content")
		}
		for _, expected := range []string{
			"[Windows File](C:\\Users\\John\\Notes\\Endpoints.md)",
			"![Windows Image](C:\\Users\\John\\Images\\diagram.png)",
		} {
			if !strings.Contains(*w.lastUpdatedContent, expected) {
				t.Fatalf("expected content to keep %q untouched, got:\n%s", expected, *w.lastUpdatedContent)
			}
		}

	})
})
