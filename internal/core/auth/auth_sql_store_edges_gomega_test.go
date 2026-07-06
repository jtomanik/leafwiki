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

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
	columns  []string
	values   [][]driver.Value
	nextErr  error
	closeErr error
	index    int
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

var _ = ginkgo.Describe("auth SQL store failure behavior", ginkgo.Label("integration"), func() {
	ginkgo.Describe("API key store", func() {
		ginkgo.It("reports API key store connection construction close and scanner failures", func() {
			openErr := errors.New("api key open failed")
			restoreOpen := setAuthSeam(&authSQLOpen, func(string, string) (*sql.DB, error) {
				return nil, openErr
			})
			_, err := NewAPIKeyStore(authTempDir())
			Expect(err).To(MatchError(openErr))
			Expect((&APIKeyStore{}).Connect()).To(MatchError(openErr))
			restoreOpen()

			schemaErr := errors.New("api key schema failed")
			closeCalls := 0
			restoreOpen = setAuthSeam(&authSQLOpen, func(string, string) (*sql.DB, error) {
				return openAuthScriptedDB(&authScriptedDBScript{
					exec: func(string, []driver.NamedValue) (driver.Result, error) {
						return nil, schemaErr
					},
					close: func() error {
						closeCalls++
						return nil
					},
				}), nil
			})
			_, err = NewAPIKeyStore(authTempDir())
			Expect(err).To(MatchError(schemaErr))
			Expect(closeCalls).To(Equal(1))
			restoreOpen()

			Expect((&APIKeyStore{}).Close()).To(Succeed())

			apiKeyCloseErr := errors.New("api key close failed")
			store := &APIKeyStore{db: openAuthScriptedDB(&authScriptedDBScript{
				close: func() error {
					return apiKeyCloseErr
				},
			})}
			Expect(store.db.Ping()).To(Succeed())
			Expect(store.Close()).To(MatchError(apiKeyCloseErr))

			_, err = scanAPIKey(authFakeScanner{err: errAPIKeyScanFailed})
			Expect(err).To(MatchError(errAPIKeyScanFailed))
			_, err = scanAPIKey(newAPIKeyScannerWithScopes("{not-json"))
			Expect(err).To(matchAuthJSONSyntaxError())
			_, _, err = scanStoredAPIKey(newStoredAPIKeyScannerWithScopes("{not-json"))
			Expect(err).To(matchAuthJSONSyntaxError())
		})

		ginkgo.It("reports API key store method SQL failures", func() {
			key := fixtureAPIKey()

			openErr := errors.New("api key method open failed")
			restoreOpen := setAuthSeam(&authSQLOpen, func(string, string) (*sql.DB, error) {
				return nil, openErr
			})
			Expect((&APIKeyStore{}).CreateAPIKey(key, "hash")).To(MatchError(openErr))
			_, err := (&APIKeyStore{}).ListActiveAPIKeys(key.UserID)
			Expect(err).To(MatchError(openErr))
			_, err = (&APIKeyStore{}).GetAPIKeyByID(key.ID)
			Expect(err).To(MatchError(openErr))
			Expect((&APIKeyStore{}).RevokeAPIKey(key.UserID, key.ID, time.Now())).To(MatchError(openErr))
			Expect((&APIKeyStore{}).MarkAPIKeyUsed(key.ID, time.Now())).To(MatchError(openErr))
			restoreOpen()

			createErr := errors.New("api key insert failed")
			store := &APIKeyStore{db: openAuthScriptedDB(&authScriptedDBScript{
				exec: func(string, []driver.NamedValue) (driver.Result, error) {
					return nil, createErr
				},
			})}
			Expect(store.CreateAPIKey(key, "hash")).To(MatchError(createErr))

			queryErr := errors.New("api key query failed")
			store = &APIKeyStore{db: openAuthScriptedDB(&authScriptedDBScript{
				query: func(string, []driver.NamedValue) (driver.Rows, error) {
					return nil, queryErr
				},
			})}
			_, err = store.ListActiveAPIKeys(key.UserID)
			Expect(err).To(MatchError(queryErr))

			restoreCloseRows := setAuthSeam(&authCloseRows, func(interface{ Close() error }) error {
				return errors.New("api key rows close failed")
			})
			store = &APIKeyStore{db: openAuthScriptedDB(&authScriptedDBScript{
				query: func(string, []driver.NamedValue) (driver.Rows, error) {
					return &authScriptedRows{
						columns: apiKeyColumns(),
					}, nil
				},
			})}
			Expect(store.ListActiveAPIKeys(key.UserID)).To(BeEmpty())
			restoreCloseRows()

			apiKeyRowErr := errors.New("api key row failed")
			store = &APIKeyStore{db: openAuthScriptedDB(&authScriptedDBScript{
				query: func(string, []driver.NamedValue) (driver.Rows, error) {
					return &authScriptedRows{
						columns: apiKeyColumns(),
						nextErr: apiKeyRowErr,
					}, nil
				},
			})}
			_, err = store.ListActiveAPIKeys(key.UserID)
			Expect(err).To(MatchError(apiKeyRowErr))

			rowsErr := errors.New("api key rows failed")
			store = &APIKeyStore{db: openAuthScriptedDB(&authScriptedDBScript{
				query: func(string, []driver.NamedValue) (driver.Rows, error) {
					return &authScriptedRows{columns: apiKeyColumns(), nextErr: rowsErr}, nil
				},
			})}
			_, err = store.ListActiveAPIKeys(key.UserID)
			Expect(err).To(MatchError(rowsErr))

			revokeErr := errors.New("api key revoke failed")
			store = &APIKeyStore{db: openAuthScriptedDB(&authScriptedDBScript{
				exec: func(string, []driver.NamedValue) (driver.Result, error) {
					return nil, revokeErr
				},
			})}
			Expect(store.RevokeAPIKey(key.UserID, key.ID, time.Now())).To(MatchError(revokeErr))

			rowsAffectedErr := errors.New("api key rows affected failed")
			store = &APIKeyStore{db: openAuthScriptedDB(&authScriptedDBScript{
				exec: func(string, []driver.NamedValue) (driver.Result, error) {
					return authScriptedResult{rowsAffectedErr: rowsAffectedErr}, nil
				},
			})}
			Expect(store.RevokeAPIKey(key.UserID, key.ID, time.Now())).To(MatchError(rowsAffectedErr))
			Expect(store.MarkAPIKeyUsed(key.ID, time.Now())).To(MatchError(rowsAffectedErr))
		})
	})

	ginkgo.Describe("session store", func() {
		ginkgo.It("reports session database open close and active-check failures", func() {
			openErr := errors.New("session open failed")
			restoreOpen := setAuthSeam(&authSQLOpen, func(string, string) (*sql.DB, error) {
				return nil, openErr
			})
			store := &SessionStore{}
			Expect(store.withDB(func(*sql.DB) error { return nil })).To(MatchError(openErr))
			restoreOpen()

			sessionCloseErr := errors.New("session close failed")
			store = &SessionStore{
				db:     openAuthScriptedDB(&authScriptedDBScript{close: func() error { return sessionCloseErr }}),
				cancel: func() {},
				done:   make(chan struct{}),
			}
			close(store.done)
			Expect(store.db.Ping()).To(Succeed())
			Expect(store.Close()).To(MatchError(sessionCloseErr))

			sessionRowErr := errors.New("session active row failed")
			store = &SessionStore{
				db: openAuthScriptedDB(&authScriptedDBScript{
					query: func(string, []driver.NamedValue) (driver.Rows, error) {
						return &authScriptedRows{
							columns: []string{"expires_at", "revoked_at"},
							nextErr: sessionRowErr,
						}, nil
					},
				}),
			}
			Expect(failedAuthSessionCheck(store.IsActive(newFixtureSessionID("session-1"), UserIDFromString("user-1"), "refresh", time.Now()))).To(MatchError(sessionRowErr))
		})
	})

	ginkgo.Describe("user resolver", func() {
		ginkgo.It("reports user resolver preload reload and cache-race failures", func() {
			service := NewUserService(&UserStore{})
			listErr := errors.New("list users failed")
			restoreUsers := setAuthSeam(&authUserStoreGetAllUsers, func(*UserStore) ([]*User, error) {
				return nil, listErr
			})
			_, err := NewUserResolver(service)
			Expect(err).To(MatchError(listErr))
			resolver := &UserResolver{userService: service, resolved: map[UserID]*UserLabel{}}
			Expect(resolver.Reload()).To(MatchError(listErr))
			restoreUsers()

			userID := UserIDFromString("user-1")
			existing := &UserLabel{ID: userID, Username: "cached"}
			resolver = &UserResolver{userService: service, resolved: map[UserID]*UserLabel{}}
			restoreGet := setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				resolver.mu.Lock()
				resolver.resolved[userID] = existing
				resolver.mu.Unlock()
				return &User{ID: userID, Username: "fresh"}, nil
			})
			label, err := resolver.ResolveUserLabel(userID)
			Expect(err).NotTo(HaveOccurred())
			Expect(label).To(BeIdenticalTo(existing))
			restoreGet()
		})
	})

	ginkgo.Describe("user store", func() {
		ginkgo.It("reports user store connection schema close and connect failures", func() {
			openErr := errors.New("user open failed")
			restoreOpen := setAuthSeam(&authSQLOpen, func(string, string) (*sql.DB, error) {
				return nil, openErr
			})
			_, err := NewUserStore(authTempDir())
			Expect(err).To(MatchError(openErr))
			Expect((&UserStore{}).Connect()).To(MatchError(openErr))
			Expect((&UserStore{}).ensureSchema()).To(MatchError(openErr))
			Expect((&UserStore{}).CreateUser(fixtureUser())).To(MatchError(openErr))
			_, err = (&UserStore{}).GetUserByID(UserIDFromString("user-1"))
			Expect(err).To(MatchError(openErr))
			_, err = (&UserStore{}).GetUserByUsername("editor")
			Expect(err).To(MatchError(openErr))
			_, err = (&UserStore{}).GetUserByEmail("editor@example.com")
			Expect(err).To(MatchError(openErr))
			Expect((&UserStore{}).UpdateUser(fixtureUser())).To(MatchError(openErr))
			Expect((&UserStore{}).DeleteUser(UserIDFromString("user-1"))).To(MatchError(openErr))
			_, err = (&UserStore{}).GetAdminUser()
			Expect(err).To(MatchError(openErr))
			_, err = (&UserStore{}).GetAllUsers()
			Expect(err).To(MatchError(openErr))
			_, err = (&UserStore{}).CountAdminUsers()
			Expect(err).To(MatchError(openErr))
			_, err = (&UserStore{}).GetUserCount()
			Expect(err).To(MatchError(openErr))
			Expect((&UserStore{}).UpdatePassword(UserIDFromString("user-1"), "password")).To(MatchError(openErr))
			restoreOpen()

			schemaErr := errors.New("user schema failed")
			store := &UserStore{db: openAuthScriptedDB(&authScriptedDBScript{
				exec: func(string, []driver.NamedValue) (driver.Result, error) {
					return nil, schemaErr
				},
			})}
			Expect(store.ensureSchema()).To(MatchError(schemaErr))

			userCloseErr := errors.New("user close failed")
			store = &UserStore{db: openAuthScriptedDB(&authScriptedDBScript{
				close: func() error {
					return userCloseErr
				},
			})}
			Expect(store.db.Ping()).To(Succeed())
			Expect(store.Close()).To(MatchError(userCloseErr))

			Expect((&UserStore{}).mapConstraintViolationToError(errPlainConstraint)).To(MatchError(errPlainConstraint))
		})

		ginkgo.It("reports user store query scan result and exec failures", func() {
			user := fixtureUser()

			insertErr := errors.New("user insert failed")
			store := &UserStore{db: openAuthScriptedDB(&authScriptedDBScript{
				exec: func(string, []driver.NamedValue) (driver.Result, error) {
					return nil, insertErr
				},
			})}
			Expect(store.CreateUser(user)).To(MatchError(insertErr))

			userRowErr := errors.New("user row failed")
			store = userStoreReturningRows(&authScriptedRows{columns: userColumns(), nextErr: userRowErr})
			_, err := store.GetUserByID(UserIDFromString(user.ID))
			Expect(err).To(MatchError(userRowErr))
			_, err = store.GetUserByUsername(user.Username)
			Expect(err).To(MatchError(userRowErr))
			_, err = store.GetUserByEmail(user.Email)
			Expect(err).To(MatchError(userRowErr))
			_, err = store.GetAdminUser()
			Expect(err).To(MatchError(userRowErr))

			queryErr := errors.New("users query failed")
			store = &UserStore{db: openAuthScriptedDB(&authScriptedDBScript{
				query: func(string, []driver.NamedValue) (driver.Rows, error) {
					return nil, queryErr
				},
			})}
			_, err = store.GetAllUsers()
			Expect(err).To(MatchError(queryErr))

			restoreCloseRows := setAuthSeam(&authCloseRows, func(interface{ Close() error }) error {
				return errors.New("users rows close failed")
			})
			store = userStoreReturningRows(&authScriptedRows{columns: userColumns()})
			Expect(store.GetAllUsers()).To(BeEmpty())
			restoreCloseRows()

			store = userStoreReturningRows(&authScriptedRows{columns: userColumns(), values: [][]driver.Value{{nil}}})
			users, err := store.GetAllUsers()
			Expect(users).To(BeNil())
			Expect(err).To(MatchError(ErrUserStoreInvalidRow))

			countRowErr := errors.New("user count row failed")
			store = userStoreReturningRows(&authScriptedRows{columns: []string{"count"}, nextErr: countRowErr})
			_, err = store.CountAdminUsers()
			Expect(err).To(MatchError(countRowErr))
			_, err = store.GetUserCount()
			Expect(err).To(MatchError(countRowErr))

			store = userStoreReturningRows(&authScriptedRows{columns: userColumns(), nextErr: userRowErr})
			Expect(store.UpdateUser(user)).To(MatchError(userRowErr))
			Expect(store.DeleteUser(UserIDFromString(user.ID))).To(MatchError(userRowErr))
			Expect(store.UpdatePassword(UserIDFromString(user.ID), "new-password")).To(MatchError(userRowErr))

			rowsAffectedErr := errors.New("user rows affected failed")
			store = userStoreWithUserAndExec(user, func(string, []driver.NamedValue) (driver.Result, error) {
				return authScriptedResult{rowsAffectedErr: rowsAffectedErr}, nil
			})
			Expect(store.UpdateUser(user)).To(MatchError(rowsAffectedErr))

			deleteErr := errors.New("delete failed")
			store = userStoreWithUserAndExec(user, func(string, []driver.NamedValue) (driver.Result, error) {
				return nil, deleteErr
			})
			Expect(store.DeleteUser(UserIDFromString(user.ID))).To(MatchError(deleteErr))

			passwordErr := errors.New("password update failed")
			store = userStoreWithUserAndExec(user, func(string, []driver.NamedValue) (driver.Result, error) {
				return nil, passwordErr
			})
			Expect(store.UpdatePassword(UserIDFromString(user.ID), "new-password")).To(MatchError(passwordErr))

			store = userStoreReturningRows(&authScriptedRows{columns: userColumns()})
			Expect(store.UpdatePassword(UserIDFromString("missing"), "new-password")).To(Equal(ErrUserNotFound))
		})
	})
})
