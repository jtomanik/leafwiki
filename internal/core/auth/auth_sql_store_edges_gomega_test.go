package auth

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

const authScriptedDriverName = "leafwiki_auth_scripted"

var (
	authScriptedDriverOnce sync.Once
	authScriptedScriptsMu  sync.Mutex
	authScriptedScriptSeq  int
	authScriptedScripts    = map[string]*authScriptedDBScript{}
	errAPIKeyScanFailed    = errors.New("api key scan failed")
	errPlainConstraint     = errors.New("plain error")
)

type authScriptedDBScript struct {
	exec  func(string, []driver.NamedValue) (driver.Result, error)
	query func(string, []driver.NamedValue) (driver.Rows, error)
	close func() error
}

type authScriptedDriver struct{}

func (authScriptedDriver) Open(name string) (driver.Conn, error) {
	authScriptedScriptsMu.Lock()
	defer authScriptedScriptsMu.Unlock()

	script, ok := authScriptedScripts[name]
	if !ok {
		return nil, fmt.Errorf("missing scripted database %q", name)
	}
	return &authScriptedConn{script: script}, nil
}

type authScriptedConn struct {
	script *authScriptedDBScript
}

func (c *authScriptedConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("scripted statements are not supported")
}

func (c *authScriptedConn) Close() error {
	if c.script.close != nil {
		return c.script.close()
	}
	return nil
}

func (c *authScriptedConn) Begin() (driver.Tx, error) {
	return nil, errors.New("scripted transactions are not supported")
}

func (c *authScriptedConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.script.exec != nil {
		return c.script.exec(query, args)
	}
	return authScriptedResult{rowsAffected: 1}, nil
}

func (c *authScriptedConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.script.query != nil {
		return c.script.query(query, args)
	}
	return &authScriptedRows{}, nil
}

type authScriptedResult struct {
	rowsAffected    int64
	rowsAffectedErr error
}

func (r authScriptedResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (r authScriptedResult) RowsAffected() (int64, error) {
	if r.rowsAffectedErr != nil {
		return 0, r.rowsAffectedErr
	}
	return r.rowsAffected, nil
}

type authScriptedRows struct {
	columns      []string
	values       [][]driver.Value
	nextErr      error
	errAfterRows error
	closeErr     error
	index        int
}

func (r authScriptedRows) Columns() []string {
	return r.columns
}

func (r authScriptedRows) Close() error {
	return r.closeErr
}

func (r *authScriptedRows) Next(dest []driver.Value) error {
	if r.nextErr != nil {
		err := r.nextErr
		r.nextErr = nil
		return err
	}
	if r.index >= len(r.values) {
		if r.errAfterRows != nil {
			err := r.errAfterRows
			r.errAfterRows = nil
			return err
		}
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

type authFakeScanner struct {
	values []any
	err    error
}

func dereferenceAuthTimePointer(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

type authAPIKeyRowContract struct {
	ID         APIKeyID
	UserID     UserID
	Scopes     []string
	LastUsedAt time.Time
	RevokedAt  time.Time
}

type authStoredAPIKeyContract struct {
	SecretHash string
	Key        authAPIKeyRowContract
}

type authUserLabelContract struct {
	ID       UserID
	Username string
}

func authAPIKeyRowFields(key *APIKey) authAPIKeyRowContract {
	if key == nil {
		return authAPIKeyRowContract{}
	}
	return authAPIKeyRowContract{
		ID:         key.ID,
		UserID:     key.UserID,
		Scopes:     key.Scopes,
		LastUsedAt: dereferenceAuthTimePointer(key.LastUsedAt),
		RevokedAt:  dereferenceAuthTimePointer(key.RevokedAt),
	}
}

func matchAuthAPIKeyRow(expected authAPIKeyRowContract) types.GomegaMatcher {
	return WithTransform(authAPIKeyRowFields, gstruct.MatchAllFields(gstruct.Fields{
		"ID":         Equal(expected.ID),
		"UserID":     Equal(expected.UserID),
		"Scopes":     Equal(expected.Scopes),
		"LastUsedAt": Equal(expected.LastUsedAt),
		"RevokedAt":  Equal(expected.RevokedAt),
	}))
}

func authStoredAPIKeyFields(stored *storedAPIKey) authStoredAPIKeyContract {
	if stored == nil {
		return authStoredAPIKeyContract{}
	}
	return authStoredAPIKeyContract{
		SecretHash: stored.secretHash,
		Key:        authAPIKeyRowFields(stored.key),
	}
}

func matchAuthStoredAPIKey(expected authStoredAPIKeyContract) types.GomegaMatcher {
	return WithTransform(authStoredAPIKeyFields, gstruct.MatchAllFields(gstruct.Fields{
		"SecretHash": Equal(expected.SecretHash),
		"Key":        Equal(expected.Key),
	}))
}

func authUserLabelFields(label *UserLabel) authUserLabelContract {
	if label == nil {
		return authUserLabelContract{}
	}
	return authUserLabelContract{
		ID:       label.ID,
		Username: label.Username,
	}
}

func matchAuthUserLabel(expected authUserLabelContract) types.GomegaMatcher {
	return WithTransform(authUserLabelFields, gstruct.MatchAllFields(gstruct.Fields{
		"ID":       Equal(expected.ID),
		"Username": Equal(expected.Username),
	}))
}

func (s authFakeScanner) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	for i, value := range s.values {
		switch target := dest[i].(type) {
		case *APIKeyID:
			*target = APIKeyIDFromString(value.(string))
		case *UserID:
			*target = UserIDFromString(value.(string))
		case *string:
			*target = value.(string)
		case *int64:
			*target = value.(int64)
		case *sql.NullInt64:
			*target = value.(sql.NullInt64)
		default:
			return fmt.Errorf("unsupported fake scan target %T", target)
		}
	}
	return nil
}
