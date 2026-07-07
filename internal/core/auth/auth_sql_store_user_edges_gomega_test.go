package auth

import (
	"database/sql"
	"database/sql/driver"
	"errors"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("auth SQL user store failure behavior", ginkgo.Label("unit"), func() {
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
			_, err = (&UserStore{}).GetUserByID(newFixtureUserID("user-1"))
			Expect(err).To(MatchError(openErr))
			_, err = (&UserStore{}).GetUserByUsername("editor")
			Expect(err).To(MatchError(openErr))
			_, err = (&UserStore{}).GetUserByEmail("editor@example.com")
			Expect(err).To(MatchError(openErr))
			Expect((&UserStore{}).UpdateUser(fixtureUser())).To(MatchError(openErr))
			Expect((&UserStore{}).DeleteUser(newFixtureUserID("user-1"))).To(MatchError(openErr))
			_, err = (&UserStore{}).GetAdminUser()
			Expect(err).To(MatchError(openErr))
			_, err = (&UserStore{}).GetAllUsers()
			Expect(err).To(MatchError(openErr))
			_, err = (&UserStore{}).CountAdminUsers()
			Expect(err).To(MatchError(openErr))
			_, err = (&UserStore{}).GetUserCount()
			Expect(err).To(MatchError(openErr))
			Expect((&UserStore{}).UpdatePassword(newFixtureUserID("user-1"), "password")).To(MatchError(openErr))
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

		ginkgo.It("hydrates and mutates user rows through scripted SQL contracts", func() {
			user := fixtureUser()
			openCalls := 0
			restoreOpen := setAuthSeam(&authSQLOpen, func(string, string) (*sql.DB, error) {
				openCalls++
				return openAuthScriptedDB(&authScriptedDBScript{}), nil
			})
			constructed, err := NewUserStore(authTempDir())
			Expect(err).To(Succeed())
			Expect(constructed.Connect()).To(Succeed())
			Expect(constructed.CreateUser(user)).To(Succeed())
			Expect(constructed.Close()).To(Succeed())
			Expect(openCalls).To(Equal(1))
			restoreOpen()

			userRows := &authScriptedRows{
				columns: userColumns(),
				values:  [][]driver.Value{{user.ID, user.Username, user.Password, user.Email, user.Role}},
			}
			store := userStoreReturningRows(userRows)

			byUsername, err := store.GetUserByUsername(user.Username)
			Expect(err).To(Succeed())
			Expect(byUsername).To(matchAuthUser(authUserContract{
				ID:       user.ID,
				Username: user.Username,
				Email:    user.Email,
				Role:     user.Role,
				Password: user.Password,
			}))
			byEmail, err := store.GetUserByEmail(user.Email)
			Expect(err).To(Succeed())
			Expect(byEmail).To(matchAuthUser(authUserContract{
				ID:       user.ID,
				Username: user.Username,
				Email:    user.Email,
				Role:     user.Role,
				Password: user.Password,
			}))
			adminRows := &authScriptedRows{
				columns: userColumns(),
				values:  [][]driver.Value{{user.ID, user.Username, user.Password, user.Email, RoleAdmin}},
			}
			store = userStoreReturningRows(adminRows)
			admin, err := store.GetAdminUser()
			Expect(err).To(Succeed())
			Expect(admin.Role).To(Equal(RoleAdmin))

			users, err := store.GetAllUsers()
			Expect(err).To(Succeed())
			Expect(users).To(ConsistOf(matchAuthUser(authUserContract{
				ID:       user.ID,
				Username: user.Username,
				Email:    user.Email,
				Role:     RoleAdmin,
				Password: user.Password,
			})))

			countStore := userStoreReturningRows(&authScriptedRows{
				columns: []string{"count"},
				values:  [][]driver.Value{{int64(2)}},
			})
			adminCount, err := countStore.CountAdminUsers()
			Expect(err).To(Succeed())
			Expect(adminCount).To(Equal(2))
			userCount, err := countStore.GetUserCount()
			Expect(err).To(Succeed())
			Expect(userCount).To(Equal(2))

			store = userStoreWithUserAndExec(user, func(string, []driver.NamedValue) (driver.Result, error) {
				return authScriptedResult{rowsAffected: 1}, nil
			})
			Expect(store.UpdateUser(user)).To(Succeed())
			Expect(store.DeleteUser(user.ID)).To(Succeed())
			Expect(store.UpdatePassword(user.ID, "new-password")).To(Succeed())

			demoteStore := userStoreWithUserAndExec(&User{
				ID:       user.ID,
				Username: user.Username,
				Password: user.Password,
				Email:    user.Email,
				Role:     RoleAdmin,
			}, func(string, []driver.NamedValue) (driver.Result, error) {
				return authScriptedResult{rowsAffected: 0}, nil
			})
			demoted := *user
			demoted.Role = RoleEditor
			Expect(demoteStore.UpdateUser(&demoted)).To(Equal(ErrLastAdminCannotBeDemoted))

			missingStore := userStoreReturningRows(&authScriptedRows{columns: userColumns()})
			_, err = missingStore.GetUserByUsername(user.Username)
			Expect(err).To(Equal(ErrUserNotFound))
			_, err = missingStore.GetUserByEmail(user.Email)
			Expect(err).To(Equal(ErrUserNotFound))
			_, err = missingStore.GetAdminUser()
			Expect(err).To(Equal(ErrUserNotFound))

			Expect((&UserStore{}).mapConstraintViolationToError(errors.New("UNIQUE constraint failed: users.username"))).
				To(Equal(ErrUserAlreadyExists))
			Expect((&UserStore{}).mapConstraintViolationToError(errors.New("UNIQUE constraint failed: users.email"))).
				To(Equal(ErrUserAlreadyExists))
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
			_, err := store.GetUserByID(user.ID)
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

			rowsErr := errors.New("users rows failed")
			store = userStoreReturningRows(&authScriptedRows{
				columns:      userColumns(),
				values:       [][]driver.Value{{user.ID, user.Username, user.Password, user.Email, user.Role}},
				errAfterRows: rowsErr,
			})
			_, err = store.GetAllUsers()
			Expect(err).To(MatchError(rowsErr))

			countRowErr := errors.New("user count row failed")
			store = userStoreReturningRows(&authScriptedRows{columns: []string{"count"}, nextErr: countRowErr})
			_, err = store.CountAdminUsers()
			Expect(err).To(MatchError(countRowErr))
			_, err = store.GetUserCount()
			Expect(err).To(MatchError(countRowErr))

			store = userStoreReturningRows(&authScriptedRows{columns: userColumns(), nextErr: userRowErr})
			Expect(store.UpdateUser(user)).To(MatchError(userRowErr))
			Expect(store.DeleteUser(user.ID)).To(MatchError(userRowErr))
			Expect(store.UpdatePassword(user.ID, "new-password")).To(MatchError(userRowErr))

			store = userStoreReturningRows(&authScriptedRows{columns: userColumns()})
			Expect(store.UpdateUser(user)).To(Equal(ErrUserNotFound))
			Expect(store.DeleteUser(user.ID)).To(Equal(ErrUserNotFound))

			rowsAffectedErr := errors.New("user rows affected failed")
			store = userStoreWithUserAndExec(user, func(string, []driver.NamedValue) (driver.Result, error) {
				return authScriptedResult{rowsAffectedErr: rowsAffectedErr}, nil
			})
			Expect(store.UpdateUser(user)).To(MatchError(rowsAffectedErr))

			updateErr := errors.New("user update failed")
			store = userStoreWithUserAndExec(user, func(string, []driver.NamedValue) (driver.Result, error) {
				return nil, updateErr
			})
			Expect(store.UpdateUser(user)).To(MatchError(updateErr))

			deleteErr := errors.New("delete failed")
			store = userStoreWithUserAndExec(user, func(string, []driver.NamedValue) (driver.Result, error) {
				return nil, deleteErr
			})
			Expect(store.DeleteUser(user.ID)).To(MatchError(deleteErr))

			passwordErr := errors.New("password update failed")
			store = userStoreWithUserAndExec(user, func(string, []driver.NamedValue) (driver.Result, error) {
				return nil, passwordErr
			})
			Expect(store.UpdatePassword(user.ID, "new-password")).To(MatchError(passwordErr))

			store = userStoreReturningRows(&authScriptedRows{columns: userColumns()})
			Expect(store.UpdatePassword(newFixtureUserID("missing"), "new-password")).To(Equal(ErrUserNotFound))
		})
	})
})
