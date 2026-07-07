package auth

import (
	"database/sql"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("auth session and resolver behavior", func() {
	ginkgo.It("removes expired refresh sessions while leaving active sessions usable", ginkgo.Label("integration"), func() {
		store, err := NewSessionStore(authTempDir())
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(closeWithErrorCheck, store.Close)

		userID := newFixtureUserID("cleanup-user")
		expiredID := newFixtureSessionID("expired-session")
		activeID := newFixtureSessionID("active-session")
		Expect(store.CreateSession(expiredID, userID, "refresh", time.Now().Add(-time.Hour))).To(Succeed())
		Expect(store.CreateSession(activeID, userID, "refresh", time.Now().Add(time.Hour))).To(Succeed())

		Expect(store.CleanupExpiredSessions()).To(Succeed())

		Expect(inactiveAuthSession(store.IsActive(expiredID, userID, "refresh", time.Now()))).To(Succeed())
		Expect(activeAuthSession(store.IsActive(activeID, userID, "refresh", time.Now()))).To(Succeed())

		var expiredRows int
		Expect(store.withDB(func(db *sql.DB) error {
			return db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ?`, expiredID).Scan(&expiredRows)
		})).To(Succeed())
		Expect(expiredRows).To(BeZero())
	})

	ginkgo.It("UserResolver preloads, lazily resolves, handles empty IDs, and reloads changed labels", ginkgo.Label("integration"), func() {
		service := setupTestUserService()
		ginkgo.DeferCleanup(closeWithErrorCheck, service.Close)

		alice, err := service.CreateUser("alice", "alice@example.com", "alicepass", RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		resolver, err := NewUserResolver(service)
		Expect(err).NotTo(HaveOccurred())

		empty, err := resolver.ResolveUserLabel(newFixtureUserID(""))
		Expect(err).NotTo(HaveOccurred())
		Expect(empty).To(BeNil())

		aliceLabel, err := resolver.ResolveUserLabel(UserIDFromString(alice.ID))
		Expect(err).NotTo(HaveOccurred())
		Expect(aliceLabel).To(Equal(&UserLabel{ID: alice.ID, Username: "alice"}))

		bob, err := service.CreateUser("bob", "bob@example.com", "bobpass", RoleViewer)
		Expect(err).NotTo(HaveOccurred())
		bobLabel, err := resolver.ResolveUserLabel(UserIDFromString(bob.ID))
		Expect(err).NotTo(HaveOccurred())
		Expect(bobLabel).To(Equal(&UserLabel{ID: bob.ID, Username: "bob"}))

		_, err = service.UpdateUser(UserIDFromString(alice.ID), "alice-renamed", "alice@example.com", "", RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		cachedLabel, err := resolver.ResolveUserLabel(UserIDFromString(alice.ID))
		Expect(err).NotTo(HaveOccurred())
		Expect(cachedLabel.Username).To(Equal("alice"))

		Expect(resolver.Reload()).To(Succeed())
		reloadedLabel, err := resolver.ResolveUserLabel(UserIDFromString(alice.ID))
		Expect(err).NotTo(HaveOccurred())
		Expect(reloadedLabel.Username).To(Equal("alice-renamed"))
	})

	ginkgo.It("accepts matching passwords and rejects wrong or missing-user credentials", ginkgo.Label("integration"), func() {
		service := setupTestUserService()
		ginkgo.DeferCleanup(closeWithErrorCheck, service.Close)

		user, err := service.CreateUser("charlie", "charlie@example.com", "correct-password", RoleEditor)
		Expect(err).NotTo(HaveOccurred())

		Expect(acceptedAuthPassword(service.DoesIDAndPasswordMatch(UserIDFromString(user.ID), "correct-password"))).To(Succeed())

		Expect(rejectedAuthPassword(service.DoesIDAndPasswordMatch(UserIDFromString(user.ID), "wrong-password"))).To(Equal(ErrUserInvalidCredentials))

		Expect(rejectedAuthPassword(service.DoesIDAndPasswordMatch(newFixtureUserID("missing-user"), "correct-password"))).To(Equal(ErrUserNotFound))
	})

	ginkgo.It("finds users by username or email and reports missing identifiers", ginkgo.Label("integration"), func() {
		service := setupTestUserService()
		ginkgo.DeferCleanup(closeWithErrorCheck, service.Close)

		user, err := service.CreateUser("dana", "dana@example.com", "password", RoleViewer)
		Expect(err).NotTo(HaveOccurred())

		byUsername, err := service.GetUserByUsername("dana")
		Expect(err).NotTo(HaveOccurred())
		Expect(byUsername.ID).To(Equal(user.ID))

		byIdentifierUsername, err := service.GetUserByIdentifier("dana")
		Expect(err).NotTo(HaveOccurred())
		Expect(byIdentifierUsername.ID).To(Equal(user.ID))

		byIdentifierEmail, err := service.GetUserByIdentifier("dana@example.com")
		Expect(err).NotTo(HaveOccurred())
		Expect(byIdentifierEmail.ID).To(Equal(user.ID))

		_, err = service.GetUserByUsername("missing")
		Expect(err).To(Equal(ErrUserNotFound))
		_, err = service.GetUserByIdentifier("missing@example.com")
		Expect(err).To(Equal(ErrUserNotFound))
	})

	ginkgo.It("requires the current password before replacing stored credentials", ginkgo.Label("integration"), func() {
		service := setupTestUserService()
		ginkgo.DeferCleanup(closeWithErrorCheck, service.Close)

		user, err := service.CreateUser("erin", "erin@example.com", "old-password", RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		userID := UserIDFromString(user.ID)

		Expect(service.ChangeOwnPassword(userID, "wrong-password", "new-password")).To(Equal(ErrUserInvalidCredentials))
		_, err = service.GetUserByEmailOrUsernameAndPassword("erin", "old-password")
		Expect(err).NotTo(HaveOccurred())

		Expect(service.ChangeOwnPassword(userID, "old-password", "new-password")).To(Succeed())
		_, err = service.GetUserByEmailOrUsernameAndPassword("erin", "old-password")
		Expect(err).To(Equal(ErrUserInvalidCredentials))
		_, err = service.GetUserByEmailOrUsernameAndPassword("erin", "new-password")
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("projects public users without exposing sensitive credentials", ginkgo.Label("unit"), func() {
		user := &User{
			ID:       newFixtureUserID("user-1"),
			Username: "frank",
			Email:    "frank@example.com",
			Password: "secret",
			Role:     RoleAdmin,
		}

		public := user.ToPublicUser()
		Expect(public).To(Equal(&PublicUser{
			ID:       newFixtureUserID("user-1"),
			Username: "frank",
			Email:    "frank@example.com",
			Role:     RoleAdmin,
		}))
	})

	ginkgo.It("accepts admin editor and viewer roles and rejects unsupported roles before storing users", ginkgo.Label("integration"), func() {
		service := setupTestUserService()
		ginkgo.DeferCleanup(closeWithErrorCheck, service.Close)

		for _, role := range []string{RoleAdmin, RoleEditor, RoleViewer} {
			user, err := service.CreateUser("frank-"+role, "frank-"+role+"@example.com", "password", role)
			Expect(err).NotTo(HaveOccurred())
			Expect(user.ToPublicUser()).To(Equal(&PublicUser{
				ID:       user.ID,
				Username: "frank-" + role,
				Email:    "frank-" + role + "@example.com",
				Role:     role,
			}))
		}

		_, err := service.CreateUser("owner", "owner@example.com", "password", "owner")
		Expect(err).To(Equal(ErrUserInvalidRole))
	})
})
