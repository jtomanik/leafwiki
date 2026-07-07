package auth

import (
	"os"
	"path/filepath"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

func setupTestUserStore() *UserStore {
	ginkgo.GinkgoHelper()

	userStore, err := NewUserStore(authTempDir())
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(closeWithErrorCheck, userStore.Close)
	return userStore
}

func matchStoredUser(user *User) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("ID", user.ID),
		HaveField("Username", user.Username),
		HaveField("Email", user.Email),
		HaveField("Role", user.Role),
		HaveField("Password", user.Password),
	)
}

var _ = ginkgo.Describe("user store", func() {
	ginkgo.It("normalizes Windows storage paths under the user database file", ginkgo.Label("unit"), func() {
		got := strings.ReplaceAll(databasePath(`C:\wiki\data`, "users.db"), `\`, `/`)

		Expect(got).To(Equal(`C:/wiki/data/users.db`))
	})

	ginkgo.It("creates the user database inside the configured storage directory", ginkgo.Label("integration"), func() {
		storageDir := authTempDir()
		userStore, err := NewUserStore(storageDir)
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheck, userStore.Close)

		Expect(os.Stat(filepath.Join(storageDir, "users.db"))).Error().To(Succeed())
	})

	ginkgo.It("persists and retrieves user records by ID", ginkgo.Label("integration"), func() {
		store := setupTestUserStore()
		user := &User{
			ID:       newFixtureUserID("1"),
			Username: "testuser",
			Password: "password",
			Email:    "user1@example.com",
			Role:     RoleAdmin,
		}

		Expect(store.CreateUser(user)).To(Succeed())

		retrievedUser, err := store.GetUserByID(UserIDFromString(user.ID))
		Expect(err).To(Succeed())
		Expect(retrievedUser).To(matchStoredUser(user))
	})

	ginkgo.It("rejects duplicate email addresses", ginkgo.Label("integration"), func() {
		store := setupTestUserStore()
		user1 := &User{
			ID:       newFixtureUserID("1"),
			Username: "testuser1",
			Password: "password1",
			Email:    "testuser1@example.com",
			Role:     RoleAdmin,
		}
		user2 := &User{
			ID:       newFixtureUserID("2"),
			Username: "testuser2",
			Password: "password2",
			Email:    user1.Email,
			Role:     RoleAdmin,
		}

		Expect(store.CreateUser(user1)).To(Succeed())

		Expect(store.CreateUser(user2)).To(MatchError(ErrUserAlreadyExists))
	})

	ginkgo.It("rejects duplicate usernames", ginkgo.Label("integration"), func() {
		store := setupTestUserStore()
		user1 := &User{
			ID:       newFixtureUserID("1"),
			Username: "testuser1",
			Password: "password1",
			Email:    "testuser1@example.com",
			Role:     RoleAdmin,
		}
		user2 := &User{
			ID:       newFixtureUserID("2"),
			Username: user1.Username,
			Password: "password2",
			Email:    "testuser2@example.com",
			Role:     RoleAdmin,
		}

		Expect(store.CreateUser(user1)).To(Succeed())

		Expect(store.CreateUser(user2)).To(MatchError(ErrUserAlreadyExists))
	})

	ginkgo.It("returns a not-found error for missing IDs while preserving existing users", ginkgo.Label("integration"), func() {
		store := setupTestUserStore()
		user := &User{
			ID:       newFixtureUserID("1"),
			Username: "testuser",
			Password: "password",
			Email:    "testuser@example.com",
			Role:     RoleAdmin,
		}
		Expect(store.CreateUser(user)).To(Succeed())

		_, err := store.GetUserByID(newFixtureUserID("non-existing-id"))
		Expect(err).To(MatchError(ErrUserNotFound))

		retrievedUser, err := store.GetUserByID(UserIDFromString(user.ID))
		Expect(err).To(Succeed())
		Expect(retrievedUser).To(matchStoredUser(user))
	})

	ginkgo.It("updates stored profile and password fields", ginkgo.Label("integration"), func() {
		store := setupTestUserStore()
		user := &User{
			ID:       newFixtureUserID("1"),
			Username: "testuser",
			Password: "password",
			Email:    "testuser1@example.com",
			Role:     RoleAdmin,
		}
		Expect(store.CreateUser(user)).To(Succeed())

		user.Username = "updateduser"
		user.Password = "newpassword"
		Expect(store.UpdateUser(user)).To(Succeed())

		retrievedUser, err := store.GetUserByID(UserIDFromString(user.ID))
		Expect(err).To(Succeed())
		Expect(retrievedUser).To(matchStoredUser(user))
		Expect(store.UpdateUser(&User{
			ID:       newFixtureUserID("non-existing-id"),
			Username: "nonexistinguser",
			Password: "nonexistingpassword",
			Email:    "nonexisting@example.com",
			Role:     RoleViewer,
		})).To(MatchError(ErrUserNotFound))
	})

	ginkgo.It("protects the final administrator from demotion", ginkgo.Label("integration"), func() {
		store := setupTestUserStore()
		admin := &User{
			ID:       newFixtureUserID("1"),
			Username: "admin",
			Password: "password",
			Email:    "admin@example.com",
			Role:     RoleAdmin,
		}
		Expect(store.CreateUser(admin)).To(Succeed())

		admin.Role = RoleViewer
		Expect(store.UpdateUser(admin)).To(MatchError(ErrLastAdminCannotBeDemoted))
	})

	ginkgo.It("rejects profile updates that would reuse another user's email", ginkgo.Label("integration"), func() {
		store := setupTestUserStore()
		user1 := &User{
			ID:       newFixtureUserID("1"),
			Username: "testuser1",
			Password: "password1",
			Email:    "testuser1@example.com",
			Role:     RoleAdmin,
		}
		user2 := &User{
			ID:       newFixtureUserID("2"),
			Username: "testuser2",
			Password: "password2",
			Email:    "testuser2@example.com",
			Role:     RoleAdmin,
		}
		Expect(store.CreateUser(user1)).To(Succeed())
		Expect(store.CreateUser(user2)).To(Succeed())

		updateUser := &User{
			ID:       user2.ID,
			Username: user2.Username,
			Password: user2.Password,
			Email:    user1.Email,
			Role:     user2.Role,
		}

		Expect(store.UpdateUser(updateUser)).To(MatchError(ErrUserAlreadyExists))
	})

	ginkgo.It("rejects profile updates that would reuse another user's username", ginkgo.Label("integration"), func() {
		store := setupTestUserStore()
		user1 := &User{
			ID:       newFixtureUserID("1"),
			Username: "testuser1",
			Password: "password1",
			Email:    "testuser1@example.com",
			Role:     RoleAdmin,
		}
		user2 := &User{
			ID:       newFixtureUserID("2"),
			Username: "testuser2",
			Password: "password2",
			Email:    "testuser2@example.com",
			Role:     RoleAdmin,
		}
		Expect(store.CreateUser(user1)).To(Succeed())
		Expect(store.CreateUser(user2)).To(Succeed())

		updateUser := &User{
			ID:       user2.ID,
			Username: user1.Username,
			Password: user2.Password,
			Email:    user2.Email,
			Role:     user2.Role,
		}

		Expect(store.UpdateUser(updateUser)).To(MatchError(ErrUserAlreadyExists))
	})

	ginkgo.It("deletes existing users and reports missing users as not found", ginkgo.Label("integration"), func() {
		store := setupTestUserStore()
		user := &User{
			ID:       newFixtureUserID("1"),
			Username: "testuser",
			Password: "password",
			Email:    "testuser@example.com",
			Role:     RoleAdmin,
		}

		Expect(store.CreateUser(user)).To(Succeed())
		users, err := store.GetAllUsers()
		Expect(err).To(Succeed())
		initialCount := len(users)

		Expect(store.DeleteUser(UserIDFromString(user.ID))).To(Succeed())

		_, err = store.GetUserByID(UserIDFromString(user.ID))
		Expect(err).To(MatchError(ErrUserNotFound))
		users, err = store.GetAllUsers()
		Expect(err).To(Succeed())
		Expect(users).To(HaveLen(initialCount - 1))
		Expect(store.DeleteUser(newFixtureUserID("non-existing-id"))).To(MatchError(ErrUserNotFound))
	})

	ginkgo.It("lists all persisted users", ginkgo.Label("integration"), func() {
		store := setupTestUserStore()
		user1 := &User{
			ID:       newFixtureUserID("1"),
			Username: "testuser1",
			Password: "password1",
			Email:    "testuser1@example.com",
			Role:     RoleAdmin,
		}
		user2 := &User{
			ID:       newFixtureUserID("2"),
			Username: "testuser2",
			Password: "password2",
			Email:    "testuser2@example.com",
			Role:     RoleAdmin,
		}

		Expect(store.CreateUser(user1)).To(Succeed())
		Expect(store.CreateUser(user2)).To(Succeed())

		users, err := store.GetAllUsers()
		Expect(err).To(Succeed())
		Expect(users).To(ConsistOf(matchStoredUser(user1), matchStoredUser(user2)))
	})

	ginkgo.It("counts persisted users", ginkgo.Label("integration"), func() {
		store := setupTestUserStore()
		user1 := &User{
			ID:       newFixtureUserID("1"),
			Username: "testuser1",
			Password: "password1",
			Email:    "testuser1@example.com",
			Role:     RoleAdmin,
		}
		user2 := &User{
			ID:       newFixtureUserID("2"),
			Username: "testuser2",
			Password: "password2",
			Email:    "testuser2@example.com",
			Role:     RoleAdmin,
		}

		Expect(store.CreateUser(user1)).To(Succeed())
		Expect(store.CreateUser(user2)).To(Succeed())

		count, err := store.GetUserCount()
		Expect(err).To(Succeed())
		Expect(count).To(Equal(2))
	})

	ginkgo.It("retrieves users by email address", ginkgo.Label("integration"), func() {
		store := setupTestUserStore()
		user1 := &User{
			ID:       newFixtureUserID("1"),
			Username: "testuser1",
			Password: "password1",
			Email:    "testuser1@example.com",
			Role:     RoleAdmin,
		}
		user2 := &User{
			ID:       newFixtureUserID("2"),
			Username: "testuser2",
			Password: "password2",
			Email:    "testuser2@example.com",
			Role:     RoleAdmin,
		}

		Expect(store.CreateUser(user1)).To(Succeed())
		Expect(store.CreateUser(user2)).To(Succeed())

		retrievedUser, err := store.GetUserByEmail(user1.Email)
		Expect(err).To(Succeed())
		Expect(retrievedUser).To(matchStoredUser(user1))
	})

	ginkgo.It("retrieves users by username", ginkgo.Label("integration"), func() {
		store := setupTestUserStore()
		user1 := &User{
			ID:       newFixtureUserID("1"),
			Username: "testuser1",
			Password: "password1",
			Email:    "testuser1@example.com",
			Role:     RoleAdmin,
		}
		user2 := &User{
			ID:       newFixtureUserID("2"),
			Username: "testuser2",
			Password: "password2",
			Email:    "testuser2@example.com",
			Role:     RoleAdmin,
		}

		Expect(store.CreateUser(user1)).To(Succeed())
		Expect(store.CreateUser(user2)).To(Succeed())

		retrievedUser, err := store.GetUserByUsername(user1.Username)
		Expect(err).To(Succeed())
		Expect(retrievedUser).To(matchStoredUser(user1))
	})

	ginkgo.It("updates a user's password by ID", ginkgo.Label("integration"), func() {
		store := setupTestUserStore()
		user := &User{
			ID:       newFixtureUserID("1"),
			Username: "testuser1",
			Password: "password1",
			Email:    "testuser1@example.com",
			Role:     RoleAdmin,
		}

		Expect(store.CreateUser(user)).To(Succeed())
		Expect(store.UpdatePassword(UserIDFromString(user.ID), "newpassword")).To(Succeed())

		retrievedUser, err := store.GetUserByID(UserIDFromString(user.ID))
		Expect(err).To(Succeed())
		Expect(retrievedUser).To(HaveField("Password", "newpassword"))
	})
})
