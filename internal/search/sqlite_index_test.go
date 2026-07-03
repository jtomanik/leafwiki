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
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/tree"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var _ = ginkgo.Describe("SQLite search index", func() {
	ginkgo.It("stores searchable page metadata and plain text content", func() {
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

	ginkgo.It("builds database paths for Windows-style storage roots", func() {
		got := strings.ReplaceAll(searchIndexDatabasePath(`C:\wiki\data`, "search.db"), `\`, `/`)
		want := `C:/wiki/data/search.db`

		Expect(got).To(Equal(want))
	})

	ginkgo.It("creates the SQLite database inside the storage directory", func() {
		tmpDir := tempSearchDir()

		index, err := NewSQLiteIndex(tmpDir)
		Expect(err).NotTo(HaveOccurred())
		closeSQLiteIndex(index)

		_, err = os.Stat(filepath.Join(tmpDir, "search.db"))
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("returns ranked highlighted results across indexed page and section content", func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage("notes/alpha", "notes/alpha.md", "alpha1", "Alpha Search Test", tree.NodeKindPage, "This content is about SQLite search.")).To(Succeed())
		Expect(index.IndexPage("notes/beta", "notes/beta.md", "beta2", "Unrelated Page", tree.NodeKindSection, "This content is not about the search term.")).To(Succeed())

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

	ginkgo.It("ranks title matches ahead of content-only matches", func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage(
			"docs/titleMatch",
			"docs/titleMatch.md",
			"titleMatch",
			"Search term in title",
			tree.NodeKindPage,
			"Lorem ipsum dolor sit amet.",
		)).To(Succeed())

		Expect(index.IndexPage(
			"docs/contentMatch",
			"docs/contentMatch.md",
			"contentMatch",
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

	ginkgo.It("ranks heading matches ahead of content-only matches", func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage(
			"docs/headingMatch",
			"docs/headingMatch.md",
			"headingMatch",
			"No search in title",
			tree.NodeKindPage,
			"## Search term in heading\n\nSome additional body text.",
		)).To(Succeed())

		Expect(index.IndexPage(
			"docs/contentOnly",
			"docs/contentOnly.md",
			"contentOnly",
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

	ginkgo.It("returns only matching page IDs from the supplied filter", func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage("docs/alpha", "docs/alpha.md", "alpha", "Alpha Page", tree.NodeKindPage, "Shared token in alpha.")).To(Succeed())
		Expect(index.IndexPage("docs/beta", "docs/beta.md", "beta", "Beta Page", tree.NodeKindPage, "Shared token in beta.")).To(Succeed())
		Expect(index.IndexPage("docs/gamma", "docs/gamma.md", "gamma", "Gamma Page", tree.NodeKindPage, "Gamma only content.")).To(Succeed())

		pageIDs, err := index.SearchPageIDs("shared token", []tree.PageID{"alpha"})
		Expect(err).NotTo(HaveOccurred())
		Expect(pageIDs).To(Equal([]tree.PageID{newFixturePageID("alpha")}))

		noMatches, err := index.SearchPageIDs("shared token", []tree.PageID{})
		Expect(err).NotTo(HaveOccurred())
		Expect(noMatches).To(BeEmpty())
	})

	ginkgo.It("filters search results to requested page IDs", func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage(
			"docs/react-guide",
			"docs/react-guide.md",
			"react-guide",
			"React guide",
			tree.NodeKindPage,
			"Search term appears here.",
		)).To(Succeed())

		Expect(index.IndexPage(
			"docs/plain-guide",
			"docs/plain-guide.md",
			"plain-guide",
			"Plain guide",
			tree.NodeKindPage,
			"Search term appears here as well.",
		)).To(Succeed())

		result, err := index.Search("search", []tree.PageID{"react-guide"}, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(matchSearchResult(gstruct.Fields{
			"Count": Equal(1),
			"Items": HaveExactElements(matchSearchItem(gstruct.Fields{
				"PageID": Equal(newFixturePageID("react-guide")),
			})),
		}))
	})

	ginkgo.It("returns no search results for an empty page ID filter", func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage(
			"docs/react-guide",
			"docs/react-guide.md",
			"react-guide",
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

	ginkgo.It("indexes shoutout labels and body text without fence markers", func() {
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

	ginkgo.It("indexes markdown emphasis as plain text", func() {
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

	ginkgo.It("extracts a single H1 heading", func() {
		got := extractHeadings("# Hello World")

		Expect(got).To(ContainSubstring("Hello World"))
	})

	ginkgo.It("extracts heading text from multiple levels", func() {
		input := "# First\n## Second\n### Third"
		got := extractHeadings(input)

		Expect(got).To(SatisfyAll(
			ContainSubstring("First"),
			ContainSubstring("Second"),
			ContainSubstring("Third"),
		))
	})

	ginkgo.It("strips inline formatting from heading text", func() {
		got := extractHeadings("## **Bold** and _italic_ heading")

		Expect(got).NotTo(Or(ContainSubstring("**"), ContainSubstring("_")))
		Expect(got).To(SatisfyAll(
			ContainSubstring("Bold"),
			ContainSubstring("italic"),
			ContainSubstring("heading"),
		))
	})

	ginkgo.It("ignores non-heading body text", func() {
		got := extractHeadings("# Title\n\nSome body paragraph that should not appear.")

		Expect(got).NotTo(ContainSubstring("body paragraph"))
		Expect(got).To(ContainSubstring("Title"))
	})

	ginkgo.It("returns empty text when markdown has no headings", func() {
		got := extractHeadings("Just plain text without any heading.")

		Expect(got).To(BeEmpty())
	})

	ginkgo.It("returns empty text for empty markdown", func() {
		got := extractHeadings("")

		Expect(got).To(BeEmpty())
	})

	ginkgo.It("strips code span markers from heading text", func() {
		got := extractHeadings("## Heading `code` here")

		Expect(got).NotTo(ContainSubstring("`"))
		Expect(got).To(SatisfyAll(
			ContainSubstring("Heading"),
			ContainSubstring("here"),
		))
	})

	ginkgo.It("Clear removes indexed pages from subsequent searches", func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage("docs/alpha", "docs/alpha.md", "alpha", "Alpha", tree.NodeKindPage, "shared token")).To(Succeed())
		Expect(index.IndexPage("docs/beta", "docs/beta.md", "beta", "Beta", tree.NodeKindPage, "shared token")).To(Succeed())

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

	ginkgo.It("RemovePage and RemovePageByFilePath remove only matching records", func() {
		index := newSQLiteIndexForSpec()

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
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage("docs/zeta", "docs/zeta.md", "zeta", "Zeta", tree.NodeKindPage, "Zeta body")).To(Succeed())
		Expect(index.IndexPage("docs/alpha-b", "docs/alpha-b.md", "alpha-b", "Alpha", tree.NodeKindPage, "Alpha B body")).To(Succeed())
		Expect(index.IndexPage("docs/alpha-a", "docs/alpha-a.md", "alpha-a", "Alpha", tree.NodeKindPage, "Alpha A body")).To(Succeed())

		result, err := index.Search("", []tree.PageID{"zeta", "alpha-b", "alpha-a"}, 0, 10)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(matchSearchResult(gstruct.Fields{
			"Count":    Equal(3),
			"StartAt":  Equal(ResultOffset(0)),
			"PageSize": Equal(ResultLimit(10)),
			"Items": HaveExactElements(
				matchSearchItem(gstruct.Fields{
					"PageID":  Equal(tree.PageID("alpha-a")),
					"Rank":    Equal(float64(1)),
					"Excerpt": ContainSubstring("Alpha A body"),
				}),
				matchSearchItem(gstruct.Fields{
					"PageID": Equal(tree.PageID("alpha-b")),
				}),
				matchSearchItem(gstruct.Fields{
					"PageID": Equal(tree.PageID("zeta")),
				}),
			),
		}))

		pageIDs, err := index.SearchPageIDs("", []tree.PageID{"zeta", "alpha-b", "alpha-a"})
		Expect(err).NotTo(HaveOccurred())
		Expect(pageIDs).To(Equal([]tree.PageID{"alpha-a", "alpha-b", "zeta"}))
	})

	ginkgo.It("pagination returns the requested window and preserves request metadata", func() {
		index := newSQLiteIndexForSpec()

		Expect(index.IndexPage("docs/alpha", "docs/alpha.md", "alpha", "Alpha", tree.NodeKindPage, "shared token")).To(Succeed())
		Expect(index.IndexPage("docs/beta", "docs/beta.md", "beta", "Beta", tree.NodeKindPage, "shared token")).To(Succeed())
		Expect(index.IndexPage("docs/gamma", "docs/gamma.md", "gamma", "Gamma", tree.NodeKindPage, "shared token")).To(Succeed())

		result, err := index.Search("", []tree.PageID{"alpha", "beta", "gamma"}, 1, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(matchSearchResult(gstruct.Fields{
			"Count":    Equal(3),
			"StartAt":  Equal(ResultOffset(1)),
			"PageSize": Equal(ResultLimit(1)),
			"Items": HaveExactElements(matchSearchItem(gstruct.Fields{
				"PageID": Equal(tree.PageID("beta")),
			})),
		}))
	})

	ginkgo.It("Ping succeeds on an open index and Close is idempotent", func() {
		index, err := NewSQLiteIndex(tempSearchDir())
		Expect(err).NotTo(HaveOccurred())

		Expect(index.Ping()).To(Succeed())
		Expect(index.Close()).To(Succeed())
		Expect(index.Close()).To(Succeed())
	})

	ginkgo.It("recovers from a corrupt database during initialization", func() {
		storageDir := tempSearchDir()
		Expect(os.WriteFile(filepath.Join(storageDir, "search.db"), []byte("not sqlite"), 0o644)).To(Succeed())

		index, err := NewSQLiteIndex(storageDir)
		Expect(err).NotTo(HaveOccurred())
		closeSQLiteIndex(index)

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

		index, err := NewSQLiteIndex(tempSearchDir())

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

		index, err := NewSQLiteIndex(tempSearchDir())

		Expect(index).To(BeNil())
		Expect(err).To(MatchError(finalErr))
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
		index := &SQLiteIndex{storageDir: tempSearchDir(), databaseFile: "search.db"}

		err := index.Ping()

		Expect(err).To(MatchError(openErr))
	})

	ginkgo.It("returns initialization errors for locked databases", func() {
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

		Expect(err).To(HaveOccurred())
		Expect(index).To(BeNil())
	})

	ginkgo.It("returns empty results for empty nil-filter searches", func() {
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

	ginkgo.It("returns malformed markdown errors before indexing", func() {
		index := newSQLiteIndexForSpec()

		err := index.IndexPage(
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
		index := newSQLiteIndexForSpec()

		Expect(index.withDB(func(db *sql.DB) error {
			_, err := db.Exec(`DROP TABLE pages`)
			return err
		})).To(Succeed())
		Expect(index.IndexPage("docs/delete-error", "docs/delete-error.md", "delete-error", "Delete Error", tree.NodeKindPage, "body")).To(matchSQLitePrimaryError())

		insertErrorIndex := newSQLiteIndexForSpec()
		Expect(insertErrorIndex.withDB(func(db *sql.DB) error {
			if _, err := db.Exec(`DROP TABLE pages`); err != nil {
				return err
			}
			_, err := db.Exec(`CREATE TABLE pages (pageID TEXT PRIMARY KEY)`)
			return err
		})).To(Succeed())
		Expect(insertErrorIndex.IndexPage("docs/insert-error", "docs/insert-error.md", "insert-error", "Insert Error", tree.NodeKindPage, "body")).To(matchSQLitePrimaryError())

		rows, err := index.RemovePageByFilePath("docs/delete-error.md")
		Expect(err).To(HaveOccurred())
		Expect(rows).To(Equal(int64(0)))
	})

	ginkgo.It("returns rows-affected errors while removing pages by filepath", func() {
		index := newSQLiteIndexForSpec()
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
		Expect(err).To(MatchError(rowsAffectedErr))
	})

	ginkgo.It("returns database query errors from malformed search tables", func() {
		index := newSQLiteIndexForSpec()
		Expect(index.withDB(func(db *sql.DB) error {
			_, err := db.Exec(`DROP TABLE pages`)
			return err
		})).To(Succeed())

		_, err := index.Search("needle", nil, 0, 10)
		Expect(err).To(matchSQLitePrimaryError())

		_, err = index.SearchPageIDs("needle", nil)
		Expect(err).To(matchSQLitePrimaryError())

		queryErrorIndex := newSQLiteIndexForSpec()
		Expect(queryErrorIndex.withDB(func(db *sql.DB) error {
			if _, err := db.Exec(`DROP TABLE pages`); err != nil {
				return err
			}
			_, err := db.Exec(`CREATE TABLE pages (pageID TEXT PRIMARY KEY)`)
			return err
		})).To(Succeed())

		_, err = queryErrorIndex.Search("", []tree.PageID{"alpha"}, 0, 10)
		Expect(err).To(matchSQLitePrimaryError())
	})

	ginkgo.It("returns row scan errors from malformed search rows", func() {
		index := newSQLiteIndexForSpec()
		Expect(index.withDB(func(db *sql.DB) error {
			_, err := db.Exec(
				`INSERT INTO pages (path, filepath, pageID, kind, title, headings, content) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				"docs/null-title",
				"docs/null-title.md",
				"null-title",
				searchSQLiteNodeKindFromNodeKind(tree.NodeKindPage),
				nil,
				"",
				"body",
			)
			return err
		})).To(Succeed())

		_, err := index.Search("", []tree.PageID{"null-title"}, 0, 10)
		Expect(err).To(HaveOccurred())

		pageIDIndex := newSQLiteIndexForSpec()
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
		Expect(err).To(MatchError(tree.ErrScanPageID))
	})

	ginkgo.It("logs row close errors from search readers", func() {
		index := newSQLiteIndexForSpec()
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
		index := newSQLiteIndexForSpec()
		Expect(index.IndexPage("docs/alpha", "docs/alpha.md", "alpha", "Alpha", tree.NodeKindPage, "shared token")).To(Succeed())
		previousRowsErr := searchRowsErr
		rowsErr := errors.New("rows failed")
		searchRowsErr = func(*sql.Rows) error {
			return rowsErr
		}
		ginkgo.DeferCleanup(func() {
			searchRowsErr = previousRowsErr
		})

		_, err := index.Search("shared", nil, 0, 10)

		Expect(err).To(MatchError(rowsErr))
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

type indexedPageRecord struct {
	path    string
	title   string
	content string
}

func tempSearchDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-search-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)

	return dir
}

func newSQLiteIndexForSpec() *SQLiteIndex {
	ginkgo.GinkgoHelper()

	index, err := NewSQLiteIndex(tempSearchDir())
	Expect(err).NotTo(HaveOccurred())
	closeSQLiteIndex(index)

	return index
}

func closeSQLiteIndex(index *SQLiteIndex) {
	ginkgo.GinkgoHelper()
	ginkgo.DeferCleanup(func() {
		Expect(index.Close()).To(Succeed())
	})
}

func storedPageRecord(index *SQLiteIndex, pageID tree.PageID) indexedPageRecord {
	ginkgo.GinkgoHelper()

	var record indexedPageRecord
	Expect(index.withDB(func(db *sql.DB) error {
		return db.QueryRow(`SELECT path, title, content FROM pages WHERE pageID = ?`, pageID).
			Scan(&record.path, &record.title, &record.content)
	})).To(Succeed())

	return record
}

func storedPageContent(index *SQLiteIndex, pageID tree.PageID) string {
	ginkgo.GinkgoHelper()

	return storedPageRecord(index, pageID).content
}

func matchIndexedPageRecord(path string, title string, content types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return SatisfyAll(
		WithTransform(func(record indexedPageRecord) string { return record.path }, Equal(path)),
		WithTransform(func(record indexedPageRecord) string { return record.title }, Equal(title)),
		WithTransform(func(record indexedPageRecord) string { return record.content }, content),
	)
}

func matchSearchResult(fields gstruct.Fields) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

func matchSearchItem(fields gstruct.Fields) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchSQLitePrimaryError() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(err error) int {
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) {
			return -1
		}
		return sqliteErr.Code() & 0xFF
	}, Equal(sqlite3.SQLITE_ERROR))
}
