package search

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
	_ "modernc.org/sqlite" // Import SQLite driver
)

var _ = ginkgo.Describe("SQLite search index", func() {
	ginkgo.It("TestSQLiteIndex_IndexPage", func() {
		t := ginkgo.GinkgoT()
		tmpDir := t.TempDir()

		index, err := NewSQLiteIndex(tmpDir)
		if err != nil {
			t.Fatalf("failed to create SQLiteIndex: %v", err)
		}
		defer closeSQLiteIndex(index)

		// Testdata
		path := "docs/test.md"
		pageID := newFixturePageID("test123")
		title := "Test Page"
		content := "This is a **test** page."
		expectedContent := "This is a test page."

		err = index.IndexPage(path, path, pageID, title, tree.NodeKindPage, content)
		if err != nil {
			t.Fatalf("IndexPage failed: %v", err)
		}

		var row *sql.Row

		if err := index.withDB(func(db *sql.DB) error {
			row = db.QueryRow(`SELECT path, title, content FROM pages WHERE pageID = ?`, pageID)
			if row == nil {
				t.Fatalf("no data found for pageID %s", pageID)
			}
			return nil
		}); err != nil {
			t.Fatalf("failed to read indexed data: %v", err)
		}

		var gotPath, gotTitle, gotContent string
		err = row.Scan(&gotPath, &gotTitle, &gotContent)
		if err != nil {
			t.Fatalf("failed to read indexed data: %v", err)
		}

		// Assertions
		if gotPath != path {
			t.Errorf("expected path %s, got %s", path, gotPath)
		}
		if gotTitle != title {
			t.Errorf("expected title %s, got %s", title, gotTitle)
		}
		if !strings.HasPrefix(gotContent, expectedContent) {
			t.Errorf("expected content '%s', got '%s'", expectedContent, gotContent)
		}
	})

	ginkgo.It("TestSearchIndexDatabasePath_WindowsPath", func() {
		t := ginkgo.GinkgoT()
		got := strings.ReplaceAll(searchIndexDatabasePath(`C:\wiki\data`, "search.db"), `\`, `/`)
		want := `C:/wiki/data/search.db`
		if got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
	})

	ginkgo.It("TestSQLiteIndex_CreatesDatabaseInStorageDir", func() {
		t := ginkgo.GinkgoT()
		tmpDir := t.TempDir()

		index, err := NewSQLiteIndex(tmpDir)
		if err != nil {
			t.Fatalf("failed to create SQLiteIndex: %v", err)
		}
		defer closeSQLiteIndex(index)

		if _, err := os.Stat(filepath.Join(tmpDir, "search.db")); err != nil {
			t.Fatalf("expected search.db in storage dir, got err: %v", err)
		}
	})

	ginkgo.It("TestSQLiteIndex_Search", func() {
		t := ginkgo.GinkgoT()
		tmpDir := t.TempDir()

		index, err := NewSQLiteIndex(tmpDir)
		if err != nil {
			t.Fatalf("failed to create SQLiteIndex: %v", err)
		}
		defer closeSQLiteIndex(index)

		// Index two pages
		err = index.IndexPage("notes/alpha", "notes/alpha.md", "alpha1", "Alpha Search Test", tree.NodeKindPage, "This content is about SQLite search.")
		if err != nil {
			t.Fatalf("failed to index alpha page: %v", err)
		}

		err = index.IndexPage("notes/beta", "notes/beta.md", "beta2", "Unrelated Page", tree.NodeKindSection, "This content is not about the search term.")
		if err != nil {
			t.Fatalf("failed to index beta page: %v", err)
		}

		// Perform search
		result, err := index.Search("content:search*", nil, 0, 10)
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		// Assertions
		if result.Count != 2 {
			t.Errorf("expected 2 result, got %d", result.Count)
		}

		if len(result.Items) != 2 {
			t.Fatalf("expected 2 result item, got %d", len(result.Items))
		}

		if result.Items[0].PageID != "alpha1" {
			t.Errorf("expected alpha1 to be ranked first, got %s", result.Items[0].PageID)
		}

		if result.Items[0].Kind != string(tree.NodeKindPage) {
			t.Errorf("expected first result kind %q, got %q", tree.NodeKindPage, result.Items[0].Kind)
		}

		if result.Items[1].Kind != string(tree.NodeKindSection) {
			t.Errorf("expected second result kind %q, got %q", tree.NodeKindSection, result.Items[1].Kind)
		}

		if !strings.Contains(result.Items[0].Excerpt, "<b>") {
			t.Errorf("expected highlighted search snippet, got %q", result.Items[0].Excerpt)
		}
	})

	ginkgo.It("TestSQLiteIndex_Search_RanksTitleMatchHigherThanContent", func() {
		t := ginkgo.GinkgoT()
		tmpDir := t.TempDir()

		index, err := NewSQLiteIndex(tmpDir)
		if err != nil {
			t.Fatalf("failed to create SQLiteIndex: %v", err)
		}
		defer closeSQLiteIndex(index)

		// page with match in title
		err = index.IndexPage(
			"docs/titleMatch",
			"docs/titleMatch.md",
			"titleMatch",
			"Search term in title",
			tree.NodeKindPage,
			"Lorem ipsum dolor sit amet.",
		)
		if err != nil {
			t.Fatalf("failed to index titleMatch page: %v", err)
		}

		// page with match only in content
		err = index.IndexPage(
			"docs/contentMatch",
			"docs/contentMatch.md",
			"contentMatch",
			"Content only match",
			tree.NodeKindPage,
			"This page has the search term only in the content.",
		)
		if err != nil {
			t.Fatalf("failed to index contentMatch page: %v", err)
		}

		// "search" is converted by buildFuzzyQuery to "search*", matching both
		result, err := index.Search("search", nil, 0, 10)
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		if result.Count != 2 {
			t.Fatalf("expected 2 results, got %d", result.Count)
		}
		if len(result.Items) != 2 {
			t.Fatalf("expected 2 result items, got %d", len(result.Items))
		}

		// Title match should be ranked higher than content match
		if result.Items[0].PageID != "titleMatch" {
			t.Errorf("expected titleMatch to be ranked first, got %s", result.Items[0].PageID)
		}

		// and the rank value should be higher (because 1/(1+score), score smaller)
		if result.Items[0].Rank < result.Items[1].Rank {
			t.Errorf("expected higher rank for titleMatch (got %f, %f)", result.Items[0].Rank, result.Items[1].Rank)
		}

		// sanity check: Ranks should be > 0 and <= 1
		for i, item := range result.Items {
			if item.Rank <= 0 || item.Rank > 1 {
				t.Errorf("expected rank for item %d to be in (0,1], got %f", i, item.Rank)
			}
		}
	})

	ginkgo.It("TestSQLiteIndex_Search_RanksHeadingHigherThanContent", func() {
		t := ginkgo.GinkgoT()
		tmpDir := t.TempDir()

		index, err := NewSQLiteIndex(tmpDir)
		if err != nil {
			t.Fatalf("failed to create SQLiteIndex: %v", err)
		}
		defer closeSQLiteIndex(index)

		// page with match in heading (Markdown heading)
		err = index.IndexPage(
			"docs/headingMatch",
			"docs/headingMatch.md",
			"headingMatch",
			"No search in title",
			tree.NodeKindPage,
			"## Search term in heading\n\nSome additional body text.",
		)
		if err != nil {
			t.Fatalf("failed to index headingMatch page: %v", err)
		}

		// page with match only in content
		err = index.IndexPage(
			"docs/contentOnly",
			"docs/contentOnly.md",
			"contentOnly",
			"No search in title",
			tree.NodeKindPage,
			"This page has the search term only in the content.",
		)
		if err != nil {
			t.Fatalf("failed to index contentOnly page: %v", err)
		}

		result, err := index.Search("search", nil, 0, 10)
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		if result.Count != 2 {
			t.Fatalf("expected 2 results, got %d", result.Count)
		}
		if len(result.Items) != 2 {
			t.Fatalf("expected 2 result items, got %d", len(result.Items))
		}

		// Heading match should be ranked higher than content match
		if result.Items[0].PageID != "headingMatch" {
			t.Errorf("expected headingMatch to be ranked first, got %s", result.Items[0].PageID)
		}

		if result.Items[0].Rank < result.Items[1].Rank {
			t.Errorf("expected higher rank for headingMatch (got %f, %f)", result.Items[0].Rank, result.Items[1].Rank)
		}
	})

	ginkgo.It("TestSQLiteIndex_SearchPageIDs_RespectsQueryAndPageFilters", func() {
		t := ginkgo.GinkgoT()
		tmpDir := t.TempDir()

		index, err := NewSQLiteIndex(tmpDir)
		if err != nil {
			t.Fatalf("failed to create SQLiteIndex: %v", err)
		}
		defer closeSQLiteIndex(index)

		err = index.IndexPage("docs/alpha", "docs/alpha.md", "alpha", "Alpha Page", tree.NodeKindPage, "Shared token in alpha.")
		if err != nil {
			t.Fatalf("failed to index alpha page: %v", err)
		}

		err = index.IndexPage("docs/beta", "docs/beta.md", "beta", "Beta Page", tree.NodeKindPage, "Shared token in beta.")
		if err != nil {
			t.Fatalf("failed to index beta page: %v", err)
		}

		err = index.IndexPage("docs/gamma", "docs/gamma.md", "gamma", "Gamma Page", tree.NodeKindPage, "Gamma only content.")
		if err != nil {
			t.Fatalf("failed to index gamma page: %v", err)
		}

		pageIDs, err := index.SearchPageIDs("shared token", []tree.PageID{"alpha"})
		if err != nil {
			t.Fatalf("SearchPageIDs failed: %v", err)
		}

		if len(pageIDs) != 1 || pageIDs[0] != "alpha" {
			t.Fatalf("expected only alpha page, got %#v", pageIDs)
		}

		noMatches, err := index.SearchPageIDs("shared token", []tree.PageID{})
		if err != nil {
			t.Fatalf("SearchPageIDs with empty page filter failed: %v", err)
		}
		if len(noMatches) != 0 {
			t.Fatalf("expected no matches for empty page filter, got %#v", noMatches)
		}
	})

	ginkgo.It("TestSQLiteIndex_Search_FiltersByPageIDs", func() {
		t := ginkgo.GinkgoT()
		tmpDir := t.TempDir()

		index, err := NewSQLiteIndex(tmpDir)
		if err != nil {
			t.Fatalf("failed to create SQLiteIndex: %v", err)
		}
		defer closeSQLiteIndex(index)

		err = index.IndexPage(
			"docs/react-guide",
			"docs/react-guide.md",
			"react-guide",
			"React guide",
			tree.NodeKindPage,
			"Search term appears here.",
		)
		if err != nil {
			t.Fatalf("failed to index react-guide page: %v", err)
		}

		err = index.IndexPage(
			"docs/plain-guide",
			"docs/plain-guide.md",
			"plain-guide",
			"Plain guide",
			tree.NodeKindPage,
			"Search term appears here as well.",
		)
		if err != nil {
			t.Fatalf("failed to index plain-guide page: %v", err)
		}

		result, err := index.Search("search", []tree.PageID{"react-guide"}, 0, 10)
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		if result.Count != 1 {
			t.Fatalf("expected 1 filtered result, got %d", result.Count)
		}
		if len(result.Items) != 1 {
			t.Fatalf("expected 1 filtered result item, got %d", len(result.Items))
		}
		if result.Items[0].PageID != "react-guide" {
			t.Fatalf("expected filtered page react-guide, got %s", result.Items[0].PageID)
		}
	})

	ginkgo.It("TestSQLiteIndex_Search_ReturnsNoResultsWhenPageIDFilterIsEmpty", func() {
		t := ginkgo.GinkgoT()
		tmpDir := t.TempDir()

		index, err := NewSQLiteIndex(tmpDir)
		if err != nil {
			t.Fatalf("failed to create SQLiteIndex: %v", err)
		}
		defer closeSQLiteIndex(index)

		err = index.IndexPage(
			"docs/react-guide",
			"docs/react-guide.md",
			"react-guide",
			"React guide",
			tree.NodeKindPage,
			"Search term appears here.",
		)
		if err != nil {
			t.Fatalf("failed to index react-guide page: %v", err)
		}

		result, err := index.Search("search", []tree.PageID{}, 0, 10)
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}

		if result.Count != 0 {
			t.Fatalf("expected 0 filtered results, got %d", result.Count)
		}
		if len(result.Items) != 0 {
			t.Fatalf("expected 0 filtered result items, got %d", len(result.Items))
		}
	})

	ginkgo.It("TestSQLiteIndex_IndexPage_StripsShoutoutFenceSyntaxButKeepsLabel", func() {
		t := ginkgo.GinkgoT()
		tmpDir := t.TempDir()

		index, err := NewSQLiteIndex(tmpDir)
		if err != nil {
			t.Fatalf("failed to create SQLiteIndex: %v", err)
		}
		defer closeSQLiteIndex(index)

		err = index.IndexPage(
			"docs/shoutout",
			"docs/shoutout.md",
			"shoutout1",
			"Shoutout Page",
			tree.NodeKindPage,
			strings.Join([]string{
				"::: blue",
				"Shoutout body text.",
				":::",
			}, "\n"),
		)
		if err != nil {
			t.Fatalf("IndexPage failed: %v", err)
		}

		var gotContent string
		if err := index.withDB(func(db *sql.DB) error {
			return db.QueryRow(`SELECT content FROM pages WHERE pageID = ?`, "shoutout1").Scan(&gotContent)
		}); err != nil {
			t.Fatalf("failed to read indexed content: %v", err)
		}

		if strings.Contains(gotContent, ":::") {
			t.Fatalf("expected indexed content to exclude shoutout fences, got %q", gotContent)
		}
		if !strings.Contains(gotContent, "blue") {
			t.Fatalf("expected indexed content to keep shoutout label, got %q", gotContent)
		}
		if !strings.Contains(gotContent, "Shoutout body text.") {
			t.Fatalf("expected indexed content to keep shoutout body, got %q", gotContent)
		}
	})

	ginkgo.It("TestSQLiteIndex_IndexPage_StripsMarkdownFormattingFromIndexedContent", func() {
		t := ginkgo.GinkgoT()
		tmpDir := t.TempDir()

		index, err := NewSQLiteIndex(tmpDir)
		if err != nil {
			t.Fatalf("failed to create SQLiteIndex: %v", err)
		}
		defer closeSQLiteIndex(index)

		err = index.IndexPage(
			"docs/markdown",
			"docs/markdown.md",
			"markdown1",
			"Markdown Page",
			tree.NodeKindPage,
			"LeafWiki **fett** und _kursiv_.",
		)
		if err != nil {
			t.Fatalf("IndexPage failed: %v", err)
		}

		var gotContent string
		if err := index.withDB(func(db *sql.DB) error {
			return db.QueryRow(`SELECT content FROM pages WHERE pageID = ?`, "markdown1").Scan(&gotContent)
		}); err != nil {
			t.Fatalf("failed to read indexed content: %v", err)
		}

		if strings.Contains(gotContent, "**") || strings.Contains(gotContent, "_") {
			t.Fatalf("expected indexed content to exclude markdown emphasis markers, got %q", gotContent)
		}
	})

	ginkgo.It("TestExtractHeadings_SingleH1", func() {
		t := ginkgo.GinkgoT()
		got := extractHeadings("# Hello World")
		if !strings.Contains(got, "Hello World") {
			t.Errorf("expected heading text, got %q", got)
		}
	})

	ginkgo.It("TestExtractHeadings_MultipleHeadings", func() {
		t := ginkgo.GinkgoT()
		input := "# First\n## Second\n### Third"
		got := extractHeadings(input)
		for _, want := range []string{"First", "Second", "Third"} {
			if !strings.Contains(got, want) {
				t.Errorf("expected %q in result, got %q", want, got)
			}
		}
	})

	ginkgo.It("TestExtractHeadings_InlineFormatting", func() {
		t := ginkgo.GinkgoT()
		got := extractHeadings("## **Bold** and _italic_ heading")
		if strings.Contains(got, "**") || strings.Contains(got, "_") {
			t.Errorf("expected formatting markers stripped, got %q", got)
		}
		if !strings.Contains(got, "Bold") || !strings.Contains(got, "italic") || !strings.Contains(got, "heading") {
			t.Errorf("expected heading words preserved, got %q", got)
		}
	})

	ginkgo.It("TestExtractHeadings_IgnoresBodyText", func() {
		t := ginkgo.GinkgoT()
		got := extractHeadings("# Title\n\nSome body paragraph that should not appear.")
		if strings.Contains(got, "body paragraph") {
			t.Errorf("body text should not appear in headings, got %q", got)
		}
		if !strings.Contains(got, "Title") {
			t.Errorf("expected heading text, got %q", got)
		}
	})

	ginkgo.It("TestExtractHeadings_NoHeadings", func() {
		t := ginkgo.GinkgoT()
		got := extractHeadings("Just plain text without any heading.")
		if got != "" {
			t.Errorf("expected empty result for no headings, got %q", got)
		}
	})

	ginkgo.It("TestExtractHeadings_EmptyInput", func() {
		t := ginkgo.GinkgoT()
		got := extractHeadings("")
		if got != "" {
			t.Errorf("expected empty result for empty input, got %q", got)
		}
	})

	ginkgo.It("TestExtractHeadings_HeadingWithCodeSpan", func() {
		t := ginkgo.GinkgoT()
		got := extractHeadings("## Heading `code` here")
		if strings.Contains(got, "`") {
			t.Errorf("expected backticks stripped, got %q", got)
		}
		if !strings.Contains(got, "Heading") || !strings.Contains(got, "here") {
			t.Errorf("expected heading words preserved, got %q", got)
		}
	})

	ginkgo.It("Clear removes indexed pages from subsequent searches", func() {
		t := ginkgo.GinkgoT()
		index, err := NewSQLiteIndex(t.TempDir())
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			Expect(index.Close()).To(Succeed())
		}()

		Expect(index.IndexPage("docs/alpha", "docs/alpha.md", "alpha", "Alpha", tree.NodeKindPage, "shared token")).To(Succeed())
		Expect(index.IndexPage("docs/beta", "docs/beta.md", "beta", "Beta", tree.NodeKindPage, "shared token")).To(Succeed())

		before, err := index.Search("shared", nil, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(before.Count).To(Equal(2))

		Expect(index.Clear()).To(Succeed())
		after, err := index.Search("shared", nil, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(after.Count).To(Equal(0))
		Expect(after.Items).To(BeEmpty())
	})

	ginkgo.It("RemovePage and RemovePageByFilePath remove only matching records", func() {
		t := ginkgo.GinkgoT()
		index, err := NewSQLiteIndex(t.TempDir())
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			Expect(index.Close()).To(Succeed())
		}()

		Expect(index.IndexPage("docs/alpha", "docs/alpha.md", "alpha", "Alpha", tree.NodeKindPage, "shared token")).To(Succeed())
		Expect(index.IndexPage("docs/beta", "docs/beta.md", "beta", "Beta", tree.NodeKindPage, "shared token")).To(Succeed())
		Expect(index.IndexPage("docs/gamma", "docs/gamma.md", "gamma", "Gamma", tree.NodeKindPage, "shared token")).To(Succeed())

		Expect(index.RemovePage("alpha")).To(Succeed())
		rows, err := index.RemovePageByFilePath("docs/beta.md")
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(Equal(int64(1)))
		rows, err = index.RemovePageByFilePath("docs/missing.md")
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(Equal(int64(0)))

		pageIDs, err := index.SearchPageIDs("shared", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(pageIDs).To(Equal([]tree.PageID{"gamma"}))
	})

	ginkgo.It("empty query with page filters returns matching pages ordered by title and path", func() {
		t := ginkgo.GinkgoT()
		index, err := NewSQLiteIndex(t.TempDir())
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			Expect(index.Close()).To(Succeed())
		}()

		Expect(index.IndexPage("docs/zeta", "docs/zeta.md", "zeta", "Zeta", tree.NodeKindPage, "Zeta body")).To(Succeed())
		Expect(index.IndexPage("docs/alpha-b", "docs/alpha-b.md", "alpha-b", "Alpha", tree.NodeKindPage, "Alpha B body")).To(Succeed())
		Expect(index.IndexPage("docs/alpha-a", "docs/alpha-a.md", "alpha-a", "Alpha", tree.NodeKindPage, "Alpha A body")).To(Succeed())

		result, err := index.Search("", []tree.PageID{"zeta", "alpha-b", "alpha-a"}, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Count).To(Equal(3))
		Expect(result.StartAt).To(Equal(ResultOffset(0)))
		Expect(result.PageSize).To(Equal(ResultLimit(10)))
		Expect(result.Items).To(HaveLen(3))
		Expect(result.Items[0].PageID).To(Equal(tree.PageID("alpha-a")))
		Expect(result.Items[1].PageID).To(Equal(tree.PageID("alpha-b")))
		Expect(result.Items[2].PageID).To(Equal(tree.PageID("zeta")))
		Expect(result.Items[0].Rank).To(Equal(float64(1)))
		Expect(result.Items[0].Excerpt).To(ContainSubstring("Alpha A body"))

		pageIDs, err := index.SearchPageIDs("", []tree.PageID{"zeta", "alpha-b", "alpha-a"})
		Expect(err).NotTo(HaveOccurred())
		Expect(pageIDs).To(Equal([]tree.PageID{"alpha-a", "alpha-b", "zeta"}))
	})

	ginkgo.It("pagination returns the requested window and preserves request metadata", func() {
		t := ginkgo.GinkgoT()
		index, err := NewSQLiteIndex(t.TempDir())
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			Expect(index.Close()).To(Succeed())
		}()

		Expect(index.IndexPage("docs/alpha", "docs/alpha.md", "alpha", "Alpha", tree.NodeKindPage, "shared token")).To(Succeed())
		Expect(index.IndexPage("docs/beta", "docs/beta.md", "beta", "Beta", tree.NodeKindPage, "shared token")).To(Succeed())
		Expect(index.IndexPage("docs/gamma", "docs/gamma.md", "gamma", "Gamma", tree.NodeKindPage, "shared token")).To(Succeed())

		result, err := index.Search("", []tree.PageID{"alpha", "beta", "gamma"}, 1, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Count).To(Equal(3))
		Expect(result.StartAt).To(Equal(ResultOffset(1)))
		Expect(result.PageSize).To(Equal(ResultLimit(1)))
		Expect(result.Items).To(HaveLen(1))
		Expect(result.Items[0].PageID).To(Equal(tree.PageID("beta")))
	})

	ginkgo.It("Ping succeeds on an open index and Close is idempotent", func() {
		index, err := NewSQLiteIndex(ginkgo.GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())

		Expect(index.Ping()).To(Succeed())
		Expect(index.Close()).To(Succeed())
		Expect(index.Close()).To(Succeed())
	})

	ginkgo.It("recovers from a corrupt database during initialization", func() {
		storageDir := ginkgo.GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(storageDir, "search.db"), []byte("not sqlite"), 0o644)).To(Succeed())

		index, err := NewSQLiteIndex(storageDir)
		Expect(err).NotTo(HaveOccurred())
		defer closeSQLiteIndex(index)

		Expect(index.Ping()).To(Succeed())
	})

	ginkgo.It("logs close errors while recovering from a corrupt database", func() {
		previousEnsure := ensureSearchSchema
		previousRecoverable := isRecoverableSearchDBError
		previousRemove := removeSearchSQLiteFiles
		previousClose := closeSQLiteSearchDB
		recoverableErr := errors.New("recoverable schema failed")
		closeErr := errors.New("close failed")
		var ensureCalls int
		var removed bool
		ensureSearchSchema = func(index *SQLiteIndex) error {
			ensureCalls++
			if ensureCalls == 1 {
				db, err := sql.Open("sqlite", searchIndexDatabasePath(index.storageDir, tree.AssetNameFromString(index.databaseFile)))
				Expect(err).NotTo(HaveOccurred())
				index.db = db
				return recoverableErr
			}
			return nil
		}
		isRecoverableSearchDBError = func(err error) bool {
			return errors.Is(err, recoverableErr)
		}
		removeSearchSQLiteFiles = func(string) {
			removed = true
		}
		closeSQLiteSearchDB = func(db *sql.DB) error {
			_ = previousClose(db)
			return closeErr
		}
		ginkgo.DeferCleanup(func() {
			ensureSearchSchema = previousEnsure
			isRecoverableSearchDBError = previousRecoverable
			removeSearchSQLiteFiles = previousRemove
			closeSQLiteSearchDB = previousClose
		})

		index, err := NewSQLiteIndex(ginkgo.GinkgoT().TempDir())

		Expect(err).NotTo(HaveOccurred())
		Expect(index).NotTo(BeNil())
		Expect(ensureCalls).To(Equal(2))
		Expect(removed).To(BeTrue())
	})

	ginkgo.It("returns second schema errors after recoverable initialization retry", func() {
		previousEnsure := ensureSearchSchema
		previousRecoverable := isRecoverableSearchDBError
		previousRemove := removeSearchSQLiteFiles
		recoverableErr := errors.New("recoverable schema failed")
		finalErr := errors.New("schema still failed")
		var ensureCalls int
		ensureSearchSchema = func(*SQLiteIndex) error {
			ensureCalls++
			if ensureCalls == 1 {
				return recoverableErr
			}
			return finalErr
		}
		isRecoverableSearchDBError = func(err error) bool {
			return errors.Is(err, recoverableErr)
		}
		removeSearchSQLiteFiles = func(string) {}
		ginkgo.DeferCleanup(func() {
			ensureSearchSchema = previousEnsure
			isRecoverableSearchDBError = previousRecoverable
			removeSearchSQLiteFiles = previousRemove
		})

		index, err := NewSQLiteIndex(ginkgo.GinkgoT().TempDir())

		Expect(index).To(BeNil())
		Expect(errors.Is(err, finalErr)).To(BeTrue())
		Expect(ensureCalls).To(Equal(2))
	})

	ginkgo.It("returns database open errors", func() {
		previousOpen := openSQLiteSearchDB
		openErr := errors.New("open failed")
		openSQLiteSearchDB = func(string) (*sql.DB, error) {
			return nil, openErr
		}
		ginkgo.DeferCleanup(func() {
			openSQLiteSearchDB = previousOpen
		})
		index := &SQLiteIndex{storageDir: ginkgo.GinkgoT().TempDir(), databaseFile: "search.db"}

		err := index.Ping()

		Expect(errors.Is(err, openErr)).To(BeTrue())
	})

	ginkgo.It("returns initialization errors for locked databases", func() {
		storageDir := ginkgo.GinkgoT().TempDir()
		db, err := sql.Open("sqlite", filepath.Join(storageDir, "search.db"))
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			_, _ = db.Exec("ROLLBACK")
			Expect(db.Close()).To(Succeed())
		})
		_, err = db.Exec("CREATE TABLE blocker (id INTEGER)")
		Expect(err).NotTo(HaveOccurred())
		_, err = db.Exec("BEGIN EXCLUSIVE")
		Expect(err).NotTo(HaveOccurred())

		index, err := NewSQLiteIndex(storageDir)

		Expect(err).To(HaveOccurred())
		Expect(index).To(BeNil())
	})

	ginkgo.It("returns empty results for empty nil-filter searches", func() {
		index, err := NewSQLiteIndex(ginkgo.GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		defer closeSQLiteIndex(index)

		result, err := index.Search("", nil, 7, 11)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Count).To(Equal(0))
		Expect(result.Items).To(BeEmpty())
		Expect(result.StartAt).To(Equal(ResultOffset(7)))
		Expect(result.PageSize).To(Equal(ResultLimit(11)))

		pageIDs, err := index.SearchPageIDs("", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(pageIDs).To(BeEmpty())
	})

	ginkgo.It("returns malformed markdown errors before indexing", func() {
		index, err := NewSQLiteIndex(ginkgo.GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		defer closeSQLiteIndex(index)

		err = index.IndexPage(
			"docs/broken",
			"docs/broken.md",
			"broken",
			"Broken",
			tree.NodeKindPage,
			"---\nleafwiki_id: [invalid: yaml: structure\n---\nBody",
		)

		Expect(err).To(HaveOccurred())
	})

	ginkgo.It("returns database errors while indexing and removing pages", func() {
		index, err := NewSQLiteIndex(ginkgo.GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		defer closeSQLiteIndex(index)

		Expect(index.withDB(func(db *sql.DB) error {
			_, err := db.Exec(`DROP TABLE pages`)
			return err
		})).To(Succeed())
		Expect(index.IndexPage("docs/delete-error", "docs/delete-error.md", "delete-error", "Delete Error", tree.NodeKindPage, "body")).To(MatchError(ContainSubstring("no such table")))

		insertErrorIndex, err := NewSQLiteIndex(ginkgo.GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		defer closeSQLiteIndex(insertErrorIndex)
		Expect(insertErrorIndex.withDB(func(db *sql.DB) error {
			if _, err := db.Exec(`DROP TABLE pages`); err != nil {
				return err
			}
			_, err := db.Exec(`CREATE TABLE pages (pageID TEXT PRIMARY KEY)`)
			return err
		})).To(Succeed())
		Expect(insertErrorIndex.IndexPage("docs/insert-error", "docs/insert-error.md", "insert-error", "Insert Error", tree.NodeKindPage, "body")).To(MatchError(ContainSubstring("no column named path")))

		rows, err := index.RemovePageByFilePath("docs/delete-error.md")
		Expect(err).To(HaveOccurred())
		Expect(rows).To(Equal(int64(0)))
	})

	ginkgo.It("returns rows-affected errors while removing pages by filepath", func() {
		index, err := NewSQLiteIndex(ginkgo.GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		defer closeSQLiteIndex(index)
		Expect(index.IndexPage("docs/delete-error", "docs/delete-error.md", "delete-error", "Delete Error", tree.NodeKindPage, "body")).To(Succeed())
		previousRowsAffected := searchRowsAffected
		rowsAffectedErr := errors.New("rows affected failed")
		searchRowsAffected = func(sql.Result) (int64, error) {
			return 0, rowsAffectedErr
		}
		ginkgo.DeferCleanup(func() {
			searchRowsAffected = previousRowsAffected
		})

		rows, err := index.RemovePageByFilePath("docs/delete-error.md")

		Expect(rows).To(Equal(int64(0)))
		Expect(errors.Is(err, rowsAffectedErr)).To(BeTrue())
	})

	ginkgo.It("returns database query errors from malformed search tables", func() {
		index, err := NewSQLiteIndex(ginkgo.GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		defer closeSQLiteIndex(index)
		Expect(index.withDB(func(db *sql.DB) error {
			_, err := db.Exec(`DROP TABLE pages`)
			return err
		})).To(Succeed())

		_, err = index.Search("needle", nil, 0, 10)
		Expect(err).To(MatchError(ContainSubstring("no such table")))

		_, err = index.SearchPageIDs("needle", nil)
		Expect(err).To(MatchError(ContainSubstring("no such table")))

		queryErrorIndex, err := NewSQLiteIndex(ginkgo.GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		defer closeSQLiteIndex(queryErrorIndex)
		Expect(queryErrorIndex.withDB(func(db *sql.DB) error {
			if _, err := db.Exec(`DROP TABLE pages`); err != nil {
				return err
			}
			_, err := db.Exec(`CREATE TABLE pages (pageID TEXT PRIMARY KEY)`)
			return err
		})).To(Succeed())

		_, err = queryErrorIndex.Search("", []tree.PageID{"alpha"}, 0, 10)
		Expect(err).To(MatchError(ContainSubstring("no such column")))
	})

	ginkgo.It("returns row scan errors from malformed search rows", func() {
		index, err := NewSQLiteIndex(ginkgo.GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		defer closeSQLiteIndex(index)
		Expect(index.withDB(func(db *sql.DB) error {
			_, err := db.Exec(
				`INSERT INTO pages (path, filepath, pageID, kind, title, headings, content) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				"docs/null-title",
				"docs/null-title.md",
				"null-title",
				string(tree.NodeKindPage),
				nil,
				"",
				"body",
			)
			return err
		})).To(Succeed())

		_, err = index.Search("", []tree.PageID{"null-title"}, 0, 10)
		Expect(err).To(HaveOccurred())

		pageIDIndex, err := NewSQLiteIndex(ginkgo.GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		defer closeSQLiteIndex(pageIDIndex)
		Expect(pageIDIndex.withDB(func(db *sql.DB) error {
			if _, err := db.Exec(`DROP TABLE pages`); err != nil {
				return err
			}
			if _, err := db.Exec(`CREATE TABLE pages (pageID INTEGER, title TEXT, path TEXT)`); err != nil {
				return err
			}
			_, err := db.Exec(`INSERT INTO pages (pageID, title, path) VALUES (?, ?, ?)`, 42, "Answer", "docs/answer")
			return err
		})).To(Succeed())

		_, err = pageIDIndex.SearchPageIDs("", []tree.PageID{"42"})
		Expect(err).To(MatchError(ContainSubstring("cannot scan int64 into PageID")))
	})

	ginkgo.It("logs row close errors from search readers", func() {
		index, err := NewSQLiteIndex(ginkgo.GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		defer closeSQLiteIndex(index)
		Expect(index.IndexPage("docs/alpha", "docs/alpha.md", "alpha", "Alpha", tree.NodeKindPage, "shared token")).To(Succeed())
		previousCloseRows := closeSearchRows
		closeRowsErr := errors.New("close rows failed")
		closeSearchRows = func(rows *sql.Rows) error {
			_ = previousCloseRows(rows)
			return closeRowsErr
		}
		ginkgo.DeferCleanup(func() {
			closeSearchRows = previousCloseRows
		})

		result, err := index.Search("shared", nil, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Count).To(Equal(1))

		pageIDs, err := index.SearchPageIDs("shared", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(pageIDs).To(Equal([]tree.PageID{"alpha"}))
	})

	ginkgo.It("returns row iteration errors from search results", func() {
		index, err := NewSQLiteIndex(ginkgo.GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		defer closeSQLiteIndex(index)
		Expect(index.IndexPage("docs/alpha", "docs/alpha.md", "alpha", "Alpha", tree.NodeKindPage, "shared token")).To(Succeed())
		previousRowsErr := searchRowsErr
		rowsErr := errors.New("rows failed")
		searchRowsErr = func(*sql.Rows) error {
			return rowsErr
		}
		ginkgo.DeferCleanup(func() {
			searchRowsErr = previousRowsErr
		})

		_, err = index.Search("shared", nil, 0, 10)

		Expect(errors.Is(err, rowsErr)).To(BeTrue())
	})

	ginkgo.DescribeTable("buildFuzzyQuery handles FTS and token edge cases",
		func(input string, want string) {
			Expect(buildFuzzyQuery(input)).To(Equal(want))
		},
		ginkgo.Entry("trims and appends wildcards", "  hello world  ", "hello* world*"),
		ginkgo.Entry("keeps explicit FTS wildcard", "hello*", "hello*"),
		ginkgo.Entry("keeps phrase query", `"hello world"`, `"hello world"`),
		ginkgo.Entry("keeps boolean OR query", "hello OR world", "hello OR world"),
		ginkgo.Entry("quotes special path-like token", "docs/page-name", `"docs/page-name"`),
		ginkgo.Entry("quotes hash token", "C# notes", `"C# notes"`),
	)
})

func closeSQLiteIndex(index *SQLiteIndex) {
	Expect(index.Close()).To(Succeed())
}
