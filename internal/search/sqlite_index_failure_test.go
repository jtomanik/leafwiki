package search

import (
	"database/sql"
	"errors"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("SQLite search index database failure behavior", func() {
	ginkgo.It("returns database errors while indexing and removing pages", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()

		Expect(index.withDB(func(db *sql.DB) error {
			_, err := db.Exec(`DROP TABLE pages`)
			return err
		})).To(Succeed())
		Expect(index.IndexPage("docs/delete-error", "docs/delete-error.md", newFixturePageID("delete-error"), "Delete Error", tree.NodeKindPage, "body")).To(matchSQLitePrimaryError())

		insertErrorIndex := newSQLiteIndexForSpec()
		Expect(insertErrorIndex.withDB(func(db *sql.DB) error {
			if _, err := db.Exec(`DROP TABLE pages`); err != nil {
				return err
			}
			_, err := db.Exec(`CREATE TABLE pages (pageID TEXT PRIMARY KEY)`)
			return err
		})).To(Succeed())
		Expect(insertErrorIndex.IndexPage("docs/insert-error", "docs/insert-error.md", newFixturePageID("insert-error"), "Insert Error", tree.NodeKindPage, "body")).To(matchSQLitePrimaryError())

		rows, err := index.RemovePageByFilePath("docs/delete-error.md")
		Expect(err).To(matchSQLitePrimaryError())
		Expect(rows).To(Equal(int64(0)))
	})

	ginkgo.It("returns rows-affected errors while removing pages by filepath", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()
		Expect(index.IndexPage("docs/delete-error", "docs/delete-error.md", newFixturePageID("delete-error"), "Delete Error", tree.NodeKindPage, "body")).To(Succeed())
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

	ginkgo.It("returns database query errors from malformed search tables", ginkgo.Label("integration"), func() {
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

		_, err = queryErrorIndex.Search("", []tree.PageID{newFixturePageID("alpha")}, 0, 10)
		Expect(err).To(matchSQLitePrimaryError())
	})

	ginkgo.It("returns row scan errors from malformed search rows", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()
		Expect(index.withDB(func(db *sql.DB) error {
			if _, err := db.Exec(`DROP TABLE pages`); err != nil {
				return err
			}
			if _, err := db.Exec(`CREATE TABLE pages (pageID INTEGER, path TEXT, kind TEXT, title TEXT, content TEXT)`); err != nil {
				return err
			}
			_, err := db.Exec(`INSERT INTO pages (pageID, path, kind, title, content) VALUES (?, ?, ?, ?, ?)`,
				42, "docs/answer", searchSQLiteNodeKindFromNodeKind(tree.NodeKindPage), "Answer", "body")
			return err
		})).To(Succeed())

		_, err := index.Search("", []tree.PageID{newFixturePageID("42")}, 0, 10)
		Expect(err).To(MatchError(tree.ErrScanPageID))

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

		_, err = pageIDIndex.SearchPageIDs("", []tree.PageID{newFixturePageID("42")})
		Expect(err).To(MatchError(tree.ErrScanPageID))
	})

	ginkgo.It("logs row close errors from search readers", ginkgo.Label("integration"), func() {
		index := newSQLiteIndexForSpec()
		Expect(index.IndexPage("docs/alpha", "docs/alpha.md", newFixturePageID("alpha"), "Alpha", tree.NodeKindPage, "shared token")).To(Succeed())
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
		Expect(pageIDs).To(Equal([]tree.PageID{newFixturePageID("alpha")}))
	})
})
