package search

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
	sqlite3 "modernc.org/sqlite/lib"
)

var _ = ginkgo.Describe("SQLite search index", func() {
	ginkgo.It("stores searchable page metadata and plain text content", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()

		path := "docs/test.md"
		pageID := newFixturePageID("test123")
		title := "Test Page"
		content := "This is a **test** page."
		expectedContent := "This is a test page."

		Expect(index.IndexPage(path, path, pageID, title, tree.NodeKindPage, content)).To(Succeed())

		record := storedPageRecord(index, pageID)
		Expect(record).To(matchIndexedPageRecord(path, title, HavePrefix(expectedContent)))
	})

	ginkgo.It("builds database paths for Windows-style storage roots", ginkgo.Label("unit"), func() {
		got := strings.ReplaceAll(searchIndexDatabasePath(`C:\wiki\data`, newFixtureAssetName("search.db")), `\`, `/`)
		want := `C:/wiki/data/search.db`

		Expect(got).To(Equal(want))
	})

	ginkgo.It("creates the SQLite database inside the storage directory", ginkgo.Label("integration"), func() {
		tmpDir := tempSearchDir()

		index, err := NewSQLiteIndex(tmpDir)
		Expect(err).NotTo(HaveOccurred())
		closeSQLiteIndex(index)

		_, err = os.Stat(filepath.Join(tmpDir, "search.db"))
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("returns ranked highlighted results across indexed page and section content", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage("notes/alpha", "notes/alpha.md", newFixturePageID("alpha1"), "Alpha Search Test", tree.NodeKindPage, "This content is about SQLite search.")).To(Succeed())
		Expect(index.IndexPage("notes/beta", "notes/beta.md", newFixturePageID("beta2"), "Unrelated Page", tree.NodeKindSection, "This content is not about the search term.")).To(Succeed())

		result, err := index.Search("content:search*", nil, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(matchSearchResult(gstruct.Fields{
			"Count": Equal(2),
			"Items": HaveExactElements(
				matchSearchItem(gstruct.Fields{
					"PageID":  Equal(newFixturePageID("alpha1")),
					"Kind":    Equal(tree.NodeKindPage),
					"Excerpt": ContainSubstring("<b>"),
				}),
				matchSearchItem(gstruct.Fields{
					"Kind": Equal(tree.NodeKindSection),
				}),
			),
		}))
	})

	ginkgo.It("ranks title matches ahead of content-only matches", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage(
			"docs/titleMatch",
			"docs/titleMatch.md",
			newFixturePageID("titleMatch"),
			"Search term in title",
			tree.NodeKindPage,
			"Lorem ipsum dolor sit amet.",
		)).To(Succeed())

		Expect(index.IndexPage(
			"docs/contentMatch",
			"docs/contentMatch.md",
			newFixturePageID("contentMatch"),
			"Content only match",
			tree.NodeKindPage,
			"This page has the search term only in the content.",
		)).To(Succeed())

		result, err := index.Search("search", nil, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(matchSearchResult(gstruct.Fields{
			"Count": Equal(2),
			"Items": HaveExactElements(
				matchSearchItem(gstruct.Fields{
					"PageID": Equal(newFixturePageID("titleMatch")),
					"Rank":   BeNumerically(">", 0),
				}),
				matchSearchItem(gstruct.Fields{
					"PageID": Equal(newFixturePageID("contentMatch")),
					"Rank":   BeNumerically(">", 0),
				}),
			),
		}))
	})

	ginkgo.It("ranks heading matches ahead of content-only matches", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage(
			"docs/headingMatch",
			"docs/headingMatch.md",
			newFixturePageID("headingMatch"),
			"No search in title",
			tree.NodeKindPage,
			"## Search term in heading\n\nSome additional body text.",
		)).To(Succeed())

		Expect(index.IndexPage(
			"docs/contentOnly",
			"docs/contentOnly.md",
			newFixturePageID("contentOnly"),
			"No search in title",
			tree.NodeKindPage,
			"This page has the search term only in the content.",
		)).To(Succeed())

		result, err := index.Search("search", nil, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(matchSearchResult(gstruct.Fields{
			"Count": Equal(2),
			"Items": HaveExactElements(
				matchSearchItem(gstruct.Fields{
					"PageID": Equal(newFixturePageID("headingMatch")),
					"Rank":   BeNumerically(">", 0),
				}),
				matchSearchItem(gstruct.Fields{
					"PageID": Equal(newFixturePageID("contentOnly")),
					"Rank":   BeNumerically(">", 0),
				}),
			),
		}))
	})

	ginkgo.It("returns only matching page IDs from the supplied filter", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage("docs/alpha", "docs/alpha.md", newFixturePageID("alpha"), "Alpha Page", tree.NodeKindPage, "Shared token in alpha.")).To(Succeed())
		Expect(index.IndexPage("docs/beta", "docs/beta.md", newFixturePageID("beta"), "Beta Page", tree.NodeKindPage, "Shared token in beta.")).To(Succeed())
		Expect(index.IndexPage("docs/gamma", "docs/gamma.md", newFixturePageID("gamma"), "Gamma Page", tree.NodeKindPage, "Gamma only content.")).To(Succeed())

		pageIDs, err := index.SearchPageIDs("shared token", []tree.PageID{newFixturePageID("alpha")})
		Expect(err).NotTo(HaveOccurred())
		Expect(pageIDs).To(Equal([]tree.PageID{newFixturePageID("alpha")}))

		noMatches, err := index.SearchPageIDs("shared token", []tree.PageID{})
		Expect(err).NotTo(HaveOccurred())
		Expect(noMatches).To(BeEmpty())
	})

	ginkgo.It("filters search results to requested page IDs", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage(
			"docs/react-guide",
			"docs/react-guide.md",
			newFixturePageID("react-guide"),
			"React guide",
			tree.NodeKindPage,
			"Search term appears here.",
		)).To(Succeed())

		Expect(index.IndexPage(
			"docs/plain-guide",
			"docs/plain-guide.md",
			newFixturePageID("plain-guide"),
			"Plain guide",
			tree.NodeKindPage,
			"Search term appears here as well.",
		)).To(Succeed())

		result, err := index.Search("search", []tree.PageID{newFixturePageID("react-guide")}, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(matchSearchResult(gstruct.Fields{
			"Count": Equal(1),
			"Items": HaveExactElements(matchSearchItem(gstruct.Fields{
				"PageID": Equal(newFixturePageID("react-guide")),
			})),
		}))
	})

	ginkgo.It("returns no search results for an empty page ID filter", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage(
			"docs/react-guide",
			"docs/react-guide.md",
			newFixturePageID("react-guide"),
			"React guide",
			tree.NodeKindPage,
			"Search term appears here.",
		)).To(Succeed())

		result, err := index.Search("search", []tree.PageID{}, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(matchSearchResult(gstruct.Fields{
			"Count": BeZero(),
			"Items": BeEmpty(),
		}))
	})

	ginkgo.It("indexes shoutout labels and body text without fence markers", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()
		pageID := newFixturePageID("shoutout1")

		Expect(index.IndexPage(
			"docs/shoutout",
			"docs/shoutout.md",
			pageID,
			"Shoutout Page",
			tree.NodeKindPage,
			strings.Join([]string{
				"::: blue",
				"Shoutout body text.",
				":::",
			}, "\n"),
		)).To(Succeed())

		gotContent := storedPageContent(index, pageID)
		Expect(gotContent).NotTo(ContainSubstring(":::"))
		Expect(gotContent).To(SatisfyAll(
			ContainSubstring("blue"),
			ContainSubstring("Shoutout body text."),
		))
	})

	ginkgo.It("indexes markdown emphasis as plain text", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()
		pageID := newFixturePageID("markdown1")

		Expect(index.IndexPage(
			"docs/markdown",
			"docs/markdown.md",
			pageID,
			"Markdown Page",
			tree.NodeKindPage,
			"LeafWiki **fett** und _kursiv_.",
		)).To(Succeed())

		Expect(storedPageContent(index, pageID)).NotTo(Or(
			ContainSubstring("**"),
			ContainSubstring("_"),
		))
	})

	ginkgo.It("extracts a single H1 heading", ginkgo.Label("unit"), func() {
		got := extractHeadings("# Hello World")

		Expect(got).To(ContainSubstring("Hello World"))
	})

	ginkgo.It("extracts heading text from multiple levels", ginkgo.Label("unit"), func() {
		input := "# First\n## Second\n### Third"
		got := extractHeadings(input)

		Expect(got).To(SatisfyAll(
			ContainSubstring("First"),
			ContainSubstring("Second"),
			ContainSubstring("Third"),
		))
	})

	ginkgo.It("strips inline formatting from heading text", ginkgo.Label("unit"), func() {
		got := extractHeadings("## **Bold** and _italic_ heading")

		Expect(got).NotTo(Or(ContainSubstring("**"), ContainSubstring("_")))
		Expect(got).To(SatisfyAll(
			ContainSubstring("Bold"),
			ContainSubstring("italic"),
			ContainSubstring("heading"),
		))
	})

	ginkgo.It("ignores non-heading body text", ginkgo.Label("unit"), func() {
		got := extractHeadings("# Title\n\nSome body paragraph that should not appear.")

		Expect(got).NotTo(ContainSubstring("body paragraph"))
		Expect(got).To(ContainSubstring("Title"))
	})

	ginkgo.It("returns empty text when markdown has no headings", ginkgo.Label("unit"), func() {
		got := extractHeadings("Just plain text without any heading.")

		Expect(got).To(BeEmpty())
	})

	ginkgo.It("returns empty text for empty markdown", ginkgo.Label("unit"), func() {
		got := extractHeadings("")

		Expect(got).To(BeEmpty())
	})

	ginkgo.It("strips code span markers from heading text", ginkgo.Label("unit"), func() {
		got := extractHeadings("## Heading `code` here")

		Expect(got).NotTo(ContainSubstring("`"))
		Expect(got).To(SatisfyAll(
			ContainSubstring("Heading"),
			ContainSubstring("here"),
		))
	})

	ginkgo.It("Clear removes indexed pages from subsequent searches", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage("docs/alpha", "docs/alpha.md", newFixturePageID("alpha"), "Alpha", tree.NodeKindPage, "shared token")).To(Succeed())
		Expect(index.IndexPage("docs/beta", "docs/beta.md", newFixturePageID("beta"), "Beta", tree.NodeKindPage, "shared token")).To(Succeed())

		before, err := index.Search("shared", nil, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(before.Count).To(Equal(2))

		Expect(index.Clear()).To(Succeed())
		after, err := index.Search("shared", nil, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(after).To(matchSearchResult(gstruct.Fields{
			"Count": BeZero(),
			"Items": BeEmpty(),
		}))
	})

	ginkgo.It("RemovePage and RemovePageByFilePath remove only matching records", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage("docs/alpha", "docs/alpha.md", newFixturePageID("alpha"), "Alpha", tree.NodeKindPage, "shared token")).To(Succeed())
		Expect(index.IndexPage("docs/beta", "docs/beta.md", newFixturePageID("beta"), "Beta", tree.NodeKindPage, "shared token")).To(Succeed())
		Expect(index.IndexPage("docs/gamma", "docs/gamma.md", newFixturePageID("gamma"), "Gamma", tree.NodeKindPage, "shared token")).To(Succeed())

		Expect(index.RemovePage(newFixturePageID("alpha"))).To(Succeed())
		rows, err := index.RemovePageByFilePath("docs/beta.md")
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(Equal(int64(1)))
		rows, err = index.RemovePageByFilePath("docs/missing.md")
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(Equal(int64(0)))

		pageIDs, err := index.SearchPageIDs("shared", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(pageIDs).To(Equal([]tree.PageID{newFixturePageID("gamma")}))
	})

	ginkgo.It("empty query with page filters returns matching pages ordered by title and path", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage("docs/zeta", "docs/zeta.md", newFixturePageID("zeta"), "Zeta", tree.NodeKindPage, "Zeta body")).To(Succeed())
		Expect(index.IndexPage("docs/alpha-b", "docs/alpha-b.md", newFixturePageID("alpha-b"), "Alpha", tree.NodeKindPage, "Alpha B body")).To(Succeed())
		Expect(index.IndexPage("docs/alpha-a", "docs/alpha-a.md", newFixturePageID("alpha-a"), "Alpha", tree.NodeKindPage, "Alpha A body")).To(Succeed())

		result, err := index.Search("", []tree.PageID{newFixturePageID("zeta"), newFixturePageID("alpha-b"), newFixturePageID("alpha-a")}, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(matchSearchResult(gstruct.Fields{
			"Count":    Equal(3),
			"StartAt":  Equal(ResultOffset(0)),
			"PageSize": Equal(ResultLimit(10)),
			"Items": HaveExactElements(
				matchSearchItem(gstruct.Fields{
					"PageID":  Equal(newFixturePageID("alpha-a")),
					"Rank":    Equal(float64(1)),
					"Excerpt": ContainSubstring("Alpha A body"),
				}),
				matchSearchItem(gstruct.Fields{
					"PageID": Equal(newFixturePageID("alpha-b")),
				}),
				matchSearchItem(gstruct.Fields{
					"PageID": Equal(newFixturePageID("zeta")),
				}),
			),
		}))

		pageIDs, err := index.SearchPageIDs("", []tree.PageID{newFixturePageID("zeta"), newFixturePageID("alpha-b"), newFixturePageID("alpha-a")})
		Expect(err).NotTo(HaveOccurred())
		Expect(pageIDs).To(Equal([]tree.PageID{newFixturePageID("alpha-a"), newFixturePageID("alpha-b"), newFixturePageID("zeta")}))
	})

	ginkgo.It("pagination returns the requested window and preserves request metadata", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage("docs/alpha", "docs/alpha.md", newFixturePageID("alpha"), "Alpha", tree.NodeKindPage, "shared token")).To(Succeed())
		Expect(index.IndexPage("docs/beta", "docs/beta.md", newFixturePageID("beta"), "Beta", tree.NodeKindPage, "shared token")).To(Succeed())
		Expect(index.IndexPage("docs/gamma", "docs/gamma.md", newFixturePageID("gamma"), "Gamma", tree.NodeKindPage, "shared token")).To(Succeed())

		result, err := index.Search("", []tree.PageID{newFixturePageID("alpha"), newFixturePageID("beta"), newFixturePageID("gamma")}, 1, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(matchSearchResult(gstruct.Fields{
			"Count":    Equal(3),
			"StartAt":  Equal(ResultOffset(1)),
			"PageSize": Equal(ResultLimit(1)),
			"Items": HaveExactElements(matchSearchItem(gstruct.Fields{
				"PageID": Equal(newFixturePageID("beta")),
			})),
		}))
	})

	ginkgo.It("Ping succeeds on an open index and Close is idempotent", ginkgo.Label("integration"), func() {
		index, err := NewSQLiteIndex(tempSearchDir())
		Expect(err).NotTo(HaveOccurred())

		Expect(index.Ping()).To(Succeed())
		Expect(index.Close()).To(Succeed())
		Expect(index.Close()).To(Succeed())
	})

	ginkgo.It("recovers from a corrupt database during initialization", ginkgo.Label("integration"), func() {
		storageDir := tempSearchDir()
		Expect(os.WriteFile(filepath.Join(storageDir, "search.db"), []byte("not sqlite"), 0o644)).To(Succeed())

		index, err := NewSQLiteIndex(storageDir)
		Expect(err).NotTo(HaveOccurred())
		closeSQLiteIndex(index)

		Expect(index.Ping()).To(Succeed())
	})

	ginkgo.It("logs close errors while recovering from a corrupt database", ginkgo.Label("integration"), func() {
		previousEnsure := ensureSearchSchema
		previousRecoverable := isRecoverableSearchDBError
		previousRemove := removeSearchSQLiteFiles
		previousClose := closeSQLiteSearchDB
		recoverableErr := errors.New("recoverable schema failed")
		closeErr := errors.New("close failed")
		var ensureCalls int
		var cleanupRequests []string
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
		removeSearchSQLiteFiles = func(dbPath string) {
			cleanupRequests = append(cleanupRequests, dbPath)
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

		storageDir := tempSearchDir()
		index, err := NewSQLiteIndex(storageDir)

		Expect(err).NotTo(HaveOccurred())
		Expect(index).NotTo(BeNil())
		Expect(ensureCalls).To(Equal(2))
		Expect(cleanupRequests).To(ConsistOf(searchIndexDatabasePath(storageDir, newFixtureAssetName("search.db"))))
	})

	ginkgo.It("returns second schema errors after recoverable initialization retry", ginkgo.Label("integration"), func() {
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

		index, err := NewSQLiteIndex(tempSearchDir())

		Expect(index).To(BeNil())
		Expect(err).To(MatchError(finalErr))
		Expect(ensureCalls).To(Equal(2))
	})

	ginkgo.It("returns database open errors", ginkgo.Label("integration"), func() {
		previousOpen := openSQLiteSearchDB
		openErr := errors.New("open failed")
		openSQLiteSearchDB = func(string) (*sql.DB, error) {
			return nil, openErr
		}
		ginkgo.DeferCleanup(func() {
			openSQLiteSearchDB = previousOpen
		})
		index := &SQLiteIndex{storageDir: tempSearchDir(), databaseFile: "search.db"}

		err := index.Ping()

		Expect(err).To(MatchError(openErr))
	})

	ginkgo.It("returns initialization errors for locked databases", ginkgo.Label("integration"), func() {
		storageDir := tempSearchDir()
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

		Expect(err).To(matchSQLitePrimaryCode(sqlite3.SQLITE_BUSY))
		Expect(index).To(BeNil())
	})

	ginkgo.It("returns empty results for empty nil-filter searches", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()

		result, err := index.Search("", nil, 7, 11)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(matchSearchResult(gstruct.Fields{
			"Count":    BeZero(),
			"Items":    BeEmpty(),
			"StartAt":  Equal(ResultOffset(7)),
			"PageSize": Equal(ResultLimit(11)),
		}))

		pageIDs, err := index.SearchPageIDs("", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(pageIDs).To(BeEmpty())
	})

	ginkgo.It("returns malformed markdown errors before indexing", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()

		err := index.IndexPage(
			"docs/broken",
			"docs/broken.md",
			newFixturePageID("broken"),
			"Broken",
			tree.NodeKindPage,
			"<!-- leafwiki",
		)

		Expect(err).To(MatchError(markdown.ErrMetadataParse))
	})

	ginkgo.DescribeTable("normalizes user search queries for full-text search", ginkgo.Label("unit"),
		func(input string, want string) {
			Expect(buildFuzzyQuery(input)).To(Equal(want))
		},
		ginkgo.Entry("trims and appends wildcards", "  hello world  ", "hello* world*"),
		ginkgo.Entry("keeps explicit full-text wildcard", "hello*", "hello*"),
		ginkgo.Entry("keeps phrase query", `"hello world"`, `"hello world"`),
		ginkgo.Entry("keeps boolean OR query", "hello OR world", "hello OR world"),
		ginkgo.Entry("quotes special path-like token", "docs/page-name", `"docs/page-name"`),
		ginkgo.Entry("quotes hash token", "C# notes", `"C# notes"`),
	)
})
