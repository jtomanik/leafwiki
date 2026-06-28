package importer

import (
	"github.com/perber/wiki/internal/core/tree"

	ginkgo "github.com/onsi/ginkgo/v2"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Reference-style link definitions are rewritten
// - Importer does not rewrite code examples
// - Importer preserves query and fragment while canonicalizing
// - Importer does not coerce assets with .md extension under asset namespaces

type contentTransformCase struct {
	name       string
	sourcePath string
	content    string
	want       string
}

var _ = ginkgo.Describe("TestContentTransformer_TransformContent_TableDriven", func() {
	for _, tt := range contentTransformCases() {
		tt := tt
		ginkgo.It(tt.name, func() {
			t := ginkgo.GinkgoT()
			tmp := t.TempDir()
			writeTmp(t, tmp, "Note.md", "# Note")
			writeTmp(t, tmp, "No)te.md", "# Note with paren")
			writeTmp(t, tmp, "Three laws of motion.md", "# Three laws")
			writeTmp(t, tmp, "foo/bar.md", "# Bar")
			writeTmp(t, tmp, "Document.pdf", "pdf-bytes")
			writeTmp(t, tmp, "Image.png", "png-bytes")
			writeTmp(t, tmp, "img.png", "png-bytes")
			writeTmp(t, tmp, "obsidian_repo.png", "png-bytes")
			writeTmp(t, tmp, "assets/manual.md", "markdown asset bytes")

			transformer := newContentTransformer(&PlanResult{
				Items: []PlanItem{
					{SourcePath: "Note.md", TargetPath: "note", Kind: tree.NodeKindPage},
					{SourcePath: "No)te.md", TargetPath: "no-te", Kind: tree.NodeKindPage},
					{SourcePath: "Three laws of motion.md", TargetPath: "three-laws-of-motion", Kind: tree.NodeKindPage},
					{SourcePath: "foo/bar.md", TargetPath: "foo/bar", Kind: tree.NodeKindPage},
				},
			}, tmp, 1234)

			page := &tree.Page{PageNode: &tree.PageNode{ID: "p1", Kind: tree.NodeKindPage}}
			wiki := &fakeExecWiki{}

			got, err := transformer.TransformContent(newFixtureUserID("editor"), newFixtureWorkspaceSourcePath(tt.sourcePath), page, tt.content, wiki)
			if err != nil {
				t.Fatalf("TransformContent err: %v", err)
			}
			if got != tt.want {
				t.Fatalf("TransformContent = %q, want %q", got, tt.want)
			}
		})
	}
})

func contentTransformCases() []contentTransformCase {
	return []contentTransformCase{
		{
			name:       "normal markdown link",
			sourcePath: "docs/current.md",
			content:    "[Note](../Note.md)",
			want:       "[Note](/note.md)",
		},
		{
			name:       "markdown link with title",
			sourcePath: "docs/current.md",
			content:    "[Note](../Note.md \"Tooltip\")",
			want:       "[Note](/note.md \"Tooltip\")",
		},
		{
			name:       "wiki link",
			sourcePath: "docs/current.md",
			content:    "[[Note]]",
			want:       "[Note](/note.md)",
		},
		{
			name:       "wiki link alias",
			sourcePath: "docs/current.md",
			content:    "[[Note|Alias]]",
			want:       "[Alias](/note.md)",
		},
		{
			name:       "wiki link anchor",
			sourcePath: "docs/current.md",
			content:    "[[Note#Heading]]",
			want:       "[Note](/note.md#Heading)",
		},
		{
			name:       "wiki link anchor alias",
			sourcePath: "docs/current.md",
			content:    "[[Note#Heading|Alias]]",
			want:       "[Alias](/note.md#Heading)",
		},
		{
			name:       "wiki link block reference",
			sourcePath: "docs/current.md",
			content:    "[[Note#^block-id]]",
			want:       "[Note](/note.md#^block-id)",
		},
		{
			name:       "unresolved wiki link falls back to dead markdown link",
			sourcePath: "docs/current.md",
			content:    "[[Missing Note]]",
			want:       "[Missing Note](/missing-note)",
		},
		{
			name:       "asset embed",
			sourcePath: "docs/current.md",
			content:    "![[../img.png]]",
			want:       "![img.png](/assets/p1/img.png)",
		},
		{
			name:       "non image wiki asset stays a link",
			sourcePath: "docs/current.md",
			content:    "[[../Document.pdf]]",
			want:       "[Document.pdf](/assets/p1/Document.pdf)",
		},
		{
			name:       "image wiki asset renders as image",
			sourcePath: "docs/current.md",
			content:    "[[../Image.png]]",
			want:       "![Image.png](/assets/p1/Image.png)",
		},
		{
			name:       "image wiki asset with underscore filename renders as image",
			sourcePath: "docs/current.md",
			content:    "![[../obsidian_repo.png]]",
			want:       "![obsidian_repo.png](/assets/p1/obsidian_repo.png)",
		},
		{
			name:       "relative markdown link",
			sourcePath: "docs/current.md",
			content:    "[Bar](../foo/bar.md)",
			want:       "[Bar](/foo/bar.md)",
		},
		{
			name:       "percent encoded markdown link",
			sourcePath: "docs/current.md",
			content:    "[Three laws](../Three%20laws%20of%20motion.md)",
			want:       "[Three laws](/three-laws-of-motion.md)",
		},
		{
			// - Importer preserves query and fragment while canonicalizing
			name:       "markdown link preserves query and fragment",
			sourcePath: "docs/current.md",
			content:    "[Note](../Note.md?mode=raw#part)",
			want:       "[Note](/note.md?mode=raw#part)",
		},
		{
			// - Importer does not coerce assets with .md extension under asset namespaces
			name:       "asset markdown path remains unchanged",
			sourcePath: "docs/current.md",
			content:    "[Manual](../assets/manual.md)",
			want:       "[Manual](../assets/manual.md)",
		},
		{
			name:       "markdown link with escaped bracket label",
			sourcePath: "docs/current.md",
			content:    "[No\\]te](../Note.md)",
			want:       "[No\\]te](/note.md)",
		},
		{
			name:       "markdown link with nested bracket label",
			sourcePath: "docs/current.md",
			content:    "[See [Note]](../Note.md)",
			want:       "[See [Note]](/note.md)",
		},
		{
			name:       "markdown link with escaped closing paren in destination",
			sourcePath: "docs/current.md",
			content:    "[Note](../No\\)te.md)\n[Real](../Note.md)",
			want:       "[Note](/no-te.md)\n[Real](/note.md)",
		},
		{
			name:       "escaped literal markdown link stays unchanged",
			sourcePath: "docs/current.md",
			content:    "\\[Note](../Note.md)\n[Real](../Note.md)",
			want:       "\\[Note](../Note.md)\n[Real](/note.md)",
		},
		{
			name:       "pseudo markdown links with whitespace before destination stay unchanged",
			sourcePath: "docs/current.md",
			content:    "[Space] (../Note.md)\n[Newline]\n(../Note.md)\n[Real](../Note.md)",
			want:       "[Space] (../Note.md)\n[Newline]\n(../Note.md)\n[Real](/note.md)",
		},
		{
			name:       "link-like title text stays unchanged",
			sourcePath: "docs/current.md",
			content:    "[Outer](../foo/bar.md \"[Inner](../Note.md)\")",
			want:       "[Outer](/foo/bar.md \"[Inner](../Note.md)\")",
		},
		{
			name:       "scheme relative url stays external",
			sourcePath: "docs/current.md",
			content:    "[CDN](//example.com/assets/note.md)",
			want:       "[CDN](//example.com/assets/note.md)",
		},
		{
			name:       "inline code stays unchanged",
			sourcePath: "docs/current.md",
			content:    "`[[Note]]`",
			want:       "`[[Note]]`",
		},
		{
			name:       "fenced code stays unchanged",
			sourcePath: "docs/current.md",
			content:    "```md\n[x](../Note.md)\n```",
			want:       "```md\n[x](../Note.md)\n```",
		},
		{
			// - Importer does not rewrite code examples
			name:       "indented code stays unchanged",
			sourcePath: "docs/current.md",
			content:    "Example:\n\n    [x](../Note.md)\n\t[[Note]]\n\n[Note](../Note.md)\n[[Note]]",
			want:       "Example:\n\n    [x](../Note.md)\n\t[[Note]]\n\n[Note](/note.md)\n[Note](/note.md)",
		},
		{
			name:       "nested list links are rewritten",
			sourcePath: "docs/current.md",
			content:    "- parent\n    - [Note](../Note.md)\n    - [[Note]]",
			want:       "- parent\n    - [Note](/note.md)\n    - [Note](/note.md)",
		},
	}
}

var _ = ginkgo.Describe("TestContentTransformer_EmitsMdForPagesAndExtensionlessForSections", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "current.md", "# Current")
		writeTmp(t, tmp, "Note.md", "# Note")
		writeTmp(t, tmp, "Guide/index.md", "# Guide")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: "current.md", TargetPath: "current", Kind: tree.NodeKindPage},
				{SourcePath: "Note.md", TargetPath: "note", Kind: tree.NodeKindPage},
				{SourcePath: "Guide/index.md", TargetPath: "guide", Kind: tree.NodeKindSection},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: "p1", Kind: tree.NodeKindPage}}
		content := "[Note](./Note.md)\n[Guide](./Guide/)"
		got, err := transformer.TransformContent("editor", "current.md", page, content, &fakeExecWiki{})
		if err != nil {
			t.Fatalf("TransformContent err: %v", err)
		}

		want := "[Note](/note.md)\n[Guide](/guide)"
		if got != want {
			t.Fatalf("TransformContent = %q, want %q", got, want)
		}

	})
})

