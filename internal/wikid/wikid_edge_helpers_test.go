package wikid

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unsafe"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
	sqlite "modernc.org/sqlite"
)

const (
	wikidWorkspaceInsertBlockedFixture = "workspace insert blocked"
	wikidWorkspaceUpdateBlockedFixture = "workspace update blocked"
)

type wikidErrorMatcher struct {
	label string
	match func(error) bool
}

func matchWikidWorkspaceIDValidationError(code sharederrors.ErrorCode) types.GomegaMatcher {
	return wikidErrorMatcher{
		label: "workspace ID validation error",
		match: func(err error) bool {
			var validationErr *workspaceid.ValidationError
			return errors.As(err, &validationErr) &&
				validationErr.Code == code &&
				validationErr.MessageID == sharederrors.MessageIDForCode(code)
		},
	}
}

func (matcher wikidErrorMatcher) Match(actual interface{}) (bool, error) {
	err, ok := actual.(error)
	if !ok {
		return false, nil
	}
	return matcher.match(err), nil
}

func (matcher wikidErrorMatcher) FailureMessage(actual interface{}) string {
	return "Expected\n\t" + format.Object(actual, 1) + "\nto satisfy " + matcher.label
}

func (matcher wikidErrorMatcher) NegatedFailureMessage(actual interface{}) string {
	return "Expected\n\t" + format.Object(actual, 1) + "\nnot to satisfy " + matcher.label
}

func newWikidEdgeLayout() Layout {
	ginkgo.GinkgoHelper()
	return GlobalLayout(filepath.Join(wikidTestTempDir(), ".leafwiki"))
}

func wikidDBPathInsideFile() string {
	ginkgo.GinkgoHelper()
	parentFile := filepath.Join(wikidTestTempDir(), "not-a-dir")
	Expect(os.WriteFile(parentFile, []byte("x"), 0o644)).To(Succeed())
	return filepath.Join(parentFile, "wikid.db")
}

func rawSQLiteDB() *sql.DB {
	ginkgo.GinkgoHelper()
	db, err := sql.Open("sqlite", filepath.Join(wikidTestTempDir(), "raw.db"))
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(db.Close()).To(Succeed())
	})
	return db
}

func rawSQLiteDBAt(path string) *sql.DB {
	ginkgo.GinkgoHelper()
	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	db, err := sql.Open("sqlite", path)
	Expect(err).NotTo(HaveOccurred())
	return db
}

func rawGrantDB() *sql.DB {
	ginkgo.GinkgoHelper()
	db := rawSQLiteDB()
	Expect(execRawSQL(db, `CREATE TABLE workspace_grants (subject TEXT, workspace_id TEXT, role TEXT)`)).To(Succeed())
	return db
}

func rawRegistryDB() *sql.DB {
	ginkgo.GinkgoHelper()
	db := rawSQLiteDB()
	Expect(execRawSQL(db, `CREATE TABLE workspaces (
		id TEXT,
		display_name TEXT,
		data_dir TEXT,
		root_dir TEXT,
		markdown_link_root_prefix TEXT,
		created_at TEXT,
		updated_at TEXT
	)`)).To(Succeed())
	return db
}

func closedSQLiteDB() *sql.DB {
	ginkgo.GinkgoHelper()
	db, err := sql.Open("sqlite", filepath.Join(wikidTestTempDir(), "closed.db"))
	Expect(err).NotTo(HaveOccurred())
	Expect(db.Close()).To(Succeed())
	return db
}

func wikidClosedDatabaseError() error {
	ginkgo.GinkgoHelper()
	db := closedSQLiteDB()
	_, err := db.Conn(context.Background())
	return err
}

func mustOpenInitializedWikidDB(path string) *sql.DB {
	ginkgo.GinkgoHelper()
	db, err := openWikidDB(path)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(db.Close()).To(Succeed())
	})
	return db
}

func execRawSQL(db *sql.DB, query string) error {
	ginkgo.GinkgoHelper()
	_, err := db.Exec(query)
	return err
}

func validWorkspaceSubject(*http.Request) (WorkspaceSubject, error) {
	return WorkspaceSubject{Subject: "user:1"}, nil
}

func privateWorkspaceAPIStatus(api http.Handler, method string, path string) int {
	ginkgo.GinkgoHelper()
	return privateWorkspaceAPIRecorder(api, method, path).Code
}

func privateWorkspaceAPIRecorder(api http.Handler, method string, path string) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	return rec
}

func privateWorkspaceAPIRecorderForWorkspace(api http.Handler, method string, id workspaceid.WorkspaceID, action string) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	req := httptest.NewRequest(method, PrivateWorkspacesPrefix+"/"+id.URLPathSegment()+"/"+action, nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	return rec
}

func privateHandlerRecorder(handler http.Handler, method string, path string, token string) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	req := httptest.NewRequest(method, path, nil)
	if strings.TrimSpace(token) != "" {
		req.Header.Set(projectdaemon.ControlTokenHeader, token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func namedHandler(name string, called map[string]int) http.Handler {
	ginkgo.GinkgoHelper()
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called[name]++
		_, _ = w.Write([]byte(name))
	})
}

type lockErrorExecer struct {
	err          error
	succeedAfter int
	calls        int
}

func (e *lockErrorExecer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	e.calls++
	if e.succeedAfter > 0 && e.calls > e.succeedAfter {
		return wikidNoopSQLResult{}, nil
	}
	return nil, e.err
}

type wikidNoopSQLResult struct{}

