package wikid

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/frontd"
	"github.com/perber/wiki/internal/projectdaemon"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = ginkgo.Describe("wikid private workspace routing", func() {
	ginkgo.Describe("private workspace API", func() {
		ginkgo.It("routes not-found and malformed workspace paths before authorization", ginkgo.Label("integration"), func() {
			api := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{})

			Expect(privateWorkspaceAPIStatus(api, http.MethodGet, "/elsewhere")).To(Equal(http.StatusNotFound))
			Expect(privateWorkspaceAPIStatus(api, http.MethodGet, PrivateWorkspacesPrefix+"/missing")).To(Equal(http.StatusNotFound))
			Expect(privateWorkspaceAPIStatus(api, http.MethodGet, PrivateWorkspacesPrefix+"/bad/id/status")).To(Equal(http.StatusNotFound))
		})

		ginkgo.It("validates subjects for list requests", ginkgo.Label("integration"), func() {
			for _, tc := range []struct {
				name    string
				subject func(*http.Request) (WorkspaceSubject, error)
			}{
				{name: "missing resolver"},
				{name: "resolver error", subject: func(*http.Request) (WorkspaceSubject, error) {
					return WorkspaceSubject{}, errors.New("no subject")
				}},
				{name: "blank subject", subject: func(*http.Request) (WorkspaceSubject, error) {
					return WorkspaceSubject{Subject: " \t "}, nil
				}},
				{name: "invalid role", subject: func(*http.Request) (WorkspaceSubject, error) {
					return WorkspaceSubject{Subject: "user:1", Role: mustDecodeGrantRole("owner")}, nil
				}},
			} {
				tc := tc
				ginkgo.By(tc.name)
				api := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
					Registry: NewRegistryService(NewRegistryStore(filepath.Join(wikidTestTempDir(), "wikid.db")), Layout{}),
					Grants:   NewGrantStore(filepath.Join(wikidTestTempDir(), "wikid.db")),
					Subject:  tc.subject,
				})

				rec := privateWorkspaceAPIRecorder(api, http.MethodGet, PrivateWorkspacesPrefix)

				Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusUnauthorized, errCodePrivateSubjectResolveFailed, sharederrors.MessageIDForCode(errCodePrivateSubjectResolveFailed)))
			}
		})

		ginkgo.It("reports registry and grant load failures separately", ginkgo.Label("integration"), func() {
			registryErrorAPI := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
				Registry: NewRegistryService(NewRegistryStore(wikidDBPathInsideFile()), Layout{}),
				Subject:  validWorkspaceSubject,
			})
			registryRec := privateWorkspaceAPIRecorder(registryErrorAPI, http.MethodGet, PrivateWorkspacesPrefix)
			Expect(registryRec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, errCodePrivateRegistryLoadFailed, sharederrors.MessageIDForCode(errCodePrivateRegistryLoadFailed)))

			layout := newWikidEdgeLayout()
			Expect(NewRegistryService(NewRegistryStore(layout.DBPath), layout).BootstrapHome()).Error().NotTo(HaveOccurred())
			grantsErrorAPI := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
				Registry: NewRegistryService(NewRegistryStore(layout.DBPath), layout),
				Grants:   NewGrantStore(wikidDBPathInsideFile()),
				Subject:  validWorkspaceSubject,
			})
			grantsRec := privateWorkspaceAPIRecorder(grantsErrorAPI, http.MethodGet, PrivateWorkspacesPrefix)
			Expect(grantsRec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, errCodePrivateGrantsLoadFailed, sharederrors.MessageIDForCode(errCodePrivateGrantsLoadFailed)))
		})

		ginkgo.It("returns registered workspace state for status and default ensure when no supervisor is configured", ginkgo.Label("integration"), func() {
			layout := newWikidEdgeLayout()
			registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
			home, err := registry.BootstrapHome()
			Expect(err).NotTo(HaveOccurred())
			grants := NewGrantStore(layout.DBPath)
			Expect(grants.Upsert(Grant{Subject: "user:1", WorkspaceID: home.ID, Role: GrantRoleViewer})).To(Succeed())
			api := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
				Registry: registry,
				Grants:   grants,
				Subject:  validWorkspaceSubject,
			})

			statusRec := privateWorkspaceAPIRecorderForWorkspace(api, http.MethodGet, home.ID, "status")
			Expect(statusRec).To(SatisfyAll(
				HaveHTTPStatus(http.StatusOK),
				HaveHTTPBody(ContainSubstring(string(WorkspaceStateRegistered))),
			))

			ensureRec := privateWorkspaceAPIRecorderForWorkspace(api, http.MethodPost, home.ID, "ensure")
			Expect(ensureRec).To(SatisfyAll(
				HaveHTTPStatus(http.StatusOK),
				HaveHTTPBody(ContainSubstring(string(WorkspaceStateRegistered))),
			))
		})

		ginkgo.It("returns supervisor status from default ensure when available", ginkgo.Label("integration"), func() {
			layout := newWikidEdgeLayout()
			registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
			home, err := registry.BootstrapHome()
			Expect(err).NotTo(HaveOccurred())
			grants := NewGrantStore(layout.DBPath)
			Expect(grants.Upsert(Grant{Subject: "user:1", WorkspaceID: home.ID, Role: GrantRoleEditor})).To(Succeed())
			supervisor := NewWorkspaceSupervisor(WorkspaceSupervisorOptions{})
			supervisor.MarkStarting(home.ID)
			api := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
				Registry:   registry,
				Grants:     grants,
				Subject:    validWorkspaceSubject,
				Supervisor: supervisor,
			})

			rec := privateWorkspaceAPIRecorderForWorkspace(api, http.MethodPost, home.ID, "ensure")

			Expect(rec).To(SatisfyAll(
				HaveHTTPStatus(http.StatusOK),
				HaveHTTPBody(ContainSubstring(string(WorkspaceStateStarting))),
			))
		})

		ginkgo.It("reports authorization and ensure failures from workspace actions", ginkgo.Label("integration"), func() {
			layout := newWikidEdgeLayout()
			registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
			home, err := registry.BootstrapHome()
			Expect(err).NotTo(HaveOccurred())

			subjectErrorAPI := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
				Registry: registry,
				Grants:   NewGrantStore(layout.DBPath),
				Subject: func(*http.Request) (WorkspaceSubject, error) {
					return WorkspaceSubject{}, errors.New("subject failed")
				},
			})
			subjectRec := privateWorkspaceAPIRecorderForWorkspace(subjectErrorAPI, http.MethodGet, home.ID, "status")
			Expect(subjectRec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, errCodePrivateWorkspaceAuthFailed, sharederrors.MessageIDForCode(errCodePrivateWorkspaceAuthFailed)))

			grantsErrorAPI := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
				Registry: registry,
				Grants:   NewGrantStore(wikidDBPathInsideFile()),
				Subject:  validWorkspaceSubject,
			})
			grantsRec := privateWorkspaceAPIRecorderForWorkspace(grantsErrorAPI, http.MethodGet, home.ID, "status")
			Expect(grantsRec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, errCodePrivateWorkspaceAuthFailed, sharederrors.MessageIDForCode(errCodePrivateWorkspaceAuthFailed)))

			registryErrorAPI := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
				Registry: NewRegistryService(NewRegistryStore(wikidDBPathInsideFile()), Layout{}),
				Grants:   NewGrantStore(layout.DBPath),
				Subject:  validWorkspaceSubject,
			})
			registryRec := privateWorkspaceAPIRecorderForWorkspace(registryErrorAPI, http.MethodGet, home.ID, "status")
			Expect(registryRec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, errCodePrivateWorkspaceAuthFailed, sharederrors.MessageIDForCode(errCodePrivateWorkspaceAuthFailed)))

			ensureErrorAPI := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
				Registry: registry,
				Grants:   NewGrantStore(layout.DBPath),
				Subject: func(*http.Request) (WorkspaceSubject, error) {
					return WorkspaceSubject{Subject: "user:admin", Role: GrantRoleAdmin}, nil
				},
				Ensure: func(context.Context, WorkspaceRecord) (WorkspaceStatus, error) {
					return WorkspaceStatus{}, errors.New("ensure failed")
				},
			})
			ensureRec := privateWorkspaceAPIRecorderForWorkspace(ensureErrorAPI, http.MethodPost, home.ID, "ensure")
			Expect(ensureRec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, errCodePrivateWorkspaceEnsureFailed, sharederrors.MessageIDForCode(errCodePrivateWorkspaceEnsureFailed)))

			defaultRec := privateWorkspaceAPIRecorderForWorkspace(ensureErrorAPI, http.MethodPatch, home.ID, "status")
			Expect(defaultRec).To(HaveHTTPStatus(http.StatusNotFound))
		})

		ginkgo.It("falls back to a structured encoding error when JSON encoding fails", ginkgo.Label("unit"), func() {
			rec := httptest.NewRecorder()

			writeJSON(rec, map[string]any{"bad": make(chan int)})

			Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, errCodePrivateEncodeResponseFailed, sharederrors.MessageIDForCode(errCodePrivateEncodeResponseFailed)))
		})
	})

	ginkgo.Describe("private handler", ginkgo.Label("integration"), func() {
		ginkgo.It("dispatches protected private routes only with the daemon token", func() {
			token := "daemon-token"
			called := map[string]int{}
			handler := NewPrivateHandler(PrivateHandlerOptions{
				DaemonToken:  token,
				ActorContext: namedHandler("actor", called),
				TokenVerify:  namedHandler("verify", called),
				WorkspaceAPI: namedHandler("workspaces", called),
				Control:      namedHandler("control", called),
			})

			for _, path := range []string{"/__leafwiki/actor-context", "/__leafwiki/token/verify", PrivateWorkspacesPrefix} {
				rec := privateHandlerRecorder(handler, http.MethodGet, path, "")
				Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusUnauthorized, errCodePrivateUnauthorized, sharederrors.MessageIDForCode(errCodePrivateUnauthorized)), path)
			}
			Expect(privateHandlerRecorder(handler, http.MethodGet, frontd.ControlPlanePrefix+"/status", "")).To(testmatchers.HaveHTTPStructuredError(http.StatusUnauthorized, errCodePrivateUnauthorized, sharederrors.MessageIDForCode(errCodePrivateUnauthorized)))

			Expect(privateHandlerRecorder(handler, http.MethodGet, "/__leafwiki/actor-context", token)).To(HaveHTTPBody("actor"))
			Expect(privateHandlerRecorder(handler, http.MethodGet, "/__leafwiki/token/verify", token)).To(HaveHTTPBody("verify"))
			Expect(privateHandlerRecorder(handler, http.MethodGet, PrivateWorkspacesPrefix, token)).To(HaveHTTPBody("workspaces"))
			Expect(privateHandlerRecorder(handler, http.MethodGet, "/public", "")).To(HaveHTTPBody("control"))
			Expect(called).To(Equal(map[string]int{"actor": 1, "verify": 1, "workspaces": 1, "control": 1}))
		})

		ginkgo.It("returns not found for missing private route handlers", func() {
			handler := NewPrivateHandler(PrivateHandlerOptions{DaemonToken: "daemon-token"})

			for _, path := range []string{
				"/__leafwiki/actor-context",
				"/__leafwiki/token/verify",
				PrivateWorkspacesPrefix,
				frontd.ControlPlanePrefix + "/status",
				"/public",
			} {
				rec := privateHandlerRecorder(handler, http.MethodGet, path, "daemon-token")
				Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), path)
			}
		})

		ginkgo.It("forwards private control-plane requests with path and header normalization", func() {
			controlPlane := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				Expect(req.URL.Path).To(Equal("/base/status"))
				Expect(req.Header.Get(projectdaemon.ControlTokenHeader)).To(BeEmpty())
				Expect(req.Header.Get(projectdaemon.ActorContextHeader)).To(BeEmpty())
				Expect(req.RemoteAddr).To(Equal("10.0.0.2:1234"))
				_, _ = w.Write([]byte(req.URL.Path))
			})
			handler := NewPrivateHandler(PrivateHandlerOptions{
				DaemonToken:  "daemon-token",
				BasePath:     "/base",
				ControlPlane: controlPlane,
			})
			req := httptest.NewRequest(http.MethodGet, frontd.ControlPlanePrefix+"/status", nil)
			req.Header.Set(projectdaemon.ControlTokenHeader, "daemon-token")
			req.Header.Set(projectdaemon.ActorContextHeader, "actor")
			req.Header.Set("X-LeafWiki-Original-Remote-Addr", "10.0.0.2:1234")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			Expect(rec).To(SatisfyAll(
				HaveHTTPStatus(http.StatusOK),
				HaveHTTPBody("/base/status"),
			))
		})

		ginkgo.It("keeps well-known control-plane paths outside the workspace base path", func() {
			controlPlane := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				Expect(req.URL.Path).To(Equal("/.well-known/oauth-protected-resource"))
				_, _ = w.Write([]byte(req.RemoteAddr))
			})
			req := httptest.NewRequest(http.MethodGet, frontd.ControlPlanePrefix+"/.well-known/oauth-protected-resource", nil)
			req.Header.Set(projectdaemon.ControlTokenHeader, "daemon-token")
			req.RemoteAddr = "127.0.0.1:9999"
			rec := httptest.NewRecorder()

			NewPrivateHandler(PrivateHandlerOptions{
				DaemonToken:  "daemon-token",
				BasePath:     "/base",
				ControlPlane: controlPlane,
			}).ServeHTTP(rec, req)

			Expect(rec).To(SatisfyAll(
				HaveHTTPStatus(http.StatusOK),
				HaveHTTPBody("127.0.0.1:9999"),
			))
		})

		ginkgo.It("forwards the control-plane prefix itself as the root path", func() {
			controlPlane := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				Expect(req.URL.Path).To(Equal("/base/"))
				_, _ = w.Write([]byte(req.URL.Path))
			})
			rec := privateHandlerRecorder(NewPrivateHandler(PrivateHandlerOptions{
				DaemonToken:  "daemon-token",
				BasePath:     "/base",
				ControlPlane: controlPlane,
			}), http.MethodGet, frontd.ControlPlanePrefix, "daemon-token")

			Expect(rec).To(SatisfyAll(
				HaveHTTPStatus(http.StatusOK),
				HaveHTTPBody("/base/"),
			))
		})
	})
})
