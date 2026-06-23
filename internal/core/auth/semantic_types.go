package auth

import "github.com/perber/wiki/internal/core/identity"

type UserID = identity.UserID

func NewUserIDUnchecked(raw string) UserID {
	return identity.NewUserIDUnchecked(raw)
}

type APIKeyID string

func (id APIKeyID) String() string {
	return string(id)
}

func NewAPIKeyIDUnchecked(raw string) APIKeyID {
	return APIKeyID(raw)
}

type SessionID string

func (id SessionID) String() string {
	return string(id)
}

func NewSessionIDUnchecked(raw string) SessionID {
	return SessionID(raw)
}
