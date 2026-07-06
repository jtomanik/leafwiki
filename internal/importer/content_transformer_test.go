package importer

import (
	"github.com/perber/wiki/internal/core/tree"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Reference-style link definitions are rewritten
// - Importer does not rewrite code examples
// - Importer preserves query and fragment while canonicalizing
// - Importer does not coerce assets with .md extension under asset namespaces

type contentTransformCase struct {
	name       string
	sourcePath tree.WorkspaceSourcePath
	content    string
	want       string
}

var _ = ginkgo.Describe("import content link rewriting", ginkgo.Label("unit"), func() {
	for _, tt := range contentTransformCases() {
		tt := tt
		ginkgo.It(tt.name, func() {
			tmp := importerTempDir()
			writeTmp(tmp, "Note.md", "# Note")
			writeTmp(tmp, "No)te.md", "# Note with paren")
			writeTmp(tmp, "Three laws of motion.md", "# Three laws")
			writeTmp(tmp, "foo/bar.md", "# Bar")
			writeTmp(tmp, "Document.pdf", "pdf-bytes")
			writeTmp(tmp, "Image.png", "png-bytes")
			writeTmp(tmp, "img.png", "png-bytes")
			writeTmp(tmp, "obsidian_repo.png", "png-bytes")
			writeTmp(tmp, "assets/manual.md", "markdown asset bytes")

			transformer := newContentTransformer(&PlanResult{
				Items: []PlanItem{
					{SourcePath: newFixtureWorkspaceSourcePath("Note.md"), TargetPath: newFixtureRoutePath("note"), Kind: tree.NodeKindPage},
					{SourcePath: newFixtureWorkspaceSourcePath("No)te.md"), TargetPath: newFixtureRoutePath("no-te"), Kind: tree.NodeKindPage},
					{SourcePath: newFixtureWorkspaceSourcePath("Three laws of motion.md"), TargetPath: newFixtureRoutePath("three-laws-of-motion"), Kind: tree.NodeKindPage},
					{SourcePath: newFixtureWorkspaceSourcePath("foo/bar.md"), TargetPath: newFixtureRoutePath("foo/bar"), Kind: tree.NodeKindPage},
				},
			}, tmp, 1234)

			page := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("p1"), Kind: tree.NodeKindPage}}
			wiki := &fakeExecWiki{}

			got, err := transformer.TransformContent(newFixtureUserID("editor"), tt.sourcePath, page, tt.content, wiki)
			Expect(err).To(Succeed())
			Expect(got).To(Equal(tt.want))
		})
	}
})

