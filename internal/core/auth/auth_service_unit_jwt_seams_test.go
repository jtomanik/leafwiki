package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("auth service JWT unit seam contracts", ginkgo.Label("unit"), func() {
	ginkgo.Describe("JWT auth service seams", func() {
		ginkgo.It("constructs token services and parses signed claims with default seams", func() {
			userID := newFixtureUserID("token-user")
			Expect(userID).To(Equal(newFixtureUserID("token-user")))
			Expect(NewAuthService(&UserService{}, &SessionStore{}, "short", time.Minute, time.Hour)).NotTo(BeNil())

			service := NewAuthService(
				&UserService{store: &UserStore{}},
				&SessionStore{},
				"0123456789abcdef0123456789abcdef",
				time.Minute,
				time.Hour,
			)
			Expect(service).NotTo(BeNil())
			claims := newFixtureJWTAccessClaims(userID)
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
			signed, err := authSignJWT(token, []byte("0123456789abcdef0123456789abcdef"))
			Expect(err).To(Succeed())

			parsed, err := service.parseClaims(signed)
			Expect(err).To(Succeed())
			Expect(parsed).To(matchJWTSubject(userID))
		})

		ginkgo.It("issues refreshes validates and revokes tokens through session seams", func() {
			user := &User{
				ID:       newFixtureUserID("token-user"),
				Username: "token-user",
				Email:    "token-user@example.test",
				Password: authUnitPasswordHash("secret"),
				Role:     RoleEditor,
			}
			service := authUnitServiceWithUser(user)
			refreshSessionID := newFixtureSessionID("refresh-session")
			setAuthSeam(&authRandRead, authUnitFillRandom)
			setAuthSeam(&authSignJWT, func(*jwt.Token, []byte) (string, error) {
				return "signed-token", nil
			})
			setAuthSeam(&authSessionStoreIsActive, func(*SessionStore, SessionID, UserID, string, time.Time) (bool, error) {
				return true, nil
			})
			setAuthSeam(&authSessionStoreRevokeSession, func(*SessionStore, SessionID) error {
				return nil
			})
			setAuthSeam(&authSessionStoreRevokeAllSessionsForUser, func(*SessionStore, UserID) error {
				return nil
			})

			tokens, err := service.Login("token-user", "secret")
			Expect(err).To(Succeed())
			Expect(tokens).To(matchAuthToken(authTokenContract{
				Token:        "signed-token",
				RefreshToken: "signed-token",
				UserID:       newFixtureUserID("token-user"),
				Username:     "token-user",
				Email:        "token-user@example.test",
				Role:         RoleEditor,
			}))

			setAuthSeam(&authJWTParse, func(string, jwt.Keyfunc, ...jwt.ParserOption) (*jwt.Token, error) {
				return &jwt.Token{Claims: newFixtureJWTRefreshClaims(newFixtureUserID("token-user"), refreshSessionID), Valid: true}, nil
			})
			refreshed, err := service.RefreshToken(tokens.RefreshToken)
			Expect(err).To(Succeed())
			Expect(refreshed).To(matchAuthToken(authTokenContract{
				Token:        "signed-token",
				RefreshToken: "signed-token",
				UserID:       newFixtureUserID("token-user"),
				Username:     "token-user",
				Email:        "token-user@example.test",
				Role:         RoleEditor,
			}))
			Expect(service.RevokeRefreshToken(tokens.RefreshToken)).To(Succeed())
			Expect(service.RevokeAllUserSessions(newFixtureUserID("token-user"))).To(Succeed())

			setAuthSeam(&authJWTParse, func(string, jwt.Keyfunc, ...jwt.ParserOption) (*jwt.Token, error) {
				return &jwt.Token{Claims: newFixtureJWTAccessClaims(newFixtureUserID("token-user")), Valid: true}, nil
			})
			validated, err := service.ValidateToken(tokens.Token)
			Expect(err).To(Succeed())
			Expect(validated).To(matchAuthUser(authUserContract{
				ID:       newFixtureUserID("token-user"),
				Username: "token-user",
				Email:    "token-user@example.test",
				Role:     RoleEditor,
				Password: "",
			}))
		})

		ginkgo.It("rejects invalid credentials token generation failures and malformed claims", func() {
			user := &User{
				ID:       newFixtureUserID("token-user"),
				Username: "token-user",
				Email:    "token-user@example.test",
				Password: authUnitPasswordHash("secret"),
				Role:     RoleEditor,
			}
			service := authUnitServiceWithUser(user)

			_, err := service.Login("token-user", "wrong-secret")
			Expect(err).To(Equal(ErrUserInvalidCredentials))
			for range loginMaxFailures - 1 {
				_, err = service.Login("token-user", "wrong-secret")
			}
			Expect(err).To(Equal(ErrUserInvalidCredentials))
			_, err = service.Login("token-user", "secret")
			Expect(err).To(Equal(ErrUserAccountLocked))
			service.attempts = newLoginAttemptTracker()

			missingUserService := authUnitServiceWithUser(nil)
			_, err = missingUserService.Login("missing", "secret")
			Expect(err).To(Equal(ErrUserInvalidCredentials))

			setAuthSeam(&authUserStoreGetUserByUsername, func(*UserStore, string) (*User, error) {
				return user, nil
			})
			setAuthSeam(&authUserStoreGetUserByEmail, func(*UserStore, string) (*User, error) {
				return user, nil
			})
			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return user, nil
			})
			setAuthSeam(&authRandRead, func([]byte) (int, error) {
				return 0, errAuthUnitRandomFailed
			})
			_, err = service.Login("token-user", "secret")
			Expect(err).To(MatchError(errAuthUnitRandomFailed))
			_, err = generateJTI()
			Expect(err).To(MatchError(errAuthUnitRandomFailed))

			user.Password = authUnitPasswordHash("secret")
			setAuthSeam(&authRandRead, authUnitFillRandom)
			setAuthSeam(&authSignJWT, func(*jwt.Token, []byte) (string, error) {
				return "", errAuthUnitSignFailed
			})
			_, err = service.Login("token-user", "secret")
			Expect(err).To(MatchError(errAuthUnitSignFailed))

			signCall := 0
			setAuthSeam(&authSignJWT, func(*jwt.Token, []byte) (string, error) {
				signCall++
				if signCall == 2 {
					return "", errAuthUnitSignFailed
				}
				return "signed-token", nil
			})
			user.Password = authUnitPasswordHash("secret")
			_, err = service.Login("token-user", "secret")
			Expect(err).To(MatchError(errAuthUnitSignFailed))

			setAuthSeam(&authSignJWT, func(*jwt.Token, []byte) (string, error) {
				return "signed-token", nil
			})
			user.Password = authUnitPasswordHash("secret")
			setAuthSeam(&authSessionStoreCreateSession, func(*SessionStore, SessionID, UserID, string, time.Time) error {
				return errAuthUnitStoreFailed
			})
			_, err = service.Login("token-user", "secret")
			Expect(err).To(MatchError(errAuthUnitStoreFailed))

			setAuthSeam(&authSessionStoreCreateSession, func(*SessionStore, SessionID, UserID, string, time.Time) error {
				return nil
			})
			setAuthSeam(&authJWTParse, func(string, jwt.Keyfunc, ...jwt.ParserOption) (*jwt.Token, error) {
				return nil, errAuthUnitStoreFailed
			})
			_, err = service.RefreshToken("bad-refresh")
			Expect(err).To(Equal(ErrInvalidToken))
			Expect(service.RevokeRefreshToken("bad-refresh")).To(Equal(ErrInvalidToken))
			_, err = service.ValidateToken("bad-access")
			Expect(err).To(Equal(ErrInvalidToken))

			setAuthSeam(&authJWTParse, func(string, jwt.Keyfunc, ...jwt.ParserOption) (*jwt.Token, error) {
				return &jwt.Token{Claims: jwt.RegisteredClaims{}, Valid: true}, nil
			})
			_, err = service.RefreshToken("bad-refresh")
			Expect(err).To(Equal(ErrInvalidToken))
			_, err = service.ValidateToken("bad-access")
			Expect(err).To(Equal(ErrInvalidToken))

			setAuthSeam(&authJWTParse, func(string, jwt.Keyfunc, ...jwt.ParserOption) (*jwt.Token, error) {
				return &jwt.Token{Claims: jwt.MapClaims{"typ": "access"}, Valid: true}, nil
			})
			_, err = service.RefreshToken("access-token")
			Expect(err).To(Equal(ErrInvalidToken))
			Expect(service.RevokeRefreshToken("access-token")).To(Equal(ErrInvalidToken))

			setAuthSeam(&authJWTParse, func(string, jwt.Keyfunc, ...jwt.ParserOption) (*jwt.Token, error) {
				return &jwt.Token{Claims: jwt.MapClaims{"typ": "refresh"}, Valid: true}, nil
			})
			_, err = service.RefreshToken("missing-subject")
			Expect(err).To(Equal(ErrInvalidToken))

			setAuthSeam(&authJWTParse, func(string, jwt.Keyfunc, ...jwt.ParserOption) (*jwt.Token, error) {
				return &jwt.Token{Claims: newFixtureJWTRefreshSubjectClaims(newFixtureUserID("token-user")), Valid: true}, nil
			})
			_, err = service.RefreshToken("missing-session")
			Expect(err).To(Equal(ErrInvalidToken))
			Expect(service.RevokeRefreshToken("missing-session")).To(Equal(ErrInvalidToken))

			setAuthSeam(&authJWTParse, func(string, jwt.Keyfunc, ...jwt.ParserOption) (*jwt.Token, error) {
				return &jwt.Token{Claims: jwt.MapClaims{}, Valid: false}, nil
			})
			_, err = service.ValidateToken("invalid-token")
			Expect(err).To(Equal(ErrInvalidToken))

			setAuthSeam(&authJWTParse, func(_ string, keyFunc jwt.Keyfunc, _ ...jwt.ParserOption) (*jwt.Token, error) {
				_, keyErr := keyFunc(&jwt.Token{Method: jwt.SigningMethodNone})
				return nil, keyErr
			})
			_, err = service.parseClaims("wrong-signing-method")
			Expect(err).To(Equal(ErrInvalidToken))
			_, err = service.ValidateToken("wrong-signing-method")
			Expect(err).To(Equal(ErrInvalidToken))

			setAuthSeam(&authJWTParse, func(string, jwt.Keyfunc, ...jwt.ParserOption) (*jwt.Token, error) {
				return &jwt.Token{Claims: jwt.MapClaims{"sub": 123}, Valid: true}, nil
			})
			_, err = service.ValidateToken("numeric-subject")
			Expect(err).To(Equal(ErrInvalidToken))

			setAuthSeam(&authJWTParse, func(_ string, keyFunc jwt.Keyfunc, _ ...jwt.ParserOption) (*jwt.Token, error) {
				_, keyErr := keyFunc(&jwt.Token{Method: jwt.SigningMethodHS256})
				if keyErr != nil {
					return nil, keyErr
				}
				return &jwt.Token{Claims: newFixtureJWTAccessClaims(newFixtureUserID("token-user")), Valid: true}, nil
			})
			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return user, nil
			})
			_, err = service.ValidateToken("signed-access")
			Expect(err).To(Succeed())

			setAuthSeam(&authJWTParse, func(string, jwt.Keyfunc, ...jwt.ParserOption) (*jwt.Token, error) {
				return &jwt.Token{Claims: newFixtureJWTAccessClaims(newFixtureUserID("token-user")), Valid: true}, nil
			})
			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return nil, errAuthUnitLookupFailed
			})
			_, err = service.ValidateToken("lookup-failed")
			Expect(err).To(MatchError(errAuthUnitLookupFailed))
		})

		ginkgo.It("maps refresh session and user lookup failures to token errors", func() {
			user := &User{
				ID:       newFixtureUserID("token-user"),
				Username: "token-user",
				Email:    "token-user@example.test",
				Password: authUnitPasswordHash("secret"),
				Role:     RoleEditor,
			}
			service := authUnitServiceWithUser(user)
			setAuthSeam(&authJWTParse, func(string, jwt.Keyfunc, ...jwt.ParserOption) (*jwt.Token, error) {
				return &jwt.Token{Claims: newFixtureJWTRefreshClaims(newFixtureUserID("token-user"), newFixtureSessionID("refresh-session")), Valid: true}, nil
			})
			setAuthSeam(&authSessionStoreIsActive, func(*SessionStore, SessionID, UserID, string, time.Time) (bool, error) {
				return false, nil
			})
			_, err := service.RefreshToken("inactive-refresh")
			Expect(err).To(Equal(ErrInvalidToken))

			setAuthSeam(&authSessionStoreIsActive, func(*SessionStore, SessionID, UserID, string, time.Time) (bool, error) {
				return false, errAuthUnitStoreFailed
			})
			_, err = service.RefreshToken("failed-session")
			Expect(err).To(Equal(ErrInvalidToken))

			setAuthSeam(&authSessionStoreIsActive, func(*SessionStore, SessionID, UserID, string, time.Time) (bool, error) {
				return true, nil
			})
			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return nil, ErrUserNotFound
			})
			_, err = service.RefreshToken("missing-user")
			Expect(err).To(Equal(ErrUserNotFound))

			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return user, nil
			})
			setAuthSeam(&authRandRead, func([]byte) (int, error) {
				return 0, errAuthUnitRandomFailed
			})
			_, err = service.RefreshToken("random-failed")
			Expect(err).To(MatchError(errAuthUnitRandomFailed))

			setAuthSeam(&authRandRead, authUnitFillRandom)
			signCall := 0
			setAuthSeam(&authSignJWT, func(*jwt.Token, []byte) (string, error) {
				signCall++
				if signCall == 2 {
					return "", errAuthUnitSignFailed
				}
				return "signed-token", nil
			})
			_, err = service.RefreshToken("refresh-sign-failed")
			Expect(err).To(MatchError(errAuthUnitSignFailed))

			setAuthSeam(&authSignJWT, func(*jwt.Token, []byte) (string, error) {
				return "signed-token", nil
			})
			setAuthSeam(&authSessionStoreCreateSession, func(*SessionStore, SessionID, UserID, string, time.Time) error {
				return errAuthUnitStoreFailed
			})
			_, err = service.RefreshToken("session-create-failed")
			Expect(err).To(MatchError(errAuthUnitStoreFailed))

			setAuthSeam(&authSessionStoreCreateSession, func(*SessionStore, SessionID, UserID, string, time.Time) error {
				return nil
			})
			setAuthSeam(&authSessionStoreRevokeSession, func(*SessionStore, SessionID) error {
				return errAuthUnitStoreFailed
			})
			_, err = service.RefreshToken("revoke-failed")
			Expect(err).To(Succeed())
		})
	})
})
