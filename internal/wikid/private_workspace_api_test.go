package wikid

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/workspaceid"
)

type privateWorkspaceAPIFixture struct {
	layout     Layout
	registry   *RegistryService
	grants     *GrantStore
	supervisor *WorkspaceSupervisor
}

func newPrivateWorkspaceAPIFixture() privateWorkspaceAPIFixture {
	ginkgo.GinkgoHelper()
	layout := GlobalLayout(filepath.Join(wikidTestTempDir(), ".leafwiki"))
	return privateWorkspaceAPIFixture{
		layout:     layout,
		registry:   NewRegistryService(NewRegistryStore(layout.DBPath), layout),
		grants:     NewGrantStore(layout.DBPath),
		supervisor: NewWorkspaceSupervisor(WorkspaceSupervisorOptions{}),
	}
}

func (fixture privateWorkspaceAPIFixture) bootstrapHomeWorkspace() WorkspaceRecord {
	ginkgo.GinkgoHelper()
	home, err := fixture.registry.BootstrapHome()
	Expect(err).To(Succeed())
	return home
}

func (fixture privateWorkspaceAPIFixture) registerWorkspace(displayName string, markdownLinkRootPrefix string) WorkspaceRecord {
	ginkgo.GinkgoHelper()
	workspace, err := fixture.registry.RegisterWorkspace(RegisterWorkspaceRequest{
		DisplayName:            displayName,
		DataDir:                filepath.Join(wikidTestTempDir(), displayName+"-data"),
		RootDir:                filepath.Join(wikidTestTempDir(), displayName+"-root"),
		MarkdownLinkRootPrefix: markdownLinkRootPrefix,
	})
	Expect(err).To(Succeed())
	return workspace
}

func (fixture privateWorkspaceAPIFixture) grantWorkspace(subject string, workspaceID workspaceid.WorkspaceID, role GrantRole) {
	ginkgo.GinkgoHelper()
	Expect(fixture.grants.Upsert(Grant{Subject: subject, WorkspaceID: workspaceID, Role: role})).To(Succeed())
}

func (fixture privateWorkspaceAPIFixture) privateWorkspaceAPI(subject WorkspaceSubject, ensure func(context.Context, WorkspaceRecord) (WorkspaceStatus, error)) http.Handler {
	ginkgo.GinkgoHelper()
	return NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
		Registry:   fixture.registry,
		Grants:     fixture.grants,
		Supervisor: fixture.supervisor,
		Subject: func(*http.Request) (WorkspaceSubject, error) {
			return subject, nil
		},
		Ensure: ensure,
	})
}

func recordPrivateWorkspaceAPIResponse(api http.Handler, method string, path string) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	return rec
}

func recordPrivateWorkspaceActionResponse(api http.Handler, method string, workspaceID workspaceid.WorkspaceID, action string) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	req := httptest.NewRequest(method, PrivateWorkspacesPrefix+"/"+workspaceID.String()+"/"+action, nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	return rec
}

func decodePrivateWorkspaceAPIResponse[T any](rec *httptest.ResponseRecorder) T {
	ginkgo.GinkgoHelper()
	var out T
	Expect(json.Unmarshal(rec.Body.Bytes(), &out)).To(Succeed())
	return out
}

func matchWorkspaceListItem(workspace WorkspaceRecord, role GrantRole, markdownLinkRootPrefix string) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ID":                     Equal(workspace.ID),
		"Role":                   Equal(role),
		"MarkdownLinkRootPrefix": Equal(markdownLinkRootPrefix),
		"DataDir":                BeZero(),
		"RootDir":                BeZero(),
	})
}

