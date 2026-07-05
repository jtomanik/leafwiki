package links

import (
	"context"
	"database/sql"
	"database/sql/driver"
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