var _ = ginkgo.Describe("TestContentTransformer_UsesMarkdownLinkRootPrefixForGeneratedPageLinks", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "Note.md", "# Note")
		transformer := newContentTransformerWithOptions(&PlanResult{
			Items: []PlanItem{
				{SourcePath: "Note.md", TargetPath: "note", Kind: tree.NodeKindPage},
			},
		}, tmp, 1234, ContentTransformerOptions{MarkdownLinkRootPrefix: "/docs"})

		page := &tree.Page{PageNode: &tree.PageNode{ID: "p1", Kind: tree.NodeKindPage}}
		got, err := transformer.TransformContent("editor", "current.md", page, "[Note](./Note.md)", &fakeExecWiki{})
		if err != nil {
			t.Fatalf("TransformContent: %v", err)
		}
		if got != "[Note](/docs/note.md)" {
			t.Fatalf("content = %q, want prefixed generated link", got)
		}

	})
})

var _ = ginkgo.Describe("TestContentTransformer_SourcePathMatchingIsCaseSensitive", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "current.md", "# Current")
		writeTmp(t, tmp, "Guide.md", "# Guide")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: "current.md", TargetPath: "current", Kind: tree.NodeKindPage},
				{SourcePath: "Guide.md", TargetPath: "guide", Kind: tree.NodeKindPage},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: "p1", Kind: tree.NodeKindPage}}
		content := "[Exact](./Guide.md)\n[Mismatch](./guide.md)"
		got, err := transformer.TransformContent("editor", "current.md", page, content, &fakeExecWiki{})
		if err != nil {
			t.Fatalf("TransformContent err: %v", err)
		}

		want := "[Exact](/guide.md)\n[Mismatch](./guide.md)"
		if got != want {
			t.Fatalf("TransformContent = %q, want %q", got, want)
		}

	})
})

