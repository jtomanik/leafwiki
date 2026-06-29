package workspaced

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/assets"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.Describe("authenticated workspaced router", func() {
	ginkgo.DescribeTable("TestAuthenticatedRouterRequiresPrivateTokenAndActorContext",
		func(token string, actor string, wantCode string, wantMessageID string) {
			now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
			w := newTestWiki()
			ginkgo.DeferCleanup(func() {
				Expect(w.Close()).To(Succeed())
			})
			router := NewAuthenticatedRouter(w, httpinternal.RouterOptions{
				PublicAccess:            true,
				AllowInsecure:           true,
				AuthDisabled:            true,
				AccessTokenTimeout:      15 * time.Minute,
				RefreshTokenTimeout:     7 * 24 * time.Hour,
				MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
			}, PrivateAuthOptions{
				DaemonToken: "private-token",
				WorkspaceID: "current",
				Now:         func() time.Time { return now },
			})

			req := newPrivateRequest(http.MethodGet, "/api/tree", token, actor)
			rec := requestWithRequest(router, req)
			Expect(rec.Code).To(Equal(http.StatusUnauthorized), rec.Body.String())
			assertStructuredPrivateAuthError(rec, wantCode, wantMessageID)
		},
		ginkgo.Entry("missing token", "", "", "private_control_token_invalid", "errors.private.control_token_invalid"),
		ginkgo.Entry("wrong token", "wrong", "", "private_control_token_invalid", "errors.private.control_token_invalid"),
		ginkgo.Entry("missing actor", "private-token", "", "private_actor_context_invalid", "errors.private.actor_context_invalid"),
	)

	ginkgo.It("TestAuthenticatedRouterRequiresPrivateTokenAndActorContext rejects wrong workspace and accepts a valid private request", func() {
		now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
		w := newTestWiki()
		ginkgo.DeferCleanup(func() {
			Expect(w.Close()).To(Succeed())
		})
		router := NewAuthenticatedRouter(w, httpinternal.RouterOptions{
			PublicAccess:            true,
			AllowInsecure:           true,
			AuthDisabled:            true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		}, PrivateAuthOptions{
			DaemonToken: "private-token",
			WorkspaceID: "current",
			Now:         func() time.Time { return now },
		})

		wrongWorkspaceActor, err := projectdaemon.EncodeActorContext(projectdaemon.ActorContext{
			Version:     1,
			Issuer:      projectdaemon.ActorContextIssuerWikid,
			Subject:     "user:admin",
			Username:    "admin",
			Role:        "admin",
			WorkspaceID: "other",
			AuthMethod:  "disabled",
			IssuedAt:    now,
			ExpiresAt:   now.Add(5 * time.Minute),
		})
		Expect(err).NotTo(HaveOccurred())
		wrongWorkspaceReq := newPrivateRequest(http.MethodGet, "/api/tree", "private-token", wrongWorkspaceActor)
		wrongWorkspaceRec := requestWithRequest(router, wrongWorkspaceReq)
		Expect(wrongWorkspaceRec.Code).To(Equal(http.StatusUnauthorized), wrongWorkspaceRec.Body.String())
		assertStructuredPrivateAuthError(wrongWorkspaceRec, "private_actor_context_invalid", "errors.private.actor_context_invalid")

		actor, err := projectdaemon.EncodeActorContext(projectdaemon.ActorContext{
			Version:     1,
			Issuer:      projectdaemon.ActorContextIssuerWikid,
			Subject:     "user:admin",
			Username:    "admin",
			Role:        "admin",
			WorkspaceID: "current",
			AuthMethod:  "disabled",
			IssuedAt:    now,
			ExpiresAt:   now.Add(5 * time.Minute),
		})
		Expect(err).NotTo(HaveOccurred())
		req := newPrivateRequest(http.MethodGet, "/api/tree", "private-token", actor)
		rec := requestWithRequest(router, req)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
	})

	ginkgo.It("rejects an empty daemon token even when the request token is empty", func() {
		now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
		w := newTestWiki()
		ginkgo.DeferCleanup(func() {
			Expect(w.Close()).To(Succeed())
		})
		actor, err := projectdaemon.EncodeActorContext(projectdaemon.ActorContext{
			Version:     1,
			Issuer:      projectdaemon.ActorContextIssuerWikid,
			Subject:     "user:admin",
			Username:    "admin",
			Role:        "admin",
			WorkspaceID: "current",
			AuthMethod:  "disabled",
			IssuedAt:    now,
			ExpiresAt:   now.Add(5 * time.Minute),
		})
		Expect(err).NotTo(HaveOccurred())
		router := NewAuthenticatedRouter(w, workspacedRouterOptions(), PrivateAuthOptions{
			DaemonToken: "",
			WorkspaceID: "current",
			Now:         func() time.Time { return now },
		})

		rec := requestWithRequest(router, newPrivateRequest(http.MethodGet, "/api/tree", "", actor))
		Expect(rec.Code).To(Equal(http.StatusUnauthorized), rec.Body.String())
		assertStructuredPrivateAuthError(rec, "private_control_token_invalid", "errors.private.control_token_invalid")
	})

	ginkgo.It("rejects an expired actor context", func() {
		now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
		w := newTestWiki()
		ginkgo.DeferCleanup(func() {
			Expect(w.Close()).To(Succeed())
		})
		actor, err := projectdaemon.EncodeActorContext(projectdaemon.ActorContext{
			Version:     1,
			Issuer:      projectdaemon.ActorContextIssuerWikid,
			Subject:     "user:admin",
			Username:    "admin",
			Role:        "admin",
			WorkspaceID: "current",
			AuthMethod:  "disabled",
			IssuedAt:    now.Add(-10 * time.Minute),
			ExpiresAt:   now.Add(-time.Minute),
		})
		Expect(err).NotTo(HaveOccurred())
		router := NewAuthenticatedRouter(w, workspacedRouterOptions(), PrivateAuthOptions{
			DaemonToken: "private-token",
			WorkspaceID: "current",
			Now:         func() time.Time { return now },
		})

		rec := requestWithRequest(router, newPrivateRequest(http.MethodGet, "/api/tree", "private-token", actor))
		Expect(rec.Code).To(Equal(http.StatusUnauthorized), rec.Body.String())
		assertStructuredPrivateAuthError(rec, "private_actor_context_invalid", "errors.private.actor_context_invalid")
	})

	ginkgo.It("TestPrivateAuthOptionsCarriesSemanticWorkspaceID", func() {
		auth := PrivateAuthOptions{WorkspaceID: workspaceid.WorkspaceID("current")}

		var _ workspaceid.WorkspaceID = auth.WorkspaceID
	})

	ginkgo.It("TestAuthenticatedRouterInstallsActorAsRequestUser", func() {
		now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
		w := newTestWiki()
		ginkgo.DeferCleanup(func() {
			Expect(w.Close()).To(Succeed())
		})
		router := NewAuthenticatedRouter(w, httpinternal.RouterOptions{
			AllowInsecure:           true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		}, PrivateAuthOptions{
			DaemonToken: "private-token",
			WorkspaceID: "current",
			Now:         func() time.Time { return now },
		})
		actor, err := projectdaemon.EncodeActorContext(projectdaemon.ActorContext{
			Version:     1,
			Issuer:      projectdaemon.ActorContextIssuerWikid,
			Subject:     "user:editor-1",
			Username:    "editor",
			Email:       "editor@example.com",
			Role:        "editor",
			WorkspaceID: "current",
			AuthMethod:  "cookie",
			IssuedAt:    now,
			ExpiresAt:   now.Add(5 * time.Minute),
		})
		Expect(err).NotTo(HaveOccurred())

		req := newPrivateRequest(http.MethodGet, "/api/tree", "private-token", actor)
		rec := requestWithRequest(router, req)
		Expect(rec.Code).To(Equal(http.StatusOK), rec.Body.String())
	})

	ginkgo.It("TestAuthenticatedRouterAllowsPrivateMutationWithoutPublicCSRFCookie", func() {
		now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
		w := newTestWiki()
		ginkgo.DeferCleanup(func() {
			Expect(w.Close()).To(Succeed())
		})
		router := NewAuthenticatedRouter(w, httpinternal.RouterOptions{
			AllowInsecure:           true,
			AccessTokenTimeout:      15 * time.Minute,
			RefreshTokenTimeout:     7 * 24 * time.Hour,
			MaxAssetUploadSizeBytes: assets.DefaultMaxUploadSizeBytes,
		}, PrivateAuthOptions{
			DaemonToken: "private-token",
			WorkspaceID: "current",
			Now:         func() time.Time { return now },
		})
		actor, err := projectdaemon.EncodeActorContext(projectdaemon.ActorContext{
			Version:     1,
			Issuer:      projectdaemon.ActorContextIssuerWikid,
			Subject:     "user:editor-1",
			Username:    "editor",
			Role:        "editor",
			WorkspaceID: "current",
			AuthMethod:  "cookie",
			IssuedAt:    now,
			ExpiresAt:   now.Add(5 * time.Minute),
		})
		Expect(err).NotTo(HaveOccurred())

		req := newPrivateRequest(http.MethodPost, "/api/pages", "private-token", actor)
		req.Body = io.NopCloser(strings.NewReader(`{"kind":"page","slug":"private-mutation","title":"Private Mutation"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := requestWithRequest(router, req)
		Expect(rec.Code == http.StatusForbidden && strings.Contains(rec.Body.String(), "CSRF")).To(BeFalse(), "private actor-context mutation was blocked by public CSRF middleware: %s", rec.Body.String())
		Expect(rec.Code).To(Equal(http.StatusCreated), rec.Body.String())
	})
})

func newPrivateRequest(method, path, token, actor string) *http.Request {
	ginkgo.GinkgoHelper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set(projectdaemon.ControlTokenHeader, token)
	}
	if actor != "" {
		req.Header.Set(projectdaemon.ActorContextHeader, actor)
	}
	return req
}

func requestWithRequest(router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func assertStructuredPrivateAuthError(rec *httptest.ResponseRecorder, code string, messageID string) {
	ginkgo.GinkgoHelper()
	var body struct {
		Error struct {
			Code      string `json:"code"`
			MessageID string `json:"messageId"`
			Message   string `json:"message"`
		} `json:"error"`
	}
	Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), "body=%s", rec.Body.String())
	Expect(body.Error.Code).To(Equal(code))
	Expect(body.Error.MessageID).To(Equal(messageID))
	Expect(body.Error.Message).NotTo(BeEmpty())
}
