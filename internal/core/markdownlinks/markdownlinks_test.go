package markdownlinks

import "testing"

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

// - Relative page links are resolved from the source file directory
func TestResolveCanonicalLink_UsesFilesystemRelativeSemanticsNotPageAsFolder(t *testing.T) {
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
}

// - Section trailing slash is accepted but canonicalized away
func TestResolveCanonicalLink_SectionTrailingSlashIsAcceptedButCanonicalizedAway(t *testing.T) {
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
}

// - Root section link remains slash
func TestResolveCanonicalLink_RelativeRootSectionCanonicalizesToSlash(t *testing.T) {
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
}

func TestResolveCanonicalLink_ClassifiesPageSectionAssetExternalInvalidAndUnresolved(t *testing.T) {
	index := NewIndex([]Entry{
		{Kind: EntryKindPage, Path: "docs/b.md"},
		{Kind: EntryKindSection, Path: "docs/sync", ContentPath: "docs/sync/index.md"},
		{Kind: EntryKindAsset, Path: "assets/page/logo.png"},
	})

	cases := []struct {
		name string
		href string
		kind TargetKind
		code string
	}{
		{name: "page", href: "/docs/b.md", kind: TargetKindPage},
		{name: "section", href: "/docs/sync", kind: TargetKindSection},
		{name: "asset namespace", href: "/assets/page/logo.png", kind: TargetKindAsset},
		{name: "asset extension", href: "/docs/manual.pdf", kind: TargetKindAsset},
		{name: "external", href: "https://example.com", kind: TargetKindExternal},
		{name: "mailto", href: "mailto:a@example.com", kind: TargetKindExternal},
		{name: "hash", href: "#heading", kind: TargetKindExternal},
		{name: "invalid percent", href: "/docs/%zz", kind: TargetKindInvalid, code: "invalid_percent_encoding"},
		{name: "escape", href: "../../../outside.md", kind: TargetKindInvalid, code: "workspace_escape"},
		{name: "unresolved", href: "/docs/missing.md", kind: TargetKindUnresolved, code: "broken_page"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := index.Resolve("docs/a/current.md", tc.href)
			if result.Kind != tc.kind {
				t.Fatalf("Kind = %q, want %q", result.Kind, tc.kind)
			}
			if result.Code != tc.code {
				t.Fatalf("Code = %q, want %q", result.Code, tc.code)
			}
		})
	}
}

// - Explicit index.md section link canonicalizes to the section
// - Explicit README.md section fallback link canonicalizes to the section
func TestResolveCanonicalLink_ExplicitSectionDefaultFilesCanonicalizeToSection(t *testing.T) {
	index := NewIndex([]Entry{
		{Kind: EntryKindSection, Path: "docs/sync", ContentPath: "docs/sync/index.md"},
		{Kind: EntryKindSection, Path: "guides", ContentPath: "guides/README.md"},
		{Kind: EntryKindPage, Path: "docs/sync/README.md"},
	})

	cases := []struct {
		href string
		want string
	}{
		{href: "/docs/sync/index.md", want: "/docs/sync"},
		{href: "/docs/%73ync/index.md", want: "/docs/%73ync"},
		{href: "/guides/README.md", want: "/guides"},
		{href: "/guid%65s/README.md", want: "/guid%65s"},
	}
	for _, tc := range cases {
		result := index.Resolve("docs/a.md", tc.href)
		if result.Kind != TargetKindSection {
			t.Fatalf("Resolve(%q).Kind = %q, want section", tc.href, result.Kind)
		}
		if result.CanonicalHref != tc.want {
			t.Fatalf("Resolve(%q).CanonicalHref = %q, want %q", tc.href, result.CanonicalHref, tc.want)
		}
	}

	readmePage := index.Resolve("docs/a.md", "/docs/sync/README.md")
	if readmePage.Kind != TargetKindPage || readmePage.CanonicalHref != "/docs/sync/README.md" {
		t.Fatalf("README page resolution = %#v, want canonical page link", readmePage)
	}
}

