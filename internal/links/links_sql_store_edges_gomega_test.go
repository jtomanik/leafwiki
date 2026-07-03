package links

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/tree"
)

const linksScriptedDriverName = "leafwiki_links_scripted"

type linksReadMethodErrors struct {
	TableColumns                       error
	Backlinks                          error
	OutgoingForPage                    error
	OutgoingForPages                   error
	RefactorMatches                    error
	RefactorMatchesForPageKind         error
	RefactorMatchesForSectionKind      error
	RefactorSourceIDs                  error
	RefactorSourceIDsForPageKind       error
	RefactorSourceIDsForSectionKind    error
	BrokenIncomingForPath              error
	BrokenIncomingForPathAndTargetKind error
}

var (
	linksScriptedDriverOnce sync.Once
	linksScriptedScriptsMu  sync.Mutex
	linksScriptedScriptSeq  int
	linksScriptedScripts    = map[string]*linksScriptedDBScript{}
)

type linksScriptedDBScript struct {
	exec        func(string, []driver.NamedValue) (driver.Result, error)
	query       func(string, []driver.NamedValue) (driver.Rows, error)
	prepare     func(string) (driver.Stmt, error)
	beginErr    error
	commitErr   error
	rollbackErr error
	closeErr    error
}

type linksScriptedDriver struct{}

func (linksScriptedDriver) Open(name string) (driver.Conn, error) {
	linksScriptedScriptsMu.Lock()
	defer linksScriptedScriptsMu.Unlock()

	script, ok := linksScriptedScripts[name]
	if !ok {
		return nil, fmt.Errorf("missing scripted links database %q", name)
	}
	return &linksScriptedConn{script: script}, nil
}

type linksScriptedConn struct {
	script *linksScriptedDBScript
}

func (c *linksScriptedConn) Prepare(query string) (driver.Stmt, error) {
	if c.script.prepare != nil {
		return c.script.prepare(query)
	}
	return &linksScriptedStmt{script: c.script, query: query}, nil
}

func (c *linksScriptedConn) PrepareContext(_ context.Context, query string) (driver.Stmt, error) {
	return c.Prepare(query)
}

func (c *linksScriptedConn) Close() error {
	return c.script.closeErr
}

func (c *linksScriptedConn) Begin() (driver.Tx, error) {
	if c.script.beginErr != nil {
		return nil, c.script.beginErr
	}
	return linksScriptedTx{script: c.script}, nil
}

func (c *linksScriptedConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}

func (c *linksScriptedConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.script.exec != nil {
		return c.script.exec(query, args)
	}
	return linksScriptedResult{rowsAffected: 1}, nil
}

func (c *linksScriptedConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.script.query != nil {
		return c.script.query(query, args)
	}
	return &linksScriptedRows{}, nil
}

type linksScriptedTx struct {
	script *linksScriptedDBScript
}

func (tx linksScriptedTx) Commit() error {
	return tx.script.commitErr
}

func (tx linksScriptedTx) Rollback() error {
	return tx.script.rollbackErr
}

type linksScriptedStmt struct {
	script   *linksScriptedDBScript
	query    string
	closeErr error
}

func (s *linksScriptedStmt) Close() error {
	return s.closeErr
}

func (s *linksScriptedStmt) NumInput() int {
	return -1
}

func (s *linksScriptedStmt) Exec([]driver.Value) (driver.Result, error) {
	if s.script != nil && s.script.exec != nil {
		return s.script.exec(s.query, nil)
	}
	return linksScriptedResult{rowsAffected: 1}, nil
}

func (s *linksScriptedStmt) ExecContext(_ context.Context, args []driver.NamedValue) (driver.Result, error) {
	if s.script != nil && s.script.exec != nil {
		return s.script.exec(s.query, args)
	}
	return linksScriptedResult{rowsAffected: 1}, nil
}

func (s *linksScriptedStmt) Query([]driver.Value) (driver.Rows, error) {
	if s.script != nil && s.script.query != nil {
		return s.script.query(s.query, nil)
	}
	return &linksScriptedRows{}, nil
}

func (s *linksScriptedStmt) QueryContext(_ context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if s.script != nil && s.script.query != nil {
		return s.script.query(s.query, args)
	}
	return &linksScriptedRows{}, nil
}

