package tree

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMapWorkspaceMarkdownRoute(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "plans"))
	mustMkdir(t, filepath.Join(root, "docs"))
	mustWriteFile(t, filepath.Join(root, "docs", "index.md"), "# Docs", 0o644)

	tests := []struct {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MapWorkspaceMarkdownRoute(root, tt.relPath, tt.isDir)
			if err != nil {
				t.Fatalf("MapWorkspaceMarkdownRoute() error = %v", err)
			}
			if got.SourcePath != tt.relPath {
				t.Fatalf("SourcePath = %q, want %q", got.SourcePath, tt.relPath)
			}
			if got.Skip != tt.wantSkip {
				t.Fatalf("Skip = %v, want %v", got.Skip, tt.wantSkip)
			}
			if got.SkipReason != tt.wantReason {
				t.Fatalf("SkipReason = %q, want %q", got.SkipReason, tt.wantReason)
			}
			if tt.wantSkip {
				return
			}
			if got.RoutePath != tt.wantRoute {
				t.Fatalf("RoutePath = %q, want %q", got.RoutePath, tt.wantRoute)
			}
			if got.Kind != tt.wantKind {
				t.Fatalf("Kind = %q, want %q", got.Kind, tt.wantKind)
			}
			if got.ContentPath != tt.wantContent {
				t.Fatalf("ContentPath = %q, want %q", got.ContentPath, tt.wantContent)
			}
		})
	}
}

func TestMapWorkspaceMarkdownRouteRejectsEmptyNormalizedSegments(t *testing.T) {
	root := t.TempDir()

	_, err := MapWorkspaceMarkdownRoute(root, "plans/!!!.md", false)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "segment") || !strings.Contains(err.Error(), "not a valid slug") {
		t.Fatalf("expected invalid slug segment error, got %v", err)
	}
}

func TestWorkspaceRouteConflictTrackerReportsNormalizedCollisions(t *testing.T) {
	tracker := newWorkspaceRouteConflictTracker()
	first := WorkspaceMarkdownRoute{SourcePath: "plans/foo_bar.md", RoutePath: "plans/foo-bar", Kind: NodeKindPage}
	second := WorkspaceMarkdownRoute{SourcePath: "plans/foo-bar.md", RoutePath: "plans/foo-bar", Kind: NodeKindPage}

	if conflict := tracker.Record(first); conflict != nil {
		t.Fatalf("first route conflict = %#v", conflict)
	}
	conflict := tracker.Record(second)
	if conflict == nil {
		t.Fatalf("expected normalized route conflict")
	}
	if conflict.RoutePath != "plans/foo-bar" || conflict.Kind != NodeKindPage {
		t.Fatalf("conflict route = %#v", conflict)
	}
	if conflict.FirstPath != "plans/foo_bar.md" || conflict.SecondPath != "plans/foo-bar.md" {
		t.Fatalf("conflict paths = %#v", conflict)
	}
}

func TestWorkspaceRouteConflictTrackerAllowsPageAndSectionTwinRoutes(t *testing.T) {
	tracker := newWorkspaceRouteConflictTracker()

	if conflict := tracker.Record(WorkspaceMarkdownRoute{SourcePath: "notes", RoutePath: "notes", Kind: NodeKindSection}); conflict != nil {
		t.Fatalf("section route conflict = %#v", conflict)
	}
	if conflict := tracker.Record(WorkspaceMarkdownRoute{SourcePath: "notes.md", RoutePath: "notes", Kind: NodeKindPage}); conflict != nil {
		t.Fatalf("page and section twin route conflict = %#v", conflict)
	}
}
