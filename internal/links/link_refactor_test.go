package links

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Existing canonical relative page links preserve relative style
// - Existing canonical absolute page links preserve absolute style
// - Section move keeps section links extensionless
// - Page and section with the same basename are not cross-rewritten
// - Broken non-canonical links are not silently rewritten by refactor

var _ = ginkgo.Describe("markdown refactor destination rewriting", ginkgo.Label("unit"), func() {
	ginkgo.It("rewrites absolute relative and subtree wiki targets without touching external or image links", ginkgo.Label("unit"), func() {
		content := `
[Absolute](/docs/b)
	[Relative](./b)
[Nested](/docs/b/child#section)
[External](https://example.com/docs/b)
![Image](/docs/b.png)
`

		result := NewMarkdownRefactorEngine().Rewrite(content, newFixtureRoutePath("/docs/a"), []RewriteRule{{
			OldPath: newFixtureRoutePath("/docs/b"),
			NewPath: newFixtureRoutePath("/guides/b"),
		}})

		Expect(result.Count()).To(Equal(3))
		Expect(result.Content).To(SatisfyAll(
			ContainSubstring("[Absolute](/guides/b)"),
			ContainSubstring("[Relative](../guides/b)"),
			ContainSubstring("[Nested](/guides/b/child#section)"),
			ContainSubstring("[External](https://example.com/docs/b)"),
			ContainSubstring("![Image](/docs/b.png)"),
		))
	})

	ginkgo.It("keeps canonical page markdown extensions on absolute and relative rewritten links", ginkgo.Label("unit"), func() {
		content := "[Absolute](/docs/b.md)\n[Relative](./b.md)"

		result := NewMarkdownRefactorEngine().Rewrite(content, newFixtureRoutePath("/docs/a"), []RewriteRule{{
			OldPath: newFixtureRoutePath("/docs/b"),
			NewPath: newFixtureRoutePath("/guides/b"),
		}})

		Expect(result.Count()).To(Equal(2))
		Expect(result.Content).To(SatisfyAll(
			ContainSubstring("[Absolute](/guides/b.md)"),
			ContainSubstring("[Relative](../guides/b.md)"),
		))
	})

	ginkgo.It("uses the configured markdown root prefix for absolute output", ginkgo.Label("unit"), func() {
		content := "[Absolute](/sync/old.md)\n[Relative](./old.md)"

		result := NewMarkdownRefactorEngineWithOptions(MarkdownRefactorOptions{
			MarkdownLinkRootPrefix: "/docs",
		}).Rewrite(content, newFixtureRoutePath("/sync/source"), []RewriteRule{{
			OldPath: newFixtureRoutePath("/sync/old"),
			NewPath: newFixtureRoutePath("/sync/new"),
			Kind:    TargetKindPage,
		}})

		Expect(result.Count()).To(Equal(2))
		Expect(result.Content).To(SatisfyAll(
			ContainSubstring("[Absolute](/docs/sync/new.md)"),
			ContainSubstring("[Relative](./new.md)"),
		))
	})

	ginkgo.It("recognizes absolute input that already includes the markdown root prefix", ginkgo.Label("unit"), func() {
		content := "[Absolute](/docs/sync/old.md)"

		result := NewMarkdownRefactorEngineWithOptions(MarkdownRefactorOptions{
			MarkdownLinkRootPrefix: "/docs",
		}).Rewrite(content, newFixtureRoutePath("/sync/source"), []RewriteRule{{
			OldPath: newFixtureRoutePath("/sync/old"),
			NewPath: newFixtureRoutePath("/sync/new"),
			Kind:    TargetKindPage,
		}})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(ContainSubstring("[Absolute](/docs/sync/new.md)"))
	})

	ginkgo.It("preserves explicit dot-slash style for same-directory page links", ginkgo.Label("unit"), func() {
		content := "[Relative](./b.md)"

		result := NewMarkdownRefactorEngine().Rewrite(content, newFixtureRoutePath("/docs/a"), []RewriteRule{{
			OldPath: newFixtureRoutePath("/docs/b"),
			NewPath: newFixtureRoutePath("/docs/c"),
			Kind:    TargetKindPage,
		}})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(Equal("[Relative](./c.md)"))
	})

	ginkgo.It("rewrites canonical page links without healing legacy extensionless page links", ginkgo.Label("unit"), func() {
		content := "[Target](/target)\n[Canonical](/target.md)"

		result := NewMarkdownRefactorEngine().Rewrite(content, newFixtureRoutePath("/source"), []RewriteRule{{
			OldPath: newFixtureRoutePath("/target"),
			NewPath: newFixtureRoutePath("/renamed-target"),
			Kind:    TargetKindPage,
		}})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(SatisfyAll(
			ContainSubstring("[Target](/target)"),
			ContainSubstring("[Canonical](/renamed-target.md)"),
		))
	})

	ginkgo.It("ignores link-like text separated from a destination by whitespace", ginkgo.Label("unit"), func() {
		content := "[Space] (/docs/b.md)\n[Newline]\n(/docs/b.md)\n[Canonical](/docs/b.md)"

		result := NewMarkdownRefactorEngine().Rewrite(content, newFixtureRoutePath("/docs/source"), []RewriteRule{{
			OldPath: newFixtureRoutePath("/docs/b"),
			NewPath: newFixtureRoutePath("/docs/c"),
			Kind:    TargetKindPage,
		}})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(SatisfyAll(
			ContainSubstring("[Space] (/docs/b.md)"),
			ContainSubstring("[Newline]\n(/docs/b.md)"),
			ContainSubstring("[Canonical](/docs/c.md)"),
		))
	})

	ginkgo.It("rewrites only the real destination when title text looks like a link", ginkgo.Label("unit"), func() {
		content := `[Outer](/docs/a.md "[Inner](/docs/b.md)")`

		result := NewMarkdownRefactorEngine().Rewrite(content, newFixtureRoutePath("/docs/source"), []RewriteRule{
			{
				OldPath: newFixtureRoutePath("/docs/a"),
				NewPath: newFixtureRoutePath("/docs/renamed-a"),
				Kind:    TargetKindPage,
			},
			{
				OldPath: newFixtureRoutePath("/docs/b"),
				NewPath: newFixtureRoutePath("/docs/renamed-b"),
				Kind:    TargetKindPage,
			},
		})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(Equal(`[Outer](/docs/renamed-a.md "[Inner](/docs/b.md)")`))
	})

	ginkgo.It("does not cross-rewrite a same-basename section link during a page refactor", ginkgo.Label("unit"), func() {
		content := "[Page](/docs/sync.md)\n[Section](/docs/sync)"

		result := NewMarkdownRefactorEngine().Rewrite(content, newFixtureRoutePath("/docs/source"), []RewriteRule{{
			OldPath: newFixtureRoutePath("/docs/sync"),
			NewPath: newFixtureRoutePath("/docs/sync-page"),
			Kind:    TargetKindPage,
		}})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(SatisfyAll(
			ContainSubstring("[Page](/docs/sync-page.md)"),
			ContainSubstring("[Section](/docs/sync)"),
		))
	})

	ginkgo.It("rewrites only the exact healed target for legacy page override rules", ginkgo.Label("unit"), func() {
		content := "[Sync page](/docs/sync)\n[Sync child](/docs/sync/child.md)"

		result := NewMarkdownRefactorEngine().Rewrite(content, newFixtureRoutePath("/docs/source"), []RewriteRule{{
			OldPath:    newFixtureRoutePath("/docs/sync"),
			NewPath:    newFixtureRoutePath("/docs/sync-page"),
			Kind:       TargetKindSection,
			OutputKind: TargetKindPage,
		}})

		Expect(result.Content).To(Equal("[Sync page](/docs/sync-page.md)\n[Sync child](/docs/sync/child.md)"))
	})

	ginkgo.It("leaves asset links unchanged while rewriting wiki links", ginkgo.Label("unit"), func() {
		content := `
[AssetAbs](/assets/abc/manual.pdf)
[AssetRel](assets/abc/manual.pdf)
[Wiki](/docs/b)
`

		result := NewMarkdownRefactorEngine().Rewrite(content, newFixtureRoutePath("/docs/a"), []RewriteRule{{
			OldPath: newFixtureRoutePath("/docs/b"),
			NewPath: newFixtureRoutePath("/guides/b"),
		}})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(SatisfyAll(
			ContainSubstring("[AssetAbs](/assets/abc/manual.pdf)"),
			ContainSubstring("[AssetRel](assets/abc/manual.pdf)"),
			ContainSubstring("[Wiki](/guides/b)"),
		))
	})
})

var _ = ginkgo.Describe("markdown refactor relative path semantics", ginkgo.Label("unit"), func() {
	ginkgo.It("uses source-file directory semantics for moved section links", ginkgo.Label("unit"), func() {
		content := "[Section](../b)"

		result := NewMarkdownRefactorEngine().Rewrite(content, newFixtureRoutePath("/docs/a/current"), []RewriteRule{{
			OldPath: newFixtureRoutePath("/docs/b"),
			NewPath: newFixtureRoutePath("/guides/b"),
		}})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(Equal("[Section](../../guides/b)"))
	})

	ginkgo.It("recalculates relative links against the moved source path", ginkgo.Label("unit"), func() {
		content := `[Relative](../shared)`

		result := NewMarkdownRefactorEngine().Rewrite(content, newFixtureRoutePath("/docs/a/page"), []RewriteRule{{
			OldPath: newFixtureRoutePath("/docs"),
			NewPath: newFixtureRoutePath("/archive/docs"),
		}})

		Expect(result.Count()).To(Equal(1))
		Expect(result.Content).To(Equal(`[Relative](../shared)`))
	})
})
