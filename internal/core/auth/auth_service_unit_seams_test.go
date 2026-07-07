package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"golang.org/x/crypto/bcrypt"
)

var (
	errAuthUnitLookupFailed    = errors.New("auth unit lookup failed")
	errAuthUnitStoreFailed     = errors.New("auth unit store failed")
	errAuthUnitHashFailed      = errors.New("auth unit hash failed")
	errAuthUnitRandomFailed    = errors.New("auth unit random failed")
	errAuthUnitSignFailed      = errors.New("auth unit sign failed")
	errAuthUnitTransientLocked = errors.New("auth unit transient lock")
)

type authUserContract struct {
	ID       UserID
	Username string
	Email    string
	Role     string
	Password string
}

type apiKeyContract struct {
	UserID          UserID
	Name            string
	Scopes          []string
	CreatedByUserID UserID
}

type apiKeyCreationContract struct {
	Key             apiKeyContract
	SecretPrefix    string
	BearerState     apiKeyBearerState
	LastUsedTracked bool
}

type apiKeyVerificationContract struct {
	Key  *APIKey
	User *User
}

type authTokenContract struct {
	Token        string
	RefreshToken string
	UserID       UserID
	Username     string
	Email        string
	Role         string
}

type authErrorCauseState string

const (
	authErrorCausePresent authErrorCauseState = "cause-present"
	authErrorCauseMissing authErrorCauseState = "cause-missing"
)

type authErrorCauseObservation struct {
	State authErrorCauseState
	Cause error
}

type roleAcceptance string

const (
	roleAccepted roleAcceptance = "accepted"
	roleRejected roleAcceptance = "rejected"
)

type apiKeyBearerState string

const (
	apiKeyBearerAccepted apiKeyBearerState = "api-key-bearer"
	apiKeyBearerRejected apiKeyBearerState = "not-api-key-bearer"
)

type jwtClaimKey string

const (
	jwtClaimSubject jwtClaimKey = "sub"
	jwtClaimType    jwtClaimKey = "typ"
	jwtClaimID      jwtClaimKey = "jti"
)

func matchAuthUser(expected authUserContract) types.GomegaMatcher {
	return WithTransform(func(user *User) authUserContract {
		if user == nil {
			return authUserContract{}
		}
		return authUserContract{
			ID:       user.ID,
			Username: user.Username,
			Email:    user.Email,
			Role:     user.Role,
			Password: user.Password,
		}
	}, gstruct.MatchAllFields(gstruct.Fields{
		"ID":       Equal(expected.ID),
		"Username": Equal(expected.Username),
		"Email":    Equal(expected.Email),
		"Role":     Equal(expected.Role),
		"Password": Equal(expected.Password),
	}))
}

func matchAPIKey(expected apiKeyContract) types.GomegaMatcher {
	return WithTransform(func(key *APIKey) apiKeyContract {
		if key == nil {
			return apiKeyContract{}
		}
		return apiKeyContract{
			UserID:          key.UserID,
			Name:            key.Name,
			Scopes:          key.Scopes,
			CreatedByUserID: key.CreatedByUserID,
		}
	}, gstruct.MatchAllFields(gstruct.Fields{
		"UserID":          Equal(expected.UserID),
		"Name":            Equal(expected.Name),
		"Scopes":          ConsistOf(expected.Scopes),
		"CreatedByUserID": Equal(expected.CreatedByUserID),
	}))
}

func apiKeyCreationFields(result *APIKeyCreateResult) apiKeyCreationContract {
	if result == nil || result.Key == nil {
		return apiKeyCreationContract{}
	}
	secretPrefix := result.Secret
	if len(secretPrefix) > len(APIKeyPrefix) {
		secretPrefix = secretPrefix[:len(APIKeyPrefix)]
	}
	return apiKeyCreationContract{
		Key: apiKeyContract{
			UserID:          result.Key.UserID,
			Name:            result.Key.Name,
			Scopes:          result.Key.Scopes,
			CreatedByUserID: result.Key.CreatedByUserID,
		},
		SecretPrefix:    secretPrefix,
		BearerState:     observeAPIKeyBearer(result.Secret),
		LastUsedTracked: result.Key.LastUsedAt != nil,
	}
}

func matchAPIKeyCreation(expected apiKeyCreationContract) types.GomegaMatcher {
	return WithTransform(apiKeyCreationFields, gstruct.MatchAllFields(gstruct.Fields{
		"Key":             Equal(expected.Key),
		"SecretPrefix":    Equal(expected.SecretPrefix),
		"BearerState":     Equal(expected.BearerState),
		"LastUsedTracked": Equal(expected.LastUsedTracked),
	}))
}

func matchAPIKeyVerificationRecord(key *APIKey, user *User) types.GomegaMatcher {
	return WithTransform(func(verification *APIKeyVerification) apiKeyVerificationContract {
		if verification == nil {
			return apiKeyVerificationContract{}
		}
		return apiKeyVerificationContract{
			Key:  verification.Key,
			User: verification.User,
		}
	}, Equal(apiKeyVerificationContract{Key: key, User: user}))
}

