package presence

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/http/middleware/security"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = ginkgo.Describe("presence route handlers", ginkgo.Label("unit"), func() {
	ginkgo.It("registers the heartbeat and session removal endpoints when a registry is available", func() {
		gin.SetMode(gin.TestMode)
		engine := gin.New()

		NewRoutes(RoutesConfig{Registry: NewWebPresenceRegistry(time.Minute, nil)}).RegisterRoutes(httpinternal.RouterContext{
			Base:       engine.Group(""),
			CSRFCookie: security.NewCSRFCookie(true, time.Hour),
			Opts:       httpinternal.RouterOptions{AuthDisabled: true},
		})

		Expect(presenceRegisteredRoutes(engine)).To(exposePresenceRouteContract())
	})

	ginkgo.It("does not register endpoints when the registry is unavailable", func() {
		gin.SetMode(gin.TestMode)
		engine := gin.New()
		var routes *Routes

		routes.RegisterRoutes(httpinternal.RouterContext{Base: engine.Group("")})
		NewRoutes(RoutesConfig{}).RegisterRoutes(httpinternal.RouterContext{Base: engine.Group("")})

		Expect(presenceRegisteredRoutes(engine)).To(BeEmpty())
	})

	ginkgo.It("records authenticated heartbeat requests", func() {
		registry := NewWebPresenceRegistry(time.Minute, nil)
		ctx, rec := newPresenceUnitContext(http.MethodPost, "/api/presence/heartbeat", `{"sessionId":"tab-1","mode":"view"}`)
		ctx.Set("user", newPresenceRouteUser())

		NewRoutes(RoutesConfig{Registry: registry}).handleHeartbeat(ctx)

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusOK),
			HaveHTTPBody(MatchJSON(`{"ok":true}`)),
		))
		Expect(registry.List(&coreauth.User{Role: coreauth.RoleAdmin})).To(HaveExactElements(matchPresenceSession(gstruct.Fields{
			"SessionID": Equal(WebSessionIDFromString("tab-1")),
		})))
	})

	ginkgo.It("returns structured heartbeat request errors", func() {
		malformedCtx, malformedRec := newPresenceUnitContext(http.MethodPost, "/api/presence/heartbeat", `{"sessionId":`)
		malformedCtx.Set("user", newPresenceRouteUser())
		NewRoutes(RoutesConfig{Registry: NewWebPresenceRegistry(time.Minute, nil)}).handleHeartbeat(malformedCtx)
		Expect(malformedRec).To(matchPresenceStructuredError(http.StatusBadRequest, ErrCodePresenceInvalidRequest), malformedRec.Body.String())

		invalidCtx, invalidRec := newPresenceUnitContext(http.MethodPost, "/api/presence/heartbeat", `{"sessionId":"","mode":"view"}`)
		invalidCtx.Set("user", newPresenceRouteUser())
		NewRoutes(RoutesConfig{Registry: NewWebPresenceRegistry(time.Minute, nil)}).handleHeartbeat(invalidCtx)
		Expect(invalidRec).To(matchPresenceStructuredError(http.StatusBadRequest, ErrCodePresenceSessionIDRequired), invalidRec.Body.String())
	})

	ginkgo.It("forbids heartbeat and session removal without authenticated users", func() {
		heartbeatCtx, heartbeatRec := newPresenceUnitContext(http.MethodPost, "/api/presence/heartbeat", `{"sessionId":"tab-1","mode":"view"}`)
		NewRoutes(RoutesConfig{Registry: NewWebPresenceRegistry(time.Minute, nil)}).handleHeartbeat(heartbeatCtx)
		Expect(heartbeatRec).To(HaveHTTPStatus(http.StatusForbidden), heartbeatRec.Body.String())

		deleteCtx, deleteRec := newPresenceUnitContext(http.MethodDelete, "/api/presence/session/tab-1", "")
		deleteCtx.Params = gin.Params{{Key: "id", Value: "tab-1"}}
		NewRoutes(RoutesConfig{Registry: NewWebPresenceRegistry(time.Minute, nil)}).handleDeleteSession(deleteCtx)
		Expect(deleteRec).To(HaveHTTPStatus(http.StatusForbidden), deleteRec.Body.String())
	})

	ginkgo.It("removes authenticated sessions by trimmed session identity", func() {
		registry := NewWebPresenceRegistry(time.Minute, nil)
		user := newPresenceRouteUser()
		Expect(registry.Record(Heartbeat{SessionID: WebSessionIDFromString("tab-1"), Mode: SessionModeView}, user, nil)).To(Succeed())
		ctx, rec := newPresenceUnitContext(http.MethodDelete, "/api/presence/session/tab-1", "")
		ctx.Set("user", user)
		ctx.Params = gin.Params{{Key: "id", Value: " tab-1 "}}

		NewRoutes(RoutesConfig{Registry: registry}).handleDeleteSession(ctx)

		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusOK),
			HaveHTTPBody(MatchJSON(`{"ok":true}`)),
		))
		Expect(registry.List(&coreauth.User{Role: coreauth.RoleAdmin})).To(BeEmpty())
	})
})

