package auth

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func signAuthClaims(service *AuthService, claims jwt.MapClaims) string {
	ginkgo.GinkgoHelper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(service.secretKey)
	Expect(err).NotTo(HaveOccurred())
	return token
}

var _ = ginkgo.Describe("auth boundary behavior", func() {
	ginkgo.Describe("login attempts", ginkgo.Label("unit"), func() {
		ginkgo.It("locks an account after repeated failed attempts and resets after the lock expires", func() {
			tracker := newLoginAttemptTracker()
			userID := newFixtureUserID("lockable-user")

			for range loginMaxFailures {
				Expect(tracker.recordAttempt(userID)).To(BeTrue())
			}
			Expect(tracker.recordAttempt(userID)).To(BeFalse())

			tracker.mu.Lock()
			tracker.entries[userID].lockedUntil = time.Now().Add(-time.Second)
			tracker.mu.Unlock()

			Expect(tracker.recordAttempt(userID)).To(BeTrue())
		})
	})

	ginkgo.Describe("semantic auth types", ginkgo.Label("unit"), func() {
		ginkgo.It("parses semantic auth identifiers", func() {
			Expect(UserIDFromString("user-1")).To(Equal(newFixtureUserID("user-1")))
			Expect(UserIDFromString("user-2")).To(Equal(newFixtureUserID("user-2")))
			Expect(APIKeyIDFromString("key-1")).To(Equal(newFixtureAPIKeyID("key-1")))
			Expect(APIKeyIDFromString("key-2")).To(Equal(newFixtureAPIKeyID("key-2")))
			Expect(SessionIDFromString("session-1")).To(Equal(newFixtureSessionID("session-1")))
			Expect(SessionIDFromString("session-2")).To(Equal(newFixtureSessionID("session-2")))
		})
	})

	ginkgo.Describe("API key service validation", func() {
		ginkgo.It("handles nil services, bearer prefixes, and malformed raw keys", ginkgo.Label("unit"), func() {
			var nilService *APIKeyService
			Expect(nilService.Close()).To(Succeed())
			Expect((&APIKeyService{}).Close()).To(Succeed())

			_, err := nilService.CreateAPIKey(newFixtureUserID("user-1"), "key", newFixtureUserID("creator-1"))
			Expect(err).To(Equal(ErrAPIKeyNotFound))
			_, err = (&APIKeyService{}).ListAPIKeys(newFixtureUserID("user-1"))
			Expect(err).To(Equal(ErrAPIKeyNotFound))
			Expect((&APIKeyService{}).RevokeAPIKey(newFixtureUserID("user-1"), newFixtureAPIKeyID("key-1"))).To(Equal(ErrAPIKeyNotFound))
			_, err = (&APIKeyService{}).VerifyAPIKey("lwk_key_secret")
			Expect(err).To(Equal(ErrInvalidToken))

			Expect(IsAPIKeyBearer("lwk_key_secret")).To(BeTrue())
			Expect(IsAPIKeyBearer("Bearer lwk_key_secret")).To(BeFalse())

			id, err := parseAPIKeyID("lwk_key_secret")
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal(newFixtureAPIKeyID("key")))

			service := &APIKeyService{store: &APIKeyStore{}, users: &UserService{store: &UserStore{}}}
			for _, raw := range []string{"", "not-lwk", "lwk_", "lwk_key_", "lwk__secret"} {
				_, err := service.VerifyAPIKey(raw)
				Expect(err).To(MatchError(ErrInvalidToken))
			}
		})

		ginkgo.It("closes a backing store through the service", ginkgo.Label("integration"), func() {
			store, err := NewAPIKeyStore(authTempDir())
			Expect(err).NotTo(HaveOccurred())
			service := NewAPIKeyService(store, nil)

			Expect(service.Close()).To(Succeed())
			Expect(store.db).To(BeNil())
		})

		ginkgo.It("rejects empty names and missing referenced users before storing a key", ginkgo.Label("integration"), func() {
			_, _, _, service, user := setupTestAPIKeyService()
			userID := UserIDFromString(user.ID)

			_, err := service.CreateAPIKey(userID, "  \t  ", userID)
			Expect(err).To(Equal(ErrAPIKeyInvalidName))

			_, err = service.CreateAPIKey(newFixtureUserID("missing-user"), "automation", userID)
			Expect(err).To(Equal(ErrUserNotFound))

			_, err = service.CreateAPIKey(userID, "automation", newFixtureUserID("missing-creator"))
			Expect(err).To(Equal(ErrUserNotFound))

			_, err = service.ListAPIKeys(newFixtureUserID("missing-user"))
			Expect(err).To(Equal(ErrUserNotFound))
			Expect(service.RevokeAPIKey(newFixtureUserID("missing-user"), newFixtureAPIKeyID("key-1"))).To(Equal(ErrUserNotFound))
		})
	})

	ginkgo.Describe("JWT claim validation", ginkgo.Label("integration"), func() {
		var service *AuthService

		ginkgo.BeforeEach(func() {
			service = setupTestAuthService()
		})

		ginkgo.AfterEach(func() {
			Expect(service.sessionStore.Close()).To(Succeed())
			Expect(service.userService.Close()).To(Succeed())
		})

		ginkgo.It("locks repeated invalid logins and preserves short configured secrets", func() {
			shortSecretService := NewAuthService(nil, nil, "short", time.Minute, time.Hour)
			Expect(shortSecretService.secretKey).To(Equal([]byte("short")))

			_, err := service.Login("missing-user", "password")
			Expect(err).To(Equal(ErrUserInvalidCredentials))

			for range loginMaxFailures {
				_, err = service.Login("testuser", "wrong-password")
				Expect(err).To(Equal(ErrUserInvalidCredentials))
			}
			_, err = service.Login("testuser", "securepass")
			Expect(err).To(Equal(ErrUserAccountLocked))
		})

		ginkgo.It("rejects malformed tokens and unsupported signing methods", func() {
			_, err := service.RefreshToken("not-a-token")
			Expect(err).To(Equal(ErrInvalidToken))
			_, err = service.ValidateToken("not-a-token")
			Expect(err).To(Equal(ErrInvalidToken))

			noneSigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
				"sub": "user-1",
				"typ": "refresh",
				"jti": "token-1",
				"exp": time.Now().Add(time.Hour).Unix(),
			}).SignedString(jwt.UnsafeAllowNoneSignatureType)
			Expect(err).NotTo(HaveOccurred())

			_, err = service.parseClaims(noneSigned)
			Expect(err).To(Equal(ErrInvalidToken))
			_, err = service.ValidateToken(noneSigned)
			Expect(err).To(Equal(ErrInvalidToken))
		})

		ginkgo.It("rejects refresh tokens with missing or wrong required claims", func() {
			accessLike := signAuthClaims(service, jwt.MapClaims{
				"sub": "user-1",
				"typ": "access",
				"jti": "token-1",
				"exp": time.Now().Add(time.Hour).Unix(),
			})
			_, err := service.RefreshToken(accessLike)
			Expect(err).To(Equal(ErrInvalidToken))

			missingSubject := signAuthClaims(service, jwt.MapClaims{
				"typ": "refresh",
				"jti": "token-2",
				"exp": time.Now().Add(time.Hour).Unix(),
			})
			_, err = service.RefreshToken(missingSubject)
			Expect(err).To(Equal(ErrInvalidToken))

			missingJTI := signAuthClaims(service, jwt.MapClaims{
				"sub": "user-1",
				"typ": "refresh",
				"exp": time.Now().Add(time.Hour).Unix(),
			})
			_, err = service.RefreshToken(missingJTI)
			Expect(err).To(Equal(ErrInvalidToken))

			Expect(service.RevokeRefreshToken(missingJTI)).To(Equal(ErrInvalidToken))
		})

		ginkgo.It("rejects refresh tokens whose backing session is inactive", func() {
			token := signAuthClaims(service, jwt.MapClaims{
				"sub": "some-user",
				"typ": "refresh",
				"jti": "absent-session",
				"exp": time.Now().Add(time.Hour).Unix(),
			})
			_, err := service.RefreshToken(token)
			Expect(err).To(Equal(ErrInvalidToken))
		})

		ginkgo.It("requires refresh sessions to belong to an existing user", func() {
			missingUserID := newFixtureUserID("missing-user")
			sessionID := newFixtureSessionID("missing-user-refresh")
			Expect(service.sessionStore.CreateSession(sessionID, missingUserID, "refresh", time.Now().Add(time.Hour))).To(Succeed())

			token := signAuthClaims(service, jwt.MapClaims{
				"sub": missingUserID,
				"typ": "refresh",
				"jti": sessionID,
				"exp": time.Now().Add(time.Hour).Unix(),
			})
			_, err := service.RefreshToken(token)
			Expect(err).To(Equal(ErrUserNotFound))
		})

		ginkgo.It("rejects access tokens without a string subject", func() {
			token := signAuthClaims(service, jwt.MapClaims{
				"typ": "access",
				"jti": "access-without-sub",
				"exp": time.Now().Add(time.Hour).Unix(),
			})
			_, err := service.ValidateToken(token)
			Expect(err).To(Equal(ErrInvalidToken))
		})
	})

	ginkgo.Describe("session store state transitions", ginkgo.Label("integration"), func() {
		ginkgo.It("treats expired, revoked, and missing sessions as inactive", func() {
			store, err := NewSessionStore(authTempDir())
			Expect(err).NotTo(HaveOccurred())
			ginkgo.DeferCleanup(closeWithErrorCheck, store.Close)

			userID := newFixtureUserID("session-user")
			expiredID := newFixtureSessionID("expired-session")
			revokedID := newFixtureSessionID("revoked-session")

			Expect(store.CreateSession(expiredID, userID, "refresh", time.Now().Add(-time.Minute))).To(Succeed())
			Expect(inactiveAuthSession(store.IsActive(expiredID, userID, "refresh", time.Now()))).To(Succeed())

			Expect(store.CreateSession(revokedID, userID, "refresh", time.Now().Add(time.Hour))).To(Succeed())
			Expect(store.RevokeSession(revokedID)).To(Succeed())
			Expect(inactiveAuthSession(store.IsActive(revokedID, userID, "refresh", time.Now()))).To(Succeed())

			Expect(inactiveAuthSession(store.IsActive(newFixtureSessionID("missing-session"), userID, "refresh", time.Now()))).To(Succeed())
		})

		ginkgo.It("cleans up a partially opened connection when schema initialization fails", func() {
			schemaErr := errors.New("session schema failed")
			closed := false
			restoreOpen := setAuthSeam(&authSQLOpen, func(string, string) (*sql.DB, error) {
				return openAuthScriptedDB(&authScriptedDBScript{
					exec: func(string, []driver.NamedValue) (driver.Result, error) {
						return nil, schemaErr
					},
					close: func() error {
						closed = true
						return nil
					},
				}), nil
			})
			ginkgo.DeferCleanup(restoreOpen)

			store, err := NewSessionStore(authTempDir())
			Expect(err).To(MatchError(schemaErr))
			Expect(store).To(BeNil())
			Expect(closed).To(BeTrue())
		})
	})

	ginkgo.Describe("user service guardrails", ginkgo.Label("integration"), func() {
		var service *UserService

		ginkgo.BeforeEach(func() {
			service = setupTestUserService()
		})

		ginkgo.AfterEach(func() {
			Expect(service.Close()).To(Succeed())
		})

		ginkgo.It("keeps InitDefaultAdmin idempotent when an admin already exists", func() {
			Expect(service.InitDefaultAdmin("first-password")).To(Succeed())
			Expect(service.InitDefaultAdmin("second-password")).To(Succeed())

			users, err := service.GetUsers()
			Expect(err).NotTo(HaveOccurred())
			Expect(users).To(HaveExactElements(HaveField("Username", Equal("admin"))))
		})

		ginkgo.It("rejects conflicting profile updates before touching the stored user", func() {
			alice, err := service.CreateUser("alice", "alice@example.com", "password", RoleEditor)
			Expect(err).NotTo(HaveOccurred())
			bob, err := service.CreateUser("bob", "bob@example.com", "password", RoleViewer)
			Expect(err).NotTo(HaveOccurred())

			_, err = service.UpdateUser(UserIDFromString(bob.ID), alice.Username, bob.Email, "", RoleViewer)
			Expect(err).To(Equal(ErrUserAlreadyExists))

			_, err = service.UpdateUser(UserIDFromString(bob.ID), bob.Username, alice.Email, "", RoleViewer)
			Expect(err).To(Equal(ErrUserAlreadyExists))
		})

		ginkgo.It("rejects missing users and invalid roles for service mutations", func() {
			_, err := service.GetUserByID(newFixtureUserID("missing-user"))
			Expect(err).To(Equal(ErrUserNotFound))

			user, err := service.CreateUser("carol", "carol@example.com", "password", RoleEditor)
			Expect(err).NotTo(HaveOccurred())
			_, err = service.UpdateUser(UserIDFromString(user.ID), user.Username, user.Email, "", "owner")
			Expect(err).To(Equal(ErrUserInvalidRole))

			_, err = service.UpdateUser(newFixtureUserID("missing-user"), "missing", "missing@example.com", "", RoleViewer)
			Expect(err).To(Equal(ErrUserNotFound))

			Expect(service.UpdatePassword(newFixtureUserID("missing-user"), "new-password")).To(Equal(ErrUserNotFound))
			Expect(service.DeleteUser(newFixtureUserID("missing-user"))).To(Equal(ErrUserNotFound))
			Expect(service.ChangeOwnPassword(newFixtureUserID("missing-user"), "old-password", "new-password")).To(Equal(ErrUserNotFound))
		})

		ginkgo.It("updates passwords directly and rejects missing password-login identifiers", func() {
			user, err := service.CreateUser("dana", "dana@example.com", "old-password", RoleEditor)
			Expect(err).NotTo(HaveOccurred())
			userID := UserIDFromString(user.ID)

			Expect(service.UpdatePassword(userID, "new-password")).To(Succeed())
			_, err = service.GetUserByEmailOrUsernameAndPassword("dana", "old-password")
			Expect(err).To(Equal(ErrUserInvalidCredentials))
			_, err = service.GetUserByEmailOrUsernameAndPassword("dana", "new-password")
			Expect(err).NotTo(HaveOccurred())

			_, err = service.GetUserByEmailOrUsernameAndPassword("missing", "new-password")
			Expect(err).To(Equal(ErrUserNotFound))
		})

		ginkgo.It("returns missing user errors from resolver lazy lookups", func() {
			resolver, err := NewUserResolver(service)
			Expect(err).NotTo(HaveOccurred())

			label, err := resolver.ResolveUserLabel(newFixtureUserID("missing-user"))
			Expect(err).To(Equal(ErrUserNotFound))
			Expect(label).To(BeNil())
		})
	})
})