var _ = ginkgo.Describe("TestContentTransformer_SourcePathSuffixMatchingIsCaseSensitive", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "current.md", "# Current")
		writeTmp(t, tmp, "Reference/Endpoints.md", "# Endpoints")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: "current.md", TargetPath: "current", Kind: tree.NodeKindPage},
				{SourcePath: "Reference/Endpoints.md", TargetPath: "reference/endpoints", Kind: tree.NodeKindPage},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: "p1", Kind: tree.NodeKindPage}}
		content := "[Exact](Reference/Endpoints.md)\n[Mismatch](/reference/endpoints)\n[[Reference/Endpoints|Exact Wiki]]\n[[reference/endpoints|Mismatch Wiki]]"
		got, err := transformer.TransformContent("editor", "current.md", page, content, &fakeExecWiki{})
		if err != nil {
			t.Fatalf("TransformContent err: %v", err)
		}

		want := "[Exact](/reference/endpoints.md)\n[Mismatch](/reference/endpoints)\n[Exact Wiki](/reference/endpoints.md)\n[[reference/endpoints|Mismatch Wiki]]"
		if got != want {
			t.Fatalf("TransformContent = %q, want %q", got, want)
		}

	})
})

var _ = ginkgo.Describe("TestContentTransformer_BasenameWikiLinkMatchingIsCaseSensitive", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "current.md", "# Current")
		writeTmp(t, tmp, "Guide.md", "# Guide")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: "current.md", TargetPath: "current", Kind: tree.NodeKindPage},
				{SourcePath: "Guide.md", TargetPath: "guide", Kind: tree.NodeKindPage},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: "p1", Kind: tree.NodeKindPage}}
		content := "[[Guide]]\n[[guide]]"
		got, err := transformer.TransformContent("editor", "current.md", page, content, &fakeExecWiki{})
		if err != nil {
			t.Fatalf("TransformContent err: %v", err)
		}

		want := "[Guide](/guide.md)\n[[guide]]"
		if got != want {
			t.Fatalf("TransformContent = %q, want %q", got, want)
		}

	})
})

