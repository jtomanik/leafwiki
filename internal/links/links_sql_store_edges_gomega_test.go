package links

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("links SQL store persistence edge behavior", func() {
	It("propagates construction, recovery, schema, migration, and close failures", Label("integration"), func() {
		openErr := errors.New("links open failed")
		restoreOpen := setLinksSeam(&linksSQLOpen, func(string, string) (*sql.DB, error) {
			return nil, openErr
		})
		_, err := NewLinksStore(linksTempDir())
		Expect(err).To(MatchError(openErr))
		Expect((&LinksStore{}).Connect()).To(MatchError(openErr))
		Expect((&LinksStore{}).ensureSchema()).To(MatchError(openErr))
		restoreOpen()

		schemaErr := errors.New("links schema failed")
		restoreOpen = setLinksSeam(&linksSQLOpen, func(string, string) (*sql.DB, error) {
			return openLinksScriptedDB(&linksScriptedDBScript{
				exec: func(string, []driver.NamedValue) (driver.Result, error) {
					return nil, schemaErr
				},
			}), nil
		})
		restoreRecoverable := setLinksSeam(&linksIsSQLiteRecoverableError, func(error) bool { return false })
		_, err = NewLinksStore(linksTempDir())
		Expect(err).To(MatchError(schemaErr))
		restoreOpen()
		restoreRecoverable()

		retryOpenErr := errors.New("links retry open failed")
		retryDir := linksTempDir()
		removedPath := ""
		openCount := 0
		restoreOpen = setLinksSeam(&linksSQLOpen, func(string, string) (*sql.DB, error) {
			openCount++
			if openCount == 1 {
				return openLinksScriptedDB(&linksScriptedDBScript{
					exec: func(string, []driver.NamedValue) (driver.Result, error) {
						return nil, schemaErr
					},
				}), nil
			}
			return nil, retryOpenErr
		})
		restoreRecoverable = setLinksSeam(&linksIsSQLiteRecoverableError, func(error) bool { return true })
		restoreRemove := setLinksSeam(&linksRemoveSQLiteFiles, func(path string) { removedPath = path })
		_, err = NewLinksStore(retryDir)
		Expect(err).To(MatchError(retryOpenErr))
		Expect(removedPath).To(Equal(linksDatabasePath(retryDir, "links.db")))
		restoreOpen()
		restoreRecoverable()
		restoreRemove()

		retrySchemaErr := errors.New("links retry schema failed")
		openCount = 0
		restoreOpen = setLinksSeam(&linksSQLOpen, func(string, string) (*sql.DB, error) {
			openCount++
			if openCount == 1 {
				return openLinksScriptedDB(&linksScriptedDBScript{
					exec: func(string, []driver.NamedValue) (driver.Result, error) {
						return nil, schemaErr
					},
				}), nil
			}
			return openLinksScriptedDB(&linksScriptedDBScript{
				exec: func(string, []driver.NamedValue) (driver.Result, error) {
					return nil, retrySchemaErr
				},
			}), nil
		})
		restoreRecoverable = setLinksSeam(&linksIsSQLiteRecoverableError, func(error) bool { return true })
		_, err = NewLinksStore(linksTempDir())
		Expect(err).To(MatchError(retrySchemaErr))
		restoreOpen()
		restoreRecoverable()

		openCount = 0
		restoreOpen = setLinksSeam(&linksSQLOpen, func(string, string) (*sql.DB, error) {
			openCount++
			if openCount == 1 {
				return openLinksScriptedDB(&linksScriptedDBScript{
					exec: func(string, []driver.NamedValue) (driver.Result, error) {
						return nil, schemaErr
					},
				}), nil
			}
			return openLinksScriptedDB(&linksScriptedDBScript{}), nil
		})
		restoreRecoverable = setLinksSeam(&linksIsSQLiteRecoverableError, func(error) bool { return true })
		store, err := NewLinksStore(linksTempDir())
		Expect(err).NotTo(HaveOccurred())
		Expect(store.Close()).To(Succeed())
		restoreOpen()
		restoreRecoverable()

		store = newAdditionalLinksStore()
		Expect(store.ensureLinksTable()).To(Succeed())

		columnsErr := errors.New("links columns failed")
		store = newScriptedLinksStore(&linksScriptedDBScript{
			query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "sqlite_master") {
					return linksRows([]string{"name"}, []driver.Value{"links"}), nil
				}
				return nil, columnsErr
			},
		})
		Expect(store.ensureLinksTable()).To(MatchError(columnsErr))

		restoreCloseRows := setLinksSeam(&linksCloseRows, func(interface{ Close() error }) error {
			return errors.New("links rows close failed")
		})
		_, err = newAdditionalLinksStore().linksTableColumns()
		Expect(err).NotTo(HaveOccurred())
		restoreCloseRows()

		tableInfoErr := errors.New("links table info rows failed")
		store = newScriptedLinksStore(&linksScriptedDBScript{
			query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return &linksScriptedRows{columns: []string{"cid", "name", "type", "notnull", "dflt_value", "pk"}, nextErr: tableInfoErr}, nil
			},
		})
		_, err = store.linksTableColumns()
		Expect(err).To(matchLinksError(tableInfoErr))

		migrateErr := errors.New("links migrate failed")
		store = newScriptedLinksStore(&linksScriptedDBScript{
			exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return nil, migrateErr
			},
		})
		Expect(store.migrateLinksTableToKindAware(nil)).To(MatchError(migrateErr))

		rollbackErr := errors.New("links rollback failed")
		store = newScriptedLinksStore(&linksScriptedDBScript{
			exec: func(string, []driver.NamedValue) (driver.Result, error) {
				return nil, migrateErr
			},
			rollbackErr: rollbackErr,
		})
		err = store.migrateLinksTableToKindAware(nil)
		Expect(err).To(SatisfyAll(MatchError(migrateErr), MatchError(rollbackErr)))

		closeErr := errors.New("links close failed")
		db := openLinksScriptedDB(&linksScriptedDBScript{closeErr: closeErr})
		Expect(db.Ping()).To(Succeed())
		store = &LinksStore{db: db}
		Expect(store.Close()).To(MatchError(closeErr))
	})
})
