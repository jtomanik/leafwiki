package auth

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func openAuthScriptedDB(script *authScriptedDBScript) *sql.DB {
	ginkgo.GinkgoHelper()

	authScriptedDriverOnce.Do(func() {
		sql.Register(authScriptedDriverName, authScriptedDriver{})
	})

	authScriptedScriptsMu.Lock()
	authScriptedScriptSeq++
	dsn := fmt.Sprintf("script-%d", authScriptedScriptSeq)
	authScriptedScripts[dsn] = script
	authScriptedScriptsMu.Unlock()

	db, err := sql.Open(authScriptedDriverName, dsn)
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(func() {
		_ = db.Close()
		authScriptedScriptsMu.Lock()
		delete(authScriptedScripts, dsn)
		authScriptedScriptsMu.Unlock()
	})
	return db
}

func fixtureAPIKey() *APIKey {
	ginkgo.GinkgoHelper()

	return &APIKey{
		ID:              newFixtureAPIKeyID("api-key-1"),
		UserID:          newFixtureUserID("user-1"),
		Name:            "automation",
		Prefix:          "lwk",
		Last4:           "1234",
		Scopes:          []string{"read", "write"},
		CreatedByUserID: newFixtureUserID("user-1"),
		CreatedAt:       time.Unix(1700000000, 0).UTC(),
	}
}

func fixtureUser() *User {
	ginkgo.GinkgoHelper()

	return &User{
		ID:       newFixtureUserID("user-1"),
		Username: "editor",
		Password: "password",
		Email:    "editor@example.com",
		Role:     RoleEditor,
	}
}

func newAPIKeyScannerWithScopes(scopes string) authFakeScanner {
	return authFakeScanner{values: []any{
		"api-key-1",
		"user-1",
		"automation",
		"lwk",
		"1234",
		scopes,
		"user-1",
		int64(1700000000),
		sql.NullInt64{},
		sql.NullInt64{},
	}}
}

func newStoredAPIKeyScannerWithScopes(scopes string) authFakeScanner {
	return authFakeScanner{values: []any{
		"api-key-1",
		"user-1",
		"automation",
		"secret-hash",
		"lwk",
		"1234",
		scopes,
		"user-1",
		int64(1700000000),
		sql.NullInt64{},
		sql.NullInt64{},
	}}
}

func apiKeyColumns() []string {
	return []string{
		"id",
		"user_id",
		"name",
		"prefix",
		"last4",
		"scopes",
		"created_by_user_id",
		"created_at",
		"last_used_at",
		"revoked_at",
	}
}

func userColumns() []string {
	return []string{"id", "username", "password", "email", "role"}
}

func userStoreReturningRows(rows *authScriptedRows) *UserStore {
	ginkgo.GinkgoHelper()

	return &UserStore{db: openAuthScriptedDB(&authScriptedDBScript{
		query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return cloneAuthScriptedRows(rows), nil
		},
	})}
}

func cloneAuthScriptedRows(rows *authScriptedRows) *authScriptedRows {
	ginkgo.GinkgoHelper()

	values := make([][]driver.Value, len(rows.values))
	for i := range rows.values {
		values[i] = append([]driver.Value(nil), rows.values[i]...)
	}
	return &authScriptedRows{
		columns:      append([]string(nil), rows.columns...),
		values:       values,
		nextErr:      rows.nextErr,
		errAfterRows: rows.errAfterRows,
		closeErr:     rows.closeErr,
	}
}

func userStoreWithUserAndExec(user *User, exec func(string, []driver.NamedValue) (driver.Result, error)) *UserStore {
	ginkgo.GinkgoHelper()

	return &UserStore{db: openAuthScriptedDB(&authScriptedDBScript{
		query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return &authScriptedRows{
				columns: userColumns(),
				values:  [][]driver.Value{{user.ID, user.Username, user.Password, user.Email, string(user.Role)}},
			}, nil
		},
		exec: exec,
	})}
}
