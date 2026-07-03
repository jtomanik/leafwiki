package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

func setupTestAPIKeyService() (string, *UserService, *APIKeyStore, *APIKeyService, *User) {
	ginkgo.GinkgoHelper()

	storageDir := authTempDir()
	userStore, err := NewUserStore(storageDir)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(closeWithErrorCheck, userStore.Close)

	userService := NewUserService(userStore)
	apiKeyStore, err := NewAPIKeyStore(storageDir)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(closeWithErrorCheck, apiKeyStore.Close)

	apiKeyService := NewAPIKeyService(apiKeyStore, userService)
	user, err := userService.CreateUser("editor", "editor@example.com", "password123", RoleEditor)
	Expect(err).NotTo(HaveOccurred())
	return storageDir, userService, apiKeyStore, apiKeyService, user
}

type sqliteExclusiveBlocker struct {
	db *sql.DB
}

func beginExclusiveSQLiteTransaction(path string) sqliteExclusiveBlocker {
	ginkgo.GinkgoHelper()
	db, err := sql.Open("sqlite", path)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		_, _ = db.Exec("ROLLBACK")
		_ = db.Close()
	})
	_, err = db.Exec("BEGIN EXCLUSIVE")
	Expect(err).NotTo(HaveOccurred())
	return sqliteExclusiveBlocker{db: db}
}

func (b sqliteExclusiveBlocker) rollback() {
	ginkgo.GinkgoHelper()
	_, err := b.db.Exec("ROLLBACK")
	Expect(err).NotTo(HaveOccurred())
}

func haveRawAPIKeySecretFor(id APIKeyID) types.GomegaMatcher {
	return WithTransform(func(raw string) APIKeyID {
		parsed, err := parseAPIKeyID(raw)
		if err != nil {
			return ""
		}
		return parsed
	}, Equal(id))
}

type createdAPIKeyMetadataMatcher struct {
	userID UserID
	name   string
}

func matchCreatedAPIKeyMetadata(userID UserID, name string) types.GomegaMatcher {
	return createdAPIKeyMetadataMatcher{userID: userID, name: name}
}

func (m createdAPIKeyMetadataMatcher) Match(actual interface{}) (bool, error) {
	key, ok := actual.(*APIKey)
	if !ok {
		return false, fmt.Errorf("expected *APIKey, got %T", actual)
	}
	return key.Name == m.name &&
		key.UserID == m.userID &&
		key.CreatedByUserID == m.userID &&
		len(key.Scopes) == 1 &&
		key.Scopes[0] == MCPAPIKeyScope &&
		key.Prefix != "" &&
		key.Last4 != "" &&
		!key.CreatedAt.IsZero() &&
		key.LastUsedAt == nil &&
		key.RevokedAt == nil, nil
}

func (m createdAPIKeyMetadataMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nto describe newly created API key metadata for user %q", actual, m.userID)
}

func (m createdAPIKeyMetadataMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nnot to describe newly created API key metadata for user %q", actual, m.userID)
}

type apiKeyCreationResultMatcher struct {
	userID UserID
	name   string
}

func matchAPIKeyCreationResult(userID UserID, name string) types.GomegaMatcher {
	return apiKeyCreationResultMatcher{userID: userID, name: name}
}

func (m apiKeyCreationResultMatcher) Match(actual interface{}) (bool, error) {
	result, ok := actual.(*APIKeyCreateResult)
	if !ok {
		return false, fmt.Errorf("expected *APIKeyCreateResult, got %T", actual)
	}
	secretMatches, err := haveRawAPIKeySecretFor(result.Key.ID).Match(result.Secret)
	if err != nil || !secretMatches {
		return false, err
	}
	return matchCreatedAPIKeyMetadata(m.userID, m.name).Match(result.Key)
}

func (m apiKeyCreationResultMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nto describe a newly created API key result for user %q", actual, m.userID)
}

func (m apiKeyCreationResultMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nnot to describe a newly created API key result for user %q", actual, m.userID)
}

type apiKeyListMetadataMatcher struct {
	id   APIKeyID
	name string
}

func matchListedAPIKeyMetadata(id APIKeyID, name string) types.GomegaMatcher {
	return apiKeyListMetadataMatcher{id: id, name: name}
}

func (m apiKeyListMetadataMatcher) Match(actual interface{}) (bool, error) {
	key, ok := actual.(*APIKey)
	if !ok {
		return false, fmt.Errorf("expected *APIKey, got %T", actual)
	}
	return key.ID == m.id && key.Name == m.name, nil
}

