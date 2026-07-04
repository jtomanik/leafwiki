package tree

import (
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("workspace markdown route mapping", func() {
	for _, tt := range []struct {
		name        string
		relPath     string
		isDir       bool
		wantRoute   string
		wantKind    NodeKind
		wantContent string
		wantSkip    bool
		wantReason  string
	}{
		{
			name:      "plan style markdown filename normalizes to route safe page",
			relPath:   "plans/agent_hooks.PLAN.md",
			wantRoute: "plans/agent-hooks-plan",
			wantKind:  NodeKindPage,
		},
		{
			name:      "valid uppercase slug segment is preserved",
			relPath:   "docs/ABCD.md",
			wantRoute: "docs/ABCD",
			wantKind:  NodeKindPage,
		},
		{
			name:        "section index maps to containing section",
			relPath:     "plans/index.md",
			wantRoute:   "plans",
			wantKind:    NodeKindSection,
			wantContent: "plans/index.md",
		},
		{
			name:        "root readme without root index maps to root section",
			relPath:     "README.md",
			wantRoute:   "",
			wantKind:    NodeKindSection,
			wantContent: "README.md",
		},
		{
			name:      "readme next to index remains a page route",
			relPath:   "docs/README.md",
			wantRoute: "docs/README",
			wantKind:  NodeKindPage,
		},
		{
			name:       "top level assets directory is skipped as static content",
			relPath:    "assets",
			isDir:      true,
			wantSkip:   true,
			wantReason: "static_assets",
		},
		{
			name:      "reserved non-static segment normalizes through slug service",
			relPath:   "api.md",
			wantRoute: "api-1",
			wantKind:  NodeKindPage,
		},
		{
			name:      "invalid directory segment normalizes to safe section route",
			relPath:   "User Guides",
			isDir:     true,
			wantRoute: "user-guides",
			wantKind:  NodeKindSection,
		},
	} {
		tt := tt
		ginkgo.It(tt.name, func() {
			root := tempTreeDir()
			createTreeDirectory(filepath.Join(root, "plans"))
			createTreeDirectory(filepath.Join(root, "docs"))
			writeTreeFile(filepath.Join(root, "docs", "index.md"), "# Docs", 0o644)

			got, err := MapWorkspaceMarkdownRoute(root, tt.relPath, tt.isDir)
			Expect(err).To(Succeed(), "MapWorkspaceMarkdownRoute() error = %v",

				err,
			)
			Expect(got).To(SatisfyAll(
				HaveField("SourcePath", Equal(WorkspaceSourcePathFromString(tt.relPath))),
				HaveField("Skip", Equal(tt.wantSkip)),
				HaveField("SkipReason", Equal(tt.wantReason)),
			), "unexpected workspace route skip decision: %#v", got)

			if tt.wantSkip {
				return
			}
			Expect(got).To(SatisfyAll(
				HaveField("RoutePath", Equal(RoutePathFromString(tt.wantRoute))),
				HaveField("Kind", Equal(tt.wantKind)),
				HaveField("ContentPath", Equal(MarkdownPathFromString(tt.wantContent))),
			), "unexpected workspace route mapping: %#v", got)

		})
	}
})

var _ = ginkgo.Describe("workspace markdown route mapping", func() {
	ginkgo.It("map workspace markdown route rejects empty normalized segments", func() {
		root := tempTreeDir()

		_, err := MapWorkspaceMarkdownRoute(root, "plans/!!!.md", false)
		Expect(err).To(MatchError(ErrSlugEmpty), "expected invalid slug segment error, got %v",
			err)

	})
})

var _ = ginkgo.Describe("workspace route conflict tracking", func() {
	ginkgo.It("workspace route conflict tracker reports normalized collisions", func() {
		tracker := newWorkspaceRouteConflictTracker()
		first := WorkspaceMarkdownRoute{SourcePath: "plans/foo_bar.md", RoutePath: "plans/foo-bar", Kind: NodeKindPage}
		second := WorkspaceMarkdownRoute{SourcePath: "plans/foo-bar.md", RoutePath: "plans/foo-bar", Kind: NodeKindPage}
		{

			conflict := tracker.Record(first)
			Expect(conflict).To(BeNil(), "first route conflict = %#v",

				conflict,
			)
		}

		conflict := tracker.Record(second)
		Expect(conflict).NotTo(BeNil(),

			"expected normalized route conflict",
		)
		Expect(conflict).To(SatisfyAll(
			HaveField("RoutePath", Equal(RoutePath("plans/foo-bar"))),
			HaveField("Kind", Equal(NodeKindPage)),
			HaveField("FirstPath", Equal(WorkspaceSourcePath("plans/foo_bar.md"))),
			HaveField("SecondPath", Equal(WorkspaceSourcePath("plans/foo-bar.md"))),
		), "conflict = %#v", conflict)

	})
})

var _ = ginkgo.Describe("workspace route conflict tracking", func() {
	ginkgo.It("workspace route conflict tracker allows page and section twin routes", func() {
		tracker := newWorkspaceRouteConflictTracker()
		{

			conflict := tracker.Record(WorkspaceMarkdownRoute{SourcePath: "notes", RoutePath: "notes", Kind: NodeKindSection})
			Expect(conflict).To(BeNil(), "section route conflict = %#v",

				conflict,
			)
		}
		{

			conflict := tracker.Record(WorkspaceMarkdownRoute{SourcePath: "notes.md", RoutePath: "notes", Kind: NodeKindPage})
			Expect(conflict).To(BeNil(), "page and section twin route conflict = %#v",

				conflict)
		}

	})
})
