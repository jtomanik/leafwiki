package markdownlinks

import (
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("markdown link resolution", ginkgo.Label("unit"), func() {
	ginkgo.When("links point at pages and sections", func() {
		ginkgo.It("resolves relative page links from the source file directory", func() {
			// Plantrace evidence: TestResolveCanonicalLink_UsesFilesystemRelativeSemanticsNotPageAsFolder.
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "docs/b/target.md"},
			})

			result := index.Resolve("docs/a/current.md", "../b/target")

			Expect(result).To(matchResolvedLink(TargetKindPage, "../b/target.md", "docs/b/target"))
		})

		ginkgo.It("canonicalizes a trailing slash section destination without changing the section route", func() {
			// Plantrace evidence: TestResolveCanonicalLink_SectionTrailingSlashIsAcceptedButCanonicalizedAway.
			index := NewIndex([]Entry{
				{Kind: EntryKindSection, Path: "docs/sync"},
			})

			result := index.Resolve("docs/a.md", "/docs/sync/")

			Expect(result).To(matchResolvedLink(TargetKindSection, "/docs/sync", "docs/sync"))
		})

		ginkgo.It("resolves a relative root section destination to a slash href", func() {
			// Plantrace evidence: TestResolveCanonicalLink_RelativeRootSectionCanonicalizesToSlash.
			index := NewIndex([]Entry{
				{Kind: EntryKindSection, Path: "", ContentPath: "index.md"},
			})

			result := index.Resolve("docs/nested/page.md", "../..")

			Expect(result).To(matchRootSection("/"))
		})

		ginkgo.It("keeps explicit README page links as pages when a section fallback also exists", func() {
			index := NewIndex([]Entry{
				{Kind: EntryKindSection, Path: "docs/sync", ContentPath: "docs/sync/index.md"},
				{Kind: EntryKindSection, Path: "guides", ContentPath: "guides/README.md"},
				{Kind: EntryKindPage, Path: "docs/sync/README.md"},
			})

			readmePage := index.Resolve("docs/a.md", "/docs/sync/README.md")

			Expect(readmePage).To(matchResolvedLink(TargetKindPage, "/docs/sync/README.md", "docs/sync/README"))
		})

		ginkgo.It("prefers the canonical section when page and section routes share an extensionless link", func() {
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "docs/sync.md"},
				{Kind: EntryKindSection, Path: "docs/sync", ContentPath: "docs/sync/index.md"},
			})

			result := index.Resolve("docs/a.md", "/docs/sync")

			Expect(result).To(matchResolvedLink(TargetKindSection, "/docs/sync", "docs/sync"))
		})
	})

	ginkgo.When("a markdown link root prefix is configured", func() {
		ginkgo.It("resolves prefixed absolute page links inside the wiki root", func() {
			// Plantrace evidence: TestResolveCanonicalLink_WithRootPrefixResolvesPageInsideWikiRoot.
			index := NewIndexWithOptions([]Entry{
				{Kind: EntryKindPage, Path: "sync/glossary.md"},
			}, Options{MarkdownLinkRootPrefix: "/docs"})

			result := index.Resolve("index.md", "/docs/sync/glossary.md")

			Expect(result).To(matchResolvedLink(TargetKindPage, "/docs/sync/glossary.md", "sync/glossary"))
		})

		ginkgo.It("canonicalizes unprefixed absolute migration links into the configured root prefix", func() {
			index := NewIndexWithOptions([]Entry{
				{Kind: EntryKindPage, Path: "sync/glossary.md"},
			}, Options{MarkdownLinkRootPrefix: "/docs"})

			result := index.ResolveForMigration("index.md", "/sync/glossary?view=1#term")

			Expect(result).To(matchResolvedLink(TargetKindPage, "/docs/sync/glossary.md?view=1#term", "sync/glossary"))
		})

		ginkgo.DescribeTable("resolves the configured prefix root to the wiki root section",
			func(href string) {
				// Plantrace evidence: TestResolveCanonicalLink_WithRootPrefixResolvesPrefixRootToWikiRoot.
				index := NewIndexWithOptions([]Entry{
					{Kind: EntryKindSection, Path: ""},
				}, Options{MarkdownLinkRootPrefix: "/docs"})

				result := index.Resolve("index.md", href)

				Expect(result).To(matchRootSection("/docs"))
			},
			ginkgo.Entry("without a trailing slash", "/docs"),
			ginkgo.Entry("with a trailing slash", "/docs/"),
		)

		ginkgo.DescribeTable("preserves root suffixes without adding an extra slash",
			func(href string, want string) {
				index := NewIndexWithOptions([]Entry{
					{Kind: EntryKindSection, Path: ""},
				}, Options{MarkdownLinkRootPrefix: "/docs"})

				result := index.Resolve("index.md", href)

				Expect(result).To(matchRootSection(want))
			},
			ginkgo.Entry("fragment suffix", "/docs#intro", "/docs#intro"),
			ginkgo.Entry("query and fragment suffix", "/docs?view=1#intro", "/docs?view=1#intro"),
		)

		ginkgo.It("distinguishes extensionless sections from explicit markdown pages", func() {
			// Plantrace evidence: TestResolveCanonicalLink_WithRootPrefixDistinguishesSectionAndPageSyntax.
			index := NewIndexWithOptions([]Entry{
				{Kind: EntryKindSection, Path: "sync", ContentPath: "sync/index.md"},
				{Kind: EntryKindPage, Path: "sync.md"},
			}, Options{MarkdownLinkRootPrefix: "/docs"})

			section := index.Resolve("index.md", "/docs/sync")
			page := index.Resolve("index.md", "/docs/sync.md")

			Expect(section).To(matchResolvedLink(TargetKindSection, "/docs/sync", "sync"))
			Expect(page).To(matchResolvedLink(TargetKindPage, "/docs/sync.md", "sync"))
		})

		ginkgo.It("leaves relative links canonical relative to the source file", func() {
			// Plantrace evidence: TestResolveCanonicalLink_WithRootPrefixLeavesRelativeLinkUnchanged.
			index := NewIndexWithOptions([]Entry{
				{Kind: EntryKindPage, Path: "glossary.md"},
			}, Options{MarkdownLinkRootPrefix: "/docs"})

			relative := index.Resolve("sync/page.md", "../glossary.md")

			Expect(relative).To(matchResolvedLink(TargetKindPage, "../glossary.md", "glossary"))
		})

		ginkgo.DescribeTable("leaves external and hash links unchanged",
			func(href string) {
				// Plantrace evidence: TestResolveCanonicalLink_WithRootPrefixLeavesExternalAndHashLinksUnchanged.
				index := NewIndexWithOptions([]Entry{
					{Kind: EntryKindPage, Path: "glossary.md"},
				}, Options{MarkdownLinkRootPrefix: "/docs"})

				result := index.Resolve("sync/page.md", href)

				Expect(result).To(matchResolvedLink(TargetKindExternal, href, ""))
			},
			ginkgo.Entry("absolute URL", "https://example.com/docs/a.md"),
			ginkgo.Entry("protocol-relative URL", "//example.com/docs/a.md"),
			ginkgo.Entry("mailto URL", "mailto:a@example.com"),
			ginkgo.Entry("same-page hash", "#local"),
		)

		ginkgo.It("resolves prefixed asset destinations as asset links", func() {
			index := NewIndexWithOptions(nil, Options{MarkdownLinkRootPrefix: "/docs"})

			result := index.Resolve("index.md", "/docs/assets/logo.png")

			Expect(result).To(matchResolvedLink(TargetKindAsset, "/docs/assets/logo.png", ""))
		})
	})

	ginkgo.When("normalizing markdown link root prefixes", func() {
		ginkgo.DescribeTable("rejects non-path or unsafe root prefix values",
			func(input string, wantErr error) {
				_, err := NormalizeMarkdownLinkRootPrefix(input)

				Expect(err).To(MatchError(wantErr))
			},
			ginkgo.Entry("repository root", "/", ErrMarkdownLinkRootPrefixRoot),
			ginkgo.Entry("relative parent traversal", "..", ErrMarkdownLinkRootPrefixTraversal),
			ginkgo.Entry("absolute parent traversal", "/../docs", ErrMarkdownLinkRootPrefixTraversal),
			ginkgo.Entry("schemed URL", "https://example.com/docs", ErrMarkdownLinkRootPrefixNotPath),
			ginkgo.Entry("query string", "/docs?x=1", ErrMarkdownLinkRootPrefixQueryOrFragment),
			ginkgo.Entry("fragment", "/docs#intro", ErrMarkdownLinkRootPrefixQueryOrFragment),
			ginkgo.Entry("backslash path", `docs\sync`, ErrMarkdownLinkRootPrefixBackslash),
			ginkgo.Entry("opaque HTTP-ish path", "http:/docs", ErrMarkdownLinkRootPrefixNotPath),
			ginkgo.Entry("mailto opaque path", "mailto:docs", ErrMarkdownLinkRootPrefixNotPath),
		)

		ginkgo.DescribeTable("accepts stable repository path forms",
			func(input string, want string) {
				Expect(NormalizeMarkdownLinkRootPrefix(input)).To(Equal(want))
			},
			ginkgo.Entry("empty value", "", ""),
			ginkgo.Entry("blank value", " \t\n ", ""),
			ginkgo.Entry("relative prefix", "docs", "/docs"),
			ginkgo.Entry("mixed case relative prefix with trailing slash", " Docs/Sync/ ", "/Docs/Sync"),
			ginkgo.Entry("absolute prefix with redundant slash", "/docs/sync/", "/docs/sync"),
		)
	})

	ginkgo.When("classifying unresolved and non-page destinations", func() {
		ginkgo.DescribeTable("reports the target kind and issue code for canonical resolution",
			func(href string, kind TargetKind, code IssueCode) {
				index := NewIndex([]Entry{
					{Kind: EntryKindPage, Path: "docs/b.md"},
					{Kind: EntryKindSection, Path: "docs/sync", ContentPath: "docs/sync/index.md"},
					{Kind: EntryKindAsset, Path: "assets/page/logo.png"},
				})

				result := index.Resolve("docs/a/current.md", href)

				Expect(result).To(matchResolution(kind, code))
			},
			ginkgo.Entry("canonical page", "/docs/b.md", TargetKindPage, IssueCode("")),
			ginkgo.Entry("canonical section", "/docs/sync", TargetKindSection, IssueCode("")),
			ginkgo.Entry("asset namespace", "/assets/page/logo.png", TargetKindAsset, IssueCode("")),
			ginkgo.Entry("asset extension", "/docs/manual.pdf", TargetKindAsset, IssueCode("")),
			ginkgo.Entry("external URL", "https://example.com", TargetKindExternal, IssueCode("")),
			ginkgo.Entry("mailto URL", "mailto:a@example.com", TargetKindExternal, IssueCode("")),
			ginkgo.Entry("same-page hash", "#heading", TargetKindExternal, IssueCode("")),
			ginkgo.Entry("invalid percent encoding", "/docs/%zz", TargetKindInvalid, IssueCodeInvalidPercentEncoding),
			ginkgo.Entry("workspace escape", "../../../outside.md", TargetKindInvalid, IssueCodeWorkspaceEscape),
			ginkgo.Entry("missing page", "/docs/missing.md", TargetKindUnresolved, IssueCodeBrokenPage),
		)

		ginkgo.DescribeTable("treats protocol-relative and schemed URLs as external destinations",
			func(href string) {
				// Plantrace evidence: TestResolveCanonicalLink_TreatsProtocolRelativeAndSchemedURLsAsExternal.
				index := NewIndex([]Entry{
					{Kind: EntryKindPage, Path: "cdn.example.com/lib.md"},
				})

				result := index.Resolve("docs/a.md", href)

				Expect(result).To(matchResolvedLink(TargetKindExternal, href, ""))
			},
			ginkgo.Entry("protocol-relative URL", "//cdn.example.com/lib.md"),
			ginkgo.Entry("schemed URL", "obsidian://open?vault=wiki"),
		)

		ginkgo.It("keeps percent-encoded path style while matching decoded filesystem paths", func() {
			// Plantrace evidence: TestResolveCanonicalLink_PreservesPercentEncodedPathStyle.
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "docs/space name.md"},
			})

			result := index.Resolve("docs/a.md", "/docs/space%20name?x=1#part")

			Expect(result).To(matchResolvedLink(TargetKindPage, "/docs/space%20name.md?x=1#part", "docs/space name"))
		})
	})

	ginkgo.When("migration mode handles legacy page and section ambiguity", func() {
		ginkgo.It("leaves ambiguous extensionless page and section twins unresolved", func() {
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "docs/sync.md"},
				{Kind: EntryKindSection, Path: "docs/sync", ContentPath: "docs/sync/index.md"},
			})

			result := index.ResolveForMigration("docs/a.md", "/docs/sync")

			Expect(result).To(matchResolution(TargetKindUnresolved, IssueCodeAmbiguousLegacyLink))
		})

		ginkgo.It("uses a trailing slash to disambiguate page and section twins to the section", func() {
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "docs/sync.md"},
				{Kind: EntryKindSection, Path: "docs/sync", ContentPath: "docs/sync/index.md"},
			})

			result := index.ResolveForMigration("docs/a.md", "/docs/sync/")

			Expect(result).To(matchResolvedLink(TargetKindSection, "/docs/sync", "docs/sync"))
		})
	})

	ginkgo.When("explicit section content files are linked", func() {
		ginkgo.DescribeTable("canonicalizes default section files to their section routes",
			func(href string, canonicalHref string, routePath string) {
				// Plantrace evidence: TestResolveCanonicalLink_ExplicitSectionDefaultFilesCanonicalizeToSection.
				index := NewIndex([]Entry{
					{Kind: EntryKindSection, Path: "docs/sync", ContentPath: "docs/sync/index.md"},
					{Kind: EntryKindSection, Path: "guides", ContentPath: "guides/README.md"},
					{Kind: EntryKindPage, Path: "docs/sync/README.md"},
				})

				result := index.Resolve("docs/a.md", href)

				Expect(result).To(matchResolvedLink(TargetKindSection, canonicalHref, routePath))
			},
			ginkgo.Entry("index markdown file", "/docs/sync/index.md", "/docs/sync", "docs/sync"),
			ginkgo.Entry("percent-encoded index markdown file", "/docs/%73ync/index.md", "/docs/%73ync", "docs/sync"),
			ginkgo.Entry("README markdown file", "/guides/README.md", "/guides", "guides"),
			ginkgo.Entry("percent-encoded README markdown file", "/guid%65s/README.md", "/guid%65s", "guides"),
		)
	})

	ginkgo.When("building an index from workspace files", func() {
		ginkgo.It("uses workspace route normalization for canonical route paths", func() {
			rootDir := markdownLinksTempDir()
			Expect(os.MkdirAll(filepath.Join(rootDir, "plans"), 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(rootDir, "plans", "index.md"), []byte("# Plans"), 0o644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(rootDir, "plans", "agent_hooks.PLAN.md"), []byte("# Agent Hooks Plan"), 0o644)).To(Succeed())

			index, err := NewIndexFromRoot(rootDir)
			Expect(err).NotTo(HaveOccurred())

			result := index.Resolve("source.md", "/plans/agent-hooks-plan.md")
			rawAbsolute := index.Resolve("source.md", "/plans/agent_hooks.PLAN.md")
			rawRelative := index.Resolve("plans/source.md", "./agent_hooks.PLAN.md")
			migration := index.ResolveForMigration("plans/source.md", "./agent_hooks.PLAN.md")

			Expect(result).To(matchResolvedLink(TargetKindPage, "/plans/agent-hooks-plan.md", "plans/agent-hooks-plan"))
			Expect(rawAbsolute).To(matchResolution(TargetKindUnresolved, IssueCodeNonCanonicalMarkdownPath))
			Expect(rawAbsolute.RoutePath).To(Equal(rawRelative.RoutePath))
			Expect(rawAbsolute.RoutePath).To(Equal(result.RoutePath))
			Expect(rawRelative).To(matchResolution(TargetKindUnresolved, IssueCodeNonCanonicalMarkdownPath))
			Expect(migration).To(matchResolvedLink(TargetKindPage, "agent-hooks-plan.md", "plans/agent-hooks-plan"))
		})

		ginkgo.It("applies markdown link root prefixes to discovered page canonical hrefs", func() {
			rootDir := markdownLinksTempDir()
			Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(rootDir, "docs", "page.md"), []byte("# Page"), 0o644)).To(Succeed())

			index, err := NewIndexFromRootWithOptions(rootDir, Options{MarkdownLinkRootPrefix: "/wiki"})
			Expect(err).NotTo(HaveOccurred())

			result := index.Resolve("source.md", "/wiki/docs/page.md")

			Expect(result).To(matchResolvedLink(TargetKindPage, "/wiki/docs/page.md", "docs/page"))
		})
	})
})

