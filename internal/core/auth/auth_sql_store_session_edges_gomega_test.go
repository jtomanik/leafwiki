package auth

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("auth SQL session and resolver failure behavior", ginkgo.Label("unit"), func() {
	ginkgo.Describe("session store", func() {
		ginkgo.It("creates revokes checks and cleans session rows through the scripted store", func() {
			restoreOpen := setAuthSeam(&authSQLOpen, func(string, string) (*sql.DB, error) {
				return openAuthScriptedDB(&authScriptedDBScript{}), nil
			})
			store, err := NewSessionStore(authTempDir())
			Expect(err).To(Succeed())
			Expect(store.Close()).To(Succeed())
			restoreOpen()

			store = &SessionStore{db: openAuthScriptedDB(&authScriptedDBScript{})}
			Expect(store.CreateSession(
				newFixtureSessionID("session-1"),
				newFixtureUserID("user-1"),
				"refresh",
				time.Now().Add(time.Hour),
			)).To(Succeed())
			Expect(store.RevokeSession(newFixtureSessionID("session-1"))).To(Succeed())
			Expect(store.RevokeAllSessionsForUser(newFixtureUserID("user-1"))).To(Succeed())
			Expect(store.CleanupExpiredSessions()).To(Succeed())

			activeStore := &SessionStore{db: openAuthScriptedDB(&authScriptedDBScript{
				query: func(string, []driver.NamedValue) (driver.Rows, error) {
					return &authScriptedRows{
						columns: []string{"expires_at", "revoked_at"},
						values:  [][]driver.Value{{time.Now().Add(time.Hour).Unix(), nil}},
					}, nil
				},
			})}
			Expect(activeAuthSession(activeStore.IsActive(
				newFixtureSessionID("session-1"),
				newFixtureUserID("user-1"),
				"refresh",
				time.Now(),
			))).To(Succeed())

			revokedStore := &SessionStore{db: openAuthScriptedDB(&authScriptedDBScript{
				query: func(string, []driver.NamedValue) (driver.Rows, error) {
					return &authScriptedRows{
						columns: []string{"expires_at", "revoked_at"},
						values:  [][]driver.Value{{time.Now().Add(time.Hour).Unix(), time.Now().Unix()}},
					}, nil
				},
			})}
			Expect(inactiveAuthSession(revokedStore.IsActive(
				newFixtureSessionID("session-1"),
				newFixtureUserID("user-1"),
				"refresh",
				time.Now(),
			))).To(Succeed())

			expiredStore := &SessionStore{db: openAuthScriptedDB(&authScriptedDBScript{
				query: func(string, []driver.NamedValue) (driver.Rows, error) {
					return &authScriptedRows{
						columns: []string{"expires_at", "revoked_at"},
						values:  [][]driver.Value{{time.Now().Add(-time.Hour).Unix(), nil}},
					}, nil
				},
			})}
			Expect(inactiveAuthSession(expiredStore.IsActive(
				newFixtureSessionID("session-1"),
				newFixtureUserID("user-1"),
				"refresh",
				time.Now(),
			))).To(Succeed())

			missingStore := &SessionStore{db: openAuthScriptedDB(&authScriptedDBScript{
				query: func(string, []driver.NamedValue) (driver.Rows, error) {
					return &authScriptedRows{columns: []string{"expires_at", "revoked_at"}}, nil
				},
			})}
			Expect(inactiveAuthSession(missingStore.IsActive(
				newFixtureSessionID("missing-session"),
				newFixtureUserID("user-1"),
				"refresh",
				time.Now(),
			))).To(Succeed())
		})

		ginkgo.It("reports session database open close and active-check failures", func() {
			openErr := errors.New("session open failed")
			restoreOpen := setAuthSeam(&authSQLOpen, func(string, string) (*sql.DB, error) {
				return nil, openErr
			})
			store := &SessionStore{}
			Expect(store.withDB(func(*sql.DB) error { return nil })).To(MatchError(openErr))
			restoreOpen()

			schemaErr := errors.New("session schema failed")
			sessionCloseCalls := 0
			restoreOpen = setAuthSeam(&authSQLOpen, func(string, string) (*sql.DB, error) {
				return openAuthScriptedDB(&authScriptedDBScript{
					exec: func(string, []driver.NamedValue) (driver.Result, error) {
						return nil, schemaErr
					},
					close: func() error {
						sessionCloseCalls++
						return nil
					},
				}), nil
			})
			_, err := NewSessionStore(authTempDir())
			Expect(err).To(MatchError(schemaErr))
			Expect(sessionCloseCalls).To(Equal(1))
			restoreOpen()

			cleanupCalls := 0
			cleanupFailures := 0
			restoreInterval := setAuthSeam(&authSessionCleanupInterval, time.Millisecond)
			restoreOpen = setAuthSeam(&authSQLOpen, func(string, string) (*sql.DB, error) {
				return openAuthScriptedDB(&authScriptedDBScript{
					exec: func(string, []driver.NamedValue) (driver.Result, error) {
						cleanupCalls++
						if cleanupCalls > 1 && cleanupFailures == 0 {
							cleanupFailures++
							return nil, errors.New("session cleanup failed")
						}
						return authScriptedResult{rowsAffected: 1}, nil
					},
				}), nil
			})
			store, err = NewSessionStore(authTempDir())
			Expect(err).To(Succeed())
			Eventually(func() int { return cleanupFailures }).Should(Equal(1))
			Expect(store.Close()).To(Succeed())
			restoreOpen()
			restoreInterval()

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
			Expect(failedAuthSessionCheck(store.IsActive(newFixtureSessionID("session-1"), newFixtureUserID("user-1"), "refresh", time.Now()))).To(MatchError(sessionRowErr))
		})
	})

	ginkgo.Describe("user resolver", func() {
		ginkgo.It("preloads resolves caches and reloads user labels", func() {
			service := NewUserService(&UserStore{})
			userID := newFixtureUserID("user-1")
			editor := &User{ID: userID, Username: "editor"}
			viewer := &User{ID: newFixtureUserID("user-2"), Username: "viewer"}
			setAuthSeam(&authUserStoreGetAllUsers, func(*UserStore) ([]*User, error) {
				return []*User{editor}, nil
			})
			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return viewer, nil
			})

			resolver, err := NewUserResolver(service)
			Expect(err).To(Succeed())
			label, err := resolver.ResolveUserLabel(userID)
			Expect(err).To(Succeed())
			Expect(label).To(matchAuthUserLabel(authUserLabelContract{ID: userID, Username: "editor"}))
			emptyLabel, err := resolver.ResolveUserLabel(newFixtureUserID(""))
			Expect(err).To(Succeed())
			Expect(emptyLabel).To(BeNil())

			label, err = resolver.ResolveUserLabel(viewer.ID)
			Expect(err).To(Succeed())
			Expect(label).To(matchAuthUserLabel(authUserLabelContract{ID: viewer.ID, Username: "viewer"}))

			reloaded := &User{ID: userID, Username: "renamed"}
			setAuthSeam(&authUserStoreGetAllUsers, func(*UserStore) ([]*User, error) {
				return []*User{reloaded}, nil
			})
			Expect(resolver.Reload()).To(Succeed())
			label, err = resolver.ResolveUserLabel(userID)
			Expect(err).To(Succeed())
			Expect(label).To(matchAuthUserLabel(authUserLabelContract{ID: userID, Username: "renamed"}))
		})

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

			userID := newFixtureUserID("user-1")
			existing := &UserLabel{ID: userID, Username: "cached"}
			resolver = &UserResolver{userService: service, resolved: map[UserID]*UserLabel{}}
			restoreGet := setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				resolver.mu.Lock()
				resolver.resolved[userID] = existing
				resolver.mu.Unlock()
				return &User{ID: userID, Username: "fresh"}, nil
			})
			label, err := resolver.ResolveUserLabel(userID)
			Expect(err).To(Succeed())
			Expect(label).To(BeIdenticalTo(existing))
			restoreGet()

			resolveErr := errors.New("resolve user failed")
			restoreGet = setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return nil, resolveErr
			})
			resolver = &UserResolver{userService: service, resolved: map[UserID]*UserLabel{}}
			_, err = resolver.ResolveUserLabel(userID)
			Expect(err).To(MatchError(resolveErr))
			restoreGet()
		})
	})
})
