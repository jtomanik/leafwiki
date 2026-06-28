package presence

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/http/middleware/security"
)

var _ = ginkgo.Describe("web presence registry", func() {
	ginkgo.It("TestWebPresenceRegistryListGatesEmailAndExpires", func() {
		now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
		registry := NewWebPresenceRegistry(time.Minute, func() time.Time { return now })

		err := registry.Record(Heartbeat{
			SessionID: "tab-1",
			Mode:      "edit",
			Dirty:     true,
		}, &coreauth.User{
			ID:       "editor-1",
			Username: "Editor One",
			Email:    "editor@example.test",
			Role:     coreauth.RoleEditor,
		}, &PageRef{
			ID:    "page-1",
			Path:  "/docs/api",
			Title: "API",
		})
		Expect(err).NotTo(HaveOccurred())

		editorView := registry.List(&coreauth.User{Role: coreauth.RoleEditor})
		Expect(editorView).To(HaveLen(1))
		Expect(editorView[0].User.Email).To(BeEmpty())
		Expect(editorView[0].State).To(Equal(SessionStateActive))
		Expect(editorView[0].Dirty).To(BeTrue())
		Expect(editorView[0].Page).NotTo(BeNil())
		Expect(editorView[0].Page.Title).To(Equal("API"))
		Expect(editorView[0].Type).To(Equal(SessionTypeWeb))
		Expect(editorView[0].Mode).To(Equal(SessionModeEdit))

		raw, err := json.Marshal(editorView[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(ContainSubstring(`"type":"web"`))
		Expect(string(raw)).To(ContainSubstring(`"mode":"edit"`))
		Expect(string(raw)).To(ContainSubstring(`"state":"active"`))

		adminView := registry.List(&coreauth.User{Role: coreauth.RoleAdmin})
		Expect(adminView).To(HaveLen(1))
		Expect(adminView[0].User.Email).To(Equal("editor@example.test"))

		now = now.Add(61 * time.Second)
		Expect(registry.List(&coreauth.User{Role: coreauth.RoleAdmin})).To(BeEmpty())
	})

	ginkgo.It("TestWebPresenceRegistryRejectsInvalidHeartbeatWithoutDroppingExistingSession", func() {
		registry := NewWebPresenceRegistry(time.Minute, nil)
		user := &coreauth.User{ID: "editor-1", Username: "Editor One", Role: coreauth.RoleEditor}
		Expect(registry.Record(Heartbeat{SessionID: "tab-1", Mode: "view"}, user, nil)).To(Succeed())

		Expect(registry.Record(Heartbeat{SessionID: "", Mode: "view"}, user, nil)).To(HaveOccurred())
		Expect(registry.Record(Heartbeat{SessionID: "tab-2", Mode: "invalid"}, user, nil)).To(HaveOccurred())
		sessions := registry.List(&coreauth.User{Role: coreauth.RoleAdmin})
		Expect(sessions).To(HaveLen(1))
		Expect(sessions[0].SessionID).To(Equal(WebSessionID("tab-1")))
	})

	ginkgo.DescribeTable("TestWebPresenceRegistryInvalidHeartbeatReturnsStableCodes",
		func(heartbeat Heartbeat, code sharederrors.ErrorCode, messageID sharederrors.MessageID) {
			registry := NewWebPresenceRegistry(time.Minute, nil)
			user := &coreauth.User{ID: "editor-1", Username: "Editor One", Role: coreauth.RoleEditor}

			err := registry.Record(heartbeat, user, nil)
			localized, ok := sharederrors.AsLocalizedError(err)
			Expect(ok).To(BeTrue(), "Record error = %T %v", err, err)
			Expect(localized.Code).To(Equal(code))
			Expect(localized.MessageID).To(Equal(messageID))
		},
		ginkgo.Entry("missing session", Heartbeat{SessionID: "", Mode: "view"}, ErrCodePresenceSessionIDRequired, sharederrors.MessageID("errors.presence.session_id_required")),
		ginkgo.Entry("invalid mode", Heartbeat{SessionID: "tab-1", Mode: "invalid"}, ErrCodePresenceModeInvalid, sharederrors.MessageID("errors.presence.mode_invalid")),
	)

	ginkgo.It("TestWebPresenceRegistryBindsSessionIDToUser", func() {
		registry := NewWebPresenceRegistry(time.Minute, nil)
		editor := &coreauth.User{ID: "editor-1", Username: "Editor One", Role: coreauth.RoleEditor}
		other := &coreauth.User{ID: "editor-2", Username: "Editor Two", Role: coreauth.RoleEditor}
		Expect(registry.Record(Heartbeat{SessionID: "shared-tab", Mode: "view"}, editor, nil)).To(Succeed())

		Expect(registry.Record(Heartbeat{SessionID: "shared-tab", Mode: "edit"}, other, nil)).To(HaveOccurred())
		Expect(registry.Remove("shared-tab", other)).To(BeFalse())

		sessions := registry.List(&coreauth.User{Role: coreauth.RoleAdmin})
		Expect(sessions).To(HaveLen(1))
		Expect(sessions[0].Mode).To(Equal(SessionModeView))
		Expect(registry.Remove("shared-tab", editor)).To(BeTrue())
		Expect(registry.List(&coreauth.User{Role: coreauth.RoleAdmin})).To(BeEmpty())
	})

	ginkgo.It("normalizes heartbeat session, mode, page ID, and page path", func() {
		normalized, err := normalizeHeartbeat(Heartbeat{
			SessionID: " tab-1 ",
			Mode:      "",
			PageID:    " page-1 ",
			Path:      " docs/api/ ",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(normalized.SessionID).To(Equal(WebSessionID("tab-1")))
		Expect(normalized.Mode).To(Equal(SessionModeUnknown))
		Expect(normalized.PageID).To(Equal(tree.PageID("page-1")))
		Expect(normalized.Path).To(Equal("/docs/api"))
		Expect(WebSessionIDFromString(" tab-2 ").String()).To(Equal("tab-2"))
		Expect(normalizePagePath("")).To(BeEmpty())
		Expect(normalizePagePath("/")).To(BeEmpty())
		Expect(normalizePagePath("docs")).To(Equal("/docs"))
	})

	ginkgo.It("covers registry defaults, JSON mode errors, removal misses, and sorted output", func() {
		var mode SessionMode
		Expect(json.Unmarshal([]byte(`{"mode":"view"}`), &mode)).To(HaveOccurred())
		registry := NewWebPresenceRegistry(0, nil)
		Expect(registry.ttl).To(Equal(DefaultWebPresenceTTL))
		Expect(registry.now).NotTo(BeNil())
		user := &coreauth.User{ID: "editor-1", Username: "Editor One", Email: "editor@example.test", Role: coreauth.RoleEditor}

		Expect(registry.Remove("", user)).To(BeFalse())
		Expect(registry.Remove("missing-tab", user)).To(BeFalse())
		Expect(registry.Record(Heartbeat{SessionID: "tab-b", Mode: "view"}, user, nil)).To(Succeed())
		Expect(registry.Record(Heartbeat{SessionID: "tab-a", Mode: "view"}, user, nil)).To(Succeed())

		sessions := registry.List(user)
		Expect(sessions).To(HaveLen(2))
		Expect([]WebSessionID{sessions[0].SessionID, sessions[1].SessionID}).To(Equal([]WebSessionID{"tab-a", "tab-b"}))
		Expect(userRefForUser(user, true).Email).To(Equal("editor@example.test"))
	})

	ginkgo.It("returns stable errors for too-long session IDs, nil registry, and nil user", func() {
		_, err := normalizeHeartbeat(Heartbeat{SessionID: WebSessionID(strings.Repeat("x", 257)), Mode: "view"})
		expectPresenceErrorCode(err, ErrCodePresenceSessionIDTooLong)

		var registry *WebPresenceRegistry
		err = registry.Record(Heartbeat{SessionID: "tab-1", Mode: "view"}, &coreauth.User{ID: "editor-1"}, nil)
		expectPresenceErrorCode(err, ErrCodePresenceRegistryUnavailable)
		Expect(registry.List(&coreauth.User{Role: coreauth.RoleAdmin})).To(BeEmpty())
		Expect(registry.Remove("tab-1", &coreauth.User{ID: "editor-1"})).To(BeFalse())

		err = NewWebPresenceRegistry(time.Minute, nil).Record(Heartbeat{SessionID: "tab-1", Mode: "view"}, nil, nil)
		expectPresenceErrorCode(err, ErrCodePresenceUserRequired)
	})
})

var _ = ginkgo.Describe("presence routes", func() {
	ginkgo.It("registers routes only when a registry is available", func() {
		gin.SetMode(gin.TestMode)
		router := gin.New()
		var nilRoutes *Routes
		nilRoutes.RegisterRoutes(httpinternal.RouterContext{Base: router})
		NewRoutes(RoutesConfig{}).RegisterRoutes(httpinternal.RouterContext{Base: router})

		registry := NewWebPresenceRegistry(time.Minute, nil)
		routes := NewRoutes(RoutesConfig{Registry: registry})
		routes.RegisterRoutes(httpinternal.RouterContext{
			Base:       router,
			CSRFCookie: security.NewCSRFCookie(true, time.Hour),
		})
		registered := map[string]struct{}{}
		for _, route := range router.Routes() {
			registered[route.Method+" "+route.Path] = struct{}{}
		}
		Expect(registered).To(HaveKey("POST /api/presence/heartbeat"))
		Expect(registered).To(HaveKey("DELETE /api/presence/session/:id"))
	})

	ginkgo.It("TestPresenceHeartbeatRouteReturnsStableErrorDetails", func() {
		gin.SetMode(gin.TestMode)
		registry := NewWebPresenceRegistry(time.Minute, nil)
		routes := NewRoutes(RoutesConfig{Registry: registry})
		router := gin.New()
		router.POST("/heartbeat", func(c *gin.Context) {
			c.Set("user", &coreauth.User{ID: "editor-1", Username: "Editor One", Role: coreauth.RoleEditor})
			routes.handleHeartbeat(c)
		})

		req := httptest.NewRequest(http.MethodPost, "/heartbeat", strings.NewReader(`{"sessionId":"","mode":"view"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusBadRequest), rec.Body.String())
		var body struct {
			Error sharederrors.LocalizedErrorDetail `json:"error"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body.Error.Code).To(Equal(ErrCodePresenceSessionIDRequired))
		Expect(body.Error.MessageID).To(Equal(sharederrors.MessageID("errors.presence.session_id_required")))
	})

	ginkgo.It("TestPresenceHeartbeatRouteReturnsStructuredInvalidRequestError", func() {
		gin.SetMode(gin.TestMode)
		registry := NewWebPresenceRegistry(time.Minute, nil)
		routes := NewRoutes(RoutesConfig{Registry: registry})
		router := gin.New()
		router.POST("/heartbeat", func(c *gin.Context) {
			c.Set("user", &coreauth.User{ID: "editor-1", Username: "Editor One", Role: coreauth.RoleEditor})
			routes.handleHeartbeat(c)
		})

		req := httptest.NewRequest(http.MethodPost, "/heartbeat", strings.NewReader(`{"sessionId":`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		Expect(rec.Code).To(Equal(http.StatusBadRequest), rec.Body.String())
		var body struct {
			Error sharederrors.LocalizedErrorDetail `json:"error"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body.Error.Code).To(Equal(ErrCodePresenceInvalidRequest))
		Expect(body.Error.MessageID).To(Equal(sharederrors.MessageID("errors.presence.invalid_request")))
		Expect(body.Error.Message).To(Equal("Invalid presence request"))
	})

	ginkgo.It("maps generic heartbeat record errors to invalid request details", func() {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)

		writePresenceRecordError(ctx, errors.New("registry failed"))

		Expect(rec.Code).To(Equal(http.StatusBadRequest), rec.Body.String())
		var body presenceErrorResponse
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed())
		Expect(body.Error.Code).To(Equal(ErrCodePresenceInvalidRequest))
		Expect(body.Error.MessageID).To(Equal(sharederrors.MessageID("errors.presence.invalid_request")))
	})

	ginkgo.It("handleHeartbeat returns early without a user and accepts valid heartbeats", func() {
		gin.SetMode(gin.TestMode)
		registry := NewWebPresenceRegistry(time.Minute, nil)
		routes := NewRoutes(RoutesConfig{Registry: registry})
		router := gin.New()
		router.POST("/heartbeat-without-user", routes.handleHeartbeat)
		router.POST("/heartbeat", func(c *gin.Context) {
			c.Set("user", &coreauth.User{ID: "editor-1", Username: "Editor One", Role: coreauth.RoleEditor})
			routes.handleHeartbeat(c)
		})

		missingUser := httptest.NewRecorder()
		missingUserReq := httptest.NewRequest(http.MethodPost, "/heartbeat-without-user", strings.NewReader(`{"sessionId":"tab-1","mode":"view"}`))
		missingUserReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(missingUser, missingUserReq)
		Expect(missingUser.Code).To(Equal(http.StatusForbidden), missingUser.Body.String())

		ok := httptest.NewRecorder()
		okReq := httptest.NewRequest(http.MethodPost, "/heartbeat", strings.NewReader(`{"sessionId":"tab-1","mode":"view"}`))
		okReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(ok, okReq)
		Expect(ok.Code).To(Equal(http.StatusOK), ok.Body.String())
		Expect(ok.Body.String()).To(Equal(`{"ok":true}`))
	})

	ginkgo.It("handleDeleteSession returns early without a user", func() {
		rec := exerciseDeleteSession(NewRoutes(RoutesConfig{Registry: NewWebPresenceRegistry(time.Minute, nil)}), "tab-1", nil)

		Expect(rec.Code).To(Equal(http.StatusForbidden), rec.Body.String())
	})

	ginkgo.It("resolvePage resolves by page ID and route path", func() {
		treeService, pageID := setupPresenceTree()
		routes := NewRoutes(RoutesConfig{TreeService: treeService})

		byID := routes.resolvePage(*pageID, "")
		Expect(byID).NotTo(BeNil())
		Expect(byID.ID).To(Equal(*pageID))
		Expect(byID.Path).To(Equal("/docs"))
		Expect(byID.Title).To(Equal("Docs"))

		byPath := routes.resolvePage("", "/docs")
		Expect(byPath).NotTo(BeNil())
		Expect(byPath.ID).To(Equal(*pageID))
		Expect(routes.resolvePage("", "/missing")).To(BeNil())
		Expect(routes.resolvePage("", "/")).To(BeNil())
		Expect(routes.resolvePage("", `bad\path`)).To(BeNil())
		Expect(NewRoutes(RoutesConfig{TreeService: tree.NewTreeServiceWithOptions(tree.TreeOptions{
			DataDir: ginkgo.GinkgoT().TempDir(),
			RootDir: ginkgo.GinkgoT().TempDir(),
		})}).resolvePage("", "/docs")).To(BeNil())
		Expect((&Routes{}).resolvePage(*pageID, "/docs")).To(BeNil())

		brokenTreeService, _, brokenRootDir := setupPresenceTreeWithRoot()
		Expect(os.Remove(filepath.Join(brokenRootDir, "docs.md"))).To(Succeed())
		Expect(NewRoutes(RoutesConfig{TreeService: brokenTreeService}).resolvePage("", "/docs")).To(BeNil())

		Expect(pageRefForPage(nil)).To(BeNil())
		Expect(pageRefForPage(&tree.Page{})).To(BeNil())
	})

	ginkgo.It("handleDeleteSession returns OK and removes only the owner session", func() {
		registry := NewWebPresenceRegistry(time.Minute, nil)
		owner := &coreauth.User{ID: "editor-1", Username: "Editor One", Role: coreauth.RoleEditor}
		other := &coreauth.User{ID: "editor-2", Username: "Editor Two", Role: coreauth.RoleEditor}
		Expect(registry.Record(Heartbeat{SessionID: "tab-1", Mode: "view"}, owner, nil)).To(Succeed())
		routes := NewRoutes(RoutesConfig{Registry: registry})

		otherRec := exerciseDeleteSession(routes, "tab-1", other)
		Expect(otherRec.Code).To(Equal(http.StatusOK), otherRec.Body.String())
		Expect(registry.List(&coreauth.User{Role: coreauth.RoleAdmin})).To(HaveLen(1))

		ownerRec := exerciseDeleteSession(routes, " tab-1 ", owner)
		Expect(ownerRec.Code).To(Equal(http.StatusOK), ownerRec.Body.String())
		Expect(ownerRec.Body.String()).To(Equal(`{"ok":true}`))
		Expect(registry.List(&coreauth.User{Role: coreauth.RoleAdmin})).To(BeEmpty())
	})
})

func expectPresenceErrorCode(err error, code sharederrors.ErrorCode) {
	ginkgo.GinkgoHelper()
	localized, ok := sharederrors.AsLocalizedError(err)
	Expect(ok).To(BeTrue(), "error = %T %v", err, err)
	Expect(localized.Code).To(Equal(code))
}

func setupPresenceTree() (*tree.TreeService, *tree.PageID) {
	treeService, pageID, _ := setupPresenceTreeWithRoot()
	return treeService, pageID
}

func setupPresenceTreeWithRoot() (*tree.TreeService, *tree.PageID, string) {
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{
		DataDir: ginkgo.GinkgoT().TempDir(),
		RootDir: ginkgo.GinkgoT().TempDir(),
	})
	Expect(treeService.LoadTree()).To(Succeed())
	kind := tree.NodeKindPage
	pageID, err := treeService.CreateNode("editor-1", nil, "Docs", "docs", &kind)
	Expect(err).NotTo(HaveOccurred())
	return treeService, pageID, treeService.RootDir()
}

func exerciseDeleteSession(routes *Routes, sessionID string, user *coreauth.User) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/presence/session/"+strings.TrimSpace(sessionID), nil)
	c.Params = gin.Params{{Key: "id", Value: sessionID}}
	if user != nil {
		c.Set("user", user)
	}
	routes.handleDeleteSession(c)
	return rec
}