func (m apiKeyListMetadataMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nto describe listed API key metadata %q", actual, m.id)
}

func (m apiKeyListMetadataMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nnot to describe listed API key metadata %q", actual, m.id)
}

type authUserIdentityMatcher struct {
	id   UserID
	role string
}

func matchAuthUserIdentity(id UserID, role string) types.GomegaMatcher {
	return authUserIdentityMatcher{id: id, role: role}
}

func (m authUserIdentityMatcher) Match(actual interface{}) (bool, error) {
	user, ok := actual.(*User)
	if !ok {
		return false, fmt.Errorf("expected *User, got %T", actual)
	}
	return UserIDFromString(user.ID) == m.id && user.Role == m.role, nil
}

func (m authUserIdentityMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nto describe auth user %q with role %q", actual, m.id, m.role)
}

func (m authUserIdentityMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nnot to describe auth user %q with role %q", actual, m.id, m.role)
}

type apiKeyVerificationMatcher struct {
	userID UserID
	role   string
}

func matchAPIKeyVerification(userID UserID, role string) types.GomegaMatcher {
	return apiKeyVerificationMatcher{userID: userID, role: role}
}

func (m apiKeyVerificationMatcher) Match(actual interface{}) (bool, error) {
	verification, ok := actual.(*APIKeyVerification)
	if !ok {
		return false, fmt.Errorf("expected *APIKeyVerification, got %T", actual)
	}
	userMatches, err := matchAuthUserIdentity(m.userID, m.role).Match(verification.User)
	if err != nil || !userMatches {
		return false, err
	}
	return verification.Key != nil && verification.Key.LastUsedAt != nil, nil
}

func (m apiKeyVerificationMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nto describe a verified API key for user %q with role %q", actual, m.userID, m.role)
}

func (m apiKeyVerificationMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nnot to describe a verified API key for user %q with role %q", actual, m.userID, m.role)
}