type linksScriptedResult struct {
	rowsAffected    int64
	rowsAffectedErr error
}

func (r linksScriptedResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (r linksScriptedResult) RowsAffected() (int64, error) {
	if r.rowsAffectedErr != nil {
		return 0, r.rowsAffectedErr
	}
	return r.rowsAffected, nil
}

type linksScriptedRows struct {
	columns  []string
	values   [][]driver.Value
	nextErr  error
	closeErr error
	index    int
}

func (r linksScriptedRows) Columns() []string {
	return r.columns
}

func (r linksScriptedRows) Close() error {
	return r.closeErr
}

func (r *linksScriptedRows) Next(dest []driver.Value) error {
	if r.nextErr != nil {
		err := r.nextErr
		r.nextErr = nil
		return err
	}
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

var _ = Describe("links SQL store persistence edge behavior", func() {
	It("propagates construction, recovery, schema, migration, and close failures", func() {
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

	It("propagates add and replace transaction failures", func() {
		fromPageID := newFixturePageID("source-page")
		targetLink := TargetLink{
			TargetPageID:   newFixturePageID("target-page"),
			TargetPagePath: "/docs/target",
			TargetKind:     TargetKindPage,
		}

		deleteErr := errors.New("links delete failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			exec: execErrorWhen("DELETE FROM links WHERE from_page_id", deleteErr),
		}).AddLinks(fromPageID, "Source", nil)).To(MatchError(deleteErr))

		rollbackErr := errors.New("links rollback failed")
		err := newScriptedLinksStore(&linksScriptedDBScript{
			exec:        execErrorWhen("DELETE FROM links WHERE from_page_id", deleteErr),
			rollbackErr: rollbackErr,
		}).AddLinks(fromPageID, "Source", nil)
		Expect(err).To(SatisfyAll(MatchError(deleteErr), MatchError(rollbackErr)))

		prepareInsertErr := errors.New("links prepare insert failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			prepare: prepareErrorWhen("INSERT OR REPLACE INTO links", prepareInsertErr),
		}).AddLinks(fromPageID, "Source", nil)).To(MatchError(prepareInsertErr))

		err = newScriptedLinksStore(&linksScriptedDBScript{
			prepare:     prepareErrorWhen("INSERT OR REPLACE INTO links", prepareInsertErr),
			rollbackErr: rollbackErr,
		}).AddLinks(fromPageID, "Source", nil)
		Expect(err).To(SatisfyAll(MatchError(prepareInsertErr), MatchError(rollbackErr)))

		insertErr := errors.New("links insert failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			exec: execErrorWhen("INSERT OR REPLACE INTO links", insertErr),
		}).AddLinks(fromPageID, "Source", []TargetLink{targetLink})).To(MatchError(insertErr))

		err = newScriptedLinksStore(&linksScriptedDBScript{
			exec:        execErrorWhen("INSERT OR REPLACE INTO links", insertErr),
			rollbackErr: rollbackErr,
		}).AddLinks(fromPageID, "Source", []TargetLink{targetLink})
		Expect(err).To(SatisfyAll(MatchError(insertErr), MatchError(rollbackErr)))

		restoreCloseStatement := setLinksSeam(&linksCloseStatement, func(interface{ Close() error }) error {
			return errors.New("links statement close failed")
		})
		store := newAdditionalLinksStore()
		Expect(store.AddLinks(fromPageID, "Source", nil)).To(Succeed())
		Expect(store.ReplaceLinksAndHeal(nil)).To(Succeed())
		restoreCloseStatement()

		update := PageLinkUpdate{
			FromPageID: fromPageID,
			FromTitle:  "Source",
			ToPath:     "/docs/source",
			ToKind:     tree.NodeKindPage,
			Targets: []TargetLink{{
				TargetPageID:   targetLink.TargetPageID,
				TargetPagePath: targetLink.TargetPagePath,
				TargetKind:     targetLink.TargetKind,
				Broken:         true,
			}},
		}
		Expect(store.ReplaceLinksAndHeal([]PageLinkUpdate{update})).To(Succeed())

		prepareDeleteErr := errors.New("links prepare delete failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			prepare: prepareErrorWhen("DELETE FROM links WHERE from_page_id", prepareDeleteErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{update})).To(MatchError(prepareDeleteErr))

		err = newScriptedLinksStore(&linksScriptedDBScript{
			prepare:     prepareErrorWhen("DELETE FROM links WHERE from_page_id", prepareDeleteErr),
			rollbackErr: rollbackErr,
		}).ReplaceLinksAndHeal([]PageLinkUpdate{update})
		Expect(err).To(SatisfyAll(MatchError(prepareDeleteErr), MatchError(rollbackErr)))

		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			prepare: prepareErrorWhen("INSERT OR REPLACE INTO links", prepareInsertErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{update})).To(MatchError(prepareInsertErr))

		prepareHealErr := errors.New("links prepare heal failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			prepare: prepareErrorWhen("WHERE to_path = ? AND to_kind = ? AND broken = 1", prepareHealErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{update})).To(MatchError(prepareHealErr))

		prepareSectionHealErr := errors.New("links prepare section heal failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			prepare: prepareErrorWhen("UPDATE OR REPLACE links", prepareSectionHealErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{update})).To(MatchError(prepareSectionHealErr))

		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			exec: execErrorWhen("DELETE FROM links WHERE from_page_id", deleteErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{update})).To(MatchError(deleteErr))

		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			exec: execErrorWhen("INSERT OR REPLACE INTO links", insertErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{update})).To(MatchError(insertErr))

		healPageErr := errors.New("links heal page failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			exec: execErrorWhen("WHERE to_path = ? AND to_kind = ? AND broken = 1", healPageErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{update})).To(MatchError(healPageErr))

		sectionUpdate := update
		sectionUpdate.ToKind = tree.NodeKindSection
		healSectionErr := errors.New("links heal section failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			exec: execErrorWhen("UPDATE OR REPLACE links", healSectionErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{sectionUpdate})).To(MatchError(healSectionErr))

		beginErr := errors.New("links begin failed")
		loadedTree := newLoadedLinksTreeService()
		createLoadedLinksPage(loadedTree, "Source", "source", "[Target](target.md)")
		service := NewLinkService("", loadedTree, newScriptedLinksStore(&linksScriptedDBScript{beginErr: beginErr}))
		Expect(service.IndexAllPages()).To(MatchError(beginErr))
	})

	It("returns stable read results and propagates query row failures", func() {
		store := newAdditionalLinksStore()
		restoreCloseRows := setLinksSeam(&linksCloseRows, func(interface{ Close() error }) error {
			return errors.New("links query close failed")
		})
		expectAllReadMethodsSucceed(store)
		restoreCloseRows()

		queryErr := errors.New("links read query failed")
		queryErrStore := newScriptedLinksStore(&linksScriptedDBScript{
			query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return nil, queryErr
			},
		})
		Expect(queryErrStore).To(HaveReadMethodsPropagate(queryErr))

		rowsErr := errors.New("links rows failed")
		rowsErrStore := newScriptedLinksStore(&linksScriptedDBScript{
			query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return &linksScriptedRows{columns: []string{"bad"}, nextErr: rowsErr}, nil
			},
		})
		Expect(rowsErrStore).To(HaveReadMethodsPropagate(rowsErr))

		nullRowsStore := newScriptedLinksStore(&linksScriptedDBScript{
			query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
				switch {
				case strings.Contains(query, "WHERE to_page_id"):
					return linksRows([]string{"from_page_id", "to_page_id", "from_title", "to_kind"}, []driver.Value{"source-page", nil, "Source", defaultStoredTargetKind}), nil
				case strings.Contains(query, "WHERE from_page_id"):
					return linksRows([]string{"from_page_id", "to_page_id", "to_path", "to_kind", "from_title", "broken"}, []driver.Value{"source-page", nil, "/docs/target", defaultStoredTargetKind, "Source", int64(0)}), nil
				default:
					return linksRows([]string{"from_page_id", "to_page_id", "from_title", "to_kind"}, []driver.Value{"source-page", nil, "Source", defaultStoredTargetKind}), nil
				}
			},
		})
		backlinks, err := nullRowsStore.GetBacklinksForPage(newFixturePageID("target-page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(backlinks).To(ContainElement(matchBacklink(gstruct.Fields{
			"ToPageID": BeEmpty(),
		})))
		outgoing, err := nullRowsStore.GetOutgoingLinksForPage(newFixturePageID("source-page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(outgoing).To(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ToPageID": BeEmpty(),
		})))
		broken, err := nullRowsStore.GetBrokenIncomingForPathAndKind("/docs/target", tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		Expect(broken).To(ContainElement(matchBacklink(gstruct.Fields{
			"ToPageID": BeEmpty(),
		})))

		validBrokenStore := newAdditionalLinksStore()
		Expect(validBrokenStore.AddLinks("broken-source", "Broken Source", []TargetLink{{
			TargetPageID:   "target-page",
			TargetPagePath: "/docs/target",
			TargetKind:     TargetKindPage,
			Broken:         true,
		}})).To(Succeed())
		broken, err = validBrokenStore.GetBrokenIncomingForPathAndKind("/docs/target", tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		Expect(broken).To(ConsistOf(matchBacklink(gstruct.Fields{
			"ToPageID": Equal(newFixturePageID("target-page")),
		})))

		statusTree := newLoadedLinksTreeService()
		sourcePage := createLoadedLinksPage(statusTree, "Source", "source", "")
		targetPage := createLoadedLinksPage(statusTree, "Target", "target", "")
		Expect(store.AddLinks(sourcePage.ID, sourcePage.Title, []TargetLink{{
			TargetPageID:   targetPage.ID,
			TargetPagePath: targetPage.CalculateRoutePath().WikiPath(),
			TargetKind:     TargetKindPage,
		}})).To(Succeed())
		status, err := NewLinkService("", statusTree, store).GetLinkStatusForPage(sourcePage.ID, sourcePage.CalculateRoutePath())
		Expect(err).NotTo(HaveOccurred())
		Expect(status).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Outgoings":       HaveLen(1),
			"BrokenOutgoings": BeEmpty(),
		})))

		brokenErr := errors.New("broken incoming failed")
		statusStore := newScriptedLinksStore(&linksScriptedDBScript{
			query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "to_kind IN") {
					return nil, brokenErr
				}
				return &linksScriptedRows{}, nil
			},
		})
		_, err = NewLinkService("", nil, statusStore).GetLinkStatusForPage("source-page", "/docs/source")
		Expect(err).To(MatchError(brokenErr))

		outgoingErr := errors.New("outgoing failed")
		statusStore = newScriptedLinksStore(&linksScriptedDBScript{
			query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "WHERE from_page_id") {
					return nil, outgoingErr
				}
				return &linksScriptedRows{}, nil
			},
		})
		_, err = NewLinkService("", nil, statusStore).GetLinkStatusForPage("source-page", "/docs/source")
		Expect(err).To(MatchError(outgoingErr))
	})
})

