package auth

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("auth service user unit seam contracts", ginkgo.Label("unit"), func() {
	ginkgo.Describe("user service seams", func() {
		ginkgo.It("creates users after validating uniqueness role hash identity and storage", func() {
			service := NewUserService(&UserStore{})
			setAuthSeam(&authUserStoreGetUserByUsername, func(*UserStore, string) (*User, error) {
				return nil, ErrUserNotFound
			})
			setAuthSeam(&authUserStoreGetUserByEmail, func(*UserStore, string) (*User, error) {
				return nil, ErrUserNotFound
			})
			setAuthSeam(&authGeneratePasswordHash, func([]byte, int) ([]byte, error) {
				return []byte("hashed-password"), nil
			})
			setAuthSeam(&authGenerateUniqueID, func() (string, error) {
				return newFixtureGeneratedUniqueID("created-user"), nil
			})
			var stored *User
			setAuthSeam(&authUserStoreCreateUser, func(_ *UserStore, user *User) error {
				stored = user
				return nil
			})

			user, err := service.CreateUser("editor", "editor@example.test", "secret", RoleEditor)

			Expect(err).To(Succeed())
			Expect(user).To(matchAuthUser(authUserContract{
				ID:       newFixtureUserID("created-user"),
				Username: "editor",
				Email:    "editor@example.test",
				Role:     RoleEditor,
				Password: "hashed-password",
			}))
			Expect(stored).To(matchAuthUser(authUserContract{
				ID:       newFixtureUserID("created-user"),
				Username: "editor",
				Email:    "editor@example.test",
				Role:     RoleEditor,
				Password: "hashed-password",
			}))

			setAuthSeam(&authUserStoreGetUserByUsername, func(*UserStore, string) (*User, error) {
				return &User{ID: newFixtureUserID("existing-user")}, nil
			})
			_, err = service.CreateUser("editor", "editor@example.test", "secret", RoleEditor)
			Expect(err).To(Equal(ErrUserAlreadyExists))

			setAuthSeam(&authUserStoreGetUserByUsername, func(*UserStore, string) (*User, error) {
				return nil, ErrUserNotFound
			})
			setAuthSeam(&authUserStoreGetUserByEmail, func(*UserStore, string) (*User, error) {
				return &User{ID: newFixtureUserID("existing-user")}, nil
			})
			_, err = service.CreateUser("editor", "editor@example.test", "secret", RoleEditor)
			Expect(err).To(Equal(ErrUserAlreadyExists))

			setAuthSeam(&authUserStoreGetUserByEmail, func(*UserStore, string) (*User, error) {
				return nil, ErrUserNotFound
			})
			Expect(struct{}{}).To(matchRoleAcceptance(RoleAdmin, roleAccepted))
			Expect(struct{}{}).To(matchRoleAcceptance("owner", roleRejected))
			_, err = service.CreateUser("editor", "editor@example.test", "secret", "owner")
			Expect(err).To(Equal(ErrUserInvalidRole))

			setAuthSeam(&authGeneratePasswordHash, func([]byte, int) ([]byte, error) {
				return nil, errAuthUnitHashFailed
			})
			_, err = service.CreateUser("editor", "editor@example.test", "secret", RoleEditor)
			Expect(err).To(MatchError(errAuthUnitHashFailed))

			setAuthSeam(&authGeneratePasswordHash, func([]byte, int) ([]byte, error) {
				return []byte("hashed-password"), nil
			})
			setAuthSeam(&authGenerateUniqueID, func() (string, error) {
				return "", errAuthUnitRandomFailed
			})
			_, err = service.CreateUser("editor", "editor@example.test", "secret", RoleEditor)
			Expect(err).To(MatchError(errAuthUnitRandomFailed))

			setAuthSeam(&authGenerateUniqueID, func() (string, error) {
				return newFixtureGeneratedUniqueID("created-user"), nil
			})
			setAuthSeam(&authUserStoreCreateUser, func(*UserStore, *User) error {
				return errAuthUnitStoreFailed
			})
			_, err = service.CreateUser("editor", "editor@example.test", "secret", RoleEditor)
			Expect(err).To(MatchError(errAuthUnitStoreFailed))
		})

		ginkgo.It("updates users while preserving admin and password contracts", func() {
			service := NewUserService(&UserStore{})
			existing := &User{
				ID:       newFixtureUserID("existing-user"),
				Username: "owner",
				Email:    "owner@example.test",
				Password: "old-hash",
				Role:     RoleAdmin,
			}
			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return existing, nil
			})
			setAuthSeam(&authUserStoreGetUserByUsername, func(*UserStore, string) (*User, error) {
				return existing, nil
			})
			setAuthSeam(&authUserStoreGetUserByEmail, func(*UserStore, string) (*User, error) {
				return existing, nil
			})
			setAuthSeam(&authUserStoreCountAdminUsers, func(*UserStore) (int, error) {
				return 2, nil
			})
			setAuthSeam(&authGeneratePasswordHash, func([]byte, int) ([]byte, error) {
				return []byte("new-hash"), nil
			})
			setAuthSeam(&authUserStoreUpdateUser, func(*UserStore, *User) error {
				return nil
			})

			updated, err := service.UpdateUser(existing.ID, "owner", "owner@example.test", "", "")
			Expect(err).To(Succeed())
			Expect(updated).To(matchAuthUser(authUserContract{
				ID:       newFixtureUserID("existing-user"),
				Username: "owner",
				Email:    "owner@example.test",
				Role:     RoleAdmin,
				Password: "old-hash",
			}))

			updated, err = service.UpdateUser(existing.ID, "owner", "owner@example.test", "new-secret", RoleEditor)
			Expect(err).To(Succeed())
			Expect(updated).To(matchAuthUser(authUserContract{
				ID:       newFixtureUserID("existing-user"),
				Username: "owner",
				Email:    "owner@example.test",
				Role:     RoleEditor,
				Password: "new-hash",
			}))

			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return nil, ErrUserNotFound
			})
			_, err = service.UpdateUser(existing.ID, "owner", "owner@example.test", "", RoleEditor)
			Expect(err).To(Equal(ErrUserNotFound))

			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return existing, nil
			})
			setAuthSeam(&authUserStoreGetUserByUsername, func(*UserStore, string) (*User, error) {
				return &User{ID: newFixtureUserID("other-user")}, nil
			})
			_, err = service.UpdateUser(existing.ID, "taken", "owner@example.test", "", RoleEditor)
			Expect(err).To(Equal(ErrUserAlreadyExists))

			setAuthSeam(&authUserStoreGetUserByUsername, func(*UserStore, string) (*User, error) {
				return existing, nil
			})
			setAuthSeam(&authUserStoreGetUserByEmail, func(*UserStore, string) (*User, error) {
				return &User{ID: newFixtureUserID("other-user")}, nil
			})
			_, err = service.UpdateUser(existing.ID, "owner", "taken@example.test", "", RoleEditor)
			Expect(err).To(Equal(ErrUserAlreadyExists))

			setAuthSeam(&authUserStoreGetUserByEmail, func(*UserStore, string) (*User, error) {
				return existing, nil
			})
			_, err = service.UpdateUser(existing.ID, "owner", "owner@example.test", "", "owner")
			Expect(err).To(Equal(ErrUserInvalidRole))

			existing.Role = RoleAdmin
			setAuthSeam(&authUserStoreCountAdminUsers, func(*UserStore) (int, error) {
				return 0, errAuthUnitStoreFailed
			})
			_, err = service.UpdateUser(existing.ID, "owner", "owner@example.test", "", RoleEditor)
			Expect(err).To(MatchError(errAuthUnitStoreFailed))

			setAuthSeam(&authUserStoreCountAdminUsers, func(*UserStore) (int, error) {
				return 1, nil
			})
			_, err = service.UpdateUser(existing.ID, "owner", "owner@example.test", "", RoleEditor)
			Expect(err).To(Equal(ErrLastAdminCannotBeDemoted))

			setAuthSeam(&authUserStoreCountAdminUsers, func(*UserStore) (int, error) {
				return 2, nil
			})
			setAuthSeam(&authGeneratePasswordHash, func([]byte, int) ([]byte, error) {
				return nil, errAuthUnitHashFailed
			})
			_, err = service.UpdateUser(existing.ID, "owner", "owner@example.test", "new-secret", RoleAdmin)
			Expect(err).To(MatchError(errAuthUnitHashFailed))

			setAuthSeam(&authGeneratePasswordHash, func([]byte, int) ([]byte, error) {
				return []byte("new-hash"), nil
			})
			setAuthSeam(&authUserStoreUpdateUser, func(*UserStore, *User) error {
				return errAuthUnitStoreFailed
			})
			_, err = service.UpdateUser(existing.ID, "owner", "owner@example.test", "", RoleAdmin)
			Expect(err).To(MatchError(errAuthUnitStoreFailed))
		})

		ginkgo.It("manages password lookup delete and default-admin service contracts", func() {
			service := NewUserService(&UserStore{})
			admin := &User{
				ID:       newFixtureUserID("admin-user"),
				Username: DefaultAdminUsername,
				Email:    DefaultAdminEmail,
				Password: authUnitPasswordHash("old-secret"),
				Role:     RoleAdmin,
			}
			editor := &User{
				ID:       newFixtureUserID("editor-user"),
				Username: "editor",
				Email:    "editor@example.test",
				Password: authUnitPasswordHash("old-secret"),
				Role:     RoleEditor,
			}
			setAuthSeam(&authUserStoreGetAdminUser, func(*UserStore) (*User, error) {
				return admin, nil
			})
			setAuthSeam(&authUserStoreGetUserByID, func(_ *UserStore, id UserID) (*User, error) {
				switch id {
				case admin.ID:
					return admin, nil
				case editor.ID:
					return editor, nil
				default:
					return nil, ErrUserNotFound
				}
			})
			setAuthSeam(&authUserStoreGetUserByUsername, func(*UserStore, string) (*User, error) {
				return editor, nil
			})
			setAuthSeam(&authUserStoreGetUserByEmail, func(*UserStore, string) (*User, error) {
				return editor, nil
			})
			setAuthSeam(&authGeneratePasswordHash, func([]byte, int) ([]byte, error) {
				return []byte("new-hash"), nil
			})
			setAuthSeam(&authGenerateRandomPassword, func(int) (string, error) {
				return "generated-secret", nil
			})
			var updatedPassword string
			setAuthSeam(&authUserStoreUpdatePassword, func(_ *UserStore, _ UserID, password string) error {
				updatedPassword = password
				return nil
			})
			var deletedID UserID
			setAuthSeam(&authUserStoreDeleteUser, func(_ *UserStore, id UserID) error {
				deletedID = id
				return nil
			})
			setAuthSeam(&authUserStoreGetAllUsers, func(*UserStore) ([]*User, error) {
				return []*User{admin, editor}, nil
			})

			Expect(service.InitDefaultAdmin("unused-secret")).To(Succeed())
			Expect(service.UpdatePassword(editor.ID, "new-secret")).To(Succeed())
			Expect(updatedPassword).To(Equal("new-hash"))

			matches, err := service.DoesIDAndPasswordMatch(editor.ID, "old-secret")
			Expect(acceptedAuthPassword(matches, err)).To(Succeed())
			Expect(rejectedAuthPassword(service.DoesIDAndPasswordMatch(newFixtureUserID("missing-user"), "old-secret"))).To(Equal(ErrUserNotFound))
			Expect(rejectedAuthPassword(service.DoesIDAndPasswordMatch(editor.ID, "wrong-secret"))).To(Equal(ErrUserInvalidCredentials))

			Expect(service.DeleteUser(admin.ID)).To(Equal(ErrUserAdminCannotBeDeleted))
			Expect(service.DeleteUser(editor.ID)).To(Succeed())
			Expect(deletedID).To(Equal(editor.ID))

			users, err := service.GetUsers()
			Expect(err).To(Succeed())
			Expect(users).To(ConsistOf(admin, editor))
			byUsername, err := service.GetUserByUsername("editor")
			Expect(err).To(Succeed())
			Expect(byUsername).To(Equal(editor))
			byIdentifier, err := service.GetUserByEmailOrUsernameAndPassword("editor", "old-secret")
			Expect(err).To(Succeed())
			Expect(byIdentifier).To(Equal(editor))
			_, err = service.GetUserByEmailOrUsernameAndPassword("editor", "wrong-secret")
			Expect(err).To(Equal(ErrUserInvalidCredentials))

			Expect(service.ChangeOwnPassword(editor.ID, "old-secret", "new-secret")).To(Succeed())
			_, err = service.ResetAdminUserPassword()
			Expect(err).To(Succeed())
			Expect(admin.Password).To(Equal("generated-secret"))
			Expect(service.Close()).To(Succeed())
		})

		ginkgo.It("reports password lookup delete reset and default-admin failures", func() {
			service := NewUserService(&UserStore{})
			editor := &User{
				ID:       newFixtureUserID("editor-user"),
				Username: "editor",
				Email:    "editor@example.test",
				Password: authUnitPasswordHash("old-secret"),
				Role:     RoleEditor,
			}
			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return nil, ErrUserNotFound
			})
			Expect(service.UpdatePassword(editor.ID, "new-secret")).To(Equal(ErrUserNotFound))
			Expect(service.DeleteUser(editor.ID)).To(Equal(ErrUserNotFound))
			Expect(service.ChangeOwnPassword(editor.ID, "old-secret", "new-secret")).To(Equal(ErrUserNotFound))

			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return editor, nil
			})
			setAuthSeam(&authGeneratePasswordHash, func([]byte, int) ([]byte, error) {
				return nil, errAuthUnitHashFailed
			})
			Expect(service.UpdatePassword(editor.ID, "new-secret")).To(MatchError(errAuthUnitHashFailed))
			Expect(service.ChangeOwnPassword(editor.ID, "old-secret", "new-secret")).To(MatchError(errAuthUnitHashFailed))
			setAuthSeam(&authGeneratePasswordHash, func([]byte, int) ([]byte, error) {
				return []byte("new-hash"), nil
			})
			err := service.ChangeOwnPassword(editor.ID, "wrong-secret", "new-secret")
			Expect(err).To(Equal(ErrUserInvalidCredentials))

			setAuthSeam(&authUserStoreUpdatePassword, func(*UserStore, UserID, string) error {
				return errAuthUnitStoreFailed
			})
			Expect(service.UpdatePassword(editor.ID, "new-secret")).To(MatchError(errAuthUnitStoreFailed))
			Expect(service.ChangeOwnPassword(editor.ID, "old-secret", "new-secret")).To(MatchError(errAuthUnitStoreFailed))

			setAuthSeam(&authUserStoreGetUserByUsername, func(*UserStore, string) (*User, error) {
				return nil, ErrUserNotFound
			})
			setAuthSeam(&authUserStoreGetUserByEmail, func(*UserStore, string) (*User, error) {
				return nil, ErrUserNotFound
			})
			_, err = service.GetUserByUsername("missing")
			Expect(err).To(Equal(ErrUserNotFound))
			_, err = service.GetUserByEmailOrUsernameAndPassword("missing", "secret")
			Expect(err).To(Equal(ErrUserNotFound))

			setAuthSeam(&authGenerateRandomPassword, func(int) (string, error) {
				return "", errAuthUnitRandomFailed
			})
			_, err = service.ResetAdminUserPassword()
			Expect(err).To(matchAuthErrorCause(errAuthUnitRandomFailed))

			setAuthSeam(&authGenerateRandomPassword, func(int) (string, error) {
				return "generated-secret", nil
			})
			setAuthSeam(&authUserStoreGetAdminUser, func(*UserStore) (*User, error) {
				return nil, errAuthUnitLookupFailed
			})
			_, err = service.ResetAdminUserPassword()
			Expect(err).To(MatchError(errAuthUnitLookupFailed))

			setAuthSeam(&authUserStoreGetAdminUser, func(*UserStore) (*User, error) {
				return nil, ErrUserNotFound
			})
			setAuthSeam(&authUserStoreGetUserByUsername, func(*UserStore, string) (*User, error) {
				return nil, ErrUserNotFound
			})
			setAuthSeam(&authUserStoreGetUserByEmail, func(*UserStore, string) (*User, error) {
				return nil, ErrUserNotFound
			})
			setAuthSeam(&authUserStoreCreateUser, func(*UserStore, *User) error {
				return errAuthUnitStoreFailed
			})
			Expect(service.InitDefaultAdmin("new-secret")).To(matchAuthErrorCause(errAuthUnitStoreFailed))
			_, err = service.ResetAdminUserPassword()
			Expect(err).To(matchAuthErrorCause(errAuthUnitStoreFailed))

			setAuthSeam(&authUserStoreCreateUser, func(*UserStore, *User) error {
				return nil
			})
			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return editor, nil
			})
			setAuthSeam(&authUserStoreUpdatePassword, func(*UserStore, UserID, string) error {
				return errAuthUnitStoreFailed
			})
			_, err = service.ResetAdminUserPassword()
			Expect(err).To(matchAuthErrorCause(errAuthUnitStoreFailed))
		})

		ginkgo.It("creates missing default admins and propagates delete storage errors", func() {
			service := NewUserService(&UserStore{})
			editor := &User{ID: newFixtureUserID("editor-user"), Role: RoleEditor}
			setAuthSeam(&authUserStoreGetAdminUser, func(*UserStore) (*User, error) {
				return nil, ErrUserNotFound
			})
			setAuthSeam(&authUserStoreGetUserByUsername, func(*UserStore, string) (*User, error) {
				return nil, ErrUserNotFound
			})
			setAuthSeam(&authUserStoreGetUserByEmail, func(*UserStore, string) (*User, error) {
				return nil, ErrUserNotFound
			})
			setAuthSeam(&authGeneratePasswordHash, func([]byte, int) ([]byte, error) {
				return []byte("admin-hash"), nil
			})
			setAuthSeam(&authGenerateUniqueID, func() (string, error) {
				return newFixtureGeneratedUniqueID("admin-user"), nil
			})
			setAuthSeam(&authUserStoreCreateUser, func(*UserStore, *User) error {
				return nil
			})

			Expect(service.InitDefaultAdmin("new-secret")).To(Succeed())

			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return editor, nil
			})
			setAuthSeam(&authUserStoreDeleteUser, func(*UserStore, UserID) error {
				return errAuthUnitStoreFailed
			})
			Expect(service.DeleteUser(editor.ID)).To(MatchError(errAuthUnitStoreFailed))
		})
	})
})