var _ = ginkgo.Describe("markdown link rewriting", ginkgo.Label("unit"), func() {
	ginkgo.When("rewriting inline and reference markdown destinations", func() {
		ginkgo.It("preserves angle destinations, titles, query strings, and fragments", func() {
			// Plantrace evidence: TestCanonicalizeMarkdownLinks_PreservesAngleDestinationsTitleQueryAndFragment.
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "docs/b.md"},
			})

			result := index.RewriteMarkdown("docs/a.md", `[B](</docs/b?mode=raw#part-two> "open B")`)

			Expect(result).To(matchRewriteUpdatesContent(`[B](</docs/b.md?mode=raw#part-two> "open B")`))
		})

		ginkgo.It("rewrites reference definitions without touching code spans or fenced code blocks", func() {
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "docs/b.md"},
			})
			content := "[B][b-ref]\n\n[b-ref]: /docs/b\n\n`[B](/docs/b)`\n\n```md\n[B](/docs/b)\n```\n"

			result := index.RewriteMarkdown("docs/a.md", content)

			want := "[B][b-ref]\n\n[b-ref]: /docs/b.md\n\n`[B](/docs/b)`\n\n```md\n[B](/docs/b)\n```\n"
			Expect(result).To(matchRewriteUpdatesContent(want))
		})

		ginkgo.It("rewrites the real link while leaving escaped literal link text unchanged", func() {
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "docs/b.md"},
			})
			content := `\[B](/docs/b)
[Real](/docs/b)`

			result := index.RewriteMarkdown("docs/a.md", content)

			want := `\[B](/docs/b)
[Real](/docs/b.md)`
			Expect(result).To(matchRewriteUpdatesContent(want))
		})

		ginkgo.It("rewrites only syntactically valid inline links when literal text resembles links", func() {
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "docs/b.md"},
			})
			content := "[Space] (/docs/b)\n[Newline]\n(/docs/b)\n[Real](/docs/b)"

			result := index.RewriteMarkdown("docs/a.md", content)

			want := "[Space] (/docs/b)\n[Newline]\n(/docs/b)\n[Real](/docs/b.md)"
			Expect(result).To(matchRewriteUpdatesContent(want))
		})

		ginkgo.It("does not rewrite malformed inline-link tails but still rewrites valid tails", func() {
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "docs/b.md"},
			})
			content := "[MissingTitleClose](/docs/b \"title\"\n[UnquotedTitle](/docs/b title)\n[NewlineTail](/docs/b\ntext)\n[LeadingSpace]( /docs/b)\n[QuotedTitle](/docs/b \"title\")\n[ParenTitle](/docs/b (title))"

			result := index.RewriteMarkdown("docs/a.md", content)

			want := "[MissingTitleClose](/docs/b \"title\"\n[UnquotedTitle](/docs/b title)\n[NewlineTail](/docs/b\ntext)\n[LeadingSpace]( /docs/b.md)\n[QuotedTitle](/docs/b.md \"title\")\n[ParenTitle](/docs/b.md (title))"
			Expect(result).To(matchRewriteUpdatesContent(want))
		})

		ginkgo.It("leaves image-only reference definitions unchanged", func() {
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "docs/b.md"},
			})
			content := "![B][b-ref]\n\n[b-ref]: /docs/b\n"

			result := index.RewriteMarkdown("docs/a.md", content)

			Expect(result).To(matchRewriteLeavesContentUnchanged(content))
		})

		ginkgo.It("rewrites links nested inside list items", func() {
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "docs/b.md"},
			})
			content := "- parent\n    - [Target](/docs/b)\n"

			result := index.RewriteMarkdown("docs/a.md", content)

			Expect(result).To(matchRewriteUpdatesContent("- parent\n    - [Target](/docs/b.md)\n"))
		})

		ginkgo.It("applies reference and inline replacements in source-order-safe offset order", func() {
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "docs/b.md"},
			})
			content := "[b-ref]: /docs/b\n\n[B](/docs/b)\n"

			result := index.RewriteMarkdown("docs/a.md", content)

			Expect(result).To(matchRewriteUpdatesContent("[b-ref]: /docs/b.md\n\n[B](/docs/b.md)\n"))
		})
	})

	ginkgo.When("link text appears inside code", func() {
		ginkgo.It("skips multi-backtick code spans while rewriting visible markdown links", func() {
			// Plantrace evidence: TestCanonicalizeMarkdownLinks_SkipsMultiBacktickCodeSpans.
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "docs/b.md"},
			})
			content := "``[B](/docs/b)``\n\n[B](/docs/b)\n"

			result := index.RewriteMarkdown("docs/a.md", content)

			Expect(result).To(matchRewriteUpdatesContent("``[B](/docs/b)``\n\n[B](/docs/b.md)\n"))
		})

		ginkgo.It("skips indented code blocks while rewriting visible markdown links", func() {
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "docs/b.md"},
			})
			content := "Example:\n\n    [B](/docs/b)\n\t[C](/docs/b)\n\n[B](/docs/b)\n"

			result := index.RewriteMarkdown("docs/a.md", content)

			want := "Example:\n\n    [B](/docs/b)\n\t[C](/docs/b)\n\n[B](/docs/b.md)\n"
			Expect(result).To(matchRewriteUpdatesContent(want))
		})
	})

	ginkgo.When("content has no canonical page rewrite", func() {
		ginkgo.It("leaves empty content unchanged without issues", func() {
			index := NewIndex(nil)

			result := index.RewriteMarkdown("index.md", "")

			Expect(result).To(matchRewriteLeavesContentUnchanged(""))
		})

		ginkgo.It("reports invalid and unresolved destinations without changing content", func() {
			index := NewIndex(nil)
			content := "[Missing](/missing.md)\n[Bad](/docs/%zz)\n"

			result := index.RewriteMarkdown("index.md", content)

			Expect(result).To(matchRewriteLeavesContentUnchanged(content,
				Issue{Code: IssueCodeBrokenPage, Destination: "/missing.md"},
				Issue{Code: IssueCodeInvalidPercentEncoding, Destination: "/docs/%zz"},
			))
		})
	})
})
