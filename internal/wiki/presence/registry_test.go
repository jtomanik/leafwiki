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
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/http/middleware/security"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = ginkgo.Describe("web presence registry", func() {
	ginkgo.It("returns role-appropriate active-session views and expires stale sessions", func() {
		now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
		registry := NewWebPresenceRegistry(time.Minute, func() time.Time { return now })

		err := registry.Record(Heartbeat{
			SessionID: WebSessionIDFromString("tab-1"),
			Mode:      SessionModeEdit,
			Dirty:     true,
		}, &coreauth.User{
			ID:       "editor-1",
			Username: "Editor One",
			Email:    "editor@example.test",
			Role:     coreauth.RoleEditor,
		}, &PageRef{
			ID:    tree.PageIDFromString("page-1"),
			Path:  "/docs/api",
			Title: "API",
		})
		Expect(err).NotTo(HaveOccurred())

		editorView := registry.List(&coreauth.User{Role: coreauth.RoleEditor})
		Expect(editorView).To(HaveExactElements(matchPresenceSession(gstruct.Fields{
			"User":  matchPresenceUserRef(gstruct.Fields{"Email": BeEmpty()}),
			"State": Equal(SessionStateActive),
			"Dirty": BeTrue(),
			"Page":  gstruct.PointTo(matchPresencePageRef(gstruct.Fields{"Title": Equal("API")})),
			"Type":  Equal(SessionTypeWeb),
			"Mode":  Equal(SessionModeEdit),
		})))

		raw, err := json.Marshal(editorView[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(ContainSubstring(`"type":"web"`))
		Expect(string(raw)).To(ContainSubstring(`"mode":"edit"`))
		Expect(string(raw)).To(ContainSubstring(`"state":"active"`))

		adminView := registry.List(&coreauth.User{Role: coreauth.RoleAdmin})
		Expect(adminView).To(HaveExactElements(matchPresenceSession(gstruct.Fields{
			"User": matchPresenceUserRef(gstruct.Fields{"Email": Equal("editor@example.test")}),
		})))

		now = now.Add(61 * time.Second)
		Expect(registry.List(&coreauth.User{Role: coreauth.RoleAdmin})).To(BeEmpty())
	})

	ginkgo.It("rejects invalid heartbeats without dropping existing sessions", func() {
		registry := NewWebPresenceRegistry(time.Minute, nil)
		user := &coreauth.User{ID: "editor-1", Username: "Editor One", Role: coreauth.RoleEditor}
		Expect(registry.Record(Heartbeat{SessionID: WebSessionIDFromString("tab-1"), Mode: SessionModeView}, user, nil)).To(Succeed())

		Expect(registry.Record(Heartbeat{SessionID: WebSessionIDFromString(""), Mode: SessionModeView}, user, nil)).To(matchPresenceErrorCode(ErrCodePresenceSessionIDRequired))
		Expect(registry.Record(Heartbeat{SessionID: WebSessionIDFromString("tab-2"), Mode: SessionModeFromString("invalid")}, user, nil)).To(matchPresenceErrorCode(ErrCodePresenceModeInvalid))
		sessions := registry.List(&coreauth.User{Role: coreauth.RoleAdmin})
		Expect(sessions).To(HaveExactElements(matchPresenceSession(gstruct.Fields{
			"SessionID": Equal(WebSessionIDFromString("tab-1")),
		})))
	})

	ginkgo.DescribeTable("returns stable localized errors for invalid heartbeats",
		func(heartbeat Heartbeat, code sharederrors.ErrorCode) {
			registry := NewWebPresenceRegistry(time.Minute, nil)
			user := &coreauth.User{ID: "editor-1", Username: "Editor One", Role: coreauth.RoleEditor}

			err := registry.Record(heartbeat, user, nil)
			Expect(err).To(testmatchers.MatchLocalizedError(code, sharederrors.MessageIDForCode(code)))
		},
		ginkgo.Entry("missing session", Heartbeat{SessionID: WebSessionIDFromString(""), Mode: SessionModeView}, ErrCodePresenceSessionIDRequired),
		ginkgo.Entry("invalid mode", Heartbeat{SessionID: WebSessionIDFromString("tab-1"), Mode: SessionModeFromString("invalid")}, ErrCodePresenceModeInvalid),
	)

	ginkgo.It("keeps a web session bound to its original user", func() {
		registry := NewWebPresenceRegistry(time.Minute, nil)
		editor := &coreauth.User{ID: "editor-1", Username: "Editor One", Role: coreauth.RoleEditor}
		other := &coreauth.User{ID: "editor-2", Username: "Editor Two", Role: coreauth.RoleEditor}
		Expect(registry.Record(Heartbeat{SessionID: WebSessionIDFromString("shared-tab"), Mode: SessionModeView}, editor, nil)).To(Succeed())

		Expect(registry.Record(Heartbeat{SessionID: WebSessionIDFromString("shared-tab"), Mode: SessionModeEdit}, other, nil)).To(matchPresenceErrorCode(ErrCodePresenceSessionUserMismatch))
		Expect(registry.Remove(WebSessionIDFromString("shared-tab"), other)).To(BeFalse())

		sessions := registry.List(&coreauth.User{Role: coreauth.RoleAdmin})
		Expect(sessions).To(HaveExactElements(matchPresenceSession(gstruct.Fields{
			"Mode": Equal(SessionModeView),
		})))
		Expect(registry.Remove(WebSessionIDFromString("shared-tab"), editor)).To(BeTrue())
		Expect(registry.List(&coreauth.User{Role: coreauth.RoleAdmin})).To(BeEmpty())
	})

	ginkgo.It("normalizes heartbeat session, mode, page ID, and page path", func() {
		normalized, err := normalizeHeartbeat(Heartbeat{
			SessionID: WebSessionIDFromString(" tab-1 "),
			Mode:      SessionModeFromString(""),
			PageID:    tree.PageIDFromString(" page-1 "),
			Path:      " docs/api/ ",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(normalized).To(matchPresenceHeartbeat(gstruct.Fields{
			"SessionID": Equal(WebSessionIDFromString("tab-1")),
			"Mode":      Equal(SessionModeUnknown),
			"PageID":    Equal(tree.PageIDFromString("page-1")),
			"Path":      Equal("/docs/api"),
		}))
		Expect(WebSessionIDFromString(" tab-2 ")).To(Equal(WebSessionIDFromString("tab-2")))
		Expect(normalizePagePath("")).To(BeEmpty())
		Expect(normalizePagePath("/")).To(BeEmpty())
		Expect(normalizePagePath("docs")).To(Equal("/docs"))
	})

	ginkgo.It("preserves registry defaults, rejects invalid JSON modes, ignores missing removals, and lists sessions in tab order", func() {
		var mode SessionMode
		Expect(json.Unmarshal([]byte(`{"mode":"view"}`), &mode)).To(BeAssignableToTypeOf(&json.UnmarshalTypeError{}))
		registry := NewWebPresenceRegistry(0, nil)
		Expect(registry).To(matchPresenceRegistryDefaults())
		user := &coreauth.User{ID: "editor-1", Username: "Editor One", Email: "editor@example.test", Role: coreauth.RoleEditor}

		Expect(registry.Remove(WebSessionIDFromString(""), user)).To(BeFalse())
		Expect(registry.Remove(WebSessionIDFromString("missing-tab"), user)).To(BeFalse())
		Expect(registry.Record(Heartbeat{SessionID: WebSessionIDFromString("tab-b"), Mode: SessionModeView}, user, nil)).To(Succeed())
		Expect(registry.Record(Heartbeat{SessionID: WebSessionIDFromString("tab-a"), Mode: SessionModeView}, user, nil)).To(Succeed())

		sessions := registry.List(user)
		Expect(sessions).To(HaveExactElements(
			matchPresenceSession(gstruct.Fields{"SessionID": Equal(WebSessionIDFromString("tab-a"))}),
			matchPresenceSession(gstruct.Fields{"SessionID": Equal(WebSessionIDFromString("tab-b"))}),
		))
		Expect(userRefForUser(user, true).Email).To(Equal("editor@example.test"))
	})

	ginkgo.It("returns stable errors for too-long session IDs, nil registry, and nil user", func() {
		_, err := normalizeHeartbeat(Heartbeat{SessionID: WebSessionIDFromString(strings.Repeat("x", 257)), Mode: SessionModeView})
		Expect(err).To(matchPresenceErrorCode(ErrCodePresenceSessionIDTooLong))

		var registry *WebPresenceRegistry
		err = registry.Record(Heartbeat{SessionID: WebSessionIDFromString("tab-1"), Mode: SessionModeView}, &coreauth.User{ID: "editor-1"}, nil)
		Expect(err).To(matchPresenceErrorCode(ErrCodePresenceRegistryUnavailable))
		Expect(registry.List(&coreauth.User{Role: coreauth.RoleAdmin})).To(BeEmpty())
		Expect(registry.Remove(WebSessionIDFromString("tab-1"), &coreauth.User{ID: "editor-1"})).To(BeFalse())

		err = NewWebPresenceRegistry(time.Minute, nil).Record(Heartbeat{SessionID: WebSessionIDFromString("tab-1"), Mode: SessionModeView}, nil, nil)
		Expect(err).To(matchPresenceErrorCode(ErrCodePresenceUserRequired))
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

	ginkgo.It("returns stable heartbeat validation error details", func() {
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

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusBadRequest, ErrCodePresenceSessionIDRequired, sharederrors.MessageIDForCode(ErrCodePresenceSessionIDRequired)))
	})

	ginkgo.It("returns structured invalid-request errors for malformed heartbeats", func() {
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

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusBadRequest, ErrCodePresenceInvalidRequest, sharederrors.MessageIDForCode(ErrCodePresenceInvalidRequest)))
	})

	ginkgo.It("maps generic heartbeat record errors to invalid request details", func() {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)

		writePresenceRecordError(ctx, errors.New("registry failed"))

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusBadRequest, ErrCodePresenceInvalidRequest, sharederrors.MessageIDForCode(ErrCodePresenceInvalidRequest)))
	})

	ginkgo.It("forbids anonymous heartbeats and records authenticated heartbeat requests", func() {
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
		Expect(missingUser).To(HaveHTTPStatus(http.StatusForbidden))

		ok := httptest.NewRecorder()
		okReq := httptest.NewRequest(http.MethodPost, "/heartbeat", strings.NewReader(`{"sessionId":"tab-1","mode":"view"}`))
		okReq.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(ok, okReq)
		Expect(ok).To(SatisfyAll(
			HaveHTTPStatus(http.StatusOK),
			HaveHTTPBody(MatchJSON(`{"ok":true}`)),
		))
	})

	ginkgo.It("forbids anonymous session deletion requests", func() {
		rec := exerciseDeleteSession(NewRoutes(RoutesConfig{Registry: NewWebPresenceRegistry(time.Minute, nil)}), "tab-1", nil)

		Expect(rec).To(HaveHTTPStatus(http.StatusForbidden))
	})

	ginkgo.It("resolves page references by page identity or route path", func() {
		treeService, pageID := setupPresenceTree()
		routes := NewRoutes(RoutesConfig{TreeService: treeService})

		byID := routes.resolvePage(*pageID, "")
		Expect(byID).To(gstruct.PointTo(matchPresencePageRef(gstruct.Fields{
			"ID":    Equal(*pageID),
			"Path":  Equal("/docs"),
			"Title": Equal("Docs"),
		})))

		byPath := routes.resolvePage("", "/docs")
		Expect(byPath).To(gstruct.PointTo(matchPresencePageRef(gstruct.Fields{
			"ID": Equal(*pageID),
		})))
		Expect(routes.resolvePage("", "/missing")).To(BeNil())
		Expect(routes.resolvePage("", "/")).To(BeNil())
		Expect(routes.resolvePage("", `bad\path`)).To(BeNil())
		Expect(NewRoutes(RoutesConfig{TreeService: tree.NewTreeServiceWithOptions(tree.TreeOptions{
			DataDir: newPresenceTempDir(),
			RootDir: newPresenceTempDir(),
		})}).resolvePage("", "/docs")).To(BeNil())
		Expect((&Routes{}).resolvePage(*pageID, "/docs")).To(BeNil())

		brokenTreeService, _, brokenRootDir := setupPresenceTreeWithRoot()
		Expect(os.Remove(filepath.Join(brokenRootDir, "docs.md"))).To(Succeed())
		Expect(NewRoutes(RoutesConfig{TreeService: brokenTreeService}).resolvePage("", "/docs")).To(BeNil())

		Expect(pageRefForPage(nil)).To(BeNil())
		Expect(pageRefForPage(&tree.Page{})).To(BeNil())
	})

	ginkgo.It("acknowledges session deletion while removing only the owner session", func() {
		registry := NewWebPresenceRegistry(time.Minute, nil)
		owner := &coreauth.User{ID: "editor-1", Username: "Editor One", Role: coreauth.RoleEditor}
		other := &coreauth.User{ID: "editor-2", Username: "Editor Two", Role: coreauth.RoleEditor}
		Expect(registry.Record(Heartbeat{SessionID: WebSessionIDFromString("tab-1"), Mode: SessionModeView}, owner, nil)).To(Succeed())
		routes := NewRoutes(RoutesConfig{Registry: registry})

		otherRec := exerciseDeleteSession(routes, "tab-1", other)
		Expect(otherRec).To(HaveHTTPStatus(http.StatusOK))
		Expect(registry.List(&coreauth.User{Role: coreauth.RoleAdmin})).To(HaveLen(1))

		ownerRec := exerciseDeleteSession(routes, " tab-1 ", owner)
		Expect(ownerRec).To(SatisfyAll(
			HaveHTTPStatus(http.StatusOK),
			HaveHTTPBody(MatchJSON(`{"ok":true}`)),
		))
		Expect(registry.List(&coreauth.User{Role: coreauth.RoleAdmin})).To(BeEmpty())
	})
})

