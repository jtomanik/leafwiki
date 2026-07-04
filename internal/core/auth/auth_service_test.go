package auth

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"time"
)

func setupTestAuthService() *AuthService {
	ginkgo.GinkgoHelper()
	store, err := NewUserStore(authTempDir())
	Expect(err).NotTo(HaveOccurred())
	userService := NewUserService(store)

	// Create test user
	_, err = userService.CreateUser("testuser", "test@example.com", "securepass", "admin")
	Expect(err).NotTo(HaveOccurred())

	// Create Session store
	sessionStore, err := NewSessionStore(authTempDir())
	Expect(err).NotTo(HaveOccurred())

	authService := NewAuthService(userService, sessionStore, "test-secret-key-for-unit-tests-1", 1*time.Hour, 24*time.Hour*7)
	return authService
}

var _ = ginkgo.Describe("auth service", func() {
	ginkgo.It("issues access and refresh tokens that validate to the authenticated user", func() {
		authService := setupTestAuthService()

		tokens, err := authService.Login("testuser", "securepass")
		Expect(err).NotTo(HaveOccurred())
		Expect(tokens).To(SatisfyAll(
			HaveField("Token", Not(BeEmpty())),
			HaveField("RefreshToken", Not(BeEmpty())),
		))

		user, err := authService.ValidateToken(tokens.Token)
		Expect(err).NotTo(HaveOccurred())
		Expect(user.Username).To(Equal("testuser"))
	})

	ginkgo.It("rejects a refresh token after it is revoked", func() {
		authService := setupTestAuthService()

		tokens, err := authService.Login("testuser", "securepass")
		Expect(err).NotTo(HaveOccurred())

		newTokens, err := authService.RefreshToken(tokens.RefreshToken)
		Expect(err).NotTo(HaveOccurred())
		Expect(newTokens).To(SatisfyAll(
			HaveField("Token", Not(BeEmpty())),
			HaveField("RefreshToken", Not(BeEmpty())),
		))

		err = authService.RevokeRefreshToken(newTokens.RefreshToken)
		Expect(err).NotTo(HaveOccurred())

		_, err = authService.RefreshToken(newTokens.RefreshToken)
		Expect(err).To(MatchError(ErrInvalidToken))
	})

	ginkgo.It("rejects revocation for malformed refresh tokens", func() {
		authService := setupTestAuthService()

		err := authService.RevokeRefreshToken("invalid-token")
		Expect(err).To(MatchError(ErrInvalidToken))
	})

	ginkgo.It("rejects revocation when an access token is supplied as refresh token", func() {
		authService := setupTestAuthService()

		tokens, err := authService.Login("testuser", "securepass")
		Expect(err).NotTo(HaveOccurred())

		err = authService.RevokeRefreshToken(tokens.Token)
		Expect(err).To(MatchError(ErrInvalidToken))
	})

	ginkgo.It("revokes all refresh sessions for one authenticated user", func() {
		authService := setupTestAuthService()

		tokens1, err := authService.Login("testuser", "securepass")
		Expect(err).NotTo(HaveOccurred())

		tokens2, err := authService.Login("testuser", "securepass")
		Expect(err).NotTo(HaveOccurred())

		_, err = authService.RefreshToken(tokens1.RefreshToken)
		Expect(err).NotTo(HaveOccurred())

		_, err = authService.RefreshToken(tokens2.RefreshToken)
		Expect(err).NotTo(HaveOccurred())

		user, err := authService.ValidateToken(tokens1.Token)
		Expect(err).NotTo(HaveOccurred())

		err = authService.RevokeAllUserSessions(UserIDFromString(user.ID))
		Expect(err).NotTo(HaveOccurred())

		_, err = authService.RefreshToken(tokens1.RefreshToken)
		Expect(err).To(MatchError(ErrInvalidToken))

		_, err = authService.RefreshToken(tokens2.RefreshToken)
		Expect(err).To(MatchError(ErrInvalidToken))
	})

	ginkgo.It("leaves other users refresh sessions active when one user is revoked", func() {
		authService := setupTestAuthService()

		userService := authService.userService
		_, err := userService.CreateUser("testuser2", "test2@example.com", "securepass2", "admin")
		Expect(err).NotTo(HaveOccurred())

		tokens1, err := authService.Login("testuser", "securepass")
		Expect(err).NotTo(HaveOccurred())

		tokens2, err := authService.Login("testuser2", "securepass2")
		Expect(err).NotTo(HaveOccurred())

		user1, err := authService.ValidateToken(tokens1.Token)
		Expect(err).NotTo(HaveOccurred())

		err = authService.RevokeAllUserSessions(UserIDFromString(user1.ID))
		Expect(err).NotTo(HaveOccurred())

		_, err = authService.RefreshToken(tokens1.RefreshToken)
		Expect(err).To(MatchError(ErrInvalidToken))

		_, err = authService.RefreshToken(tokens2.RefreshToken)
		Expect(err).NotTo(HaveOccurred())
	})
})
