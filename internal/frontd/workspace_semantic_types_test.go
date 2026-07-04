package frontd

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = Describe("workspace router semantic IDs", Label("unit"), func() {
	It("parses workspace routes into typed workspace identifiers", func() {
		Expect(workspaceAPIPathResult("/api/workspaces/home/tree")).To(ResolveWorkspaceAPIPath(
			workspaceid.WorkspaceID("home"),
			"/api/tree",
		))
	})

	It("accepts typed workspace callbacks for resolver and actor dependencies", func() {
		route := WorkspaceRoute{WorkspaceID: workspaceid.WorkspaceID("home")}
		var _ workspaceid.WorkspaceID = route.WorkspaceID

		opts := WorkspaceRouterProxyOptions{
			Resolve: func(*http.Request, workspaceid.WorkspaceID) (WorkspaceRoute, error) {
				return route, nil
			},
			Actor: func(*http.Request, workspaceid.WorkspaceID) (projectdaemon.ActorContext, error) {
				return projectdaemon.ActorContext{}, nil
			},
		}

		Expect(opts).To(SatisfyAll(
			HaveField("Resolve", Not(BeNil())),
			HaveField("Actor", Not(BeNil())),
		))
	})
})
