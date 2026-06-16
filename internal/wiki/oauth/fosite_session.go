package oauth

import "github.com/ory/fosite"

func newFositeSession(userID, username string) *fosite.DefaultSession {
	return &fosite.DefaultSession{
		Subject:  userID,
		Username: username,
		Extra:    map[string]interface{}{},
	}
}
