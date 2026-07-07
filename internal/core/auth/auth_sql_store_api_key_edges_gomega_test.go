package auth

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("auth SQL API key store failure behavior", ginkgo.Label("unit"), func() {
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

			store = &APIKeyStore{db: openAuthScriptedDB(&authScriptedDBScript{
				query: func(string, []driver.NamedValue) (driver.Rows, error) {
					return &authScriptedRows{
						columns: apiKeyColumns(),
						values:  [][]driver.Value{{nil}},
					}, nil
				},
			})}
			_, err = store.ListActiveAPIKeys(key.UserID)
			Expect(err).NotTo(Succeed())

			storedColumns := append([]string(nil), append(apiKeyColumns()[:3], append([]string{"secret_hash"}, apiKeyColumns()[3:]...)...)...)
			store = &APIKeyStore{db: openAuthScriptedDB(&authScriptedDBScript{
				query: func(string, []driver.NamedValue) (driver.Rows, error) {
					return &authScriptedRows{
						columns: storedColumns,
						values:  [][]driver.Value{{nil}},
					}, nil
				},
			})}
			_, err = store.GetAPIKeyByID(key.ID)
			Expect(err).NotTo(Succeed())

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

			store = &APIKeyStore{db: openAuthScriptedDB(&authScriptedDBScript{
				exec: func(string, []driver.NamedValue) (driver.Result, error) {
					return authScriptedResult{rowsAffected: 0}, nil
				},
			})}
			Expect(store.RevokeAPIKey(key.UserID, key.ID, time.Now())).To(Equal(ErrAPIKeyNotFound))
			Expect(store.MarkAPIKeyUsed(key.ID, time.Now())).To(Equal(ErrAPIKeyNotFound))

			markErr := errors.New("api key mark used failed")
			store = &APIKeyStore{db: openAuthScriptedDB(&authScriptedDBScript{
				exec: func(string, []driver.NamedValue) (driver.Result, error) {
					return nil, markErr
				},
			})}
			Expect(store.MarkAPIKeyUsed(key.ID, time.Now())).To(MatchError(markErr))
		})

		ginkgo.It("hydrates listed and stored API key timestamps from SQL rows", func() {
			key := fixtureAPIKey()
			openCalls := 0
			restoreOpen := setAuthSeam(&authSQLOpen, func(string, string) (*sql.DB, error) {
				openCalls++
				return openAuthScriptedDB(&authScriptedDBScript{}), nil
			})
			constructed, err := NewAPIKeyStore(authTempDir())
			Expect(err).To(Succeed())
			Expect(constructed.Connect()).To(Succeed())
			Expect(constructed.CreateAPIKey(key, "secret-hash")).To(Succeed())
			Expect(constructed.RevokeAPIKey(key.UserID, key.ID, time.Now())).To(Succeed())
			Expect(constructed.MarkAPIKeyUsed(key.ID, time.Now())).To(Succeed())
			Expect(constructed.Close()).To(Succeed())
			Expect(openCalls).To(Equal(1))
			restoreOpen()

			lastUsedAt := time.Unix(1700000100, 0).UTC()
			revokedAt := time.Unix(1700000200, 0).UTC()
			keyRows := &authScriptedRows{
				columns: apiKeyColumns(),
				values: [][]driver.Value{{
					"api-key-1",
					"user-1",
					"automation",
					"lwk",
					"1234",
					`["read","write"]`,
					"user-1",
					int64(1700000000),
					lastUsedAt.Unix(),
					nil,
				}},
			}
			store := &APIKeyStore{db: openAuthScriptedDB(&authScriptedDBScript{
				query: func(string, []driver.NamedValue) (driver.Rows, error) {
					return cloneAuthScriptedRows(keyRows), nil
				},
			})}
			keys, err := store.ListActiveAPIKeys(newFixtureUserID("user-1"))
			Expect(err).To(Succeed())
			Expect(keys).To(ConsistOf(matchAuthAPIKeyRow(authAPIKeyRowContract{
				ID:         newFixtureAPIKeyID("api-key-1"),
				UserID:     newFixtureUserID("user-1"),
				Scopes:     []string{"read", "write"},
				LastUsedAt: lastUsedAt,
			})))

			storedRows := &authScriptedRows{
				columns: append([]string(nil), append(apiKeyColumns()[:3], append([]string{"secret_hash"}, apiKeyColumns()[3:]...)...)...),
				values: [][]driver.Value{{
					"api-key-1",
					"user-1",
					"automation",
					"secret-hash",
					"lwk",
					"1234",
					`["read","write"]`,
					"user-1",
					int64(1700000000),
					lastUsedAt.Unix(),
					revokedAt.Unix(),
				}},
			}
			store = &APIKeyStore{db: openAuthScriptedDB(&authScriptedDBScript{
				query: func(string, []driver.NamedValue) (driver.Rows, error) {
					return cloneAuthScriptedRows(storedRows), nil
				},
			})}
			stored, err := store.GetAPIKeyByID(newFixtureAPIKeyID("api-key-1"))
			Expect(err).To(Succeed())
			Expect(stored).To(matchAuthStoredAPIKey(authStoredAPIKeyContract{
				SecretHash: "secret-hash",
				Key: authAPIKeyRowContract{
					ID:         newFixtureAPIKeyID("api-key-1"),
					UserID:     newFixtureUserID("user-1"),
					Scopes:     []string{"read", "write"},
					LastUsedAt: lastUsedAt,
					RevokedAt:  revokedAt,
				},
			}))

			missingStore := &APIKeyStore{db: openAuthScriptedDB(&authScriptedDBScript{
				query: func(string, []driver.NamedValue) (driver.Rows, error) {
					return &authScriptedRows{columns: storedRows.columns}, nil
				},
			})}
			_, err = missingStore.GetAPIKeyByID(newFixtureAPIKeyID("missing-key"))
			Expect(err).To(Equal(ErrAPIKeyNotFound))
		})
	})
})