func contentTransformCases() []contentTransformCase {
	currentSourcePath := newFixtureWorkspaceSourcePath("docs/current.md")
	return []contentTransformCase{
		{
			name:       "normal markdown link",
			sourcePath: currentSourcePath,
			content:    "[Note](../Note.md)",
			want:       "[Note](/note.md)",
		},
		{
			name:       "markdown link with title",
			sourcePath: currentSourcePath,
			content:    "[Note](../Note.md \"Tooltip\")",
			want:       "[Note](/note.md \"Tooltip\")",
		},
		{
			name:       "wiki link",
			sourcePath: currentSourcePath,
			content:    "[[Note]]",
			want:       "[Note](/note.md)",
		},
		{
			name:       "wiki link alias",
			sourcePath: currentSourcePath,
			content:    "[[Note|Alias]]",
			want:       "[Alias](/note.md)",
		},
		{
			name:       "wiki link anchor",
			sourcePath: currentSourcePath,
			content:    "[[Note#Heading]]",
			want:       "[Note](/note.md#Heading)",
		},
		{
			name:       "wiki link anchor alias",
			sourcePath: currentSourcePath,
			content:    "[[Note#Heading|Alias]]",
			want:       "[Alias](/note.md#Heading)",
		},
		{
			name:       "wiki link block reference",
			sourcePath: currentSourcePath,
			content:    "[[Note#^block-id]]",
			want:       "[Note](/note.md#^block-id)",
		},
		{
			name:       "unresolved wiki link falls back to dead markdown link",
			sourcePath: currentSourcePath,
			content:    "[[Missing Note]]",
			want:       "[Missing Note](/missing-note)",
		},
		{
			name:       "asset embed",
			sourcePath: currentSourcePath,
			content:    "![[../img.png]]",
			want:       "![img.png](/assets/p1/img.png)",
		},
		{
			name:       "non image wiki asset stays a link",
			sourcePath: currentSourcePath,
			content:    "[[../Document.pdf]]",
			want:       "[Document.pdf](/assets/p1/Document.pdf)",
		},
		{
			name:       "image wiki asset renders as image",
			sourcePath: currentSourcePath,
			content:    "[[../Image.png]]",
			want:       "![Image.png](/assets/p1/Image.png)",
		},
		{
			name:       "image wiki asset with underscore filename renders as image",
			sourcePath: currentSourcePath,
			content:    "![[../obsidian_repo.png]]",
			want:       "![obsidian_repo.png](/assets/p1/obsidian_repo.png)",
		},
		{
			name:       "relative markdown link",
			sourcePath: currentSourcePath,
			content:    "[Bar](../foo/bar.md)",
			want:       "[Bar](/foo/bar.md)",
		},
		{
			name:       "percent encoded markdown link",
			sourcePath: currentSourcePath,
			content:    "[Three laws](../Three%20laws%20of%20motion.md)",
			want:       "[Three laws](/three-laws-of-motion.md)",
		},
		{
			// - Importer preserves query and fragment while canonicalizing
			name:       "markdown link preserves query and fragment",
			sourcePath: currentSourcePath,
			content:    "[Note](../Note.md?mode=raw#part)",
			want:       "[Note](/note.md?mode=raw#part)",
		},
		{
			// - Importer does not coerce assets with .md extension under asset namespaces
			name:       "asset markdown path remains unchanged",
			sourcePath: currentSourcePath,
			content:    "[Manual](../assets/manual.md)",
			want:       "[Manual](../assets/manual.md)",
		},
		{
			name:       "markdown link with escaped bracket label",
			sourcePath: currentSourcePath,
			content:    "[No\\]te](../Note.md)",
			want:       "[No\\]te](/note.md)",
		},
		{
			name:       "markdown link with nested bracket label",
			sourcePath: currentSourcePath,
			content:    "[See [Note]](../Note.md)",
			want:       "[See [Note]](/note.md)",
		},
		{
			name:       "markdown link with escaped closing paren in destination",
			sourcePath: currentSourcePath,
			content:    "[Note](../No\\)te.md)\n[Real](../Note.md)",
			want:       "[Note](/no-te.md)\n[Real](/note.md)",
		},
		{
			name:       "escaped literal markdown link stays unchanged",
			sourcePath: currentSourcePath,
			content:    "\\[Note](../Note.md)\n[Real](../Note.md)",
			want:       "\\[Note](../Note.md)\n[Real](/note.md)",
		},
		{
			name:       "pseudo markdown links with whitespace before destination stay unchanged",
			sourcePath: currentSourcePath,
			content:    "[Space] (../Note.md)\n[Newline]\n(../Note.md)\n[Real](../Note.md)",
			want:       "[Space] (../Note.md)\n[Newline]\n(../Note.md)\n[Real](/note.md)",
		},
		{
			name:       "link-like title text stays unchanged",
			sourcePath: currentSourcePath,
			content:    "[Outer](../foo/bar.md \"[Inner](../Note.md)\")",
			want:       "[Outer](/foo/bar.md \"[Inner](../Note.md)\")",
		},
		{
			name:       "scheme relative url stays external",
			sourcePath: currentSourcePath,
			content:    "[CDN](//example.com/assets/note.md)",
			want:       "[CDN](//example.com/assets/note.md)",
		},
		{
			name:       "inline code stays unchanged",
			sourcePath: currentSourcePath,
			content:    "`[[Note]]`",
			want:       "`[[Note]]`",
		},
		{
			name:       "fenced code stays unchanged",
			sourcePath: currentSourcePath,
			content:    "```md\n[x](../Note.md)\n```",
			want:       "```md\n[x](../Note.md)\n```",
		},
		{
			// - Importer does not rewrite code examples
			name:       "indented code stays unchanged",
			sourcePath: currentSourcePath,
			content:    "Example:\n\n    [x](../Note.md)\n\t[[Note]]\n\n[Note](../Note.md)\n[[Note]]",
			want:       "Example:\n\n    [x](../Note.md)\n\t[[Note]]\n\n[Note](/note.md)\n[Note](/note.md)",
		},
		{
			name:       "nested list links are rewritten",
			sourcePath: currentSourcePath,
			content:    "- parent\n    - [Note](../Note.md)\n    - [[Note]]",
			want:       "- parent\n    - [Note](/note.md)\n    - [Note](/note.md)",
		},
	}
}