var _ = ginkgo.Describe("TestContentTransformer_DoesNotResolveArbitraryMarkdownPageLinkToSection", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "current.md", "# Current")
		writeTmp(t, tmp, "Guide/index.md", "# Guide")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: "current.md", TargetPath: "current", Kind: tree.NodeKindPage},
				{SourcePath: "Guide/index.md", TargetPath: "guide", Kind: tree.NodeKindSection},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: "p1", Kind: tree.NodeKindPage}}
		content := "[Guide page](./Guide.md)\n[Guide index](./Guide/index.md)"
		got, err := transformer.TransformContent("editor", "current.md", page, content, &fakeExecWiki{})
		if err != nil {
			t.Fatalf("TransformContent err: %v", err)
		}

		want := "[Guide page](./Guide.md)\n[Guide index](/guide)"
		if got != want {
			t.Fatalf("TransformContent = %q, want %q", got, want)
		}

	})
})

var _ = ginkgo.Describe("TestContentTransformer_FormatsSameRoutePageAndSectionTargetsByMatchedKind", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "current.md", "# Current")
		writeTmp(t, tmp, "Guide.md", "# Guide page")
		writeTmp(t, tmp, "Guide/index.md", "# Guide section")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: "current.md", TargetPath: "current", Kind: tree.NodeKindPage},
				{SourcePath: "Guide.md", TargetPath: "guide", Kind: tree.NodeKindPage},
				{SourcePath: "Guide/index.md", TargetPath: "guide", Kind: tree.NodeKindSection},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: "p1", Kind: tree.NodeKindPage}}
		content := "[Guide page](./Guide.md)\n[Guide section](./Guide/index.md)"
		got, err := transformer.TransformContent("editor", "current.md", page, content, &fakeExecWiki{})
		if err != nil {
			t.Fatalf("TransformContent err: %v", err)
		}

		want := "[Guide page](/guide.md)\n[Guide section](/guide)"
		if got != want {
			t.Fatalf("TransformContent = %q, want %q", got, want)
		}

	})
})