var _ = ginkgo.Describe("private workspace API", ginkgo.Label("integration"), func() {
	ginkgo.It("lists only workspaces granted to the authenticated subject", func() {
		fixture := newPrivateWorkspaceAPIFixture()
		home := fixture.bootstrapHomeWorkspace()
		alpha := fixture.registerWorkspace("Alpha", "")
		fixture.grantWorkspace("user:1", home.ID, GrantRoleViewer)
		fixture.grantWorkspace("user:2", alpha.ID, GrantRoleViewer)
		api := fixture.privateWorkspaceAPI(WorkspaceSubject{Subject: "user:1"}, nil)

		rec := recordPrivateWorkspaceAPIResponse(api, http.MethodGet, PrivateWorkspacesPrefix)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		out := decodePrivateWorkspaceAPIResponse[WorkspaceListResponse](rec)
		Expect(out.Workspaces).To(ConsistOf(matchWorkspaceListItem(home, GrantRoleViewer, "")))
	})

	ginkgo.It("normalizes markdown link root prefixes on granted workspace listings", func() {
		fixture := newPrivateWorkspaceAPIFixture()
		alpha := fixture.registerWorkspace("Alpha", "docs/")
		fixture.grantWorkspace("user:1", alpha.ID, GrantRoleViewer)
		api := fixture.privateWorkspaceAPI(WorkspaceSubject{Subject: "user:1"}, nil)

		rec := recordPrivateWorkspaceAPIResponse(api, http.MethodGet, PrivateWorkspacesPrefix)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		out := decodePrivateWorkspaceAPIResponse[WorkspaceListResponse](rec)
		Expect(out.Workspaces).To(ConsistOf(matchWorkspaceListItem(alpha, GrantRoleViewer, "/docs")))
	})

	ginkgo.It("lets administrators list and start registered workspaces without stored grants", func() {
		fixture := newPrivateWorkspaceAPIFixture()
		home := fixture.bootstrapHomeWorkspace()
		alpha := fixture.registerWorkspace("Alpha", "")
		var ensured workspaceid.WorkspaceID
		api := fixture.privateWorkspaceAPI(WorkspaceSubject{Subject: "user:admin", Role: GrantRoleAdmin}, func(_ context.Context, workspace WorkspaceRecord) (WorkspaceStatus, error) {
			ensured = workspace.ID
			fixture.supervisor.MarkReady(workspace.ID, 321, "http://127.0.0.1:42001")
			return fixture.supervisor.Status(workspace.ID), nil
		})

		listRec := recordPrivateWorkspaceAPIResponse(api, http.MethodGet, PrivateWorkspacesPrefix)

		Expect(listRec).To(HaveHTTPStatus(http.StatusOK))
		list := decodePrivateWorkspaceAPIResponse[WorkspaceListResponse](listRec)
		Expect(list.Workspaces).To(ConsistOf(
			matchWorkspaceListItem(home, GrantRoleAdmin, ""),
			matchWorkspaceListItem(alpha, GrantRoleAdmin, ""),
		))

		ensureRec := recordPrivateWorkspaceActionResponse(api, http.MethodPost, alpha.ID, "ensure")

		Expect(ensureRec).To(HaveHTTPStatus(http.StatusOK))
		Expect(ensured).To(Equal(alpha.ID))
		stored, err := fixture.grants.GrantsForSubject("user:admin")
		Expect(err).To(Succeed())
		Expect(stored).To(BeEmpty())
	})

	ginkgo.It("starts granted workspaces and returns the public running status", func() {
		fixture := newPrivateWorkspaceAPIFixture()
		alpha := fixture.registerWorkspace("Alpha", "docs/")
		fixture.grantWorkspace("user:1", alpha.ID, GrantRoleEditor)
		var ensured workspaceid.WorkspaceID
		api := fixture.privateWorkspaceAPI(WorkspaceSubject{Subject: "user:1"}, func(_ context.Context, workspace WorkspaceRecord) (WorkspaceStatus, error) {
			ensured = workspace.ID
			fixture.supervisor.MarkReady(workspace.ID, 123, "http://127.0.0.1:41001")
			return fixture.supervisor.Status(workspace.ID), nil
		})

		rec := recordPrivateWorkspaceActionResponse(api, http.MethodPost, alpha.ID, "ensure")

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		Expect(ensured).To(Equal(alpha.ID))
		out := decodePrivateWorkspaceAPIResponse[WorkspaceStatusResponse](rec)
		Expect(out).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Workspace": matchWorkspaceListItem(alpha, GrantRoleEditor, "/docs"),
			"Status": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"WorkspaceID": Equal(alpha.ID),
				"State":       Equal(WorkspaceStateRunning),
				"URL":         Equal("http://127.0.0.1:41001"),
			}),
		}))
	})

	ginkgo.It("reports supervisor lifecycle state in workspace listings and snapshots", func() {
		fixture := newPrivateWorkspaceAPIFixture()
		now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
		fixture.supervisor = NewWorkspaceSupervisor(WorkspaceSupervisorOptions{
			MaxRestarts: 1,
			Backoff:     time.Minute,
			Now:         func() time.Time { return now },
		})
		alpha := fixture.registerWorkspace("Alpha", "")
		bravo := fixture.registerWorkspace("Bravo", "")
		fixture.grantWorkspace("user:1", alpha.ID, GrantRoleEditor)
		fixture.grantWorkspace("user:1", bravo.ID, GrantRoleViewer)
		fixture.supervisor.MarkStatus(WorkspaceStatus{
			WorkspaceID: alpha.ID,
			State:       WorkspaceStateStarting,
		})
		Expect(recordWorkspaceCrash(fixture.supervisor, bravo.ID, workspaceCrashProcessExit.String())).To(SatisfyAll(
			HaveField("Decision", Equal(workspaceRestartScheduled)),
			HaveField("RestartAt", BeTemporally("==", now.Add(time.Minute))),
		))
		api := fixture.privateWorkspaceAPI(WorkspaceSubject{Subject: "user:1"}, nil)

		rec := recordPrivateWorkspaceAPIResponse(api, http.MethodGet, PrivateWorkspacesPrefix)

		Expect(rec).To(HaveHTTPStatus(http.StatusOK))
		out := decodePrivateWorkspaceAPIResponse[WorkspaceListResponse](rec)
		Expect(out.Workspaces).To(ConsistOf(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ID":   Equal(alpha.ID),
				"Role": Equal(GrantRoleEditor),
				"Status": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"WorkspaceID": Equal(alpha.ID),
					"State":       Equal(WorkspaceStateStarting),
					"UpdatedAt":   BeTemporally("==", now),
				}),
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ID":     Equal(bravo.ID),
				"Role":   Equal(GrantRoleViewer),
				"Status": matchWorkspaceFailureState(WorkspaceStateRestarting, workspaceCrashProcessExit),
			}),
		))
		Expect(fixture.supervisor.Statuses()).To(HaveExactElements(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"WorkspaceID": Equal(alpha.ID),
				"State":       Equal(WorkspaceStateStarting),
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"WorkspaceID": Equal(bravo.ID),
				"State":       Equal(WorkspaceStateRestarting),
			}),
		))
	})

	ginkgo.It("returns a localized denial for workspaces not granted to the subject", func() {
		fixture := newPrivateWorkspaceAPIFixture()
		alpha := fixture.registerWorkspace("Alpha", "")
		api := fixture.privateWorkspaceAPI(WorkspaceSubject{Subject: "user:1"}, nil)

		rec := recordPrivateWorkspaceActionResponse(api, http.MethodGet, alpha.ID, "status")

		Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusForbidden, ErrCodeWorkspaceGrantDenied, sharederrors.MessageIDForCode(ErrCodeWorkspaceGrantDenied)))
	})

	ginkgo.It("returns not found for unknown workspace status requests", func() {
		fixture := newPrivateWorkspaceAPIFixture()
		fixture.bootstrapHomeWorkspace()
		api := fixture.privateWorkspaceAPI(WorkspaceSubject{Subject: "user:1"}, nil)

		rec := recordPrivateWorkspaceAPIResponse(api, http.MethodGet, PrivateWorkspacesPrefix+"/missing/status")

		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
	})

	ginkgo.It("rejects invalid workspace identifiers before starting a workspace", func() {
		fixture := newPrivateWorkspaceAPIFixture()
		home := fixture.bootstrapHomeWorkspace()
		fixture.grantWorkspace("user:1", home.ID, GrantRoleEditor)
		api := fixture.privateWorkspaceAPI(WorkspaceSubject{Subject: "user:1"}, func(_ context.Context, workspace WorkspaceRecord) (WorkspaceStatus, error) {
			fixture.supervisor.MarkReady(workspace.ID, 999, "http://127.0.0.1:49999")
			return fixture.supervisor.Status(workspace.ID), nil
		})

		rec := recordPrivateWorkspaceAPIResponse(api, http.MethodPost, PrivateWorkspacesPrefix+"/%20home/ensure")

		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
	})
})