var _ = ginkgo.Describe("presence error responses", ginkgo.Label("unit"), func() {
	ginkgo.It("writes localized and generic record failures as structured invalid requests", func() {
		localized := newPresenceErrorResponseRecorder(func(ctx *gin.Context) {
			writePresenceRecordError(ctx, sharederrors.NewLocalizedErrorFromCode(ErrCodePresenceModeInvalid, nil))
		})
		Expect(localized).To(matchPresenceStructuredError(http.StatusBadRequest, ErrCodePresenceModeInvalid), localized.Body.String())

		generic := newPresenceErrorResponseRecorder(func(ctx *gin.Context) {
			writePresenceRecordError(ctx, errors.New("record failed"))
		})
		Expect(generic).To(matchPresenceStructuredError(http.StatusBadRequest, ErrCodePresenceInvalidRequest), generic.Body.String())
	})
})

var _ = ginkgo.Describe("presence semantic values", ginkgo.Label("unit"), func() {
	ginkgo.It("normalizes session modes from blank known and unknown strings", func() {
		Expect(SessionModeFromString("")).To(WithTransform(observeSessionMode, Equal(sessionModeBlank)))
		Expect(SessionModeFromString(" edit ")).To(Equal(SessionModeEdit))
		Expect(SessionModeFromString("preview")).To(Equal(sessionModeInvalid))
	})

	ginkgo.It("serializes and parses web session identifiers", func() {
		sessionID := WebSessionIDFromString(" tab-1 ")

		raw, err := json.Marshal(sessionID)
		Expect(err).To(Succeed())
		Expect(raw).To(MatchJSON(`"tab-1"`))
		Expect(sessionID).To(Equal(WebSessionIDFromString("tab-1")))
		Expect(sessionID).To(WithTransform(roundTripWebSessionIDRouteValue, Equal(sessionID)))

		var parsed WebSessionID
		Expect(json.Unmarshal([]byte(`" tab-2 "`), &parsed)).To(Succeed())
		Expect(parsed).To(Equal(WebSessionIDFromString("tab-2")))

		Expect(json.Unmarshal([]byte(`42`), &parsed)).To(BeAssignableToTypeOf(&json.UnmarshalTypeError{}))
	})
})

type sessionModeObservation string

const (
	sessionModeBlank sessionModeObservation = "blank"
	sessionModeNamed sessionModeObservation = "named"
)

func observeSessionMode(mode SessionMode) sessionModeObservation {
	if mode == "" {
		return sessionModeBlank
	}
	return sessionModeNamed
}

func roundTripWebSessionIDRouteValue(sessionID WebSessionID) WebSessionID {
	return WebSessionIDFromString(fmt.Sprint(sessionID))
}

func newPresenceUnitContext(method string, target string, body string) (*gin.Context, *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	return ctx, rec
}

func newPresenceErrorResponseRecorder(write func(*gin.Context)) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()

	ctx, rec := newPresenceUnitContext(http.MethodPost, "/api/presence/heartbeat", "")
	write(ctx)
	return rec
}

func newPresenceRouteUser() *coreauth.User {
	return &coreauth.User{ID: newFixtureUserID("editor-1"), Username: "Editor One", Role: coreauth.RoleEditor}
}

func presenceRegisteredRoutes(engine *gin.Engine) []string {
	routes := engine.Routes()
	out := make([]string, 0, len(routes))
	for _, route := range routes {
		out = append(out, route.Method+" "+route.Path)
	}
	return out
}

func exposePresenceRouteContract() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return ConsistOf(
		"POST /api/presence/heartbeat",
		"DELETE /api/presence/session/:id",
	)
}

func matchPresenceStructuredError(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return testmatchers.HaveHTTPStructuredError(status, code, sharederrors.MessageIDForCode(code))
}
