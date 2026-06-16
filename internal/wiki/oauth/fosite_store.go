package oauth

import (
	"context"
	"sync"
	"time"

	"github.com/ory/fosite"
	fositeoauth2 "github.com/ory/fosite/handler/oauth2"
	"github.com/ory/fosite/handler/pkce"
)

type fositeStore struct {
	mu                  sync.RWMutex
	clients             map[string]*fosite.DefaultClient
	authorizeCodes      map[string]storedAuthorizeCode
	pkceRequests        map[string]fosite.Requester
	accessTokens        map[string]fosite.Requester
	requestIDsToAccess  map[string]string
	refreshTokens       map[string]storedRefreshToken
	requestIDsToRefresh map[string]string
	revokedRequestIDs   map[string]bool
	clientAssertionJTIs map[string]time.Time
}

type storedAuthorizeCode struct {
	requester fosite.Requester
	active    bool
}

type storedRefreshToken struct {
	requester       fosite.Requester
	accessSignature string
	active          bool
}

func newFositeStore() *fositeStore {
	return &fositeStore{
		clients:             map[string]*fosite.DefaultClient{},
		authorizeCodes:      map[string]storedAuthorizeCode{},
		pkceRequests:        map[string]fosite.Requester{},
		accessTokens:        map[string]fosite.Requester{},
		requestIDsToAccess:  map[string]string{},
		refreshTokens:       map[string]storedRefreshToken{},
		requestIDsToRefresh: map[string]string{},
		revokedRequestIDs:   map[string]bool{},
		clientAssertionJTIs: map[string]time.Time{},
	}
}

func (s *fositeStore) setClient(client oauthClient) error {
	if s == nil {
		return fosite.ErrServerError
	}
	fositeClient := client.fositeClient()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[fositeClient.GetID()] = fositeClient
	return nil
}

