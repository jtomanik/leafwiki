package auth

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func setupTestUserService() *UserService {
	ginkgo.GinkgoHelper()
	store, err := NewUserStore(authTempDir())
	Expect(err).NotTo(HaveOccurred())
	return NewUserService(store)
}

var _ = ginkgo.Describe("user service", ginkgo.Label("integration"), func() {
	ginkgo.It("creates users with persisted profile and role fields", func() {
		service := setupTestUserService()

		user, err := service.CreateUser("alice", "alice@example.com", "secure", "admin")
		Expect(err).NotTo(HaveOccurred())
		Expect(user).To(SatisfyAll(
			HaveField("Username", Equal("alice")),
			HaveField("Email", Equal("alice@example.com")),
			HaveField("Role", Equal(RoleAdmin)),
		))
	})

	ginkgo.It("rejects duplicate usernames and emails", func() {
		service := setupTestUserService()

		_, _ = service.CreateUser("alice", "alice@example.com", "secure", "editor")

		_, err := service.CreateUser("alice", "alice2@example.com", "secure", "editor")
		Expect(err).To(MatchError(ErrUserAlreadyExists))

		_, err = service.CreateUser("bob", "alice@example.com", "secure", "editor")
		Expect(err).To(MatchError(ErrUserAlreadyExists))
	})

	ginkgo.It("rejects unsupported user roles", func() {
		service := setupTestUserService()

		_, err := service.CreateUser("bob", "bob@example.com", "secure", "guest")
		Expect(err).To(MatchError(ErrUserInvalidRole))
	})

	ginkgo.It("authenticates by username or email and rejects wrong passwords", func() {
		service := setupTestUserService()
		_, _ = service.CreateUser("alice", "alice@example.com", "mypassword", "editor")

		_, err := service.GetUserByEmailOrUsernameAndPassword("alice", "mypassword")
		Expect(err).NotTo(HaveOccurred())

		_, err = service.GetUserByEmailOrUsernameAndPassword("alice@example.com", "mypassword")
		Expect(err).NotTo(HaveOccurred())

		_, err = service.GetUserByEmailOrUsernameAndPassword("alice", "wrongpass")
		Expect(err).To(MatchError(ErrUserInvalidCredentials))
	})

	ginkgo.It("updates profile, password, and role fields", func() {
		service := setupTestUserService()

		user, _ := service.CreateUser("bob", "bob@example.com", "initial", "editor")
		userID := UserIDFromString(user.ID)

		updated, err := service.UpdateUser(userID, "bobnew", "bobnew@example.com", "newpass", "admin")
		Expect(err).NotTo(HaveOccurred())
		Expect(updated).To(SatisfyAll(
			HaveField("Username", Equal("bobnew")),
			HaveField("Email", Equal("bobnew@example.com")),
			HaveField("Role", Equal(RoleAdmin)),
		))
	})

	ginkgo.It("preserves the existing role when updates omit a role", func() {
		service := setupTestUserService()

		user, _ := service.CreateUser("bob", "bob@example.com", "initial", RoleEditor)
		userID := UserIDFromString(user.ID)

		updated, err := service.UpdateUser(userID, "bobnew", "bobnew@example.com", "", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(updated).To(SatisfyAll(
			HaveField("Username", Equal("bobnew")),
			HaveField("Email", Equal("bobnew@example.com")),
			HaveField("Role", Equal(RoleEditor)),
		))
	})
	ginkgo.It("prevents demoting the last admin", func() {
		service := setupTestUserService()

		admin, _ := service.CreateUser("admin", "admin@example.com", "pass", RoleAdmin)

		_, err := service.UpdateUser(UserIDFromString(admin.ID), admin.Username, admin.Email, "", RoleViewer)
		Expect(err).To(MatchError(ErrLastAdminCannotBeDemoted))
	})

	ginkgo.It("allows demoting an admin when another admin remains", func() {
		service := setupTestUserService()

		admin1, _ := service.CreateUser("admin1", "admin1@example.com", "pass", RoleAdmin)
		_, _ = service.CreateUser("admin2", "admin2@example.com", "pass", RoleAdmin)

		updated, err := service.UpdateUser(UserIDFromString(admin1.ID), admin1.Username, admin1.Email, "", RoleViewer)
		Expect(err).NotTo(HaveOccurred())
		Expect(updated.Role).To(Equal(RoleViewer))
	})

	ginkgo.It("protects admin accounts and deletes non-admin users", func() {
		service := setupTestUserService()

		admin, _ := service.CreateUser("admin", "admin@example.com", "secret", "admin")
		err := service.DeleteUser(UserIDFromString(admin.ID))
		Expect(err).To(MatchError(ErrUserAdminCannotBeDeleted))

		editor, _ := service.CreateUser("editor", "editor@example.com", "secret", "editor")
		err = service.DeleteUser(UserIDFromString(editor.ID))
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("initializes a default admin account", func() {
		store, _ := NewUserStore(authTempDir())
		service := NewUserService(store)

		err := service.InitDefaultAdmin("")
		Expect(err).NotTo(HaveOccurred())

		users, err := service.GetUsers()
		Expect(err).NotTo(HaveOccurred())
		Expect(users).To(ConsistOf(HaveField("Username", Equal(DefaultAdminUsername))))
	})

	ginkgo.It("resets an existing admin password and rejects the old password", func() {
		service := setupTestUserService()
		ginkgo.DeferCleanup(closeWithErrorCheck, service.Close)

		_, err := service.CreateUser("admin", "admin@example.com", "oldpassword", "admin")
		Expect(err).NotTo(HaveOccurred())

		adminUser, err := service.ResetAdminUserPassword()
		Expect(err).NotTo(HaveOccurred())
		Expect(adminUser).To(SatisfyAll(
			HaveField("Username", Equal(DefaultAdminUsername)),
			HaveField("Password", SatisfyAll(Not(BeEmpty()), Not(Equal("oldpassword")))),
		))

		_, err = service.GetUserByEmailOrUsernameAndPassword("admin", adminUser.Password)
		Expect(err).NotTo(HaveOccurred())

		_, err = service.GetUserByEmailOrUsernameAndPassword("admin", "oldpassword")
		Expect(err).To(MatchError(ErrUserInvalidCredentials))
	})

	ginkgo.It("creates a default admin when resetting without an existing admin", func() {
		service := setupTestUserService()
		ginkgo.DeferCleanup(closeWithErrorCheck, service.Close)

		adminUser, err := service.ResetAdminUserPassword()
		Expect(err).NotTo(HaveOccurred())
		Expect(adminUser).To(SatisfyAll(
			HaveField("Username", Equal(DefaultAdminUsername)),
			HaveField("Email", Equal(DefaultAdminEmail)),
			HaveField("Password", Not(BeEmpty())),
		))

		_, err = service.GetUserByEmailOrUsernameAndPassword("admin", adminUser.Password)
		Expect(err).NotTo(HaveOccurred())
	})
})