func openLinksScriptedDB(script *linksScriptedDBScript) *sql.DB {
	GinkgoHelper()

	linksScriptedDriverOnce.Do(func() {
		sql.Register(linksScriptedDriverName, linksScriptedDriver{})
	})

	linksScriptedScriptsMu.Lock()
	linksScriptedScriptSeq++
	dsn := fmt.Sprintf("script-%d", linksScriptedScriptSeq)
	linksScriptedScripts[dsn] = script
	linksScriptedScriptsMu.Unlock()

	db, err := sql.Open(linksScriptedDriverName, dsn)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		_ = db.Close()
		linksScriptedScriptsMu.Lock()
		delete(linksScriptedScripts, dsn)
		linksScriptedScriptsMu.Unlock()
	})
	return db
}

func newScriptedLinksStore(script *linksScriptedDBScript) *LinksStore {
	GinkgoHelper()
	return &LinksStore{db: openLinksScriptedDB(script)}
}

func linksRows(columns []string, values ...[]driver.Value) *linksScriptedRows {
	return &linksScriptedRows{
		columns: append([]string(nil), columns...),
		values:  cloneDriverValues(values),
	}
}

func cloneDriverValues(values [][]driver.Value) [][]driver.Value {
	cloned := make([][]driver.Value, len(values))
	for i := range values {
		cloned[i] = append([]driver.Value(nil), values[i]...)
	}
	return cloned
}