func (wikidNoopSQLResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (wikidNoopSQLResult) RowsAffected() (int64, error) {
	return 0, nil
}

func wikidSQLiteErrorWithCode(code int) error {
	e := &sqlite.Error{}
	v := reflect.ValueOf(e).Elem().FieldByName("code")
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().SetInt(int64(code))
	return e
}

type workspaceOrderSnapshot struct {
	FirstID           workspaceid.WorkspaceID
	SecondDisplayName string
	ThirdDisplayName  string
}

type workspaceOrderTieState string

const (
	workspaceOrderTieMissing workspaceOrderTieState = "missing"
	workspaceOrderTieFolded  workspaceOrderTieState = "equal-fold"
	workspaceOrderTieSplit   workspaceOrderTieState = "split"
)

func haveHomeWorkspaceFirstAndEqualFoldTie() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return SatisfyAll(
		HaveLen(3),
		WithTransform(func(workspaces []WorkspaceRecord) workspaceOrderSnapshot {
			if len(workspaces) < 3 {
				return workspaceOrderSnapshot{}
			}
			return workspaceOrderSnapshot{
				FirstID:           workspaces[0].ID,
				SecondDisplayName: workspaces[1].DisplayName,
				ThirdDisplayName:  workspaces[2].DisplayName,
			}
		}, SatisfyAll(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"FirstID": Equal(HomeWorkspaceID),
			}),
			WithTransform(workspaceOrderTieStateFromSnapshot, Equal(workspaceOrderTieFolded)),
		)),
	)
}

func matchCreatedWorkspaceRegistration(id workspaceid.WorkspaceID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(workspaceRegistrationStateFromResult, Equal(workspaceRegistrationState{
		id:      id,
		outcome: workspaceRegistrationCreated,
	}))
}

type workspaceRegistrationOutcome string

const (
	workspaceRegistrationMissing workspaceRegistrationOutcome = "missing"
	workspaceRegistrationCreated workspaceRegistrationOutcome = "created"
	workspaceRegistrationReused  workspaceRegistrationOutcome = "reused"
)

type workspaceRegistrationState struct {
	id      workspaceid.WorkspaceID
	outcome workspaceRegistrationOutcome
}

func workspaceRegistrationStateFromResult(result RegisterWorkspaceResult) workspaceRegistrationState {
	outcome := workspaceRegistrationReused
	if result.Created {
		outcome = workspaceRegistrationCreated
	}
	if result.Workspace.ID == "" {
		outcome = workspaceRegistrationMissing
	}
	return workspaceRegistrationState{
		id:      result.Workspace.ID,
		outcome: outcome,
	}
}

func workspaceOrderTieStateFromSnapshot(snapshot workspaceOrderSnapshot) workspaceOrderTieState {
	if snapshot.SecondDisplayName == "" || snapshot.ThirdDisplayName == "" {
		return workspaceOrderTieMissing
	}
	if strings.EqualFold(snapshot.SecondDisplayName, snapshot.ThirdDisplayName) {
		return workspaceOrderTieFolded
	}
	return workspaceOrderTieSplit
}

func matchWikidSQLitePrimaryError(code int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(err error) int {
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) {
			return -1
		}
		return sqliteErr.Code() & 0xFF
	}, Equal(code))
}

func matchWikidTimeParseError() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return Satisfy(func(err error) bool {
		var parseErr *time.ParseError
		return errors.As(err, &parseErr)
	})
}

type errRows struct {
	err error
}

func (r errRows) Next() bool {
	return false
}

func (r errRows) Scan(...any) error {
	return nil
}

func (r errRows) Err() error {
	return r.err
}

type closeErrorFile struct {
	err error
}

func (f closeErrorFile) Close() error {
	return f.err
}

func captureWikidAuthSeams() func() {
	ginkgo.GinkgoHelper()
	remove := wikidRemove
	mkdirAll := wikidMkdirAll
	newUserStore := wikidNewUserStore
	newSessionStore := wikidNewSessionStore
	newAPIKeyStore := wikidNewAPIKeyStore
	return func() {
		wikidRemove = remove
		wikidMkdirAll = mkdirAll
		wikidNewUserStore = newUserStore
		wikidNewSessionStore = newSessionStore
		wikidNewAPIKeyStore = newAPIKeyStore
	}
}

func captureWikidPathSeams() func() {
	ginkgo.GinkgoHelper()
	filepathAbs := wikidFilepathAbs
	evalSymlinks := wikidEvalSymlinks
	return func() {
		wikidFilepathAbs = filepathAbs
		wikidEvalSymlinks = evalSymlinks
	}
}

func captureWikidSQLiteSeams() func() {
	ginkgo.GinkgoHelper()
	mkdirAll := wikidMkdirAll
	openFile := wikidOpenFile
	chmod := wikidChmod
	sqlOpen := wikidSQLOpen
	initialize := wikidInitializeWikidDB
	openDB := wikidOpenWikidDB
	closeDB := wikidCloseDB
	closeConn := wikidCloseConn
	execWithRetry := wikidExecSQLiteWithLockRetry
	return func() {
		wikidMkdirAll = mkdirAll
		wikidOpenFile = openFile
		wikidChmod = chmod
		wikidSQLOpen = sqlOpen
		wikidInitializeWikidDB = initialize
		wikidOpenWikidDB = openDB
		wikidCloseDB = closeDB
		wikidCloseConn = closeConn
		wikidExecSQLiteWithLockRetry = execWithRetry
	}
}
