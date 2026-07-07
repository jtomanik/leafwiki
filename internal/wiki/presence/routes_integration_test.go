package presence

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	coreauth "github.com/perber/wiki/internal/core/auth"
)

var _ = ginkgo.Describe("presence HTTP route page context", ginkgo.Label("integration"), func() {
	ginkgo.It("records page references resolved by page identity and route path", func() {
		gin.SetMode(gin.TestMode)
		treeService, pageID := setupPresenceTree()
		registry := NewWebPresenceRegistry(time.Minute, nil)
		routes := NewRoutes(RoutesConfig{Registry: registry, TreeService: treeService})
		router := gin.New()
		router.POST("/heartbeat", authenticatedPresenceHeartbeat(routes, newPresenceRouteUser()))

		byID := postPresenceHeartbeat(router, `{"sessionId":"tab-by-id","mode":"view","pageId":"`+pageID.MetadataValue()+`"}`)
		Expect(byID).To(SatisfyAll(
			HaveHTTPStatus(http.StatusOK),
			HaveHTTPBody(MatchJSON(`{"ok":true}`)),
		))
		byPath := postPresenceHeartbeat(router, `{"sessionId":"tab-by-path","mode":"edit","path":" /docs/ ","dirty":true}`)
		Expect(byPath).To(SatisfyAll(
			HaveHTTPStatus(http.StatusOK),
			HaveHTTPBody(MatchJSON(`{"ok":true}`)),
		))

		sessions := registry.List(&coreauth.User{Role: coreauth.RoleAdmin})
		Expect(sessions).To(ConsistOf(
			SatisfyAll(
				matchPresenceSession(gstruct.Fields{"SessionID": Equal(WebSessionIDFromString("tab-by-id"))}),
				matchPresenceSession(gstruct.Fields{"Page": gstruct.PointTo(matchPresencePageRef(gstruct.Fields{
					"ID":    Equal(*pageID),
					"Path":  Equal("/docs"),
					"Title": Equal("Docs"),
				}))}),
			),
			SatisfyAll(
				matchPresenceSession(gstruct.Fields{"SessionID": Equal(WebSessionIDFromString("tab-by-path"))}),
				matchPresenceSessionEditWithUnsavedChanges(),
				matchPresenceSession(gstruct.Fields{"Page": gstruct.PointTo(matchPresencePageRef(gstruct.Fields{
					"ID":    Equal(*pageID),
					"Path":  Equal("/docs"),
					"Title": Equal("Docs"),
				}))}),
			),
		))
	})

	ginkgo.It("records heartbeats without page context when route paths cannot be resolved", func() {
		gin.SetMode(gin.TestMode)
		treeService, _ := setupPresenceTree()
		registry := NewWebPresenceRegistry(time.Minute, nil)
		routes := NewRoutes(RoutesConfig{Registry: registry, TreeService: treeService})
		router := gin.New()
		router.POST("/heartbeat", authenticatedPresenceHeartbeat(routes, newPresenceRouteUser()))

		rec := postPresenceHeartbeat(router, `{"sessionId":"tab-missing-page","mode":"view","path":"bad\\path"}`)
		Expect(rec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusOK),
			HaveHTTPBody(MatchJSON(`{"ok":true}`)),
		))

		Expect(registry.List(&coreauth.User{Role: coreauth.RoleAdmin})).To(HaveExactElements(
			matchPresenceSession(gstruct.Fields{
				"SessionID": Equal(WebSessionIDFromString("tab-missing-page")),
				"Page":      BeNil(),
			}),
		))
	})
})

func authenticatedPresenceHeartbeat(routes *Routes, user *coreauth.User) gin.HandlerFunc {
	ginkgo.GinkgoHelper()
	return func(c *gin.Context) {
		c.Set("user", user)
		routes.handleHeartbeat(c)
	}
}

func postPresenceHeartbeat(router *gin.Engine, body string) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/heartbeat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return rec
}
