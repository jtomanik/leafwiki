package frontd

import (
	. "github.com/onsi/ginkgo/v2"
	"net/http"

	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = It("TestWorkspaceRouterProxyUsesSemanticWorkspaceID", func() {
	t := GinkgoT()
	routeID, upstreamPath, ok := parseWorkspaceAPIPath("/api/workspaces/home/tree")
	if !ok {
		t.Fatal("parseWorkspaceAPIPath did not parse workspace route")
	}
	var typedRouteID workspaceid.WorkspaceID = routeID
	if typedRouteID != workspaceid.WorkspaceID("home") || upstreamPath != "/api/tree" {
		t.Fatalf("route = %q path = %q", typedRouteID, upstreamPath)
	}

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
	if opts.Resolve == nil || opts.Actor == nil {
		t.Fatal("typed workspace callbacks were not assigned")
	}

})
