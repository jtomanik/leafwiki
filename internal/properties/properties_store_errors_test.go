package properties

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

var _ = ginkgo.Describe("property persistence error handling and recovery", ginkgo.Label("integration"), func() {
	ginkgo.It("returns wrapped database open errors", func() {
		previousOpen := openPropertiesDB
		openErr := errors.New("open failed")
		openPropertiesDB = func(string) (*sql.DB, error) {
			return nil, openErr
		}
		ginkgo.DeferCleanup(func() {
			openPropertiesDB = previousOpen
		})

		store, err := NewPropertiesStore(propertiesTempDir())

		Expect(store).To(BeNil())
		Expect(err).To(MatchError(openErr))
	})

	ginkgo.It("recovers from a corrupt properties database during initialization", func() {
		storageDir := propertiesTempDir()
		Expect(os.WriteFile(filepath.Join(storageDir, "properties.db"), []byte("not sqlite"), 0o644)).To(Succeed())

		store, err := NewPropertiesStore(storageDir)

		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			Expect(store.Close()).To(Succeed())
		})
		Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))).To(Succeed())
	})

	ginkgo.It("returns wrapped reopen errors after recoverable initialization failures", func() {
		previousOpen := openPropertiesDB
		previousEnsure := ensurePropertiesSchema
		previousRecoverable := isRecoverablePropertiesDBError
		previousRemove := removePropertiesSQLiteFiles
		recoverableErr := errors.New("recoverable schema failed")
		reopenErr := errors.New("reopen failed")
		var openCalls int
		var removedPaths []string
		storageDir := propertiesTempDir()
		openPropertiesDB = func(dbPath string) (*sql.DB, error) {
			openCalls++
			if openCalls == 1 {
				return sql.Open("sqlite", dbPath)
			}
			return nil, reopenErr
		}
		ensurePropertiesSchema = func(*PropertiesStore) error {
			return recoverableErr
		}
		isRecoverablePropertiesDBError = func(err error) bool {
			return errors.Is(err, recoverableErr)
		}
		removePropertiesSQLiteFiles = func(path string) {
			removedPaths = append(removedPaths, path)
		}
		ginkgo.DeferCleanup(func() {
			openPropertiesDB = previousOpen
			ensurePropertiesSchema = previousEnsure
			isRecoverablePropertiesDBError = previousRecoverable
			removePropertiesSQLiteFiles = previousRemove
		})

		store, err := NewPropertiesStore(storageDir)

		Expect(store).To(BeNil())
		Expect(err).To(MatchError(reopenErr))
		Expect(openCalls).To(Equal(2))
		Expect(removedPaths).To(HaveExactElements(filepath.Join(storageDir, "properties.db")))
	})

	ginkgo.It("returns second schema errors after recoverable initialization retry", func() {
		previousOpen := openPropertiesDB
		previousEnsure := ensurePropertiesSchema
		previousRecoverable := isRecoverablePropertiesDBError
		previousRemove := removePropertiesSQLiteFiles
		recoverableErr := errors.New("recoverable schema failed")
		finalErr := errors.New("schema still failed")
		var openCalls int
		var ensureCalls int
		openPropertiesDB = func(dbPath string) (*sql.DB, error) {
			openCalls++
			return sql.Open("sqlite", dbPath)
		}
		ensurePropertiesSchema = func(*PropertiesStore) error {
			ensureCalls++
			if ensureCalls == 1 {
				return recoverableErr
			}
			return finalErr
		}
		isRecoverablePropertiesDBError = func(err error) bool {
			return errors.Is(err, recoverableErr)
		}
		removePropertiesSQLiteFiles = func(string) {}
		ginkgo.DeferCleanup(func() {
			openPropertiesDB = previousOpen
			ensurePropertiesSchema = previousEnsure
			isRecoverablePropertiesDBError = previousRecoverable
			removePropertiesSQLiteFiles = previousRemove
		})

		store, err := NewPropertiesStore(propertiesTempDir())

		Expect(store).To(BeNil())
		Expect(err).To(MatchError(finalErr))
		Expect(openCalls).To(Equal(2))
		Expect(ensureCalls).To(Equal(2))
	})

	ginkgo.It("returns initialization errors for locked databases", func() {
		storageDir := propertiesTempDir()
		db, err := sql.Open("sqlite", filepath.Join(storageDir, "properties.db"))
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			_, _ = db.Exec("ROLLBACK")
			Expect(db.Close()).To(Succeed())
		})
		_, err = db.Exec("CREATE TABLE blocker (id INTEGER)")
		Expect(err).NotTo(HaveOccurred())
		_, err = db.Exec("BEGIN EXCLUSIVE")
		Expect(err).NotTo(HaveOccurred())

		store, err := NewPropertiesStore(storageDir)

		Expect(err).To(matchPropertiesSQLitePrimaryError(sqlite3.SQLITE_BUSY))
		Expect(store).To(BeNil())
	})

	ginkgo.It("returns begin errors when the database is closed under the store", func() {
		store := newTestStore()
		Expect(store.db.Close()).To(Succeed())

		err := store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))

		Expect(err).To(MatchError(ErrPropertiesBeginTransaction))
	})

	ginkgo.It("returns write errors from malformed property tables", func() {
		store := newTestStore()
		execPropertiesSQL(store, `DROP TABLE page_properties`)

		err := store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))
		Expect(err).To(matchPropertiesSQLitePrimaryError(sqlite3.SQLITE_ERROR))

		prepareErrorStore := newTestStore()
		execPropertiesSQL(prepareErrorStore,
			`DROP TABLE page_properties`,
			`CREATE TABLE page_properties (page_id TEXT PRIMARY KEY)`,
		)
		err = prepareErrorStore.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))
		Expect(err).To(matchPropertiesSQLitePrimaryError(sqlite3.SQLITE_ERROR))

		insertErrorStore := newTestStore()
		execPropertiesSQL(insertErrorStore,
			`CREATE TRIGGER block_property_insert
			 BEFORE INSERT ON page_properties
			 BEGIN
				SELECT RAISE(ABORT, 'insert blocked');
			 END`,
		)
		err = insertErrorStore.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))
		Expect(err).To(matchPropertiesSQLitePrimaryError(sqlite3.SQLITE_CONSTRAINT))
	})

	ginkgo.It("returns read query errors from missing property tables", func() {
		store := newTestStore()
		execPropertiesSQL(store, `DROP TABLE page_properties`)

		keys, err := store.GetAllPropertyKeys("", 50)
		Expect(keys).To(BeNil())
		Expect(err).To(matchPropertiesSQLitePrimaryError(sqlite3.SQLITE_ERROR))

		pageIDs, err := store.GetPageIDsByProperty("status", "draft")
		Expect(pageIDs).To(BeNil())
		Expect(err).To(matchPropertiesSQLitePrimaryError(sqlite3.SQLITE_ERROR))

		byPage, err := store.GetPropertiesForPages(testPageIDs("page-1"))
		Expect(byPage).To(BeNil())
		Expect(err).To(matchPropertiesSQLitePrimaryError(sqlite3.SQLITE_ERROR))
	})

	ginkgo.It("returns scan errors from malformed property rows", func() {
		pageIDStore := newTestStore()
		execPropertiesSQL(pageIDStore,
			`DROP TABLE page_properties`,
			`CREATE TABLE page_properties (page_id INTEGER, key TEXT, value TEXT, type TEXT)`,
			`INSERT INTO page_properties (page_id, key, value, type) VALUES (42, 'status', 'draft', 'text')`,
		)
		pageIDs, err := pageIDStore.GetPageIDsByProperty("status", "draft")
		Expect(pageIDs).To(BeNil())
		Expect(err).To(SatisfyAll(
			MatchError(ErrPropertiesScanRow),
			MatchError(tree.ErrScanPageID),
		))

		propertiesStore := newTestStore()
		execPropertiesSQL(propertiesStore,
			`DROP TABLE page_properties`,
			`CREATE TABLE page_properties (page_id TEXT, key TEXT, value TEXT, type TEXT)`,
			`INSERT INTO page_properties (page_id, key, value, type) VALUES ('page-1', NULL, 'draft', 'text')`,
		)
		byPage, err := propertiesStore.GetPropertiesForPages(testPageIDs("page-1"))
		Expect(byPage).To(BeNil())
		Expect(err).To(MatchError(ErrPropertiesScanRow))
	})

	ginkgo.It("returns key-count scan errors", func() {
		store := newTestStore()
		Expect(store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))).To(Succeed())
		previousScan := scanPropertyKeyCount
		scanErr := errors.New("scan failed")
		scanPropertyKeyCount = func(propertyKeyCountScanner, *PropertyKeyCount) error {
			return scanErr
		}
		ginkgo.DeferCleanup(func() {
			scanPropertyKeyCount = previousScan
		})

		keys, err := store.GetAllPropertyKeys("", 0)

		Expect(keys).To(BeNil())
		Expect(err).To(MatchError(scanErr))
	})

	ginkgo.It("returns close errors without clearing the database handle", func() {
		store, err := NewPropertiesStore(propertiesTempDir())
		Expect(err).NotTo(HaveOccurred())
		previousClose := closePropertiesDB
		closeErr := errors.New("close failed")
		closePropertiesDB = func(*sql.DB) error {
			return closeErr
		}
		ginkgo.DeferCleanup(func() {
			closePropertiesDB = previousClose
			Expect(store.Close()).To(Succeed())
		})

		err = store.Close()

		Expect(err).To(MatchError(closeErr))
		Expect(store.db).NotTo(BeNil())
	})
})

func execPropertiesSQL(store *PropertiesStore, statements ...string) {
	ginkgo.GinkgoHelper()
	for _, statement := range statements {
		_, err := store.db.Exec(statement)
		Expect(err).NotTo(HaveOccurred())
	}
}

func matchPropertiesSQLitePrimaryError(code int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(err error) int {
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) {
			return -1
		}
		return sqliteErr.Code() & 0xFF
	}, Equal(code))
}