func matchPresenceErrorCode(code sharederrors.ErrorCode) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return testmatchers.MatchLocalizedError(code, sharederrors.MessageIDForCode(code))
}

func matchPresenceHeartbeat(fields gstruct.Fields) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchPresenceSession(fields gstruct.Fields) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchPresenceUserRef(fields gstruct.Fields) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchPresencePageRef(fields gstruct.Fields) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

type presenceRegistryDefaults struct {
	ttl      time.Duration
	hasClock bool
}

func matchPresenceRegistryDefaults() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(registry *WebPresenceRegistry) presenceRegistryDefaults {
		if registry == nil {
			return presenceRegistryDefaults{}
		}
		return presenceRegistryDefaults{
			ttl:      registry.ttl,
			hasClock: registry.now != nil,
		}
	}, Equal(presenceRegistryDefaults{
		ttl:      DefaultWebPresenceTTL,
		hasClock: true,
	}))
}

func setupPresenceTree() (*tree.TreeService, *tree.PageID) {
	ginkgo.GinkgoHelper()
	treeService, pageID, _ := setupPresenceTreeWithRoot()
	return treeService, pageID
}

func setupPresenceTreeWithRoot() (*tree.TreeService, *tree.PageID, string) {
	ginkgo.GinkgoHelper()
	treeService := tree.NewTreeServiceWithOptions(tree.TreeOptions{
		DataDir: newPresenceTempDir(),
		RootDir: newPresenceTempDir(),
	})
	Expect(treeService.LoadTree()).To(Succeed())
	kind := tree.NodeKindPage
	pageID, err := treeService.CreateNode("editor-1", nil, "Docs", "docs", &kind)
	Expect(err).NotTo(HaveOccurred())
	return treeService, pageID, treeService.RootDir()
}

func newPresenceTempDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-presence-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func exerciseDeleteSession(routes *Routes, sessionID string, user *coreauth.User) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
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