func execErrorWhen(fragment string, err error) func(string, []driver.NamedValue) (driver.Result, error) {
	return func(query string, _ []driver.NamedValue) (driver.Result, error) {
		if strings.Contains(query, fragment) {
			return nil, err
		}
		return linksScriptedResult{rowsAffected: 1}, nil
	}
}

func prepareErrorWhen(fragment string, err error) func(string) (driver.Stmt, error) {
	return func(query string) (driver.Stmt, error) {
		if strings.Contains(query, fragment) {
			return nil, err
		}
		return &linksScriptedStmt{script: &linksScriptedDBScript{}, query: query}, nil
	}
}

func expectAllReadMethodsSucceed(store *LinksStore) {
	GinkgoHelper()

	_, err := store.linksTableColumns()
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetBacklinksForPage("target-page")
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetOutgoingLinksForPage("source-page")
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetOutgoingLinksForPages([]tree.PageID{"source-page"})
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetRefactorMatchesForPrefix("/docs")
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetRefactorMatchesForPrefixAndKind("/docs", tree.NodeKindPage)
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetRefactorMatchesForPrefixAndKind("/docs", tree.NodeKindSection)
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetRefactorSourcePageIDsForPrefix("/docs")
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetRefactorSourcePageIDsForPrefixAndKind("/docs", tree.NodeKindPage)
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetRefactorSourcePageIDsForPrefixAndKind("/docs", tree.NodeKindSection)
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetBrokenIncomingForPath("/docs")
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetBrokenIncomingForPathAndKind("/docs", tree.NodeKindPage)
	Expect(err).NotTo(HaveOccurred())
}

