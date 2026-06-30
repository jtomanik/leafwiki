package auth

import (
	"errors"
	"log"
	"time"

	"github.com/golang-jwt/jwt/v5"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

var _ = ginkgo.Describe("auth seam-driven edge coverage", func() {
	ginkgo.Describe("API key service branches", func() {
		ginkgo.It("propagates random and store failures while creating API keys", func() {
			_, _, _, service, user := setupTestAPIKeyService(ginkgo.GinkgoT())
			userID := newFixtureUserID(user.ID)

			idRandomErr := errors.New("id random failed")
			restoreRand := setAuthSeam(&authRandRead, func([]byte) (int, error) {
				return 0, idRandomErr
			})
			_, err := service.CreateAPIKey(userID, "automation", userID)
			Expect(err).To(MatchError(idRandomErr))
			restoreRand()

			call := 0
			secretRandomErr := errors.New("secret random failed")
			restoreRand = setAuthSeam(&authRandRead, func(buf []byte) (int, error) {
				call++
				if call == 2 {
					return 0, secretRandomErr
				}
				for i := range buf {
					buf[i] = byte(i + 1)
				}
				return len(buf), nil
			})
			_, err = service.CreateAPIKey(userID, "automation", userID)
			Expect(err).To(MatchError(secretRandomErr))
			restoreRand()

			storeCreateErr := errors.New("store create failed")
			restoreCreate := setAuthSeam(&authAPIKeyStoreCreateAPIKey, func(*APIKeyStore, *APIKey, string) error {
				return storeCreateErr
			})
			_, err = service.CreateAPIKey(userID, "automation", userID)
			Expect(err).To(MatchError(storeCreateErr))
			restoreCreate()
		})

		ginkgo.It("maps verification store and user failures through token semantics", func() {
			_, _, _, service, _ := setupTestAPIKeyService(ginkgo.GinkgoT())
			raw := "lwk_key_secret"
			key := &APIKey{ID: newFixtureAPIKeyID("key"), UserID: newFixtureUserID("user-1")}
			stored := &storedAPIKey{key: key, secretHash: hashAPIKey(raw)}

			restoreGet := setAuthSeam(&authAPIKeyStoreGetAPIKeyByID, func(*APIKeyStore, APIKeyID) (*storedAPIKey, error) {
				return nil, ErrAPIKeyNotFound
			})
			_, err := service.VerifyAPIKey(raw)
			Expect(err).To(Equal(ErrInvalidToken))
			restoreGet()

			storeUnavailableErr := errors.New("store unavailable")
			restoreGet = setAuthSeam(&authAPIKeyStoreGetAPIKeyByID, func(*APIKeyStore, APIKeyID) (*storedAPIKey, error) {
				return nil, storeUnavailableErr
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(MatchError(storeUnavailableErr))
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

			userStoreErr := errors.New("user store failed")
			restoreUser = setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return nil, userStoreErr
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(MatchError(userStoreErr))
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

			markErr := errors.New("mark failed")
			restoreMark = setAuthSeam(&authAPIKeyStoreMarkAPIKeyUsed, func(*APIKeyStore, APIKeyID, time.Time) error {
				return markErr
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(MatchError(markErr))
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

			jtiErr := errors.New("jti failed")
			restoreRand := setAuthSeam(&authRandRead, func([]byte) (int, error) {
				return 0, jtiErr
			})
			_, err := service.Login("testuser", "securepass")
			Expect(err).To(MatchError(jtiErr))
			restoreRand()

			call := 0
			refreshJTIErr := errors.New("refresh jti failed")
			restoreRand = setAuthSeam(&authRandRead, func(buf []byte) (int, error) {
				call++
				if call == 2 {
					return 0, refreshJTIErr
				}
				for i := range buf {
					buf[i] = byte(i + 1)
				}
				return len(buf), nil
			})
			_, err = service.Login("testuser", "securepass")
			Expect(err).To(MatchError(refreshJTIErr))
			restoreRand()

			signErr := errors.New("sign failed")
			restoreSign := setAuthSeam(&authSignJWT, func(*jwt.Token, []byte) (string, error) {
				return "", signErr
			})
			_, err = service.Login("testuser", "securepass")
			Expect(err).To(MatchError(signErr))
			restoreSign()

			sessionCreateErr := errors.New("session create failed")
			restoreSession := setAuthSeam(&authSessionStoreCreateSession, func(*SessionStore, SessionID, UserID, string, time.Time) error {
				return sessionCreateErr
			})
			_, err = service.Login("testuser", "securepass")
			Expect(err).To(MatchError(sessionCreateErr))
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

			accessJTIErr := errors.New("access jti failed")
			restoreRand := setAuthSeam(&authRandRead, func([]byte) (int, error) {
				return 0, accessJTIErr
			})
			_, err = service.RefreshToken(tokens.RefreshToken)
			Expect(err).To(MatchError(accessJTIErr))
			restoreRand()

			call := 0
			refreshJTIErr := errors.New("refresh jti failed")
			restoreRand = setAuthSeam(&authRandRead, func(buf []byte) (int, error) {
				call++
				if call == 2 {
					return 0, refreshJTIErr
				}
				for i := range buf {
					buf[i] = byte(i + 1)
				}
				return len(buf), nil
			})
			_, err = service.RefreshToken(tokens.RefreshToken)
			Expect(err).To(MatchError(refreshJTIErr))
			restoreRand()

			sessionCreateErr := errors.New("session create failed")
			restoreSession := setAuthSeam(&authSessionStoreCreateSession, func(*SessionStore, SessionID, UserID, string, time.Time) error {
				return sessionCreateErr
			})
			_, err = service.RefreshToken(tokens.RefreshToken)
			Expect(err).To(MatchError(sessionCreateErr))
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

			randomErr := errors.New("random failed")
			restoreRand := setAuthSeam(&authRandRead, func([]byte) (int, error) {
				return 0, randomErr
			})
			_, err := generateJTI()
			Expect(err).To(MatchError(randomErr))
			_, _, _, err = service.generateToken(&User{ID: "user-1"}, time.Minute, "access")
			Expect(err).To(MatchError(randomErr))
			restoreRand()

			signErr := errors.New("sign failed")
			restoreSign := setAuthSeam(&authSignJWT, func(*jwt.Token, []byte) (string, error) {
				return "", signErr
			})
			_, _, _, err = service.generateToken(&User{ID: "user-1"}, time.Minute, "access")
			Expect(err).To(MatchError(signErr))
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
			stillLockedErr := errors.New("still locked")
			_, err = retryAPIKeyTransientLocks(func() (struct{}, error) {
				return struct{}{}, stillLockedErr
			})
			Expect(err).To(MatchError(stillLockedErr))
			restoreTransient()
			restoreDelay()
			restoreAttempts()

			restoreInterval := setAuthSeam(&authSessionCleanupInterval, time.Nanosecond)
			ginkgo.DeferCleanup(restoreInterval)
			logBuffer := gbytes.NewBuffer()
			previousLogOutput := log.Writer()
			log.SetOutput(logBuffer)
			ginkgo.DeferCleanup(log.SetOutput, previousLogOutput)

			store, err := NewSessionStore(ginkgo.GinkgoT().TempDir())
			Expect(err).NotTo(HaveOccurred())
			store.mu.Lock()
			Expect(store.db.Close()).To(Succeed())
			store.mu.Unlock()
			Eventually(logBuffer).WithTimeout(3 * time.Second).Should(gbytes.Say("failed to cleanup expired sessions"))
			Expect(store.Close()).To(Succeed())
		})
	})

	ginkgo.Describe("user service branches", func() {
		ginkgo.It("propagates create, update, password, delete, and reset failures", func() {
			service := setupTestUserService(ginkgo.GinkgoT())
			ginkgo.DeferCleanup(func() {
				Expect(service.Close()).To(Succeed())
			})

			hashErr := errors.New("hash failed")
			restoreHash := setAuthSeam(&authGeneratePasswordHash, func([]byte, int) ([]byte, error) {
				return nil, hashErr
			})
			_, err := service.CreateUser("hash", "hash@example.com", "password", RoleEditor)
			Expect(err).To(MatchError(hashErr))
			restoreHash()

			idErr := errors.New("id failed")
			restoreID := setAuthSeam(&authGenerateUniqueID, func() (string, error) {
				return "", idErr
			})
			_, err = service.CreateUser("id", "id@example.com", "password", RoleEditor)
			Expect(err).To(MatchError(idErr))
			restoreID()

			storeCreateErr := errors.New("store create failed")
			restoreCreate := setAuthSeam(&authUserStoreCreateUser, func(*UserStore, *User) error {
				return storeCreateErr
			})
			_, err = service.CreateUser("store", "store@example.com", "password", RoleEditor)
			Expect(err).To(MatchError(storeCreateErr))
			restoreCreate()

			admin, err := service.CreateUser("admin", "admin@example.com", "password", RoleAdmin)
			Expect(err).NotTo(HaveOccurred())
			adminID := newFixtureUserID(admin.ID)

			countErr := errors.New("count failed")
			restoreCount := setAuthSeam(&authUserStoreCountAdminUsers, func(*UserStore) (int, error) {
				return 0, countErr
			})
			_, err = service.UpdateUser(adminID, "admin", "admin@example.com", "", RoleEditor)
			Expect(err).To(MatchError(countErr))
			restoreCount()

			updateHashErr := errors.New("update hash failed")
			restoreHash = setAuthSeam(&authGeneratePasswordHash, func([]byte, int) ([]byte, error) {
				return nil, updateHashErr
			})
			_, err = service.UpdateUser(adminID, "admin", "admin@example.com", "new-password", RoleAdmin)
			Expect(err).To(MatchError(updateHashErr))
			Expect(service.UpdatePassword(adminID, "new-password")).To(MatchError(updateHashErr))
			restoreHash()

			storeUpdateErr := errors.New("store update failed")
			restoreUpdate := setAuthSeam(&authUserStoreUpdateUser, func(*UserStore, *User) error {
				return storeUpdateErr
			})
			_, err = service.UpdateUser(adminID, "admin", "admin@example.com", "", RoleAdmin)
			Expect(err).To(MatchError(storeUpdateErr))
			restoreUpdate()

			passwordUpdateErr := errors.New("password update failed")
			restorePassword := setAuthSeam(&authUserStoreUpdatePassword, func(*UserStore, UserID, string) error {
				return passwordUpdateErr
			})
			Expect(service.UpdatePassword(adminID, "new-password")).To(MatchError(passwordUpdateErr))
			Expect(service.ChangeOwnPassword(adminID, "password", "new-password")).To(MatchError(passwordUpdateErr))
			restorePassword()

			editor, err := service.CreateUser("editor", "editor@example.com", "password", RoleEditor)
			Expect(err).NotTo(HaveOccurred())
			deleteErr := errors.New("delete failed")
			restoreDelete := setAuthSeam(&authUserStoreDeleteUser, func(*UserStore, UserID) error {
				return deleteErr
			})
			Expect(service.DeleteUser(newFixtureUserID(editor.ID))).To(MatchError(deleteErr))
			restoreDelete()

			listErr := errors.New("list failed")
			restoreUsers := setAuthSeam(&authUserStoreGetAllUsers, func(*UserStore) ([]*User, error) {
				return nil, listErr
			})
			_, err = service.GetUsers()
			Expect(err).To(MatchError(listErr))
			restoreUsers()

			passwordGenerationErr := errors.New("password generation failed")
			restoreGenerate := setAuthSeam(&authGenerateRandomPassword, func(int) (string, error) {
				return "", passwordGenerationErr
			})
			_, err = service.ResetAdminUserPassword()
			Expect(err).To(MatchError(passwordGenerationErr))
			restoreGenerate()

			adminLookupErr := errors.New("admin lookup failed")
			restoreAdmin := setAuthSeam(&authUserStoreGetAdminUser, func(*UserStore) (*User, error) {
				return nil, adminLookupErr
			})
			_, err = service.ResetAdminUserPassword()
			Expect(err).To(MatchError(adminLookupErr))
			restoreAdmin()

			resetUpdateErr := errors.New("reset update failed")
			restorePassword = setAuthSeam(&authUserStoreUpdatePassword, func(*UserStore, UserID, string) error {
				return resetUpdateErr
			})
			_, err = service.ResetAdminUserPassword()
			Expect(err).To(MatchError(resetUpdateErr))
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
			createAdminErr := errors.New("create admin failed")
			restoreCreate := setAuthSeam(&authUserStoreCreateUser, func(*UserStore, *User) error {
				return createAdminErr
			})
			Expect(service.InitDefaultAdmin("password")).To(MatchError(createAdminErr))
			_, err := service.ResetAdminUserPassword()
			Expect(err).To(MatchError(createAdminErr))
			restoreCreate()
			restoreAdmin()
		})

		ginkgo.It("covers missing user paths through service seams", func() {
			service := setupTestUserService(ginkgo.GinkgoT())
			ginkgo.DeferCleanup(func() {
				Expect(service.Close()).To(Succeed())
			})
			backendErr := errors.New("backend failed")
			restoreGet := setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return nil, backendErr
			})

			_, err := service.GetUserByID(newFixtureUserID("missing"))
			Expect(err).To(MatchError(backendErr))
			_, err = service.UpdateUser(newFixtureUserID("missing"), "missing", "missing@example.com", "", RoleEditor)
			Expect(err).To(Equal(ErrUserNotFound))
			Expect(service.UpdatePassword(newFixtureUserID("missing"), "password")).To(MatchError(backendErr))
			Expect(service.DeleteUser(newFixtureUserID("missing"))).To(Equal(ErrUserNotFound))
			Expect(service.ChangeOwnPassword(newFixtureUserID("missing"), "old", "new")).To(Equal(ErrUserNotFound))
			restoreGet()

			user, err := service.CreateUser("owner", "owner@example.com", "old", RoleEditor)
			Expect(err).NotTo(HaveOccurred())
			ownHashErr := errors.New("own hash failed")
			restoreHash := setAuthSeam(&authGeneratePasswordHash, func([]byte, int) ([]byte, error) {
				return nil, ownHashErr
			})
			Expect(service.ChangeOwnPassword(newFixtureUserID(user.ID), "old", "new")).To(MatchError(ownHashErr))
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