func matchAuthToken(expected authTokenContract) types.GomegaMatcher {
	return WithTransform(func(token *AuthToken) authTokenContract {
		if token == nil || token.User == nil {
			return authTokenContract{}
		}
		return authTokenContract{
			Token:        token.Token,
			RefreshToken: token.RefreshToken,
			UserID:       token.User.ID,
			Username:     token.User.Username,
			Email:        token.User.Email,
			Role:         token.User.Role,
		}
	}, gstruct.MatchAllFields(gstruct.Fields{
		"Token":        Equal(expected.Token),
		"RefreshToken": Equal(expected.RefreshToken),
		"UserID":       Equal(expected.UserID),
		"Username":     Equal(expected.Username),
		"Email":        Equal(expected.Email),
		"Role":         Equal(expected.Role),
	}))
}

func observeAuthErrorCause(err error, cause error) authErrorCauseObservation {
	if errors.Is(err, cause) {
		return authErrorCauseObservation{State: authErrorCausePresent, Cause: cause}
	}
	return authErrorCauseObservation{State: authErrorCauseMissing, Cause: cause}
}

func matchAuthErrorCause(cause error) types.GomegaMatcher {
	return WithTransform(func(err error) authErrorCauseObservation {
		return observeAuthErrorCause(err, cause)
	}, Equal(authErrorCauseObservation{State: authErrorCausePresent, Cause: cause}))
}

func observeRoleAcceptance(role string) roleAcceptance {
	if IsValidRole(role) {
		return roleAccepted
	}
	return roleRejected
}

func matchRoleAcceptance(role string, state roleAcceptance) types.GomegaMatcher {
	return WithTransform(func(struct{}) roleAcceptance {
		return observeRoleAcceptance(role)
	}, Equal(state))
}

func observeAPIKeyBearer(token string) apiKeyBearerState {
	if IsAPIKeyBearer(token) {
		return apiKeyBearerAccepted
	}
	return apiKeyBearerRejected
}

func matchAPIKeyBearer(token string, state apiKeyBearerState) types.GomegaMatcher {
	return WithTransform(func(struct{}) apiKeyBearerState {
		return observeAPIKeyBearer(token)
	}, Equal(state))
}

func authUnitPasswordHash(password string) string {
	ginkgo.GinkgoHelper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	Expect(err).To(Succeed())
	return string(hash)
}

func authUnitFillRandom(buf []byte) (int, error) {
	for i := range buf {
		buf[i] = byte(i + 1)
	}
	return len(buf), nil
}

func newFixtureRawAPIKey(keyID APIKeyID, secret string) string {
	return APIKeyPrefix + fmt.Sprint(keyID) + "_" + secret
}

func newFixtureGeneratedUniqueID[T ~string](raw T) string {
	return string(raw)
}

func newFixtureJWTAccessClaims(userID UserID) jwt.MapClaims {
	return jwt.MapClaims{string(jwtClaimSubject): fmt.Sprint(userID)}
}

func newFixtureJWTRefreshClaims(userID UserID, sessionID SessionID) jwt.MapClaims {
	return jwt.MapClaims{
		string(jwtClaimType):    "refresh",
		string(jwtClaimSubject): fmt.Sprint(userID),
		string(jwtClaimID):      fmt.Sprint(sessionID),
	}
}

func newFixtureJWTRefreshSubjectClaims(userID UserID) jwt.MapClaims {
	return jwt.MapClaims{
		string(jwtClaimType):    "refresh",
		string(jwtClaimSubject): fmt.Sprint(userID),
	}
}

func matchJWTSubject(userID UserID) types.GomegaMatcher {
	return WithTransform(func(claims jwt.MapClaims) UserID {
		raw, _ := claims[string(jwtClaimSubject)].(string)
		return UserIDFromString(raw)
	}, Equal(userID))
}

func authUnitServiceWithUser(user *User) *AuthService {
	ginkgo.GinkgoHelper()

	service := NewAuthService(
		&UserService{store: &UserStore{}},
		&SessionStore{},
		"0123456789abcdef0123456789abcdef",
		time.Minute,
		time.Hour,
	)
	setAuthSeam(&authUserStoreGetUserByUsername, func(*UserStore, string) (*User, error) {
		if user == nil {
			return nil, ErrUserNotFound
		}
		return user, nil
	})
	setAuthSeam(&authUserStoreGetUserByEmail, func(*UserStore, string) (*User, error) {
		if user == nil {
			return nil, ErrUserNotFound
		}
		return user, nil
	})
	setAuthSeam(&authUserStoreGetUserByID, func(*UserStore, UserID) (*User, error) {
		if user == nil {
			return nil, ErrUserNotFound
		}
		return user, nil
	})
	setAuthSeam(&authRandRead, authUnitFillRandom)
	setAuthSeam(&authSignJWT, func(*jwt.Token, []byte) (string, error) {
		return "signed-token", nil
	})
	setAuthSeam(&authSessionStoreCreateSession, func(*SessionStore, SessionID, UserID, string, time.Time) error {
		return nil
	})
	return service
}