func HaveReadMethodsPropagate(target error) types.GomegaMatcher {
	GinkgoHelper()
	return WithTransform(collectLinksReadMethodErrors, gstruct.MatchAllFields(gstruct.Fields{
		"TableColumns":                       matchLinksError(target),
		"Backlinks":                          matchLinksError(target),
		"OutgoingForPage":                    matchLinksError(target),
		"OutgoingForPages":                   matchLinksError(target),
		"RefactorMatches":                    matchLinksError(target),
		"RefactorMatchesForPageKind":         matchLinksError(target),
		"RefactorMatchesForSectionKind":      matchLinksError(target),
		"RefactorSourceIDs":                  matchLinksError(target),
		"RefactorSourceIDsForPageKind":       matchLinksError(target),
		"RefactorSourceIDsForSectionKind":    matchLinksError(target),
		"BrokenIncomingForPath":              matchLinksError(target),
		"BrokenIncomingForPathAndTargetKind": matchLinksError(target),
	}))
}

func collectLinksReadMethodErrors(store *LinksStore) linksReadMethodErrors {
	var errs linksReadMethodErrors
	_, errs.TableColumns = store.linksTableColumns()
	_, errs.Backlinks = store.GetBacklinksForPage("target-page")
	_, errs.OutgoingForPage = store.GetOutgoingLinksForPage("source-page")
	_, errs.OutgoingForPages = store.GetOutgoingLinksForPages([]tree.PageID{"source-page"})
	_, errs.RefactorMatches = store.GetRefactorMatchesForPrefix("/docs")
	_, errs.RefactorMatchesForPageKind = store.GetRefactorMatchesForPrefixAndKind("/docs", tree.NodeKindPage)
	_, errs.RefactorMatchesForSectionKind = store.GetRefactorMatchesForPrefixAndKind("/docs", tree.NodeKindSection)
	_, errs.RefactorSourceIDs = store.GetRefactorSourcePageIDsForPrefix("/docs")
	_, errs.RefactorSourceIDsForPageKind = store.GetRefactorSourcePageIDsForPrefixAndKind("/docs", tree.NodeKindPage)
	_, errs.RefactorSourceIDsForSectionKind = store.GetRefactorSourcePageIDsForPrefixAndKind("/docs", tree.NodeKindSection)
	_, errs.BrokenIncomingForPath = store.GetBrokenIncomingForPath("/docs")
	_, errs.BrokenIncomingForPathAndTargetKind = store.GetBrokenIncomingForPathAndKind("/docs", tree.NodeKindPage)
	return errs
}
