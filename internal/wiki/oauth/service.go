package oauth

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"sync"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/ory/fosite"
	coreauth "github.com/perber/wiki/internal/core/auth"
)

const (
	ClientID    = "leafwiki-local-mcp"
	ScopeMCP    = "leafwiki:mcp"
	approvalTTL = 10 * time.Minute
)

// Service owns the local in-memory OAuth server used by authenticated MCP.
type Service struct {
	auth           *coreauth.AuthService
	users          *coreauth.UserService
	fositeConfig   *fosite.Config
	fositeProvider fosite.OAuth2Provider
	store          *fositeStore
	clientsMu      sync.RWMutex
	clients        map[string]registeredClient
	approvalMu     sync.Mutex
	approvals      map[string]oauthApproval
}

type ServiceConfig struct {
	AuthService         *coreauth.AuthService
	UserService         *coreauth.UserService
	AccessTokenTimeout  time.Duration
	RefreshTokenTimeout time.Duration
}

func NewService(cfg ServiceConfig) (*Service, error) {
	fositeSecret, err := randomFositeSecret()
	if err != nil {
		return nil, err
	}
	fositeStore := newFositeStore()
	if err := fositeStore.setClient(fixedOAuthClient()); err != nil {
		return nil, fmt.Errorf("register fixed oauth client: %w", err)
	}
	fositeConfig := newFositeConfig(cfg, fositeSecret)
	fositeProvider := newFositeProvider(fositeConfig, fositeStore)

	service := &Service{
		auth:           cfg.AuthService,
		users:          cfg.UserService,
		fositeConfig:   fositeConfig,
		fositeProvider: fositeProvider,
		store:          fositeStore,
		clients: map[string]registeredClient{
			ClientID: {
				ClientName:    "LeafWiki local MCP",
				GrantTypes:    []string{string(fosite.GrantTypeAuthorizationCode), string(fosite.GrantTypeRefreshToken)},
				ResponseTypes: []string{responseTypeCode},
				Scope:         ScopeMCP,
			},
		},
		approvals: map[string]oauthApproval{},
	}

	return service, nil
}

func randomFositeSecret() ([]byte, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("create fosite oauth secret: %w", err)
	}
	return secret, nil
}

func (s *Service) VerifyBearerToken(ctx context.Context, token string, req *http.Request) (*sdkauth.TokenInfo, error) {
	if s == nil || s.fositeProvider == nil || s.users == nil {
		return nil, fmt.Errorf("%w: oauth unavailable", sdkauth.ErrInvalidToken)
	}

	session := newFositeSession("", "")
	tokenUse, requester, err := s.fositeProvider.IntrospectToken(ctx, token, fosite.AccessToken, session, ScopeMCP)
	if err != nil || requester == nil {
		return nil, fmt.Errorf("%w: %v", sdkauth.ErrInvalidToken, err)
	}
	if tokenUse != fosite.AccessToken {
		return nil, fmt.Errorf("%w: oauth bearer must be an access token", sdkauth.ErrInvalidToken)
	}

	user, err := s.users.GetUserByID(coreauth.NewUserIDUnchecked(requester.GetSession().GetSubject()))
	if err != nil {
		return nil, fmt.Errorf("%w: user not found", sdkauth.ErrInvalidToken)
	}

	return &sdkauth.TokenInfo{
		UserID:     user.ID,
		Scopes:     []string(requester.GetGrantedScopes()),
		Expiration: requester.GetSession().GetExpiresAt(fosite.AccessToken),
	}, nil
}

func (s *Service) client(clientID string) (registeredClient, bool) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	client, ok := s.clients[clientID]
	return client, ok
}
