package auth

import (
	"errors"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("auth service API key unit seam contracts", ginkgo.Label("unit"), func() {
	ginkgo.Describe("API key service seams", func() {
		ginkgo.It("creates lists revokes and verifies API keys through typed service contracts", func() {
			userID := newFixtureUserID("api-user")
			createdByUserID := newFixtureUserID("creator-user")
			user := &User{ID: userID, Username: "api-user", Role: RoleEditor}
			service := NewAPIKeyService(&APIKeyStore{}, &UserService{store: &UserStore{}})
			service.now = func() time.Time {
				return time.Unix(123, 0)
			}
			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return user, nil
			})
			randomCall := 0
			setAuthSeam(&authRandRead, func(buf []byte) (int, error) {
				randomCall++
				for i := range buf {
					buf[i] = byte(randomCall + i)
				}
				return len(buf), nil
			})
			var stored *storedAPIKey
			setAuthSeam(&authAPIKeyStoreCreateAPIKey, func(_ *APIKeyStore, key *APIKey, secretHash string) error {
				stored = &storedAPIKey{key: key, secretHash: secretHash}
				return nil
			})
			setAuthSeam(&authAPIKeyStoreListActiveAPIKeys, func(*APIKeyStore, UserID) ([]*APIKey, error) {
				return []*APIKey{stored.key}, nil
			})
			setAuthSeam(&authAPIKeyStoreRevokeAPIKey, func(*APIKeyStore, UserID, APIKeyID, time.Time) error {
				return nil
			})
			setAuthSeam(&authAPIKeyStoreGetAPIKeyByID, func(*APIKeyStore, APIKeyID) (*storedAPIKey, error) {
				return stored, nil
			})
			setAuthSeam(&authAPIKeyStoreMarkAPIKeyUsed, func(*APIKeyStore, APIKeyID, time.Time) error {
				return nil
			})

			result, err := service.CreateAPIKey(userID, " automation ", createdByUserID)
			Expect(err).To(Succeed())
			Expect(result).To(matchAPIKeyCreation(apiKeyCreationContract{
				Key: apiKeyContract{
					UserID:          userID,
					Name:            "automation",
					CreatedByUserID: createdByUserID,
					Scopes:          []string{MCPAPIKeyScope},
				},
				SecretPrefix:    APIKeyPrefix,
				BearerState:     apiKeyBearerAccepted,
				LastUsedTracked: false,
			}))
			Expect(struct{}{}).To(matchAPIKeyBearer(result.Secret, apiKeyBearerAccepted))
			Expect(struct{}{}).To(matchAPIKeyBearer("plain-token", apiKeyBearerRejected))

			keys, err := service.ListAPIKeys(userID)
			Expect(err).To(Succeed())
			Expect(keys).To(ConsistOf(result.Key))
			Expect(service.RevokeAPIKey(userID, result.Key.ID)).To(Succeed())

			verification, err := service.VerifyAPIKey(result.Secret)
			Expect(err).To(Succeed())
			Expect(verification).To(matchAPIKeyVerificationRecord(result.Key, user))
			Expect(result).To(matchAPIKeyCreation(apiKeyCreationContract{
				Key: apiKeyContract{
					UserID:          userID,
					Name:            "automation",
					CreatedByUserID: createdByUserID,
					Scopes:          []string{MCPAPIKeyScope},
				},
				SecretPrefix:    APIKeyPrefix,
				BearerState:     apiKeyBearerAccepted,
				LastUsedTracked: true,
			}))

			setAuthSeam(&authUserStoreGetUserByID, func(_ *UserStore, id UserID) (*User, error) {
				if id == createdByUserID {
					return nil, errAuthUnitLookupFailed
				}
				return user, nil
			})
			_, err = service.CreateAPIKey(userID, "automation", createdByUserID)
			Expect(err).To(MatchError(errAuthUnitLookupFailed))
		})

		ginkgo.It("maps API key validation store and retry failures to token semantics", func() {
			userID := newFixtureUserID("api-user")
			service := NewAPIKeyService(&APIKeyStore{}, &UserService{store: &UserStore{}})

			_, err := service.CreateAPIKey(userID, " ", userID)
			Expect(err).To(Equal(ErrAPIKeyInvalidName))

			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return nil, ErrUserNotFound
			})
			_, err = service.CreateAPIKey(userID, "automation", userID)
			Expect(err).To(Equal(ErrUserNotFound))
			_, err = service.ListAPIKeys(userID)
			Expect(err).To(Equal(ErrUserNotFound))
			Expect(service.RevokeAPIKey(userID, newFixtureAPIKeyID("key"))).To(Equal(ErrUserNotFound))

			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return &User{ID: userID}, nil
			})
			setAuthSeam(&authRandRead, func([]byte) (int, error) {
				return 0, errAuthUnitRandomFailed
			})
			_, err = service.CreateAPIKey(userID, "automation", userID)
			Expect(err).To(MatchError(errAuthUnitRandomFailed))

			randomCall := 0
			setAuthSeam(&authRandRead, func(buf []byte) (int, error) {
				randomCall++
				if randomCall == 2 {
					return 0, errAuthUnitRandomFailed
				}
				return authUnitFillRandom(buf)
			})
			_, err = service.CreateAPIKey(userID, "automation", userID)
			Expect(err).To(MatchError(errAuthUnitRandomFailed))

			setAuthSeam(&authRandRead, authUnitFillRandom)
			setAuthSeam(&authAPIKeyStoreCreateAPIKey, func(*APIKeyStore, *APIKey, string) error {
				return errAuthUnitStoreFailed
			})
			_, err = service.CreateAPIKey(userID, "automation", userID)
			Expect(err).To(MatchError(errAuthUnitStoreFailed))

			raw := newFixtureRawAPIKey(newFixtureAPIKeyID("key"), "secret")
			_, err = (*APIKeyService)(nil).VerifyAPIKey(raw)
			Expect(err).To(Equal(ErrInvalidToken))
			_, err = service.VerifyAPIKey("malformed")
			Expect(err).To(Equal(ErrInvalidToken))

			setAuthSeam(&authAPIKeyStoreGetAPIKeyByID, func(*APIKeyStore, APIKeyID) (*storedAPIKey, error) {
				return nil, ErrAPIKeyNotFound
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(Equal(ErrInvalidToken))

			setAuthSeam(&authAPIKeyStoreGetAPIKeyByID, func(*APIKeyStore, APIKeyID) (*storedAPIKey, error) {
				return nil, errAuthUnitStoreFailed
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(MatchError(errAuthUnitStoreFailed))

			revokedAt := time.Unix(456, 0)
			setAuthSeam(&authAPIKeyStoreGetAPIKeyByID, func(*APIKeyStore, APIKeyID) (*storedAPIKey, error) {
				return &storedAPIKey{
					key:        &APIKey{ID: newFixtureAPIKeyID("key"), UserID: userID, RevokedAt: &revokedAt},
					secretHash: hashAPIKey(raw),
				}, nil
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(Equal(ErrInvalidToken))

			setAuthSeam(&authAPIKeyStoreGetAPIKeyByID, func(*APIKeyStore, APIKeyID) (*storedAPIKey, error) {
				return &storedAPIKey{
					key:        &APIKey{ID: newFixtureAPIKeyID("key"), UserID: userID},
					secretHash: hashAPIKey(newFixtureRawAPIKey(newFixtureAPIKeyID("key"), "different")),
				}, nil
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(Equal(ErrInvalidToken))

			setAuthSeam(&authAPIKeyStoreGetAPIKeyByID, func(*APIKeyStore, APIKeyID) (*storedAPIKey, error) {
				return &storedAPIKey{
					key:        &APIKey{ID: newFixtureAPIKeyID("key"), UserID: userID},
					secretHash: hashAPIKey(raw),
				}, nil
			})
			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return nil, ErrUserNotFound
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(Equal(ErrInvalidToken))

			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return nil, errAuthUnitLookupFailed
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(MatchError(errAuthUnitLookupFailed))

			setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
				return &User{ID: userID}, nil
			})
			setAuthSeam(&authAPIKeyStoreMarkAPIKeyUsed, func(*APIKeyStore, APIKeyID, time.Time) error {
				return ErrAPIKeyNotFound
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(Equal(ErrInvalidToken))

			setAuthSeam(&authAPIKeyStoreMarkAPIKeyUsed, func(*APIKeyStore, APIKeyID, time.Time) error {
				return errAuthUnitStoreFailed
			})
			_, err = service.VerifyAPIKey(raw)
			Expect(err).To(MatchError(errAuthUnitStoreFailed))

			setAuthSeam(&authAPIKeyRetryMaxAttempts, 2)
			setAuthSeam(&authAPIKeyRetryDelay, time.Duration(0))
			lockAttempts := 0
			setAuthSeam(&authIsSQLiteTransientLock, func(err error) bool {
				return errors.Is(err, errAuthUnitTransientLocked)
			})
			_, err = retryAPIKeyTransientLocks(func() (struct{}, error) {
				lockAttempts++
				if lockAttempts == 1 {
					return struct{}{}, errAuthUnitTransientLocked
				}
				return struct{}{}, nil
			})
			Expect(err).To(Succeed())

			_, err = retryAPIKeyTransientLocks(func() (struct{}, error) {
				return struct{}{}, errAuthUnitStoreFailed
			})
			Expect(err).To(MatchError(errAuthUnitStoreFailed))

			_, err = retryAPIKeyTransientLocks(func() (struct{}, error) {
				return struct{}{}, errAuthUnitTransientLocked
			})
			Expect(err).To(MatchError(errAuthUnitTransientLocked))

			Expect((&APIKeyService{store: &APIKeyStore{}}).Close()).To(Succeed())
		})
	})
})
