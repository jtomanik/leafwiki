package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/shared/sqliteutil"
)

const (
	APIKeyPrefix   = "lwk_"
	MCPAPIKeyScope = "leafwiki:mcp"
)

type APIKeyCreateResult struct {
	Key    *APIKey `json:"key"`
	Secret string  `json:"secret"`
}

type APIKeyVerification struct {
	Key  *APIKey
	User *User
}

type APIKeyService struct {
	store *APIKeyStore
	users *UserService
	now   func() time.Time
}

func NewAPIKeyService(store *APIKeyStore, users *UserService) *APIKeyService {
	return &APIKeyService{
		store: store,
		users: users,
		now:   time.Now,
	}
}

func (s *APIKeyService) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

func (s *APIKeyService) CreateAPIKey(userID UserID, name string, createdByUserID UserID) (*APIKeyCreateResult, error) {
	if s == nil || s.store == nil || s.users == nil {
		return nil, ErrAPIKeyNotFound
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrAPIKeyInvalidName
	}
	if _, err := s.users.GetUserByID(userID); err != nil {
		return nil, err
	}
	if _, err := s.users.GetUserByID(createdByUserID); err != nil {
		return nil, err
	}

	id, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	secretPart, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	raw := APIKeyPrefix + id + "_" + secretPart
	key := &APIKey{
		ID:              APIKeyIDFromString(id),
		UserID:          userID,
		Name:            name,
		Prefix:          APIKeyPrefix + id,
		Last4:           raw[len(raw)-4:],
		Scopes:          []string{MCPAPIKeyScope},
		CreatedByUserID: createdByUserID,
		CreatedAt:       s.now().UTC(),
	}
	if err := s.store.CreateAPIKey(key, hashAPIKey(raw)); err != nil {
		return nil, err
	}
	return &APIKeyCreateResult{Key: key, Secret: raw}, nil
}

func (s *APIKeyService) ListAPIKeys(userID UserID) ([]*APIKey, error) {
	if s == nil || s.store == nil || s.users == nil {
		return nil, ErrAPIKeyNotFound
	}
	if _, err := s.users.GetUserByID(userID); err != nil {
		return nil, err
	}
	return s.store.ListActiveAPIKeys(userID)
}

func (s *APIKeyService) RevokeAPIKey(userID UserID, keyID APIKeyID) error {
	if s == nil || s.store == nil || s.users == nil {
		return ErrAPIKeyNotFound
	}
	if _, err := s.users.GetUserByID(userID); err != nil {
		return err
	}
	return s.store.RevokeAPIKey(userID, keyID, s.now().UTC())
}

func (s *APIKeyService) VerifyAPIKey(raw string) (*APIKeyVerification, error) {
	if s == nil || s.store == nil || s.users == nil || s.users.store == nil {
		return nil, ErrInvalidToken
	}
	keyID, err := parseAPIKeyID(raw)
	if err != nil {
		return nil, ErrInvalidToken
	}
	stored, err := retryAPIKeyTransientLocks(func() (*storedAPIKey, error) {
		return s.store.GetAPIKeyByID(keyID)
	})
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	if stored.key.RevokedAt != nil {
		return nil, ErrInvalidToken
	}
	if subtle.ConstantTimeCompare([]byte(hashAPIKey(raw)), []byte(stored.secretHash)) != 1 {
		return nil, ErrInvalidToken
	}
	user, err := retryAPIKeyTransientLocks(func() (*User, error) {
		return s.users.store.GetUserByID(stored.key.UserID)
	})
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	usedAt := s.now().UTC()
	if err := s.markAPIKeyUsedWithRetry(stored.key.ID, usedAt); err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	stored.key.LastUsedAt = &usedAt
	return &APIKeyVerification{Key: stored.key, User: user}, nil
}

func (s *APIKeyService) markAPIKeyUsedWithRetry(keyID APIKeyID, usedAt time.Time) error {
	_, err := retryAPIKeyTransientLocks(func() (struct{}, error) {
		return struct{}{}, s.store.MarkAPIKeyUsed(keyID, usedAt)
	})
	return err
}

func retryAPIKeyTransientLocks[T any](operation func() (T, error)) (T, error) {
	const (
		maxAttempts = 100
		delay       = 20 * time.Millisecond
	)
	var zero T
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		result, err := operation()
		if err == nil {
			return result, nil
		}
		if !sqliteutil.IsSQLiteTransientLockError(err) {
			return zero, err
		}
		lastErr = err
		time.Sleep(delay)
	}
	return zero, lastErr
}

func IsAPIKeyBearer(token string) bool {
	return strings.HasPrefix(token, APIKeyPrefix)
}

func parseAPIKeyID(raw string) (APIKeyID, error) {
	if !strings.HasPrefix(raw, APIKeyPrefix) {
		return "", fmt.Errorf("missing api key prefix")
	}
	rest := strings.TrimPrefix(raw, APIKeyPrefix)
	id, secret, ok := strings.Cut(rest, "_")
	if !ok || id == "" || secret == "" {
		return "", fmt.Errorf("malformed api key")
	}
	return APIKeyIDFromString(id), nil
}

func hashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func randomHex(byteCount int) (string, error) {
	buf := make([]byte, byteCount)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
