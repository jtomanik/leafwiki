package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("auth seam-driven edge coverage", func() {
	ginkgo.Describe("API key service branches", func() {
		ginkgo.It("propagates random and store failures while creating API keys", func() {
			_, _, _, service, user := setupTestAPIKeyService(ginkgo.GinkgoT())
			userID := newFixtureUserID(user.ID)

			restoreRand := setAuthSeam(&authRandRead, func([]byte) (int, error) {
				return 0, errors.New("id random failed")
			})
			_, err := service.CreateAPIKey(userID, "automation", userID)
			Expect(err).To(MatchError("id random failed"))
			restoreRand()

			call := 0
			restoreRand = setAuthSeam(&authRandRead, func(buf []byte) (int, error) {
				call++
				if call == 2 {
					return 0, errors.New("secret random failed")
				}
				for i := range buf {
					buf[i] = byte(i + 1)
				}
				return len(buf), nil
			})
			_, err = service.CreateAPIKey(userID, "automation", userID)
			Expect(err).To(MatchError("secret random failed"))
			restoreRand()

			restoreCreate := setAuthSeam(&authAPIKeyStoreCreateAPIKey, func(*APIKeyStore, *APIKey, string) error {
				return errors.New("store create failed")
			})
			_, err = service.CreateAPIKey(userID, "automation", userID)
			Expect(err).To(MatchError("store create failed"))
			restoreCreate()
		})

		ginkgo.It("maps verification store and user failures through token semantics", func() {
			_, _, _, service, _ := setupTestAPIKeyService(ginkgo.GinkgoT())
			raw := "lwk_key_secret"
			key := &APIKey{ID: NewAPIKeyIDUnchecked("key"), UserID: newFixtureUserID("user-1")}
			stored := &storedAPIKey{key: key, secretHash: hashAPIKey(raw)}

			restoreGet := setAuthSeam(&authAPIKeyStoreGetAPIKeyByID, func(*APIKeyStore, APIKeyID) (*storedAPIKey, error) {
				return nil, ErrAPIKeyNotFound
			})
			_, err := service.VerifyAPIKey(raw)
			Expect(err).To(Equal(ErrInvalidToken))
			restoreGet()

			restoreGet = setAuthSeam(&authAPIKeyStoreGetAPIKeyByID, func(*APIKeyStore, APIKeyID) (*storedAPIKey, error) {
				return nil, errors.New("store unavailable")
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(MatchError("store unavailable"))
			restoreGet()

			revokedAt := time.Now().UTC()
			restoreGet = setAuthSeam(&authAPIKeyStoreGetAPIKeyByID, func(*APIKeyStore, APIKeyID) (*storedAPIKey, error) {
				return &storedAPIKey{key: &APIKey{ID: key.ID, UserID: key.UserID, RevokedAt: &revokedAt}, secretHash: hashAPIKey(raw)}, nil
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(Equal(ErrInvalidToken))
			restoreGet()

			restoreGet = setAuthSeam(&authAPIKeyStoreGetAPIKeyByID, func(*APIKeyStore, APIKeyID) (*storedAPIKey, error) {
				return stored, nil
			})
			restoreUser := setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return nil, ErrUserNotFound
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(Equal(ErrInvalidToken))
			restoreUser()

			restoreUser = setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return nil, errors.New("user store failed")
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(MatchError("user store failed"))
			restoreUser()

			restoreUser = setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return &User{ID: "user-1", Username: "editor", Role: RoleEditor}, nil
			})
			restoreMark := setAuthSeam(&authAPIKeyStoreMarkAPIKeyUsed, func(*APIKeyStore, APIKeyID, time.Time) error {
				return ErrAPIKeyNotFound
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(Equal(ErrInvalidToken))
			restoreMark()

			restoreMark = setAuthSeam(&authAPIKeyStoreMarkAPIKeyUsed, func(*APIKeyStore, APIKeyID, time.Time) error {
				return errors.New("mark failed")
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(MatchError("mark failed"))
			restoreMark()
			restoreUser()
			restoreGet()
		})
	})

	ginkgo.Describe("JWT auth service branches", func() {
		ginkgo.It("surfaces token generation and session creation failures during login", func() {
			service := setupTestAuthService(ginkgo.GinkgoT())
			ginkgo.DeferCleanup(func() {
				Expect(service.sessionStore.Close()).To(Succeed())
				Expect(service.userService.Close()).To(Succeed())
			})

			restoreRand := setAuthSeam(&authRandRead, func([]byte) (int, error) {
				return 0, errors.New("jti failed")
			})
			_, err := service.Login("testuser", "securepass")
			Expect(err).To(MatchError("jti failed"))
			restoreRand()

			call := 0
			restoreRand = setAuthSeam(&authRandRead, func(buf []byte) (int, error) {
				call++
				if call == 2 {
					return 0, errors.New("refresh jti failed")
				}
				for i := range buf {
					buf[i] = byte(i + 1)
				}
				return len(buf), nil
			})
			_, err = service.Login("testuser", "securepass")
			Expect(err).To(MatchError("refresh jti failed"))
			restoreRand()

			restoreSign := setAuthSeam(&authSignJWT, func(*jwt.Token, []byte) (string, error) {
				return "", errors.New("sign failed")
			})
			_, err = service.Login("testuser", "securepass")
			Expect(err).To(MatchError("sign failed"))
			restoreSign()

			restoreSession := setAuthSeam(&authSessionStoreCreateSession, func(*SessionStore, SessionID, UserID, string, time.Time) error {
				return errors.New("session create failed")
			})
			_, err = service.Login("testuser", "securepass")
			Expect(err).To(MatchError("session create failed"))
			restoreSession()
		})

		ginkgo.It("surfaces token generation and session creation failures during refresh", func() {
			service := setupTestAuthService(ginkgo.GinkgoT())
			ginkgo.DeferCleanup(func() {
				Expect(service.sessionStore.Close()).To(Succeed())
				Expect(service.userService.Close()).To(Succeed())
			})
			tokens, err := service.Login("testuser", "securepass")
			Expect(err).NotTo(HaveOccurred())

			restoreRand := setAuthSeam(&authRandRead, func([]byte) (int, error) {
				return 0, errors.New("access jti failed")
			})
			_, err = service.RefreshToken(tokens.RefreshToken)
			Expect(err).To(MatchError("access jti failed"))
			restoreRand()

			call := 0
			restoreRand = setAuthSeam(&authRandRead, func(buf []byte) (int, error) {
				call++
				if call == 2 {
					return 0, errors.New("refresh jti failed")
				}
				for i := range buf {
					buf[i] = byte(i + 1)
				}
				return len(buf), nil
			})
			_, err = service.RefreshToken(tokens.RefreshToken)
			Expect(err).To(MatchError("refresh jti failed"))
			restoreRand()

			restoreSession := setAuthSeam(&authSessionStoreCreateSession, func(*SessionStore, SessionID, UserID, string, time.Time) error {
				return errors.New("session create failed")
			})
			_, err = service.RefreshToken(tokens.RefreshToken)
			Expect(err).To(MatchError("session create failed"))
			restoreSession()

			restoreRevoke := setAuthSeam(&authSessionStoreRevokeSession, func(*SessionStore, SessionID) error {
				return errors.New("revoke failed")
			})
			refreshed, err := service.RefreshToken(tokens.RefreshToken)
			Expect(err).NotTo(HaveOccurred())
			Expect(refreshed.RefreshToken).NotTo(BeEmpty())
			restoreRevoke()
		})

		ginkgo.It("covers direct token helper errors", func() {
			service := setupTestAuthService(ginkgo.GinkgoT())
			ginkgo.DeferCleanup(func() {
				Expect(service.sessionStore.Close()).To(Succeed())
				Expect(service.userService.Close()).To(Succeed())
			})

			restoreRand := setAuthSeam(&authRandRead, func([]byte) (int, error) {
				return 0, errors.New("random failed")
			})
			_, err := generateJTI()
			Expect(err).To(MatchError("random failed"))
			_, _, _, err = service.generateToken(&User{ID: "user-1"}, time.Minute, "access")
			Expect(err).To(MatchError("random failed"))
			restoreRand()

			restoreSign := setAuthSeam(&authSignJWT, func(*jwt.Token, []byte) (string, error) {
				return "", errors.New("sign failed")
			})
			_, _, _, err = service.generateToken(&User{ID: "user-1"}, time.Minute, "access")
			Expect(err).To(MatchError("sign failed"))
			restoreSign()
		})

		ginkgo.It("covers parser type assertions and cleanup retry seams", func() {
			service := setupTestAuthService(ginkgo.GinkgoT())
			ginkgo.DeferCleanup(func() {
				Expect(service.sessionStore.Close()).To(Succeed())
				Expect(service.userService.Close()).To(Succeed())
			})

			restoreParse := setAuthSeam(&authJWTParse, func(string, jwt.Keyfunc, ...jwt.ParserOption) (*jwt.Token, error) {
				return &jwt.Token{Claims: jwt.RegisteredClaims{}, Valid: true}, nil
			})
			_, err := service.parseClaims("not-map-claims")
			Expect(err).To(Equal(ErrInvalidToken))
			_, err = service.ValidateToken("not-map-claims")
			Expect(err).To(Equal(ErrInvalidToken))
			restoreParse()

			restoreAttempts := setAuthSeam(&authAPIKeyRetryMaxAttempts, 2)
			restoreDelay := setAuthSeam(&authAPIKeyRetryDelay, time.Duration(0))
			restoreTransient := setAuthSeam(&authIsSQLiteTransientLock, func(error) bool {
				return true
			})
			_, err = retryAPIKeyTransientLocks(func() (struct{}, error) {
				return struct{}{}, errors.New("still locked")
			})
			Expect(err).To(MatchError("still locked"))
			restoreTransient()
			restoreDelay()
			restoreAttempts()

			restoreInterval := setAuthSeam(&authSessionCleanupInterval, time.Nanosecond)
			store, err := NewSessionStore(ginkgo.GinkgoT().TempDir())
			Expect(err).NotTo(HaveOccurred())
			store.mu.Lock()
			Expect(store.db.Close()).To(Succeed())
			store.mu.Unlock()
			time.Sleep(5 * time.Millisecond)
			Expect(store.Close()).To(Succeed())
			restoreInterval()
		})
	})

	ginkgo.Describe("user service branches", func() {
		ginkgo.It("propagates create, update, password, delete, and reset failures", func() {
			service := setupTestUserService(ginkgo.GinkgoT())
			ginkgo.DeferCleanup(func() {
				Expect(service.Close()).To(Succeed())
			})

			restoreHash := setAuthSeam(&authGeneratePasswordHash, func([]byte, int) ([]byte, error) {
				return nil, errors.New("hash failed")
			})
			_, err := service.CreateUser("hash", "hash@example.com", "password", RoleEditor)
			Expect(err).To(MatchError("hash failed"))
			restoreHash()

			restoreID := setAuthSeam(&authGenerateUniqueID, func() (string, error) {
				return "", errors.New("id failed")
			})
			_, err = service.CreateUser("id", "id@example.com", "password", RoleEditor)
			Expect(err).To(MatchError("id failed"))
			restoreID()

			restoreCreate := setAuthSeam(&authUserStoreCreateUser, func(*UserStore, *User) error {
				return errors.New("store create failed")
			})
			_, err = service.CreateUser("store", "store@example.com", "password", RoleEditor)
			Expect(err).To(MatchError("store create failed"))
			restoreCreate()

			admin, err := service.CreateUser("admin", "admin@example.com", "password", RoleAdmin)
			Expect(err).NotTo(HaveOccurred())
			adminID := newFixtureUserID(admin.ID)

			restoreCount := setAuthSeam(&authUserStoreCountAdminUsers, func(*UserStore) (int, error) {
				return 0, errors.New("count failed")
			})
			_, err = service.UpdateUser(adminID, "admin", "admin@example.com", "", RoleEditor)
			Expect(err).To(MatchError("count failed"))
			restoreCount()

			restoreHash = setAuthSeam(&authGeneratePasswordHash, func([]byte, int) ([]byte, error) {
				return nil, errors.New("update hash failed")
			})
			_, err = service.UpdateUser(adminID, "admin", "admin@example.com", "new-password", RoleAdmin)
			Expect(err).To(MatchError("update hash failed"))
			Expect(service.UpdatePassword(adminID, "new-password")).To(MatchError("update hash failed"))
			restoreHash()

			restoreUpdate := setAuthSeam(&authUserStoreUpdateUser, func(*UserStore, *User) error {
				return errors.New("store update failed")
			})
			_, err = service.UpdateUser(adminID, "admin", "admin@example.com", "", RoleAdmin)
			Expect(err).To(MatchError("store update failed"))
			restoreUpdate()

			restorePassword := setAuthSeam(&authUserStoreUpdatePassword, func(*UserStore, UserID, string) error {
				return errors.New("password update failed")
			})
			Expect(service.UpdatePassword(adminID, "new-password")).To(MatchError("password update failed"))
			Expect(service.ChangeOwnPassword(adminID, "password", "new-password")).To(MatchError("password update failed"))
			restorePassword()

			editor, err := service.CreateUser("editor", "editor@example.com", "password", RoleEditor)
			Expect(err).NotTo(HaveOccurred())
			restoreDelete := setAuthSeam(&authUserStoreDeleteUser, func(*UserStore, UserID) error {
				return errors.New("delete failed")
			})
			Expect(service.DeleteUser(newFixtureUserID(editor.ID))).To(MatchError("delete failed"))
			restoreDelete()

			restoreUsers := setAuthSeam(&authUserStoreGetAllUsers, func(*UserStore) ([]*User, error) {
				return nil, errors.New("list failed")
			})
			_, err = service.GetUsers()
			Expect(err).To(MatchError("list failed"))
			restoreUsers()

			restoreGenerate := setAuthSeam(&authGenerateRandomPassword, func(int) (string, error) {
				return "", errors.New("password generation failed")
			})
			_, err = service.ResetAdminUserPassword()
			Expect(err).To(MatchError(ContainSubstring("failed to generate password")))
			restoreGenerate()

			restoreAdmin := setAuthSeam(&authUserStoreGetAdminUser, func(*UserStore) (*User, error) {
				return nil, errors.New("admin lookup failed")
			})
			_, err = service.ResetAdminUserPassword()
			Expect(err).To(MatchError("admin lookup failed"))
			restoreAdmin()

			restorePassword = setAuthSeam(&authUserStoreUpdatePassword, func(*UserStore, UserID, string) error {
				return errors.New("reset update failed")
			})
			_, err = service.ResetAdminUserPassword()
			Expect(err).To(MatchError(ContainSubstring("failed to update admin password")))
			restorePassword()
		})

		ginkgo.It("covers default-admin creation failure branches", func() {
			service := setupTestUserService(ginkgo.GinkgoT())
			ginkgo.DeferCleanup(func() {
				Expect(service.Close()).To(Succeed())
			})

			restoreAdmin := setAuthSeam(&authUserStoreGetAdminUser, func(*UserStore) (*User, error) {
				return nil, ErrUserNotFound
			})
			restoreCreate := setAuthSeam(&authUserStoreCreateUser, func(*UserStore, *User) error {
				return errors.New("create admin failed")
			})
			Expect(service.InitDefaultAdmin("password")).To(MatchError(ContainSubstring("failed to create default admin")))
			_, err := service.ResetAdminUserPassword()
			Expect(err).To(MatchError(ContainSubstring("failed to create default admin")))
			restoreCreate()
			restoreAdmin()
		})

		ginkgo.It("covers missing user paths through service seams", func() {
			service := setupTestUserService(ginkgo.GinkgoT())
			ginkgo.DeferCleanup(func() {
				Expect(service.Close()).To(Succeed())
			})
			restoreGet := setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return nil, errors.New("backend failed")
			})

			_, err := service.GetUserByID(newFixtureUserID("missing"))
			Expect(err).To(MatchError("backend failed"))
			_, err = service.UpdateUser(newFixtureUserID("missing"), "missing", "missing@example.com", "", RoleEditor)
			Expect(err).To(Equal(ErrUserNotFound))
			Expect(service.UpdatePassword(newFixtureUserID("missing"), "password")).To(MatchError("backend failed"))
			Expect(service.DeleteUser(newFixtureUserID("missing"))).To(Equal(ErrUserNotFound))
			Expect(service.ChangeOwnPassword(newFixtureUserID("missing"), "old", "new")).To(Equal(ErrUserNotFound))
			restoreGet()

			user, err := service.CreateUser("owner", "owner@example.com", "old", RoleEditor)
			Expect(err).NotTo(HaveOccurred())
			restoreHash := setAuthSeam(&authGeneratePasswordHash, func([]byte, int) ([]byte, error) {
				return nil, errors.New("own hash failed")
			})
			Expect(service.ChangeOwnPassword(newFixtureUserID(user.ID), "old", "new")).To(MatchError("own hash failed"))
			restoreHash()
		})
	})
})

func setAuthSeam[T any](target *T, replacement T) func() {
	ginkgo.GinkgoHelper()

	original := *target
	*target = replacement
	restored := false
	restore := func() {
		if restored {
			return
		}
		*target = original
		restored = true
	}
	ginkgo.DeferCleanup(restore)
	return restore
}