var _ = ginkgo.Describe("import content link formatting", ginkgo.Label("unit"), func() {
	ginkgo.It("rewrites formatted markdown links to imported page and section routes", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "current.md", "# Current")
		writeTmp(tmp, "Note.md", "# Note")
		writeTmp(tmp, "Guide/index.md", "# Guide")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("current.md"), TargetPath: newFixtureRoutePath("current"), Kind: tree.NodeKindPage},
				{SourcePath: newFixtureWorkspaceSourcePath("Note.md"), TargetPath: newFixtureRoutePath("note"), Kind: tree.NodeKindPage},
				{SourcePath: newFixtureWorkspaceSourcePath("Guide/index.md"), TargetPath: newFixtureRoutePath("guide"), Kind: tree.NodeKindSection},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("p1"), Kind: tree.NodeKindPage}}
		content := "[Note](./Note.md)\n[Guide](./Guide/)"
		got, err := transformer.TransformContent(newFixtureUserID("editor"), newFixtureWorkspaceSourcePath("current.md"), page, content, &fakeExecWiki{})
		Expect(err).To(Succeed())

		want := "[Note](/note.md)\n[Guide](/guide)"
		Expect(got).To(Equal(want))

	})
})

var _ = ginkgo.Describe("import content generated wiki links", ginkgo.Label("unit"), func() {
	ginkgo.It("applies the markdown root prefix to generated link hrefs", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "Note.md", "# Note")
		transformer := newContentTransformerWithOptions(&PlanResult{
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("Note.md"), TargetPath: newFixtureRoutePath("note"), Kind: tree.NodeKindPage},
			},
		}, tmp, 1234, ContentTransformerOptions{MarkdownLinkRootPrefix: "/docs"})

		page := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("p1"), Kind: tree.NodeKindPage}}
		got, err := transformer.TransformContent(newFixtureUserID("editor"), newFixtureWorkspaceSourcePath("current.md"), page, "[Note](./Note.md)", &fakeExecWiki{})
		Expect(err).To(Succeed())
		Expect(got).To(Equal("[Note](/docs/note.md)"))

	})
})

var _ = ginkgo.Describe("import content source path matching", ginkgo.Label("unit"), func() {
	ginkgo.It("rewrites only case-exact source path matches", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "current.md", "# Current")
		writeTmp(tmp, "Guide.md", "# Guide")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("current.md"), TargetPath: newFixtureRoutePath("current"), Kind: tree.NodeKindPage},
				{SourcePath: newFixtureWorkspaceSourcePath("Guide.md"), TargetPath: newFixtureRoutePath("guide"), Kind: tree.NodeKindPage},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("p1"), Kind: tree.NodeKindPage}}
		content := "[Exact](./Guide.md)\n[Mismatch](./guide.md)"
		got, err := transformer.TransformContent(newFixtureUserID("editor"), newFixtureWorkspaceSourcePath("current.md"), page, content, &fakeExecWiki{})
		Expect(err).To(Succeed())

		want := "[Exact](/guide.md)\n[Mismatch](./guide.md)"
		Expect(got).To(Equal(want))

	})
})

var _ = ginkgo.Describe("import content suffix matching", ginkgo.Label("unit"), func() {
	ginkgo.It("resolves exact suffix matches while preserving case mismatches", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "current.md", "# Current")
		writeTmp(tmp, "Reference/Endpoints.md", "# Endpoints")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("current.md"), TargetPath: newFixtureRoutePath("current"), Kind: tree.NodeKindPage},
				{SourcePath: newFixtureWorkspaceSourcePath("Reference/Endpoints.md"), TargetPath: newFixtureRoutePath("reference/endpoints"), Kind: tree.NodeKindPage},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("p1"), Kind: tree.NodeKindPage}}
		content := "[Exact](Reference/Endpoints.md)\n[Mismatch](/reference/endpoints)\n[[Reference/Endpoints|Exact Wiki]]\n[[reference/endpoints|Mismatch Wiki]]"
		got, err := transformer.TransformContent(newFixtureUserID("editor"), newFixtureWorkspaceSourcePath("current.md"), page, content, &fakeExecWiki{})
		Expect(err).To(Succeed())

		want := "[Exact](/reference/endpoints.md)\n[Mismatch](/reference/endpoints)\n[Exact Wiki](/reference/endpoints.md)\n[[reference/endpoints|Mismatch Wiki]]"
		Expect(got).To(Equal(want))

	})
})

