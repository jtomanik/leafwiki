package properties

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("PropertiesStore error and recovery branches", func() {
	ginkgo.It("returns wrapped database open errors", func() {
		previousOpen := openPropertiesDB
		openErr := errors.New("open failed")
		openPropertiesDB = func(string) (*sql.DB, error) {
			return nil, openErr
		}
		ginkgo.DeferCleanup(func() {
			openPropertiesDB = previousOpen
		})

		store, err := NewPropertiesStore(ginkgo.GinkgoT().TempDir())

		Expect(store).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("failed to open properties database")))
		Expect(errors.Is(err, openErr)).To(BeTrue())
	})

	ginkgo.It("recovers from a corrupt properties database during initialization", func() {
		storageDir := ginkgo.GinkgoT().TempDir()
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
		var removed bool
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
		removePropertiesSQLiteFiles = func(string) {
			removed = true
		}
		ginkgo.DeferCleanup(func() {
			openPropertiesDB = previousOpen
			ensurePropertiesSchema = previousEnsure
			isRecoverablePropertiesDBError = previousRecoverable
			removePropertiesSQLiteFiles = previousRemove
		})

		store, err := NewPropertiesStore(ginkgo.GinkgoT().TempDir())

		Expect(store).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("failed to reopen properties database after recovery")))
		Expect(errors.Is(err, reopenErr)).To(BeTrue())
		Expect(openCalls).To(Equal(2))
		Expect(removed).To(BeTrue())
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

		store, err := NewPropertiesStore(ginkgo.GinkgoT().TempDir())

		Expect(store).To(BeNil())
		Expect(errors.Is(err, finalErr)).To(BeTrue())
		Expect(openCalls).To(Equal(2))
		Expect(ensureCalls).To(Equal(2))
	})

	ginkgo.It("returns initialization errors for locked databases", func() {
		storageDir := ginkgo.GinkgoT().TempDir()
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

		Expect(err).To(HaveOccurred())
		Expect(store).To(BeNil())
	})

	ginkgo.It("returns begin errors when the database is closed under the store", func() {
		store := newTestStore(ginkgo.GinkgoT())
		Expect(store.db.Close()).To(Succeed())

		err := store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))

		Expect(err).To(MatchError("sql: database is closed"))
	})

	ginkgo.It("returns write errors from malformed property tables", func() {
		store := newTestStore(ginkgo.GinkgoT())
		execPropertiesSQL(store, `DROP TABLE page_properties`)

		err := store.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))
		Expect(err).To(MatchError(ContainSubstring("no such table")))

		prepareErrorStore := newTestStore(ginkgo.GinkgoT())
		execPropertiesSQL(prepareErrorStore,
			`DROP TABLE page_properties`,
			`CREATE TABLE page_properties (page_id TEXT PRIMARY KEY)`,
		)
		err = prepareErrorStore.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))
		Expect(err).To(MatchError(ContainSubstring("no column named key")))

		insertErrorStore := newTestStore(ginkgo.GinkgoT())
		execPropertiesSQL(insertErrorStore,
			`CREATE TRIGGER block_property_insert
			 BEFORE INSERT ON page_properties
			 BEGIN
				SELECT RAISE(ABORT, 'insert blocked');
			 END`,
		)
		err = insertErrorStore.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))
		Expect(err).To(MatchError(ContainSubstring("insert blocked")))
	})

	ginkgo.It("returns read query errors from missing property tables", func() {
		store := newTestStore(ginkgo.GinkgoT())
		execPropertiesSQL(store, `DROP TABLE page_properties`)

		keys, err := store.GetAllPropertyKeys("", 50)
		Expect(keys).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("no such table")))

		pageIDs, err := store.GetPageIDsByProperty("status", "draft")
		Expect(pageIDs).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("no such table")))

		byPage, err := store.GetPropertiesForPages(testPageIDs("page-1"))
		Expect(byPage).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("no such table")))
	})

	ginkgo.It("returns scan errors from malformed property rows", func() {
		pageIDStore := newTestStore(ginkgo.GinkgoT())
		execPropertiesSQL(pageIDStore,
			`DROP TABLE page_properties`,
			`CREATE TABLE page_properties (page_id INTEGER, key TEXT, value TEXT, type TEXT)`,
			`INSERT INTO page_properties (page_id, key, value, type) VALUES (42, 'status', 'draft', 'text')`,
		)
		pageIDs, err := pageIDStore.GetPageIDsByProperty("status", "draft")
		Expect(pageIDs).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("cannot scan int64 into PageID")))

		propertiesStore := newTestStore(ginkgo.GinkgoT())
		execPropertiesSQL(propertiesStore,
			`DROP TABLE page_properties`,
			`CREATE TABLE page_properties (page_id TEXT, key TEXT, value TEXT, type TEXT)`,
			`INSERT INTO page_properties (page_id, key, value, type) VALUES ('page-1', NULL, 'draft', 'text')`,
		)
		byPage, err := propertiesStore.GetPropertiesForPages(testPageIDs("page-1"))
		Expect(byPage).To(BeNil())
		Expect(err).To(HaveOccurred())
	})

	ginkgo.It("returns key-count scan errors", func() {
		store := newTestStore(ginkgo.GinkgoT())
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
		Expect(errors.Is(err, scanErr)).To(BeTrue())
	})

	ginkgo.It("returns close errors without clearing the database handle", func() {
		store, err := NewPropertiesStore(ginkgo.GinkgoT().TempDir())
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

		Expect(errors.Is(err, closeErr)).To(BeTrue())
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
