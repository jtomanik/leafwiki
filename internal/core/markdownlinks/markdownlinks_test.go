package markdownlinks

import (
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - Relative page links are resolved from the source file directory
// - Section trailing slash is accepted but canonicalized away
// - Root section link remains slash
// - Query string and fragment are preserved byte-for-byte
// - Link title and angle-bracket destination syntax are preserved
// - Percent-encoded paths use exact filesystem matching
// - External and non-page links are ignored
// - Link-like text in inline code and fenced code is ignored
// - Relative old page link migrates to relative .md
// - Existing canonical .md page link is not rewritten
// - Explicit index.md section link canonicalizes to the section
// - Explicit README.md section fallback link canonicalizes to the section

var _ = ginkgo.Describe("markdown links", func() {
	// - Relative page links are resolved from the source file directory
	ginkgo.It("TestResolveCanonicalLink_UsesFilesystemRelativeSemanticsNotPageAsFolder", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindPage, Path: "docs/b/target.md"},
		})

		result := index.Resolve("docs/a/current.md", "../b/target")

		if result.Kind != TargetKindPage {
			t.Fatalf("Kind = %q, want %q", result.Kind, TargetKindPage)
		}
		if result.CanonicalHref != "../b/target.md" {
			t.Fatalf("CanonicalHref = %q, want %q", result.CanonicalHref, "../b/target.md")
		}
		if result.RoutePath != "docs/b/target" {
			t.Fatalf("RoutePath = %q, want docs/b/target", result.RoutePath)
		}
	})

	// - Prefixed absolute page link resolves inside wiki root
	ginkgo.It("TestResolveCanonicalLink_WithRootPrefixResolvesPageInsideWikiRoot", func() {
		t := ginkgo.GinkgoT()
		index := NewIndexWithOptions([]Entry{
			{Kind: EntryKindPage, Path: "sync/glossary.md"},
		}, Options{MarkdownLinkRootPrefix: "/docs"})

		result := index.Resolve("index.md", "/docs/sync/glossary.md")

		if result.Kind != TargetKindPage {
			t.Fatalf("Kind = %q, want %q: %#v", result.Kind, TargetKindPage, result)
		}
		if result.RoutePath != "sync/glossary" {
			t.Fatalf("RoutePath = %q, want sync/glossary", result.RoutePath)
		}
		if result.CanonicalHref != "/docs/sync/glossary.md" {
			t.Fatalf("CanonicalHref = %q, want /docs/sync/glossary.md", result.CanonicalHref)
		}
	})

	// - Unprefixed absolute page link still resolves but canonicalizes to the configured prefix
	ginkgo.It("TestResolveForMigration_WithRootPrefixCanonicalizesUnprefixedAbsolutePage", func() {
		t := ginkgo.GinkgoT()
		index := NewIndexWithOptions([]Entry{
			{Kind: EntryKindPage, Path: "sync/glossary.md"},
		}, Options{MarkdownLinkRootPrefix: "/docs"})

		result := index.ResolveForMigration("index.md", "/sync/glossary?view=1#term")

		if result.Kind != TargetKindPage {
			t.Fatalf("Kind = %q, want %q: %#v", result.Kind, TargetKindPage, result)
		}
		if result.RoutePath != "sync/glossary" {
			t.Fatalf("RoutePath = %q, want sync/glossary", result.RoutePath)
		}
		if result.CanonicalHref != "/docs/sync/glossary.md?view=1#term" {
			t.Fatalf("CanonicalHref = %q, want /docs/sync/glossary.md?view=1#term", result.CanonicalHref)
		}
	})

	// - Configured prefix root resolves to the wiki root section
	ginkgo.DescribeTable("TestResolveCanonicalLink_WithRootPrefixResolvesPrefixRootToWikiRoot",
		func(href string) {
			t := ginkgo.GinkgoT()
			index := NewIndexWithOptions([]Entry{
				{Kind: EntryKindSection, Path: ""},
			}, Options{MarkdownLinkRootPrefix: "/docs"})

			result := index.Resolve("index.md", href)
			if result.Kind != TargetKindSection {
				t.Fatalf("Resolve(%q).Kind = %q, want %q: %#v", href, result.Kind, TargetKindSection, result)
			}
			if result.RoutePath != "" {
				t.Fatalf("Resolve(%q).RoutePath = %q, want root route", href, result.RoutePath)
			}
			if result.CanonicalHref != "/docs" {
				t.Fatalf("Resolve(%q).CanonicalHref = %q, want /docs", href, result.CanonicalHref)
			}
		},
		ginkgo.Entry("/docs", "/docs"),
		ginkgo.Entry("/docs/", "/docs/"),
	)

	ginkgo.DescribeTable("TestResolveCanonicalLink_WithRootPrefixPreservesRootSuffixWithoutExtraSlash",
		func(href string, want string) {
			t := ginkgo.GinkgoT()
			index := NewIndexWithOptions([]Entry{
				{Kind: EntryKindSection, Path: ""},
			}, Options{MarkdownLinkRootPrefix: "/docs"})

			result := index.Resolve("index.md", href)
			if result.Kind != TargetKindSection {
				t.Fatalf("Resolve(%q).Kind = %q, want section: %#v", href, result.Kind, result)
			}
			if result.RoutePath != "" {
				t.Fatalf("Resolve(%q).RoutePath = %q, want root route", href, result.RoutePath)
			}
			if result.CanonicalHref != want {
				t.Fatalf("Resolve(%q).CanonicalHref = %q, want %q", href, result.CanonicalHref, want)
			}
		},
		ginkgo.Entry("/docs#intro", "/docs#intro", "/docs#intro"),
		ginkgo.Entry("/docs?view=1#intro", "/docs?view=1#intro", "/docs?view=1#intro"),
	)

	ginkgo.DescribeTable("TestNormalizeMarkdownLinkRootPrefixRejectsInvalidValues",
		func(input string) {
			t := ginkgo.GinkgoT()
			if got, err := NormalizeMarkdownLinkRootPrefix(input); err == nil {
				t.Fatalf("NormalizeMarkdownLinkRootPrefix(%q) = %q, nil; want error", input, got)
			}
		},
		ginkgo.Entry("/", "/"),
		ginkgo.Entry("..", ".."),
		ginkgo.Entry("/../docs", "/../docs"),
		ginkgo.Entry("https://example.com/docs", "https://example.com/docs"),
		ginkgo.Entry("/docs?x=1", "/docs?x=1"),
		ginkgo.Entry("/docs#intro", "/docs#intro"),
		ginkgo.Entry(`docs\sync`, `docs\sync`),
		ginkgo.Entry("http:/docs", "http:/docs"),
		ginkgo.Entry("mailto:docs", "mailto:docs"),
	)

	ginkgo.It("TestResolveCanonicalLink_WithRootPrefixDistinguishesSectionAndPageSyntax", func() {
		t := ginkgo.GinkgoT()
		index := NewIndexWithOptions([]Entry{
			{Kind: EntryKindSection, Path: "sync", ContentPath: "sync/index.md"},
			{Kind: EntryKindPage, Path: "sync.md"},
		}, Options{MarkdownLinkRootPrefix: "/docs"})

		section := index.Resolve("index.md", "/docs/sync")
		if section.Kind != TargetKindSection {
			t.Fatalf("section Kind = %q, want %q: %#v", section.Kind, TargetKindSection, section)
		}
		if section.RoutePath != "sync" {
			t.Fatalf("section RoutePath = %q, want sync", section.RoutePath)
		}
		if section.CanonicalHref != "/docs/sync" {
			t.Fatalf("section CanonicalHref = %q, want /docs/sync", section.CanonicalHref)
		}

		page := index.Resolve("index.md", "/docs/sync.md")
		if page.Kind != TargetKindPage {
			t.Fatalf("page Kind = %q, want %q: %#v", page.Kind, TargetKindPage, page)
		}
		if page.RoutePath != "sync" {
			t.Fatalf("page RoutePath = %q, want sync", page.RoutePath)
		}
		if page.CanonicalHref != "/docs/sync.md" {
			t.Fatalf("page CanonicalHref = %q, want /docs/sync.md", page.CanonicalHref)
		}
	})

	ginkgo.It("TestResolveCanonicalLink_WithRootPrefixLeavesRelativeLinkUnchanged", func() {
		t := ginkgo.GinkgoT()
		index := NewIndexWithOptions([]Entry{
			{Kind: EntryKindPage, Path: "glossary.md"},
		}, Options{MarkdownLinkRootPrefix: "/docs"})

		relative := index.Resolve("sync/page.md", "../glossary.md")
		if relative.Kind != TargetKindPage || relative.CanonicalHref != "../glossary.md" {
			t.Fatalf("relative link resolved as %#v, want page with unchanged relative href", relative)
		}
	})

	ginkgo.DescribeTable("TestResolveCanonicalLink_WithRootPrefixLeavesExternalAndHashLinksUnchanged",
		func(href string) {
			t := ginkgo.GinkgoT()
			index := NewIndexWithOptions([]Entry{
				{Kind: EntryKindPage, Path: "glossary.md"},
			}, Options{MarkdownLinkRootPrefix: "/docs"})

			result := index.Resolve("sync/page.md", href)
			if result.Kind != TargetKindExternal {
				t.Fatalf("Resolve(%q).Kind = %q, want external", href, result.Kind)
			}
			if result.CanonicalHref != href {
				t.Fatalf("Resolve(%q).CanonicalHref = %q, want unchanged", href, result.CanonicalHref)
			}
		},
		ginkgo.Entry("https://example.com/docs/a.md", "https://example.com/docs/a.md"),
		ginkgo.Entry("//example.com/docs/a.md", "//example.com/docs/a.md"),
		ginkgo.Entry("mailto:a@example.com", "mailto:a@example.com"),
		ginkgo.Entry("#local", "#local"),
	)

	ginkgo.It("TestResolveCanonicalLink_WithRootPrefixResolvesPrefixedAssets", func() {
		t := ginkgo.GinkgoT()
		index := NewIndexWithOptions(nil, Options{MarkdownLinkRootPrefix: "/docs"})

		result := index.Resolve("index.md", "/docs/assets/logo.png")

		if result.Kind != TargetKindAsset {
			t.Fatalf("Kind = %q, want %q: %#v", result.Kind, TargetKindAsset, result)
		}
		if result.CanonicalHref != "/docs/assets/logo.png" {
			t.Fatalf("CanonicalHref = %q, want /docs/assets/logo.png", result.CanonicalHref)
		}
	})

	// - Section trailing slash is accepted but canonicalized away
	ginkgo.It("TestResolveCanonicalLink_SectionTrailingSlashIsAcceptedButCanonicalizedAway", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindSection, Path: "docs/sync"},
		})

		result := index.Resolve("docs/a.md", "/docs/sync/")

		if result.Kind != TargetKindSection {
			t.Fatalf("Kind = %q, want %q", result.Kind, TargetKindSection)
		}
		if result.CanonicalHref != "/docs/sync" {
			t.Fatalf("CanonicalHref = %q, want /docs/sync", result.CanonicalHref)
		}
	})

	// - Root section link remains slash
	ginkgo.It("TestResolveCanonicalLink_RelativeRootSectionCanonicalizesToSlash", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindSection, Path: "", ContentPath: "index.md"},
		})

		result := index.Resolve("docs/nested/page.md", "../..")

		if result.Kind != TargetKindSection {
			t.Fatalf("Kind = %q, want %q", result.Kind, TargetKindSection)
		}
		if result.CanonicalHref != "/" {
			t.Fatalf("CanonicalHref = %q, want /", result.CanonicalHref)
		}
	})

	ginkgo.DescribeTable("TestResolveCanonicalLink_ClassifiesPageSectionAssetExternalInvalidAndUnresolved",
		func(href string, kind TargetKind, code IssueCode) {
			t := ginkgo.GinkgoT()
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "docs/b.md"},
				{Kind: EntryKindSection, Path: "docs/sync", ContentPath: "docs/sync/index.md"},
				{Kind: EntryKindAsset, Path: "assets/page/logo.png"},
			})

			result := index.Resolve("docs/a/current.md", href)
			if result.Kind != kind {
				t.Fatalf("Kind = %q, want %q", result.Kind, kind)
			}
			if result.Code != code {
				t.Fatalf("Code = %q, want %q", result.Code, code)
			}
		},
		ginkgo.Entry("page", "/docs/b.md", TargetKindPage, IssueCode("")),
		ginkgo.Entry("section", "/docs/sync", TargetKindSection, IssueCode("")),
		ginkgo.Entry("asset namespace", "/assets/page/logo.png", TargetKindAsset, IssueCode("")),
		ginkgo.Entry("asset extension", "/docs/manual.pdf", TargetKindAsset, IssueCode("")),
		ginkgo.Entry("external", "https://example.com", TargetKindExternal, IssueCode("")),
		ginkgo.Entry("mailto", "mailto:a@example.com", TargetKindExternal, IssueCode("")),
		ginkgo.Entry("hash", "#heading", TargetKindExternal, IssueCode("")),
		ginkgo.Entry("invalid percent", "/docs/%zz", TargetKindInvalid, IssueCodeInvalidPercentEncoding),
		ginkgo.Entry("escape", "../../../outside.md", TargetKindInvalid, IssueCodeWorkspaceEscape),
		ginkgo.Entry("unresolved", "/docs/missing.md", TargetKindUnresolved, IssueCodeBrokenPage),
	)

	// - Explicit index.md section link canonicalizes to the section
	// - Explicit README.md section fallback link canonicalizes to the section
	ginkgo.DescribeTable("TestResolveCanonicalLink_ExplicitSectionDefaultFilesCanonicalizeToSection",
		func(href string, want string) {
			t := ginkgo.GinkgoT()
			index := NewIndex([]Entry{
				{Kind: EntryKindSection, Path: "docs/sync", ContentPath: "docs/sync/index.md"},
				{Kind: EntryKindSection, Path: "guides", ContentPath: "guides/README.md"},
				{Kind: EntryKindPage, Path: "docs/sync/README.md"},
			})

			result := index.Resolve("docs/a.md", href)
			if result.Kind != TargetKindSection {
				t.Fatalf("Resolve(%q).Kind = %q, want section", href, result.Kind)
			}
			if result.CanonicalHref != want {
				t.Fatalf("Resolve(%q).CanonicalHref = %q, want %q", href, result.CanonicalHref, want)
			}
		},
		ginkgo.Entry("index md", "/docs/sync/index.md", "/docs/sync"),
		ginkgo.Entry("percent-encoded index md", "/docs/%73ync/index.md", "/docs/%73ync"),
		ginkgo.Entry("readme md", "/guides/README.md", "/guides"),
		ginkgo.Entry("percent-encoded readme md", "/guid%65s/README.md", "/guid%65s"),
	)

	ginkgo.It("TestResolveCanonicalLink_ExplicitREADMEPageTwinStaysPage", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindSection, Path: "docs/sync", ContentPath: "docs/sync/index.md"},
			{Kind: EntryKindSection, Path: "guides", ContentPath: "guides/README.md"},
			{Kind: EntryKindPage, Path: "docs/sync/README.md"},
		})

		readmePage := index.Resolve("docs/a.md", "/docs/sync/README.md")
		if readmePage.Kind != TargetKindPage || readmePage.CanonicalHref != "/docs/sync/README.md" {
			t.Fatalf("README page resolution = %#v, want canonical page link", readmePage)
		}
	})

	ginkgo.It("TestNewIndexFromRootUsesWorkspaceRouteNormalization", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(rootDir, "plans"), 0o755); err != nil {
			t.Fatalf("create plans dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "plans", "index.md"), []byte("# Plans"), 0o644); err != nil {
			t.Fatalf("write plans index markdown: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "plans", "agent_hooks.PLAN.md"), []byte("# Agent Hooks Plan"), 0o644); err != nil {
			t.Fatalf("write plan markdown: %v", err)
		}

		index, err := NewIndexFromRoot(rootDir)
		if err != nil {
			t.Fatalf("NewIndexFromRoot: %v", err)
		}
		result := index.Resolve("source.md", "/plans/agent-hooks-plan.md")

		if result.Kind != TargetKindPage {
			t.Fatalf("Kind = %q, want %q: %#v", result.Kind, TargetKindPage, result)
		}
		if result.RoutePath != "plans/agent-hooks-plan" {
			t.Fatalf("RoutePath = %q, want plans/agent-hooks-plan", result.RoutePath)
		}
		if result.CanonicalHref != "/plans/agent-hooks-plan.md" {
			t.Fatalf("CanonicalHref = %q, want /plans/agent-hooks-plan.md", result.CanonicalHref)
		}

		rawAbsolute := index.Resolve("source.md", "/plans/agent_hooks.PLAN.md")
		if rawAbsolute.Kind != TargetKindUnresolved {
			t.Fatalf("raw absolute Kind = %q, want %q: %#v", rawAbsolute.Kind, TargetKindUnresolved, rawAbsolute)
		}
		if rawAbsolute.Code != "non_canonical_markdown_path" {
			t.Fatalf("raw absolute Code = %q, want non_canonical_markdown_path", rawAbsolute.Code)
		}
		if rawAbsolute.RoutePath != "plans/agent-hooks-plan" {
			t.Fatalf("raw absolute RoutePath = %q, want plans/agent-hooks-plan", rawAbsolute.RoutePath)
		}

		rawRelative := index.Resolve("plans/source.md", "./agent_hooks.PLAN.md")
		if rawRelative.Kind != TargetKindUnresolved {
			t.Fatalf("raw relative Kind = %q, want %q: %#v", rawRelative.Kind, TargetKindUnresolved, rawRelative)
		}
		if rawRelative.Code != "non_canonical_markdown_path" {
			t.Fatalf("raw relative Code = %q, want non_canonical_markdown_path", rawRelative.Code)
		}
		if rawRelative.RoutePath != "plans/agent-hooks-plan" {
			t.Fatalf("raw relative RoutePath = %q, want plans/agent-hooks-plan", rawRelative.RoutePath)
		}

		migration := index.ResolveForMigration("plans/source.md", "./agent_hooks.PLAN.md")
		if migration.Kind != TargetKindPage {
			t.Fatalf("migration Kind = %q, want %q: %#v", migration.Kind, TargetKindPage, migration)
		}
		if migration.RoutePath != "plans/agent-hooks-plan" {
			t.Fatalf("migration RoutePath = %q, want plans/agent-hooks-plan", migration.RoutePath)
		}
		if migration.CanonicalHref != "agent-hooks-plan.md" {
			t.Fatalf("migration CanonicalHref = %q, want agent-hooks-plan.md", migration.CanonicalHref)
		}
	})

	ginkgo.It("TestResolveForMigration_AmbiguousLegacyPageAndSectionLinkIsUnresolved", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindPage, Path: "docs/sync.md"},
			{Kind: EntryKindSection, Path: "docs/sync", ContentPath: "docs/sync/index.md"},
		})

		result := index.ResolveForMigration("docs/a.md", "/docs/sync")

		if result.Kind != TargetKindUnresolved {
			t.Fatalf("Kind = %q, want unresolved", result.Kind)
		}
		if result.Code != "ambiguous_legacy_link" {
			t.Fatalf("Code = %q, want ambiguous_legacy_link", result.Code)
		}
	})

	ginkgo.It("TestResolveForMigration_TrailingSlashTwinCanonicalizesToSection", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindPage, Path: "docs/sync.md"},
			{Kind: EntryKindSection, Path: "docs/sync", ContentPath: "docs/sync/index.md"},
		})

		result := index.ResolveForMigration("docs/a.md", "/docs/sync/")

		if result.Kind != TargetKindSection {
			t.Fatalf("Kind = %q, want section", result.Kind)
		}
		if result.Code != "" {
			t.Fatalf("Code = %q, want empty code", result.Code)
		}
		if result.CanonicalHref != "/docs/sync" {
			t.Fatalf("CanonicalHref = %q, want /docs/sync", result.CanonicalHref)
		}
		if result.RoutePath != "docs/sync" {
			t.Fatalf("RoutePath = %q, want docs/sync", result.RoutePath)
		}
	})

	ginkgo.It("TestResolveCanonicalLink_PrefersCanonicalSectionForExtensionlessTwin", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindPage, Path: "docs/sync.md"},
			{Kind: EntryKindSection, Path: "docs/sync", ContentPath: "docs/sync/index.md"},
		})

		result := index.Resolve("docs/a.md", "/docs/sync")

		if result.Kind != TargetKindSection {
			t.Fatalf("Kind = %q, want section", result.Kind)
		}
		if result.Code != "" {
			t.Fatalf("Code = %q, want empty code", result.Code)
		}
		if result.CanonicalHref != "/docs/sync" {
			t.Fatalf("CanonicalHref = %q, want /docs/sync", result.CanonicalHref)
		}
		if result.RoutePath != "docs/sync" {
			t.Fatalf("RoutePath = %q, want docs/sync", result.RoutePath)
		}
	})

	// - Query string and fragment are preserved byte-for-byte
	// - Link title and angle-bracket destination syntax are preserved
	ginkgo.It("TestCanonicalizeMarkdownLinks_PreservesAngleDestinationsTitleQueryAndFragment", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindPage, Path: "docs/b.md"},
		})

		result := index.RewriteMarkdown("docs/a.md", `[B](</docs/b?mode=raw#part-two> "open B")`)

		if result.Content != `[B](</docs/b.md?mode=raw#part-two> "open B")` {
			t.Fatalf("Content = %q", result.Content)
		}
		if !result.Changed {
			t.Fatalf("Changed = false, want true")
		}
		if len(result.Issues) != 0 {
			t.Fatalf("Issues = %#v, want none", result.Issues)
		}
	})

	// - Relative old page link migrates to relative .md
	// - Existing canonical .md page link is not rewritten
	ginkgo.It("TestCanonicalizeMarkdownLinks_RewritesReferenceDefinitionsAndSkipsCode", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindPage, Path: "docs/b.md"},
		})
		content := "[B][b-ref]\n\n[b-ref]: /docs/b\n\n`[B](/docs/b)`\n\n```md\n[B](/docs/b)\n```\n"

		result := index.RewriteMarkdown("docs/a.md", content)

		want := "[B][b-ref]\n\n[b-ref]: /docs/b.md\n\n`[B](/docs/b)`\n\n```md\n[B](/docs/b)\n```\n"
		if result.Content != want {
			t.Fatalf("Content = %q, want %q", result.Content, want)
		}
		if !result.Changed {
			t.Fatalf("Changed = false, want true")
		}
	})

	ginkgo.It("TestCanonicalizeMarkdownLinks_DoesNotRewriteEscapedLiteralLinks", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindPage, Path: "docs/b.md"},
		})
		content := `\[B](/docs/b)
[Real](/docs/b)`

		result := index.RewriteMarkdown("docs/a.md", content)

		want := `\[B](/docs/b)
[Real](/docs/b.md)`
		if result.Content != want {
			t.Fatalf("Content = %q, want %q", result.Content, want)
		}
		if !result.Changed {
			t.Fatalf("Changed = false, want true")
		}
	})

	ginkgo.It("TestCanonicalizeMarkdownLinks_DoesNotRewriteLiteralTextWithWhitespaceBeforeDestination", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindPage, Path: "docs/b.md"},
		})
		content := "[Space] (/docs/b)\n[Newline]\n(/docs/b)\n[Real](/docs/b)"

		result := index.RewriteMarkdown("docs/a.md", content)

		want := "[Space] (/docs/b)\n[Newline]\n(/docs/b)\n[Real](/docs/b.md)"
		if result.Content != want {
			t.Fatalf("Content = %q, want %q", result.Content, want)
		}
		if !result.Changed {
			t.Fatalf("Changed = false, want true")
		}
	})

	ginkgo.It("TestCanonicalizeMarkdownLinks_DoesNotRewriteMalformedInlineLinkTails", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindPage, Path: "docs/b.md"},
		})
		content := "[MissingTitleClose](/docs/b \"title\"\n[UnquotedTitle](/docs/b title)\n[NewlineTail](/docs/b\ntext)\n[LeadingSpace]( /docs/b)\n[QuotedTitle](/docs/b \"title\")\n[ParenTitle](/docs/b (title))"

		result := index.RewriteMarkdown("docs/a.md", content)

		want := "[MissingTitleClose](/docs/b \"title\"\n[UnquotedTitle](/docs/b title)\n[NewlineTail](/docs/b\ntext)\n[LeadingSpace]( /docs/b.md)\n[QuotedTitle](/docs/b.md \"title\")\n[ParenTitle](/docs/b.md (title))"
		if result.Content != want {
			t.Fatalf("Content = %q, want %q", result.Content, want)
		}
		if !result.Changed {
			t.Fatalf("Changed = false, want true")
		}
	})

	ginkgo.It("TestCanonicalizeMarkdownLinks_DoesNotRewriteImageOnlyReferenceDefinitions", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindPage, Path: "docs/b.md"},
		})
		content := "![B][b-ref]\n\n[b-ref]: /docs/b\n"

		result := index.RewriteMarkdown("docs/a.md", content)

		if result.Content != content {
			t.Fatalf("Content = %q, want image reference definition unchanged", result.Content)
		}
		if result.Changed {
			t.Fatalf("Changed = true, want false")
		}
	})

	ginkgo.It("TestCanonicalizeMarkdownLinks_RewritesNestedListLinks", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindPage, Path: "docs/b.md"},
		})
		content := "- parent\n    - [Target](/docs/b)\n"

		result := index.RewriteMarkdown("docs/a.md", content)

		want := "- parent\n    - [Target](/docs/b.md)\n"
		if result.Content != want {
			t.Fatalf("Content = %q, want %q", result.Content, want)
		}
		if !result.Changed {
			t.Fatalf("Changed = false, want true")
		}
	})

	ginkgo.It("TestCanonicalizeMarkdownLinks_SortsReferenceAndInlineReplacementsByOffset", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindPage, Path: "docs/b.md"},
		})
		content := "[b-ref]: /docs/b\n\n[B](/docs/b)\n"

		result := index.RewriteMarkdown("docs/a.md", content)

		want := "[b-ref]: /docs/b.md\n\n[B](/docs/b.md)\n"
		if result.Content != want {
			t.Fatalf("Content = %q, want %q", result.Content, want)
		}
		if !result.Changed {
			t.Fatalf("Changed = false, want true")
		}
	})

	// - Link-like text in inline code and fenced code is ignored
	ginkgo.It("TestCanonicalizeMarkdownLinks_SkipsMultiBacktickCodeSpans", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindPage, Path: "docs/b.md"},
		})
		content := "``[B](/docs/b)``\n\n[B](/docs/b)\n"

		result := index.RewriteMarkdown("docs/a.md", content)

		want := "``[B](/docs/b)``\n\n[B](/docs/b.md)\n"
		if result.Content != want {
			t.Fatalf("Content = %q, want %q", result.Content, want)
		}
	})

	ginkgo.It("TestCanonicalizeMarkdownLinks_SkipsIndentedCodeBlocks", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindPage, Path: "docs/b.md"},
		})
		content := "Example:\n\n    [B](/docs/b)\n\t[C](/docs/b)\n\n[B](/docs/b)\n"

		result := index.RewriteMarkdown("docs/a.md", content)

		want := "Example:\n\n    [B](/docs/b)\n\t[C](/docs/b)\n\n[B](/docs/b.md)\n"
		if result.Content != want {
			t.Fatalf("Content = %q, want %q", result.Content, want)
		}
	})

	// - External and non-page links are ignored
	ginkgo.DescribeTable("TestResolveCanonicalLink_TreatsProtocolRelativeAndSchemedURLsAsExternal",
		func(href string) {
			t := ginkgo.GinkgoT()
			index := NewIndex([]Entry{
				{Kind: EntryKindPage, Path: "cdn.example.com/lib.md"},
			})

			result := index.Resolve("docs/a.md", href)
			if result.Kind != TargetKindExternal {
				t.Fatalf("Resolve(%q).Kind = %q, want external", href, result.Kind)
			}
			if result.CanonicalHref != href {
				t.Fatalf("Resolve(%q).CanonicalHref = %q, want original href", href, result.CanonicalHref)
			}
		},
		ginkgo.Entry("protocol-relative", "//cdn.example.com/lib.md"),
		ginkgo.Entry("schemed URL", "obsidian://open?vault=wiki"),
	)

	// - Percent-encoded paths use exact filesystem matching
	ginkgo.It("TestResolveCanonicalLink_PreservesPercentEncodedPathStyle", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex([]Entry{
			{Kind: EntryKindPage, Path: "docs/space name.md"},
		})

		result := index.Resolve("docs/a.md", "/docs/space%20name?x=1#part")

		if result.Kind != TargetKindPage {
			t.Fatalf("Kind = %q, want %q", result.Kind, TargetKindPage)
		}
		if result.CanonicalHref != "/docs/space%20name.md?x=1#part" {
			t.Fatalf("CanonicalHref = %q, want /docs/space%%20name.md?x=1#part", result.CanonicalHref)
		}
	})

	ginkgo.DescribeTable("NormalizeMarkdownLinkRootPrefix accepts stable path forms",
		func(input string, want string) {
			t := ginkgo.GinkgoT()
			got, err := NormalizeMarkdownLinkRootPrefix(input)
			if err != nil {
				t.Fatalf("NormalizeMarkdownLinkRootPrefix(%q) error = %v", input, err)
			}
			if got != want {
				t.Fatalf("NormalizeMarkdownLinkRootPrefix(%q) = %q, want %q", input, got, want)
			}
		},
		ginkgo.Entry("empty", "", ""),
		ginkgo.Entry("blank", " \t\n ", ""),
		ginkgo.Entry("relative prefix", "docs", "/docs"),
		ginkgo.Entry("mixed case relative prefix with trailing slash", " Docs/Sync/ ", "/Docs/Sync"),
		ginkgo.Entry("absolute prefix with redundant slash", "/docs/sync/", "/docs/sync"),
	)

	ginkgo.It("RewriteMarkdown leaves empty content unchanged", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex(nil)

		result := index.RewriteMarkdown("index.md", "")

		if result.Content != "" {
			t.Fatalf("Content = %q, want empty", result.Content)
		}
		if result.Changed {
			t.Fatalf("Changed = true, want false")
		}
		if len(result.Issues) != 0 {
			t.Fatalf("Issues = %#v, want none", result.Issues)
		}
	})

	ginkgo.It("RewriteMarkdown reports invalid and unresolved destinations without rewriting", func() {
		t := ginkgo.GinkgoT()
		index := NewIndex(nil)
		content := "[Missing](/missing.md)\n[Bad](/docs/%zz)\n"

		result := index.RewriteMarkdown("index.md", content)

		if result.Content != content {
			t.Fatalf("Content = %q, want unchanged %q", result.Content, content)
		}
		if result.Changed {
			t.Fatalf("Changed = true, want false")
		}
		wantIssues := []Issue{
			{Code: IssueCodeBrokenPage, Destination: "/missing.md"},
			{Code: IssueCodeInvalidPercentEncoding, Destination: "/docs/%zz"},
		}
		if len(result.Issues) != len(wantIssues) {
			t.Fatalf("Issues = %#v, want %#v", result.Issues, wantIssues)
		}
		for i := range wantIssues {
			if result.Issues[i] != wantIssues[i] {
				t.Fatalf("Issues[%d] = %#v, want %#v", i, result.Issues[i], wantIssues[i])
			}
		}
	})

	ginkgo.It("ScanInlineDestinations skips images by default and can include them", func() {
		t := ginkgo.GinkgoT()
		content := "![Logo](/assets/logo.png) [Page](/docs/page)"

		defaultDestinations := ScanInlineDestinations(content, InlineScanOptions{})
		if len(defaultDestinations) != 1 {
			t.Fatalf("default destinations = %#v, want one page link", defaultDestinations)
		}
		if defaultDestinations[0].Destination != "/docs/page" || defaultDestinations[0].Image {
			t.Fatalf("default destination = %#v, want non-image page link", defaultDestinations[0])
		}

		withImages := ScanInlineDestinations(content, InlineScanOptions{IncludeImages: true})
		if len(withImages) != 2 {
			t.Fatalf("with images destinations = %#v, want image plus page", withImages)
		}
		if withImages[0].Destination != "/assets/logo.png" || !withImages[0].Image {
			t.Fatalf("withImages[0] = %#v, want image destination", withImages[0])
		}
		if withImages[1].Destination != "/docs/page" || withImages[1].Image {
			t.Fatalf("withImages[1] = %#v, want non-image page link", withImages[1])
		}
	})

	ginkgo.It("ScanInlineDestinations can include links inside code ranges", func() {
		t := ginkgo.GinkgoT()
		content := "`[Code](/docs/code)` [Page](/docs/page)"

		defaultDestinations := ScanInlineDestinations(content, InlineScanOptions{})
		if len(defaultDestinations) != 1 || defaultDestinations[0].Destination != "/docs/page" {
			t.Fatalf("default destinations = %#v, want only page link", defaultDestinations)
		}

		withCode := ScanInlineDestinations(content, InlineScanOptions{IgnoreCodeRanges: true})
		if len(withCode) != 2 {
			t.Fatalf("with code destinations = %#v, want code plus page", withCode)
		}
		if withCode[0].Destination != "/docs/code" || withCode[1].Destination != "/docs/page" {
			t.Fatalf("with code destinations = %#v, want code then page", withCode)
		}
	})

	ginkgo.It("NewIndexFromRootWithOptions applies markdown link root prefix to canonical hrefs", func() {
		t := ginkgo.GinkgoT()
		rootDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755); err != nil {
			t.Fatalf("create docs dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(rootDir, "docs", "page.md"), []byte("# Page"), 0o644); err != nil {
			t.Fatalf("write page markdown: %v", err)
		}

		index, err := NewIndexFromRootWithOptions(rootDir, Options{MarkdownLinkRootPrefix: "/wiki"})
		if err != nil {
			t.Fatalf("NewIndexFromRootWithOptions: %v", err)
		}

		result := index.Resolve("source.md", "/wiki/docs/page.md")
		if result.Kind != TargetKindPage {
			t.Fatalf("Kind = %q, want %q: %#v", result.Kind, TargetKindPage, result)
		}
		if string(result.RoutePath) != "docs/page" {
			t.Fatalf("RoutePath = %q, want docs/page", result.RoutePath)
		}
		if result.CanonicalHref != "/wiki/docs/page.md" {
			t.Fatalf("CanonicalHref = %q, want /wiki/docs/page.md", result.CanonicalHref)
		}
	})
})