var _ = ginkgo.Describe("import content basename wiki-link matching", ginkgo.Label("unit"), func() {
	ginkgo.It("rewrites only case-exact basename wiki links", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "current.md", "# Current")
		writeTmp(tmp, "Guide.md", "# Guide")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("current.md"), TargetPath: newFixtureRoutePath("current"), Kind: tree.NodeKindPage},
				{SourcePath: newFixtureWorkspaceSourcePath("Guide.md"), TargetPath: newFixtureRoutePath("guide"), Kind: tree.NodeKindPage},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("p1"), Kind: tree.NodeKindPage}}
		content := "[[Guide]]\n[[guide]]"
		got, err := transformer.TransformContent(newFixtureUserID("editor"), newFixtureWorkspaceSourcePath("current.md"), page, content, &fakeExecWiki{})
		Expect(err).To(Succeed())

		want := "[Guide](/guide.md)\n[[guide]]"
		Expect(got).To(Equal(want))

	})
})

var _ = ginkgo.Describe("import content markdown page links", ginkgo.Label("unit"), func() {
	ginkgo.It("resolves section index files without inventing markdown page links", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "current.md", "# Current")
		writeTmp(tmp, "Guide/index.md", "# Guide")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("current.md"), TargetPath: newFixtureRoutePath("current"), Kind: tree.NodeKindPage},
				{SourcePath: newFixtureWorkspaceSourcePath("Guide/index.md"), TargetPath: newFixtureRoutePath("guide"), Kind: tree.NodeKindSection},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("p1"), Kind: tree.NodeKindPage}}
		content := "[Guide page](./Guide.md)\n[Guide index](./Guide/index.md)"
		got, err := transformer.TransformContent(newFixtureUserID("editor"), newFixtureWorkspaceSourcePath("current.md"), page, content, &fakeExecWiki{})
		Expect(err).To(Succeed())

		want := "[Guide page](./Guide.md)\n[Guide index](/guide)"
		Expect(got).To(Equal(want))

	})
})

var _ = ginkgo.Describe("import content same-route page and section links", ginkgo.Label("unit"), func() {
	ginkgo.It("distinguishes page and section hrefs for shared routes", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "current.md", "# Current")
		writeTmp(tmp, "Guide.md", "# Guide page")
		writeTmp(tmp, "Guide/index.md", "# Guide section")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("current.md"), TargetPath: newFixtureRoutePath("current"), Kind: tree.NodeKindPage},
				{SourcePath: newFixtureWorkspaceSourcePath("Guide.md"), TargetPath: newFixtureRoutePath("guide"), Kind: tree.NodeKindPage},
				{SourcePath: newFixtureWorkspaceSourcePath("Guide/index.md"), TargetPath: newFixtureRoutePath("guide"), Kind: tree.NodeKindSection},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("p1"), Kind: tree.NodeKindPage}}
		content := "[Guide page](./Guide.md)\n[Guide section](./Guide/index.md)"
		got, err := transformer.TransformContent(newFixtureUserID("editor"), newFixtureWorkspaceSourcePath("current.md"), page, content, &fakeExecWiki{})
		Expect(err).To(Succeed())

		want := "[Guide page](/guide.md)\n[Guide section](/guide)"
		Expect(got).To(Equal(want))

	})
})