func (s *fositeStore) GetClient(ctx context.Context, id string) (fosite.Client, error) {
	if s == nil {
		return nil, fosite.ErrNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	client, ok := s.clients[id]
	if !ok {
		return nil, fosite.ErrNotFound
	}
	if redirectURI, ok := fixedClientRedirectFromContext(ctx, id); ok {
		copied := *client
		copied.RedirectURIs = []string{redirectURI}
		return &copied, nil
	}
	return client, nil
}

func fixedClientRedirectFromContext(ctx context.Context, id string) (string, bool) {
	if id != ClientID {
		return "", false
	}
	redirectURI, ok := ctx.Value(fixedClientRedirectContextKey{}).(string)
	return redirectURI, ok && redirectURI != ""
}

func (s *fositeStore) ClientAssertionJWTValid(_ context.Context, jti string) error {
	if s == nil {
		return fosite.ErrNotFound
	}
	now := time.Now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	exp, ok := s.clientAssertionJTIs[jti]
	if ok && exp.After(now) {
		return fosite.ErrJTIKnown
	}
	return nil
}

func (s *fositeStore) SetClientAssertionJWT(_ context.Context, jti string, exp time.Time) error {
	if s == nil {
		return fosite.ErrServerError
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, value := range s.clientAssertionJTIs {
		if !value.After(now) {
			delete(s.clientAssertionJTIs, key)
		}
	}
	if existing, ok := s.clientAssertionJTIs[jti]; ok && existing.After(now) {
		return fosite.ErrJTIKnown
	}
	s.clientAssertionJTIs[jti] = exp
	return nil
}

func (s *fositeStore) CreateAuthorizeCodeSession(_ context.Context, code string, request fosite.Requester) error {
	if s == nil {
		return fosite.ErrServerError
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authorizeCodes[code] = storedAuthorizeCode{requester: request, active: true}
	return nil
}

func (s *fositeStore) GetAuthorizeCodeSession(_ context.Context, code string, _ fosite.Session) (fosite.Requester, error) {
	if s == nil {
		return nil, fosite.ErrNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	stored, ok := s.authorizeCodes[code]
	if !ok {
		return nil, fosite.ErrNotFound
	}
	if !stored.active {
		return stored.requester, fosite.ErrInvalidatedAuthorizeCode
	}
	return stored.requester, nil
}

func (s *fositeStore) InvalidateAuthorizeCodeSession(_ context.Context, code string) error {
	if s == nil {
		return fosite.ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.authorizeCodes[code]
	if !ok {
		return fosite.ErrNotFound
	}
	stored.active = false
	s.authorizeCodes[code] = stored
	return nil
}

func (s *fositeStore) CreatePKCERequestSession(_ context.Context, signature string, request fosite.Requester) error {
	if s == nil {
		return fosite.ErrServerError
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pkceRequests[signature] = request
	return nil
}

func (s *fositeStore) GetPKCERequestSession(_ context.Context, signature string, _ fosite.Session) (fosite.Requester, error) {
	if s == nil {
		return nil, fosite.ErrNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	request, ok := s.pkceRequests[signature]
	if !ok {
		return nil, fosite.ErrNotFound
	}
	return request, nil
}

func (s *fositeStore) DeletePKCERequestSession(_ context.Context, signature string) error {
	if s == nil {
		return fosite.ErrServerError
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pkceRequests, signature)
	return nil
}

func (s *fositeStore) CreateAccessTokenSession(_ context.Context, signature string, request fosite.Requester) error {
	if s == nil {
		return fosite.ErrServerError
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accessTokens[signature] = request
	s.requestIDsToAccess[request.GetID()] = signature
	return nil
}

func (s *fositeStore) GetAccessTokenSession(_ context.Context, signature string, _ fosite.Session) (fosite.Requester, error) {
	if s == nil {
		return nil, fosite.ErrNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	request, ok := s.accessTokens[signature]
	if !ok {
		return nil, fosite.ErrNotFound
	}
	return request, nil
}

func (s *fositeStore) DeleteAccessTokenSession(_ context.Context, signature string) error {
	if s == nil {
		return fosite.ErrServerError
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.accessTokens, signature)
	for requestID, storedSignature := range s.requestIDsToAccess {
		if storedSignature == signature {
			delete(s.requestIDsToAccess, requestID)
		}
	}
	return nil
}

func (s *fositeStore) CreateRefreshTokenSession(_ context.Context, signature string, accessSignature string, request fosite.Requester) error {
	if s == nil {
		return fosite.ErrServerError
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshTokens[signature] = storedRefreshToken{
		requester:       request,
		accessSignature: accessSignature,
		active:          true,
	}
	s.requestIDsToRefresh[request.GetID()] = signature
	return nil
}

func (s *fositeStore) GetRefreshTokenSession(_ context.Context, signature string, _ fosite.Session) (fosite.Requester, error) {
	if s == nil {
		return nil, fosite.ErrNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	stored, ok := s.refreshTokens[signature]
	if !ok {
		return nil, fosite.ErrNotFound
	}
	if !stored.active {
		return stored.requester, fosite.ErrInactiveToken
	}
	return stored.requester, nil
}

func (s *fositeStore) DeleteRefreshTokenSession(_ context.Context, signature string) error {
	if s == nil {
		return fosite.ErrServerError
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.refreshTokens, signature)
	for requestID, storedSignature := range s.requestIDsToRefresh {
		if storedSignature == signature {
			delete(s.requestIDsToRefresh, requestID)
		}
	}
	return nil
}

func (s *fositeStore) RevokeRefreshToken(_ context.Context, requestID string) error {
	if s == nil {
		return fosite.ErrServerError
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	signature, ok := s.requestIDsToRefresh[requestID]
	if !ok {
		return nil
	}
	stored, ok := s.refreshTokens[signature]
	if !ok {
		return fosite.ErrNotFound
	}
	stored.active = false
	s.refreshTokens[signature] = stored
	s.revokedRequestIDs[requestID] = true
	return nil
}

func (s *fositeStore) RevokeAccessToken(_ context.Context, requestID string) error {
	if s == nil {
		return fosite.ErrServerError
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	signature, ok := s.requestIDsToAccess[requestID]
	if !ok {
		return nil
	}
	delete(s.accessTokens, signature)
	s.revokedRequestIDs[requestID] = true
	return nil
}

func (s *fositeStore) RotateRefreshToken(_ context.Context, requestID string, signature string) error {
	if s == nil {
		return fosite.ErrServerError
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, ok := s.refreshTokens[signature]
	if !ok {
		return fosite.ErrNotFound
	}
	if !stored.active {
		return fosite.ErrInactiveToken
	}
	if stored.requester.GetID() != requestID {
		return fosite.ErrInactiveToken
	}

	stored.active = false
	s.refreshTokens[signature] = stored
	if currentSignature, ok := s.requestIDsToRefresh[requestID]; ok && currentSignature == signature {
		delete(s.requestIDsToRefresh, requestID)
	}
	if stored.accessSignature != "" {
		delete(s.accessTokens, stored.accessSignature)
		if currentAccessSignature, ok := s.requestIDsToAccess[requestID]; ok && currentAccessSignature == stored.accessSignature {
			delete(s.requestIDsToAccess, requestID)
		}
	}
	s.revokedRequestIDs[requestID] = true
	return nil
}

var _ fosite.Storage = (*fositeStore)(nil)
var _ fositeoauth2.CoreStorage = (*fositeStore)(nil)
var _ fositeoauth2.TokenRevocationStorage = (*fositeStore)(nil)
var _ pkce.PKCERequestStorage = (*fositeStore)(nil)
