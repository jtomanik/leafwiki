package tags

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/tree"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

var _ = ginkgo.Describe("TagsStore error and recovery branches", func() {
	ginkgo.It("returns wrapped database open errors", func() {
		previousOpen := openTagsDB
		openErr := errors.New("open failed")
		openTagsDB = func(string) (*sql.DB, error) {
			return nil, openErr
		}
		ginkgo.DeferCleanup(func() {
			openTagsDB = previousOpen
		})

		store, err := NewTagsStore(tempTagsDir())

		Expect(store).To(BeNil())
		Expect(err).To(MatchError(openErr))
	})

	ginkgo.It("recovers from a corrupt tags database during initialization", func() {
		storageDir := tempTagsDir()
		Expect(os.WriteFile(filepath.Join(storageDir, "tags.db"), []byte("not sqlite"), 0o644)).To(Succeed())

		store, err := NewTagsStore(storageDir)

		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			Expect(store.Close()).To(Succeed())
		})
		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"go"})).To(Succeed())
		tagsByPage, err := store.GetTagsForPages(testPageIDs("page-1"))
		Expect(err).NotTo(HaveOccurred())
		Expect(tagsByPage).To(HaveKeyWithValue(newFixturePageID("page-1"), []string{"go"}))
	})

	ginkgo.It("returns wrapped reopen errors after recoverable initialization failures", func() {
		previousOpen := openTagsDB
		previousEnsure := ensureTagsSchema
		previousRecoverable := isRecoverableTagsDBError
		previousRemove := removeTagsSQLiteFiles
		recoverableErr := errors.New("recoverable schema failed")
		reopenErr := errors.New("reopen failed")
		var openCalls int
		var cleanupRequests []string
		openTagsDB = func(dbPath string) (*sql.DB, error) {
			openCalls++
			if openCalls == 1 {
				return sql.Open("sqlite", dbPath)
			}
			return nil, reopenErr
		}
		ensureTagsSchema = func(*TagsStore) error {
			return recoverableErr
		}
		isRecoverableTagsDBError = func(err error) bool {
			return errors.Is(err, recoverableErr)
		}
		removeTagsSQLiteFiles = func(dbPath string) {
			cleanupRequests = append(cleanupRequests, dbPath)
		}
		ginkgo.DeferCleanup(func() {
			openTagsDB = previousOpen
			ensureTagsSchema = previousEnsure
			isRecoverableTagsDBError = previousRecoverable
			removeTagsSQLiteFiles = previousRemove
		})

		storageDir := tempTagsDir()
		store, err := NewTagsStore(storageDir)

		Expect(store).To(BeNil())
		Expect(err).To(MatchError(reopenErr))
		Expect(openCalls).To(Equal(2))
		Expect(cleanupRequests).To(ConsistOf(filepath.Join(storageDir, "tags.db")))
	})

	ginkgo.It("returns second schema errors after recoverable initialization retry", func() {
		previousOpen := openTagsDB
		previousEnsure := ensureTagsSchema
		previousRecoverable := isRecoverableTagsDBError
		previousRemove := removeTagsSQLiteFiles
		recoverableErr := errors.New("recoverable schema failed")
		finalErr := errors.New("schema still failed")
		var openCalls int
		var ensureCalls int
		openTagsDB = func(dbPath string) (*sql.DB, error) {
			openCalls++
			return sql.Open("sqlite", dbPath)
		}
		ensureTagsSchema = func(*TagsStore) error {
			ensureCalls++
			if ensureCalls == 1 {
				return recoverableErr
			}
			return finalErr
		}
		isRecoverableTagsDBError = func(err error) bool {
			return errors.Is(err, recoverableErr)
		}
		removeTagsSQLiteFiles = func(string) {}
		ginkgo.DeferCleanup(func() {
			openTagsDB = previousOpen
			ensureTagsSchema = previousEnsure
			isRecoverableTagsDBError = previousRecoverable
			removeTagsSQLiteFiles = previousRemove
		})

		store, err := NewTagsStore(tempTagsDir())

		Expect(store).To(BeNil())
		Expect(err).To(MatchError(finalErr))
		Expect(openCalls).To(Equal(2))
		Expect(ensureCalls).To(Equal(2))
	})

	ginkgo.It("returns initialization errors for locked databases", func() {
		storageDir := tempTagsDir()
		db, err := sql.Open("sqlite", filepath.Join(storageDir, "tags.db"))
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			_, _ = db.Exec("ROLLBACK")
			Expect(db.Close()).To(Succeed())
		})
		_, err = db.Exec("CREATE TABLE blocker (id INTEGER)")
		Expect(err).NotTo(HaveOccurred())
		_, err = db.Exec("BEGIN EXCLUSIVE")
		Expect(err).NotTo(HaveOccurred())

		store, err := NewTagsStore(storageDir)

		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_BUSY))
		Expect(store).To(BeNil())
	})

	ginkgo.It("returns begin errors when the database is closed under the store", func() {
		for _, subject := range []struct {
			name string
			call func(*TagsStore) error
		}{
			{
				name: "SetTagsForPage",
				call: func(store *TagsStore) error {
					return store.SetTagsForPage(newFixturePageID("page-1"), []string{"go"})
				},
			},
			{
				name: "SetPageIndex",
				call: func(store *TagsStore) error {
					return store.SetPageIndex(newFixturePageID("page-1"), []string{"go"}, "excerpt")
				},
			},
			{
				name: "DeletePageIndex",
				call: func(store *TagsStore) error {
					return store.DeletePageIndex(newFixturePageID("page-1"))
				},
			},
			{
				name: "Clear",
				call: func(store *TagsStore) error {
					return store.Clear()
				},
			},
		} {
			store := newTestStore()
			Expect(store.db.Close()).To(Succeed(), subject.name)

			err := subject.call(store)

			Expect(err).To(MatchError(ErrTagsBeginTransaction), subject.name)
		}
	})

	ginkgo.It("returns SetTagsForPage write errors from malformed tag tables", func() {
		store := newTestStore()
		execTagsSQL(store, `DROP TABLE page_tags`)

		err := store.SetTagsForPage(newFixturePageID("page-1"), []string{"go"})
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_ERROR))

		prepareErrorStore := newTestStore()
		execTagsSQL(prepareErrorStore,
			`DROP TABLE page_tags`,
			`CREATE TABLE page_tags (page_id TEXT PRIMARY KEY)`,
		)
		err = prepareErrorStore.SetTagsForPage(newFixturePageID("page-1"), []string{"go"})
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_ERROR))

		insertErrorStore := newTestStore()
		execTagsSQL(insertErrorStore,
			`CREATE TRIGGER block_tag_insert
			 BEFORE INSERT ON page_tags
			 BEGIN
				SELECT RAISE(ABORT, 'tag insert blocked');
			 END`,
		)
		err = insertErrorStore.SetTagsForPage(newFixturePageID("page-1"), []string{"go"})
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_CONSTRAINT))
	})

	ginkgo.It("returns SetPageIndex write errors from malformed tag and excerpt tables", func() {
		store := newTestStore()
		execTagsSQL(store, `DROP TABLE page_tags`)

		err := store.SetPageIndex(newFixturePageID("page-1"), []string{"go"}, "excerpt")
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_ERROR))

		prepareErrorStore := newTestStore()
		execTagsSQL(prepareErrorStore,
			`DROP TABLE page_tags`,
			`CREATE TABLE page_tags (page_id TEXT PRIMARY KEY)`,
		)
		err = prepareErrorStore.SetPageIndex(newFixturePageID("page-1"), []string{"go"}, "excerpt")
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_ERROR))

		insertErrorStore := newTestStore()
		execTagsSQL(insertErrorStore,
			`CREATE TRIGGER block_page_index_tag_insert
			 BEFORE INSERT ON page_tags
			 BEGIN
				SELECT RAISE(ABORT, 'page index tag insert blocked');
			 END`,
		)
		err = insertErrorStore.SetPageIndex(newFixturePageID("page-1"), []string{"go"}, "excerpt")
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_CONSTRAINT))

		excerptErrorStore := newTestStore()
		execTagsSQL(excerptErrorStore, `DROP TABLE page_meta`)
		err = excerptErrorStore.SetPageIndex(newFixturePageID("page-1"), nil, "excerpt")
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_ERROR))
	})

	ginkgo.It("returns DeletePageIndex write errors from malformed tag and meta tables", func() {
		store := newTestStore()
		execTagsSQL(store, `DROP TABLE page_tags`)

		err := store.DeletePageIndex(newFixturePageID("page-1"))
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_ERROR))

		metaErrorStore := newTestStore()
		execTagsSQL(metaErrorStore, `DROP TABLE page_meta`)
		err = metaErrorStore.DeletePageIndex(newFixturePageID("page-1"))
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_ERROR))
	})

	ginkgo.It("returns Clear write errors from malformed tag and meta tables", func() {
		store := newTestStore()
		execTagsSQL(store, `DROP TABLE page_tags`)

		err := store.Clear()
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_ERROR))

		metaErrorStore := newTestStore()
		execTagsSQL(metaErrorStore, `DROP TABLE page_meta`)
		err = metaErrorStore.Clear()
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_ERROR))
	})

	ginkgo.It("returns read query errors from missing tag tables", func() {
		store := newTestStore()
		execTagsSQL(store, `DROP TABLE page_tags`)

		tagCounts, err := store.GetAllTags("", 50)
		Expect(tagCounts).To(BeNil())
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_ERROR))

		tagCounts, err = store.GetAllTagsForSelection("g", []string{"go"}, 50)
		Expect(tagCounts).To(BeNil())
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_ERROR))

		tagCounts, err = store.GetAllTagsForSelection("g", nil, 50)
		Expect(tagCounts).To(BeNil())
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_ERROR))

		pageIDs, err := store.GetPageIDsByTags([]string{"go"})
		Expect(pageIDs).To(BeNil())
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_ERROR))

		tagsByPage, err := store.GetTagsForPages(testPageIDs("page-1"))
		Expect(tagsByPage).To(BeNil())
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_ERROR))
	})

	ginkgo.It("returns read query errors from missing excerpt tables", func() {
		store := newTestStore()
		execTagsSQL(store, `DROP TABLE page_meta`)

		excerpts, err := store.GetExcerptsForPages(testPageIDs("page-1"))

		Expect(excerpts).To(BeNil())
		Expect(err).To(matchTagsSQLitePrimaryError(sqlite3.SQLITE_ERROR))
	})

	ginkgo.It("returns scan errors from malformed tag rows", func() {
		excerptsStore := newTestStore()
		execTagsSQL(excerptsStore,
			`DROP TABLE page_meta`,
			`CREATE TABLE page_meta (page_id INTEGER, excerpt TEXT)`,
			`INSERT INTO page_meta (page_id, excerpt) VALUES (42, 'excerpt')`,
		)
		excerpts, err := excerptsStore.GetExcerptsForPages([]tree.PageID{"42"})
		Expect(excerpts).To(BeNil())
		Expect(err).To(SatisfyAll(
			MatchError(ErrTagsScanRow),
			MatchError(tree.ErrScanPageID),
		))

		pageIDsStore := newTestStore()
		execTagsSQL(pageIDsStore,
			`DROP TABLE page_tags`,
			`CREATE TABLE page_tags (page_id INTEGER, tag TEXT)`,
			`INSERT INTO page_tags (page_id, tag) VALUES (42, 'go')`,
		)
		pageIDs, err := pageIDsStore.GetPageIDsByTags([]string{"go"})
		Expect(pageIDs).To(BeNil())
		Expect(err).To(SatisfyAll(
			MatchError(ErrTagsScanRow),
			MatchError(tree.ErrScanPageID),
		))

		tagsByPageStore := newTestStore()
		execTagsSQL(tagsByPageStore,
			`DROP TABLE page_tags`,
			`CREATE TABLE page_tags (page_id TEXT, tag TEXT)`,
			`INSERT INTO page_tags (page_id, tag) VALUES ('page-1', NULL)`,
		)
		tagsByPage, err := tagsByPageStore.GetTagsForPages(testPageIDs("page-1"))
		Expect(tagsByPage).To(BeNil())
		Expect(err).To(MatchError(ErrTagsScanRow))
	})

	ginkgo.It("returns tag-count scan errors from all tag-count readers", func() {
		store := newTestStore()
		Expect(store.SetTagsForPage(newFixturePageID("page-1"), []string{"go", "docs"})).To(Succeed())
		Expect(store.SetTagsForPage(newFixturePageID("page-2"), []string{"go"})).To(Succeed())
		previousScan := scanTagCount
		scanErr := errors.New("scan failed")
		scanTagCount = func(tagCountScanner, *TagCount) error {
			return scanErr
		}
		ginkgo.DeferCleanup(func() {
			scanTagCount = previousScan
		})

		for _, subject := range []struct {
			name string
			call func() ([]TagCount, error)
		}{
			{
				name: "GetAllTags",
				call: func() ([]TagCount, error) {
					return store.GetAllTags("", 0)
				},
			},
			{
				name: "GetAllTagsForSelection",
				call: func() ([]TagCount, error) {
					return store.GetAllTagsForSelection("", []string{"go"}, 0)
				},
			},
			{
				name: "getAllTagsLocked via empty selection",
				call: func() ([]TagCount, error) {
					return store.GetAllTagsForSelection("", nil, 0)
				},
			},
		} {
			tagCounts, err := subject.call()
			Expect(tagCounts).To(BeNil(), subject.name)
			Expect(err).To(MatchError(scanErr), subject.name)
		}
	})

	ginkgo.It("returns close errors without clearing the database handle", func() {
		store, err := NewTagsStore(tempTagsDir())
		Expect(err).NotTo(HaveOccurred())
		previousClose := closeTagsDB
		closeErr := errors.New("close failed")
		closeTagsDB = func(*sql.DB) error {
			return closeErr
		}
		ginkgo.DeferCleanup(func() {
			closeTagsDB = previousClose
			Expect(store.Close()).To(Succeed())
		})

		err = store.Close()

		Expect(err).To(MatchError(closeErr))
		Expect(store.db).NotTo(BeNil())
	})
})

func execTagsSQL(store *TagsStore, statements ...string) {
	ginkgo.GinkgoHelper()
	for _, statement := range statements {
		_, err := store.db.Exec(statement)
		Expect(err).NotTo(HaveOccurred())
	}
}

func matchTagsSQLitePrimaryError(code int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(err error) int {
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) {
			return -1
		}
		return sqliteErr.Code() & 0xFF
	}, Equal(code))
}