var _ = ginkgo.Describe("import content suffix fallback formatting", ginkgo.Label("unit"), func() {
	ginkgo.It("formats suffix matches as page hrefs", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "current.md", "# Current")
		writeTmp(tmp, "Tools/Kubernetes/Resources/Guide.md", "# Guide page")
		writeTmp(tmp, "Tools/Kubernetes/Resources/Guide/index.md", "# Guide section")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("current.md"), TargetPath: newFixtureRoutePath("current"), Kind: tree.NodeKindPage},
				{SourcePath: newFixtureWorkspaceSourcePath("Tools/Kubernetes/Resources/Guide.md"), TargetPath: newFixtureRoutePath("knowledge-main/tools/kubernetes/resources/guide"), Kind: tree.NodeKindPage},
				{SourcePath: newFixtureWorkspaceSourcePath("Tools/Kubernetes/Resources/Guide/index.md"), TargetPath: newFixtureRoutePath("knowledge-main/tools/kubernetes/resources/guide"), Kind: tree.NodeKindSection},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("p1"), Kind: tree.NodeKindPage}}
		content := "[[Tools/Kubernetes/Resources/Guide.md]]"
		got, err := transformer.TransformContent(newFixtureUserID("editor"), newFixtureWorkspaceSourcePath("current.md"), page, content, &fakeExecWiki{})
		Expect(err).To(Succeed())

		want := "[Guide](/knowledge-main/tools/kubernetes/resources/guide.md)"
		Expect(got).To(Equal(want))

	})
})

var _ = ginkgo.Describe("import content README folder fallbacks", ginkgo.Label("unit"), func() {
	ginkgo.It("rewrites README folder references to section hrefs", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "current.md", "# Current")
		writeTmp(tmp, "Guides/README.md", "# Guides")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("current.md"), TargetPath: newFixtureRoutePath("current"), Kind: tree.NodeKindPage},
				{SourcePath: newFixtureWorkspaceSourcePath("Guides/README.md"), TargetPath: newFixtureRoutePath("guides"), Kind: tree.NodeKindSection},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("p1"), Kind: tree.NodeKindPage}}
		content := "[Guide folder](./Guides/)\n[Guide path](./Guides)\n[guide-ref]: ./Guides"
		got, err := transformer.TransformContent(newFixtureUserID("editor"), newFixtureWorkspaceSourcePath("current.md"), page, content, &fakeExecWiki{})
		Expect(err).To(Succeed())

		want := "[Guide folder](/guides)\n[Guide path](/guides)\n[guide-ref]: /guides"
		Expect(got).To(Equal(want))

	})
})

// - Reference-style link definitions are rewritten
var _ = ginkgo.Describe("import content reference definitions", ginkgo.Label("unit"), func() {
	ginkgo.It("rewrites reference definitions while preserving titles and image assets", func() {
		tmp := importerTempDir()
		writeTmp(tmp, "current.md", "# Current")
		writeTmp(tmp, "Note.md", "# Note")
		writeTmp(tmp, "Photo.png", "png-bytes")

		transformer := newContentTransformer(&PlanResult{
			Items: []PlanItem{
				{SourcePath: newFixtureWorkspaceSourcePath("current.md"), TargetPath: newFixtureRoutePath("current"), Kind: tree.NodeKindPage},
				{SourcePath: newFixtureWorkspaceSourcePath("Note.md"), TargetPath: newFixtureRoutePath("note"), Kind: tree.NodeKindPage},
			},
		}, tmp, 1234)

		page := &tree.Page{PageNode: &tree.PageNode{ID: newFixturePageID("p1"), Kind: tree.NodeKindPage}}
		content := "[Note][note-ref]\n\n[note-ref]: <./Note.md> \"Note title\""
		got, err := transformer.TransformContent(newFixtureUserID("editor"), newFixtureWorkspaceSourcePath("current.md"), page, content, &fakeExecWiki{})
		Expect(err).To(Succeed())

		want := "[Note][note-ref]\n\n[note-ref]: </note.md> \"Note title\""
		Expect(got).To(Equal(want))

		imageOnlyContent := "![Note][note-ref]\n![Photo][photo-ref]\n\n[note-ref]: <./Note.md> \"Note title\"\n[photo-ref]: ./Photo.png"
		got, err = transformer.TransformContent(newFixtureUserID("editor"), newFixtureWorkspaceSourcePath("current.md"), page, imageOnlyContent, &fakeExecWiki{})
		Expect(err).To(Succeed())

		imageOnlyWant := "![Note][note-ref]\n![Photo][photo-ref]\n\n[note-ref]: <./Note.md> \"Note title\"\n[photo-ref]: /assets/p1/Photo.png"
		Expect(got).To(Equal(imageOnlyWant))

	})
})
