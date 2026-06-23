package oauth

import (
	"github.com/gin-gonic/gin"
	"github.com/ory/fosite"
	coreauth "github.com/perber/wiki/internal/core/auth"
)

func (r *Routes) handleToken(c *gin.Context) {
	request, err := r.service.fositeProvider.NewAccessRequest(c.Request.Context(), c.Request, newFositeSession("", ""))
	if err != nil {
		writeTokenError(c, err)
		return
	}
	if err := r.service.validateTokenSubject(request); err != nil {
		writeTokenError(c, err)
		return
	}
	response, err := r.service.fositeProvider.NewAccessResponse(c.Request.Context(), request)
	if err != nil {
		writeTokenError(c, err)
		return
	}
	writeTokenResponse(c, response)
}

func (s *Service) validateTokenSubject(request fosite.AccessRequester) error {
	if s == nil || s.users == nil || request == nil || request.GetSession() == nil {
		return fosite.ErrInvalidGrant
	}
	if _, err := s.users.GetUserByID(coreauth.NewUserIDUnchecked(request.GetSession().GetSubject())); err != nil {
		return fosite.ErrInvalidGrant
	}
	return nil
}
