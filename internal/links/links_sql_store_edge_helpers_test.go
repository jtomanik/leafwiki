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

type linksSQLWriteOperation uint8

const (
	linksSQLDeleteOutgoing linksSQLWriteOperation = iota
	linksSQLInsertOutgoing
	linksSQLHealPageLinks
	linksSQLHealSectionLinks
)

func execErrorWhen(operation linksSQLWriteOperation, err error) func(string, []driver.NamedValue) (driver.Result, error) {
	return func(query string, _ []driver.NamedValue) (driver.Result, error) {
		if linksSQLQueryMatches(query, operation) {
			return nil, err
		}
		return linksScriptedResult{rowsAffected: 1}, nil
	}
}

func prepareErrorWhen(operation linksSQLWriteOperation, err error) func(string) (driver.Stmt, error) {
	return func(query string) (driver.Stmt, error) {
		if linksSQLQueryMatches(query, operation) {
			return nil, err
		}
		return &linksScriptedStmt{script: &linksScriptedDBScript{}, query: query}, nil
	}
}

func linksSQLQueryMatches(query string, operation linksSQLWriteOperation) bool {
	switch operation {
	case linksSQLDeleteOutgoing:
		return strings.Contains(query, "DELETE FROM links WHERE from_page_id")
	case linksSQLInsertOutgoing:
		return strings.Contains(query, "INSERT OR REPLACE INTO links")
	case linksSQLHealPageLinks:
		return strings.Contains(query, "WHERE to_path = ? AND to_kind = ? AND broken = 1")
	case linksSQLHealSectionLinks:
		return strings.Contains(query, "UPDATE OR REPLACE links")
	default:
		return false
	}
}

func expectAllReadMethodsSucceed(store *LinksStore) {
	GinkgoHelper()

	_, err := store.linksTableColumns()
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetBacklinksForPage(newFixturePageID("target-page"))
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetOutgoingLinksForPage(newFixturePageID("source-page"))
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetOutgoingLinksForPages([]tree.PageID{newFixturePageID("source-page")})
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetRefactorMatchesForPrefix(newFixtureRoutePath("/docs"))
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetRefactorMatchesForPrefixAndKind(newFixtureRoutePath("/docs"), tree.NodeKindPage)
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetRefactorMatchesForPrefixAndKind(newFixtureRoutePath("/docs"), tree.NodeKindSection)
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetRefactorSourcePageIDsForPrefix(newFixtureRoutePath("/docs"))
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetRefactorSourcePageIDsForPrefixAndKind(newFixtureRoutePath("/docs"), tree.NodeKindPage)
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetRefactorSourcePageIDsForPrefixAndKind(newFixtureRoutePath("/docs"), tree.NodeKindSection)
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetBrokenIncomingForPath(newFixtureRoutePath("/docs"))
	Expect(err).NotTo(HaveOccurred())
	_, err = store.GetBrokenIncomingForPathAndKind(newFixtureRoutePath("/docs"), tree.NodeKindPage)
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
	_, errs.Backlinks = store.GetBacklinksForPage(newFixturePageID("target-page"))
	_, errs.OutgoingForPage = store.GetOutgoingLinksForPage(newFixturePageID("source-page"))
	_, errs.OutgoingForPages = store.GetOutgoingLinksForPages([]tree.PageID{newFixturePageID("source-page")})
	_, errs.RefactorMatches = store.GetRefactorMatchesForPrefix(newFixtureRoutePath("/docs"))
	_, errs.RefactorMatchesForPageKind = store.GetRefactorMatchesForPrefixAndKind(newFixtureRoutePath("/docs"), tree.NodeKindPage)
	_, errs.RefactorMatchesForSectionKind = store.GetRefactorMatchesForPrefixAndKind(newFixtureRoutePath("/docs"), tree.NodeKindSection)
	_, errs.RefactorSourceIDs = store.GetRefactorSourcePageIDsForPrefix(newFixtureRoutePath("/docs"))
	_, errs.RefactorSourceIDsForPageKind = store.GetRefactorSourcePageIDsForPrefixAndKind(newFixtureRoutePath("/docs"), tree.NodeKindPage)
	_, errs.RefactorSourceIDsForSectionKind = store.GetRefactorSourcePageIDsForPrefixAndKind(newFixtureRoutePath("/docs"), tree.NodeKindSection)
	_, errs.BrokenIncomingForPath = store.GetBrokenIncomingForPath(newFixtureRoutePath("/docs"))
	_, errs.BrokenIncomingForPathAndTargetKind = store.GetBrokenIncomingForPathAndKind(newFixtureRoutePath("/docs"), tree.NodeKindPage)
	return errs
}