var _ = ginkgo.Describe("api key service", func() {
	ginkgo.It("stores only the API key hash and lists the public key metadata", func() {
		_, _, store, service, user := setupTestAPIKeyService()
		userID := newFixtureUserID(user.ID)

		created, err := service.CreateAPIKey(userID, "  Local Codex  ", userID)
		Expect(err).To(Succeed())

		Expect(created).To(matchAPIKeyCreationResult(userID, "Local Codex"))
		var _ APIKeyID = created.Key.ID

		var rawSecretCount int
		Expect(store.db.QueryRow(`SELECT COUNT(*) FROM api_keys WHERE secret_hash LIKE '%' || ? || '%'`, created.Secret).Scan(&rawSecretCount)).To(Succeed())
		Expect(rawSecretCount).To(BeZero())

		listed, err := service.ListAPIKeys(userID)
		Expect(err).To(Succeed())
		Expect(listed).To(ConsistOf(matchListedAPIKeyMetadata(created.Key.ID, "Local Codex")))
	})

	ginkgo.It("verifies active API keys and rejects malformed revoked or orphaned credentials", func() {
		_, userService, _, service, user := setupTestAPIKeyService()
		userID := newFixtureUserID(user.ID)

		created, err := service.CreateAPIKey(userID, "MCP client", userID)
		Expect(err).To(Succeed())

		verified, err := service.VerifyAPIKey(created.Secret)
		Expect(err).To(Succeed())
		Expect(verified).To(matchAPIKeyVerification(userID, RoleEditor))

		badInputs := []string{
			"",
			"not-an-api-key",
			"lwk_missing_parts",
			fmt.Sprintf("lwk_%s_wrongsecret", created.Key.ID),
		}
		for _, input := range badInputs {
			_, err := service.VerifyAPIKey(input)
			Expect(err).To(MatchError(ErrInvalidToken), "credential %q should be rejected", input)
		}

		Expect(service.RevokeAPIKey(userID, created.Key.ID)).To(Succeed())
		_, err = service.VerifyAPIKey(created.Secret)
		Expect(err).To(MatchError(ErrInvalidToken))
		listed, err := service.ListAPIKeys(userID)
		Expect(err).To(Succeed())
		Expect(listed).To(BeEmpty())

		second, err := service.CreateAPIKey(userID, "After revoke", userID)
		Expect(err).To(Succeed())
		_, err = userService.UpdateUser(userID, user.Username, user.Email, "", RoleViewer)
		Expect(err).To(Succeed())
		verifiedAfterRoleChange, err := service.VerifyAPIKey(second.Secret)
		Expect(err).To(Succeed())
		Expect(verifiedAfterRoleChange).To(matchAPIKeyVerification(userID, RoleViewer))
		Expect(userService.DeleteUser(userID)).To(Succeed())
		_, err = service.VerifyAPIKey(second.Secret)
		Expect(err).To(MatchError(ErrInvalidToken))
	})

	ginkgo.It("waits out a transient last-used write lock while verifying a key", func() {
		storageDir, _, _, service, user := setupTestAPIKeyService()
		userID := newFixtureUserID(user.ID)
		created, err := service.CreateAPIKey(userID, "MCP client", userID)
		Expect(err).To(Succeed())

		blocker, err := sql.Open("sqlite", filepath.Join(storageDir, "api_keys.db"))
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(closeWithErrorCheck, blocker.Close)
		_, err = blocker.Exec("BEGIN IMMEDIATE")
		Expect(err).To(Succeed())

		errs := make(chan error, 1)
		go func() {
			verified, err := service.VerifyAPIKey(created.Secret)
			if err != nil {
				errs <- err
				return
			}
			if verified.User.ID != user.ID {
				errs <- errors.New("verified wrong user")
				return
			}
			errs <- nil
		}()

		Consistently(errs).WithTimeout(100 * time.Millisecond).ShouldNot(Receive())
		Expect(blocker.Exec("ROLLBACK")).Error().NotTo(HaveOccurred())
		Eventually(errs).WithTimeout(3 * time.Second).Should(Receive(Succeed()))
	})

	ginkgo.It("waits out a transient API key lookup lock while verifying a key", func() {
		storageDir, _, _, service, user := setupTestAPIKeyService()
		userID := newFixtureUserID(user.ID)
		created, err := service.CreateAPIKey(userID, "MCP client", userID)
		Expect(err).To(Succeed())

		blocker := beginExclusiveSQLiteTransaction(filepath.Join(storageDir, "api_keys.db"))
		errs := make(chan error, 1)
		go func() {
			verified, err := service.VerifyAPIKey(created.Secret)
			if err != nil {
				errs <- err
				return
			}
			if verified.User.ID != user.ID {
				errs <- errors.New("verified wrong user")
				return
			}
			errs <- nil
		}()

		Consistently(errs).WithTimeout(100 * time.Millisecond).ShouldNot(Receive())
		blocker.rollback()
		Eventually(errs).WithTimeout(3 * time.Second).Should(Receive(Succeed()))
	})

	ginkgo.It("waits out a transient user lookup lock while verifying a key", func() {
		storageDir, _, _, service, user := setupTestAPIKeyService()
		userID := newFixtureUserID(user.ID)
		created, err := service.CreateAPIKey(userID, "MCP client", userID)
		Expect(err).To(Succeed())

		blocker := beginExclusiveSQLiteTransaction(filepath.Join(storageDir, "users.db"))
		errs := make(chan error, 1)
		go func() {
			verified, err := service.VerifyAPIKey(created.Secret)
			if err != nil {
				errs <- err
				return
			}
			if verified.User.ID != user.ID {
				errs <- errors.New("verified wrong user")
				return
			}
			errs <- nil
		}()

		Consistently(errs).WithTimeout(100 * time.Millisecond).ShouldNot(Receive())
		blocker.rollback()
		Eventually(errs).WithTimeout(3 * time.Second).Should(Receive(Succeed()))
	})

	ginkgo.It("keeps another user's API key active when revocation is requested by the wrong owner", func() {
		_, userService, _, service, user := setupTestAPIKeyService()
		userID := newFixtureUserID(user.ID)
		other, err := userService.CreateUser("other", "other@example.com", "password123", RoleEditor)
		Expect(err).To(Succeed())

		created, err := service.CreateAPIKey(newFixtureUserID(other.ID), "Other key", userID)
		Expect(err).To(Succeed())

		Expect(service.RevokeAPIKey(userID, created.Key.ID)).To(SatisfyAny(
			MatchError(sql.ErrNoRows),
			MatchError(ErrAPIKeyNotFound),
		))
		_, err = service.VerifyAPIKey(created.Secret)
		Expect(err).To(Succeed())
	})

	ginkgo.It("rejects last-used updates for revoked API keys", func() {
		_, _, store, service, user := setupTestAPIKeyService()
		userID := newFixtureUserID(user.ID)

		created, err := service.CreateAPIKey(userID, "Race key", userID)
		Expect(err).To(Succeed())
		Expect(service.RevokeAPIKey(userID, created.Key.ID)).To(Succeed())

		Expect(store.MarkAPIKeyUsed(created.Key.ID, service.now())).To(MatchError(ErrAPIKeyNotFound))
	})
})