func TestResolveForMigration_AmbiguousLegacyPageAndSectionLinkIsUnresolved(t *testing.T) {
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
}

func TestResolveForMigration_TrailingSlashTwinCanonicalizesToSection(t *testing.T) {
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
}

func TestResolveCanonicalLink_PrefersCanonicalSectionForExtensionlessTwin(t *testing.T) {
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
}

// - Query string and fragment are preserved byte-for-byte
// - Link title and angle-bracket destination syntax are preserved
func TestCanonicalizeMarkdownLinks_PreservesAngleDestinationsTitleQueryAndFragment(t *testing.T) {
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
}

// - Relative old page link migrates to relative .md
// - Existing canonical .md page link is not rewritten
func TestCanonicalizeMarkdownLinks_RewritesReferenceDefinitionsAndSkipsCode(t *testing.T) {
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
}

func TestCanonicalizeMarkdownLinks_DoesNotRewriteEscapedLiteralLinks(t *testing.T) {
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
}

func TestCanonicalizeMarkdownLinks_DoesNotRewriteLiteralTextWithWhitespaceBeforeDestination(t *testing.T) {
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
}

func TestCanonicalizeMarkdownLinks_DoesNotRewriteMalformedInlineLinkTails(t *testing.T) {
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
}

func TestCanonicalizeMarkdownLinks_DoesNotRewriteImageOnlyReferenceDefinitions(t *testing.T) {
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
}

func TestCanonicalizeMarkdownLinks_RewritesNestedListLinks(t *testing.T) {
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
}

func TestCanonicalizeMarkdownLinks_SortsReferenceAndInlineReplacementsByOffset(t *testing.T) {
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
}

// - Link-like text in inline code and fenced code is ignored
func TestCanonicalizeMarkdownLinks_SkipsMultiBacktickCodeSpans(t *testing.T) {
	index := NewIndex([]Entry{
		{Kind: EntryKindPage, Path: "docs/b.md"},
	})
	content := "``[B](/docs/b)``\n\n[B](/docs/b)\n"

	result := index.RewriteMarkdown("docs/a.md", content)

	want := "``[B](/docs/b)``\n\n[B](/docs/b.md)\n"
	if result.Content != want {
		t.Fatalf("Content = %q, want %q", result.Content, want)
	}
}

func TestCanonicalizeMarkdownLinks_SkipsIndentedCodeBlocks(t *testing.T) {
	index := NewIndex([]Entry{
		{Kind: EntryKindPage, Path: "docs/b.md"},
	})
	content := "Example:\n\n    [B](/docs/b)\n\t[C](/docs/b)\n\n[B](/docs/b)\n"

	result := index.RewriteMarkdown("docs/a.md", content)

	want := "Example:\n\n    [B](/docs/b)\n\t[C](/docs/b)\n\n[B](/docs/b.md)\n"
	if result.Content != want {
		t.Fatalf("Content = %q, want %q", result.Content, want)
	}
}

// - External and non-page links are ignored
func TestResolveCanonicalLink_TreatsProtocolRelativeAndSchemedURLsAsExternal(t *testing.T) {
	index := NewIndex([]Entry{
		{Kind: EntryKindPage, Path: "cdn.example.com/lib.md"},
	})

	for _, href := range []string{"//cdn.example.com/lib.md", "obsidian://open?vault=wiki"} {
		result := index.Resolve("docs/a.md", href)
		if result.Kind != TargetKindExternal {
			t.Fatalf("Resolve(%q).Kind = %q, want external", href, result.Kind)
		}
		if result.CanonicalHref != href {
			t.Fatalf("Resolve(%q).CanonicalHref = %q, want original href", href, result.CanonicalHref)
		}
	}
}

// - Percent-encoded paths use exact filesystem matching
func TestResolveCanonicalLink_PreservesPercentEncodedPathStyle(t *testing.T) {
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
}