var _ = ginkgo.Describe("TestContentTransformer_FormatsSameRouteSuffixFallbackByRequestedKind", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "current.md", "# Current")
		writeTmp(t, tmp, "Tools/Kubernetes/Resources/Guide.md", "# Guide page")
		writeTmp(t, tmp, "Tools/Kubernetes/Resources/Guide/index.md", "# Guide section")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: "current.md", TargetPath: "current", Kind: tree.NodeKindPage},
				{SourcePath: "Tools/Kubernetes/Resources/Guide.md", TargetPath: "knowledge-main/tools/kubernetes/resources/guide", Kind: tree.NodeKindPage},
				{SourcePath: "Tools/Kubernetes/Resources/Guide/index.md", TargetPath: "knowledge-main/tools/kubernetes/resources/guide", Kind: tree.NodeKindSection},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: "p1", Kind: tree.NodeKindPage}}
		content := "[[Tools/Kubernetes/Resources/Guide.md]]"
		got, err := transformer.TransformContent("editor", "current.md", page, content, &fakeExecWiki{})
		if err != nil {
			t.Fatalf("TransformContent err: %v", err)
		}

		want := "[Guide](/knowledge-main/tools/kubernetes/resources/guide.md)"
		if got != want {
			t.Fatalf("TransformContent = %q, want %q", got, want)
		}

	})
})

var _ = ginkgo.Describe("TestContentTransformer_ResolvesReadmeFallbackFolderLinks", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "current.md", "# Current")
		writeTmp(t, tmp, "Guides/README.md", "# Guides")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: "current.md", TargetPath: "current", Kind: tree.NodeKindPage},
				{SourcePath: "Guides/README.md", TargetPath: "guides", Kind: tree.NodeKindSection},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: "p1", Kind: tree.NodeKindPage}}
		content := "[Guide folder](./Guides/)\n[Guide path](./Guides)\n[guide-ref]: ./Guides"
		got, err := transformer.TransformContent("editor", "current.md", page, content, &fakeExecWiki{})
		if err != nil {
			t.Fatalf("TransformContent err: %v", err)
		}

		want := "[Guide folder](/guides)\n[Guide path](/guides)\n[guide-ref]: /guides"
		if got != want {
			t.Fatalf("TransformContent = %q, want %q", got, want)
		}

	})
})

// - Reference-style link definitions are rewritten
var _ = ginkgo.Describe("TestContentTransformer_RewritesReferenceDefinitions", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		writeTmp(t, tmp, "current.md", "# Current")
		writeTmp(t, tmp, "Note.md", "# Note")
		writeTmp(t, tmp, "Photo.png", "png-bytes")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: "current.md", TargetPath: "current", Kind: tree.NodeKindPage},
				{SourcePath: "Note.md", TargetPath: "note", Kind: tree.NodeKindPage},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: "p1", Kind: tree.NodeKindPage}}
		content := "[Note][note-ref]\n\n[note-ref]: <./Note.md> \"Note title\""
		got, err := transformer.TransformContent("editor", "current.md", page, content, &fakeExecWiki{})
		if err != nil {
			t.Fatalf("TransformContent err: %v", err)
		}

		want := "[Note][note-ref]\n\n[note-ref]: </note.md> \"Note title\""
		if got != want {
			t.Fatalf("TransformContent = %q, want %q", got, want)
		}

		imageOnlyContent := "![Note][note-ref]\n![Photo][photo-ref]\n\n[note-ref]: <./Note.md> \"Note title\"\n[photo-ref]: ./Photo.png"
		got, err = transformer.TransformContent("editor", "current.md", page, imageOnlyContent, &fakeExecWiki{})
		if err != nil {
			t.Fatalf("TransformContent image refs err: %v", err)
		}

		imageOnlyWant := "![Note][note-ref]\n![Photo][photo-ref]\n\n[note-ref]: <./Note.md> \"Note title\"\n[photo-ref]: /assets/p1/Photo.png"
		if got != imageOnlyWant {
			t.Fatalf("TransformContent image refs = %q, want %q", got, imageOnlyWant)
		}

	})
})
