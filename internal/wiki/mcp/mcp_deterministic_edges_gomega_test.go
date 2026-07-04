package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	httpinternal "github.com/perber/wiki/internal/http"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspacesync"
)

var _ = Describe("MCP deterministic edge behavior", func() {
	Describe("checkpoint store guards", Label("unit"), func() {
		It("defaults limits and fills generated fields", func() {
			store := newContextCheckpointStore(0)
			before := time.Now().UTC()

			previous, history := store.record("session", contextCheckpoint{})

			Expect(previous).To(BeNil())
			Expect(history).To(HaveExactElements(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Token":     Equal("ctx_1"),
				"CreatedAt": And(Not(BeZero()), BeTemporally(">=", before)),
			})))
			Expect(store.limit).To(Equal(10))
		})

		It("honors disabled pruning and session limits", func() {
			base := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
			store := newContextCheckpointStore(2)
			store.ttl = 0
			store.maxSessions = 0
			store.sessions["empty"] = nil
			store.sessions["old"] = []contextCheckpoint{{Token: "old", CreatedAt: base.Add(-time.Hour)}}

			store.pruneExpiredLocked(base)
			store.evictOverflowLocked("old")
			store.evictForNewSessionLocked("new")

			Expect(store.sessions).To(HaveKey("empty"))
			Expect(store.sessions).To(HaveKey("old"))

			onlyStore := newContextCheckpointStore(2)
			onlyStore.sessions["only"] = []contextCheckpoint{{Token: "only", CreatedAt: base}}

			Expect(checkpointEvictionDecisionFor(onlyStore.evictOldestSessionLocked("only"))).To(Equal(checkpointSessionRetained))
			Expect(onlyStore.sessions).To(HaveKey("only"))
		})

		It("prunes expired checkpoints without deleting empty sessions", func() {
			base := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)
			store := newContextCheckpointStore(3)
			store.ttl = time.Minute
			store.sessions["empty"] = nil
			store.sessions["expired"] = []contextCheckpoint{
				{Token: "expired", CreatedAt: base.Add(-2 * time.Minute)},
			}
			store.sessions["mixed"] = []contextCheckpoint{
				{Token: "old", CreatedAt: base.Add(-2 * time.Minute)},
				{Token: "fresh", CreatedAt: base.Add(-30 * time.Second)},
			}

			store.pruneExpiredLocked(base)

			Expect(store.sessions).To(HaveKey("empty"))
			Expect(store.sessions).NotTo(HaveKey("expired"))
			Expect(store.sessions).To(HaveKeyWithValue("mixed", []contextCheckpoint{
				{Token: "fresh", CreatedAt: base.Add(-30 * time.Second)},
			}))
		})
	})

	Describe("request actor guards", Label("unit"), func() {
		It("returns public editor only when auth is disabled", func() {
			routes := &Routes{authDisabled: true}

			user, err := routes.actorForRequest(nil)

			Expect(err).NotTo(HaveOccurred())
			Expect(user).To(Equal(publicEditor()))
		})

		It("rejects authenticated token info when the user service is unavailable", func() {
			routes := &Routes{}
			req := &sdkmcp.CallToolRequest{Extra: &sdkmcp.RequestExtra{
				TokenInfo: &sdkauth.TokenInfo{UserID: "editor-1"},
			}}

			user, err := routes.actorForRequest(req)

			Expect(user).To(BeNil())
			Expect(err).To(matchLocalizedErrorCode(errCodeMCPAuthenticatedUserServiceUnavailable, sharederrors.MessageIDForCode(errCodeMCPAuthenticatedUserServiceUnavailable)))
		})

		It("allows optional private actor context headers to be absent", func() {
			routes := &Routes{actorContextAllowed: true}

			Expect(privateActorContextFor(routes, http.Header{})).To(matchPrivateActorContext(
				privateActorContextDeferred,
				BeNil(),
				Succeed(),
			))
		})

		It("rejects invalid required private actor context headers", func() {
			header := http.Header{}
			header.Set(projectdaemon.ActorContextHeader, "not-base64-context")
			routes := &Routes{
				actorContextAllowed:  true,
				actorContextRequired: true,
			}

			Expect(privateActorContextFor(routes, header)).To(matchPrivateActorContext(
				privateActorContextHandled,
				BeNil(),
				matchLocalizedErrorCode(errCodeMCPActorContextInvalid, sharederrors.MessageIDForCode(errCodeMCPActorContextInvalid)),
			))
		})

		It("reports unavailable API-key service for missing-token STDIO auth", func() {
			routes := &Routes{stdioAPIKey: "lwk_valid_looking"}

			user, err := routes.actorForMissingTokenInfo()

			Expect(user).To(BeNil())
			Expect(err).To(matchLocalizedErrorCode(errCodeMCPAuthenticatedUserServiceUnavailable, sharederrors.MessageIDForCode(errCodeMCPAuthenticatedUserServiceUnavailable)))
		})
	})

	Describe("HTTP route guards", Label("integration"), func() {
		It("extracts bearer tokens defensively", func() {
			Expect(bearerTokenFromRequest(nil)).To(BeEmpty())

			req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
			Expect(bearerTokenFromRequest(req)).To(BeEmpty())

			req.Header.Set("Authorization", "Basic abc")
			Expect(bearerTokenFromRequest(req)).To(BeEmpty())

			req.Header.Set("Authorization", "bearer   token-value  ")
			Expect(bearerTokenFromRequest(req)).To(Equal("token-value"))
		})

		It("protects private STDIO HTTP before constructing a server", func() {
			routes := &Routes{}
			handler := routes.requirePrivateStdioAPIKey(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))

			By("rejecting missing and non-API-key bearers")
			for _, authHeader := range []string{"", "Bearer not-api-key"} {
				req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
				req.Header.Set("Authorization", authHeader)
				rec := httptest.NewRecorder()

				handler.ServeHTTP(rec, req)

				Expect(rec).To(HaveHTTPStatus(http.StatusUnauthorized))
			}

			By("classifying an unavailable verifier separately")
			_, _, _, created, _ := newMCPAPIKeyAuthFixture()
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			req.Header.Set("Authorization", "Bearer "+created.Secret)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))
		})

		It("constructs MCP handlers across authentication modes", func() {
			routes := NewRoutes(RoutesConfig{})
			opts := httpinternal.RouterOptions{AuthDisabled: true}

			Expect(routes.NewHTTPHandler(opts)).NotTo(BeNil())
			Expect(routes.NewPrivateHTTPHandler(opts)).NotTo(BeNil())
			Expect(routes.NewActorContextHTTPHandler(opts)).NotTo(BeNil())
			Expect(routes.NewActorContextServer(opts)).NotTo(BeNil())
		})

		It("skips route registration when MCP or OAuth prerequisites are missing", func() {
			routes := &Routes{}

			Expect(func() {
				routes.RegisterRoutes(httpinternal.RouterContext{})
			}).NotTo(Panic())

			Expect(func() {
				routes.RegisterRoutes(httpinternal.RouterContext{
					Opts: httpinternal.RouterOptions{
						MCPEnabled:  true,
						MCPBindHost: "127.0.0.1",
					},
				})
			}).NotTo(Panic())
		})
	})

	Describe("context helper guards", func() {
		It("reports default sync, session, actor, and formatting values", Label("unit"), func() {
			Expect(treeDisplayDepth(-1).ChildDepth()).To(Equal(treeDisplayDepth(-1)))
			Expect(treeDisplayDepth(2).ChildDepth()).To(Equal(treeDisplayDepth(1)))
			Expect((&Routes{}).currentWorkspaceSyncStatus()).To(Equal(workspacesync.SyncStatus{Enabled: false}))
			Expect(workspaceActorForToolActor(toolActor{})).To(Equal(workspacesync.PublicEditorActor()))
			Expect(contextSessionKey(nil, toolActor{ID: "actor-1"})).To(Equal("actor-1:sessionless"))
			Expect(formatContextTime(time.Time{})).To(BeEmpty())
			Expect(formatContextTime(time.Date(2026, 6, 20, 12, 30, 0, 0, time.FixedZone("CET", 3600)))).To(Equal("2026-06-20T11:30:00Z"))
			Expect(toolIDMembershipFor([]ToolID{ToolGetPage}, ToolGetPage)).To(Equal(toolIDIncluded))
			Expect(toolIDMembershipFor([]ToolID{ToolGetPage}, ToolValidateWiki)).To(Equal(toolIDExcluded))
		})

		It("falls back from snapshot listing to sync status changed paths", Label("integration"), func() {
			routes := newContextToolTestRoutes()
			home, err := routes.treeService.FindPageByRoutePathAndKind(newFixtureRoutePath("home"), tree.NodeKindPage)
			Expect(err).NotTo(HaveOccurred())
			status := workspacesync.SyncStatus{
				LastCommitHash:             "latest",
				LastSyncTime:               time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC),
				RecentChangedMarkdownPaths: []string{"home.md"},
			}
			routes.listWorkspaceSnapshots = func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				return workspacesync.SnapshotList{}, errors.New("snapshot backend unavailable")
			}

			Expect(routes.recentChanges(context.Background(), status, 0)).To(BeEmpty())
			changes := routes.recentChanges(context.Background(), status, 1)

			Expect(changes).To(HaveExactElements(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"CommitID":     Equal("latest"),
				"ChangedCount": Equal(1),
				"ChangedPaths": Equal([]string{"home.md"}),
				"PageIDs":      ConsistOf(home.ID),
			})))
		})

		It("reports commit deltas across pagination outcomes", Label("integration"), func() {
			routes := newContextToolTestRoutes()
			status := workspacesync.SyncStatus{LastCommitHash: "latest"}

			Expect(changesSinceCommitOutcomeFor(routes, status, workspacesync.CommitHashFromString("missing"))).To(matchChangesSinceCommit(
				changesStoppedBeforeRequestedCommit,
				BeNil(),
			))

			calls := 0
			routes.listWorkspaceSnapshots = func(_ context.Context, cursor workspacesync.CommitHash, limit workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				calls++
				Expect(limit).To(BeNumerically("<=", workspacesync.SnapshotLimit(50)))
				switch cursor {
				case "":
					return workspacesync.SnapshotList{
						Snapshots:  []workspacesync.Snapshot{{ID: "newer", ChangedMarkdownPaths: []string{"home.md"}}},
						NextCursor: "cursor-2",
					}, nil
				case "cursor-2":
					return workspacesync.SnapshotList{
						Snapshots: []workspacesync.Snapshot{{ID: "target"}},
					}, nil
				default:
					return workspacesync.SnapshotList{}, errors.New("unexpected cursor")
				}
			}

			completeChanges := changesSinceCommitOutcomeFor(routes, status, workspacesync.CommitHashFromString("target"))

			Expect(calls).To(Equal(2))
			Expect(completeChanges).To(matchChangesSinceCommit(
				changesReachedRequestedCommit,
				HaveExactElements(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"CommitID": Equal("newer"),
				})),
			))

			routes.listWorkspaceSnapshots = func(context.Context, workspacesync.CommitHash, workspacesync.SnapshotLimit) (workspacesync.SnapshotList, error) {
				return workspacesync.SnapshotList{
					Snapshots: []workspacesync.Snapshot{{ID: "newer"}},
				}, nil
			}
			Expect(changesSinceCommitOutcomeFor(routes, status, workspacesync.CommitHashFromString("missing"))).To(matchChangesSinceCommit(
				changesStoppedBeforeRequestedCommit,
				HaveLen(1),
			))
		})

		It("handles page ID lookup guard branches for recent changes", Label("integration"), func() {
			routes := newContextToolTestRoutes()
			sectionID, err := routes.treeService.CreateNode("system", nil, "Guide", "guide", testNodeKindPtr(tree.NodeKindSection))
			Expect(err).NotTo(HaveOccurred())

			Expect((&Routes{}).pageIDsForMarkdownPaths([]string{"guide.md"})).To(BeNil())
			Expect(routes.pageIDForRecentChangeRoute("", tree.NodeKindSection)).To(Equal(tree.RootPageID))
			Expect(routes.pageIDForRecentChangeRoute(newFixtureRoutePath("guide"), "")).To(Equal(*sectionID))
			Expect(routes.pageIDForRecentChangeRoute(newFixtureRoutePath("missing"), tree.NodeKindPage)).To(BeEmpty())
			Expect(routes.pageIDForMarkdownPath("bad path.md")).To(BeEmpty())
		})
	})

	Describe("validation helper guards", func() {
		It("caches asset predicates and handles nil factories", Label("unit"), func() {
			nilFactory := cachedValidationAssetExists(nil)
			Expect(cachedValidationAssetDestinationFor(nilFactory, newFixturePageID("page-1"), "image.png")).To(matchValidationAssetPresence(validationAssetAbsent, Equal("image.png")))

			calls := 0
			exists := cachedValidationAssetExists(func(pageID tree.PageID) func(string) bool {
				calls++
				if pageID == newFixturePageID("page-2") {
					return nil
				}
				return func(destination string) bool { return destination == "hit.png" }
			})

			Expect(cachedValidationAssetDestinationFor(exists, "", "hit.png")).To(matchValidationAssetPresence(validationAssetAbsent, Equal("hit.png")))
			Expect(calls).To(BeZero())
			Expect(cachedValidationAssetDestinationFor(exists, newFixturePageID("page-1"), "hit.png")).To(matchValidationAssetPresence(validationAssetPresent, Equal("hit.png")))
			Expect(cachedValidationAssetDestinationFor(exists, newFixturePageID("page-1"), "miss.png")).To(matchValidationAssetPresence(validationAssetAbsent, Equal("miss.png")))
			Expect(cachedValidationAssetDestinationFor(exists, newFixturePageID("page-1"), "hit.png")).To(matchValidationAssetPresence(validationAssetPresent, Equal("hit.png")))
			Expect(calls).To(Equal(1))
			Expect(cachedValidationAssetDestinationFor(exists, newFixturePageID("page-2"), "hit.png")).To(matchValidationAssetPresence(validationAssetAbsent, Equal("hit.png")))
			Expect(calls).To(Equal(2))
		})

		It("deduplicates validation issues by the full issue key", Label("unit"), func() {
			duplicate := wikivalidation.Issue{
				Severity:  wikivalidation.IssueSeverityError,
				Code:      wikivalidation.IssueCodeBrokenLink,
				RoutePath: newFixtureRoutePath("docs"),
				PageID:    newFixturePageID("page-1"),
				Message:   "broken",
			}
			distinct := duplicate
			distinct.Message = "different"

			Expect(dedupeValidationIssues([]wikivalidation.Issue{duplicate, duplicate, distinct})).To(Equal([]wikivalidation.Issue{duplicate, distinct}))
		})

		It("normalizes content validation paths and source kinds", Label("integration"), func() {
			routes := newContextToolTestRoutes()
			sectionID, err := routes.treeService.CreateNode("system", nil, "Guide", "guide", testNodeKindPtr(tree.NodeKindSection))
			Expect(err).NotTo(HaveOccurred())
			pageID, err := routes.treeService.CreateNode("system", nil, "Guide Page", "guide-page", testNodeKindPtr(tree.NodeKindPage))
			Expect(err).NotTo(HaveOccurred())

			Expect((&Routes{}).validationSourceKind(newFixturePageID("missing"))).To(Equal(tree.NodeKindPage))
			Expect(routes.validationSourceKind("")).To(Equal(tree.NodeKindPage))
			Expect(routes.validationSourceKind(*sectionID)).To(Equal(tree.NodeKindSection))
			Expect(routes.validationSourceKind(*pageID)).To(Equal(tree.NodeKindPage))
			Expect((&Routes{}).validationSourceKindForRoute(newFixtureRoutePath("guide"))).To(Equal(tree.NodeKindPage))
			Expect(routes.validationSourceKindForRoute("")).To(Equal(tree.NodeKindPage))
			Expect(routes.validationSourceKindForRoute(newFixtureRoutePath("guide"))).To(Equal(tree.NodeKindSection))
			Expect(routes.validationSourceKindForRoute(newFixtureRoutePath("missing"))).To(Equal(tree.NodeKindPage))
			Expect(routes.validationSourceMarkdownFile(newFixtureRoutePath("guide"))).To(Equal(tree.MarkdownPath("guide/index.md")))
			Expect((&Routes{}).validationSourceMarkdownFile(newFixtureRoutePath("guide"))).To(Equal(tree.MarkdownPath("guide.md")))

			Expect(validationPageIDResolutionFor(routes, newFixtureRoutePath("guide"))).To(matchValidationPageID(validationResolved, Equal(*sectionID)))
			Expect(validationPageIDResolutionFor(routes, "")).To(matchValidationPageID(validationUnresolved, BeEmpty()))
			Expect(validationPageIDKindResolutionFor(routes, newFixtureRoutePath("guide"), tree.NodeKindSection)).To(matchValidationPageID(validationResolved, Equal(*sectionID)))
			Expect(validationPageIDKindResolutionFor(routes, "", tree.NodeKindPage)).To(matchValidationPageID(validationUnresolved, BeEmpty()))

			routePath, kind, err := routes.normalizeValidationContentPathInput("guide.md", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(Equal(newFixtureRoutePath("guide")))
			Expect(kind).To(Equal(tree.NodeKindPage))

			_, _, err = routes.normalizeValidationContentPathInput("guide.md", tree.NodeKindSection)
			Expect(err).To(MatchError(errValidationContentKindMismatch))

			guideDir := filepath.Join(routes.treeService.RootDir(), "guide")
			Expect(os.MkdirAll(guideDir, 0o755)).To(Succeed())
			Expect(os.Remove(filepath.Join(guideDir, "index.md"))).To(Succeed())
			Expect(os.WriteFile(filepath.Join(guideDir, "README.md"), []byte("# Guide\n"), 0o644)).To(Succeed())
			routePath, kind, err = routes.normalizeValidationContentPathInput("guide/README.md", tree.NodeKindSection)
			Expect(err).NotTo(HaveOccurred())
			Expect(routePath).To(Equal(newFixtureRoutePath("guide")))
			Expect(kind).To(Equal(tree.NodeKindSection))
		})

		It("cleans validation asset destinations and resolves root markdown targets", Label("integration"), func() {
			routes := newContextToolTestRoutes()

			Expect(cleanValidationAssetDestination(" <image.png?size=large#preview> ")).To(Equal("image.png"))
			Expect(markdownTargetNodeKind("unexpected")).To(Equal(tree.NodeKindPage))

			Expect(validationMarkdownLinkResolutionFor(routes, "", tree.NodeKindSection, "/")).To(matchValidationMarkdownLink(
				validationResolved,
				BeEmpty(),
				Equal(tree.NodeKindSection),
				BeEmpty(),
			))
		})
	})

	Describe("subtree helper guards", func() {
		It("handles nil nodes and depth bounds", Label("unit"), func() {
			ctx := context.Background()
			routes := &Routes{}
			negative := -1
			zero := 0
			huge := 99

			Expect(parentPathForNode(nil)).To(BeEmpty())
			Expect(routes.subtreeNode(ctx, nil, "", 0, subtreeOptions{})).To(BeNil())
			Expect(routes.subtreeLinkCounts(ctx, nil)).To(BeNil())
			Expect(routes.subtreeContentPreview(newFixturePageID("page-1"))).To(BeEmpty())
			Expect(func() { ensureSubtreeNodeChildrenArray(nil) }).NotTo(Panic())
			Expect(subtreeExtentFor(nil, treeDisplayDepth(1))).To(Equal(subtreeExtentComplete))
			Expect(subtreeExtentFor(&tree.PageNode{Children: []*tree.PageNode{{}}}, treeDisplayDepth(-1))).To(Equal(subtreeExtentComplete))

			_, err := boundedSubtreeDepth(&negative)
			Expect(err).To(MatchError(errSubtreeDepthInvalid))

			depth, err := boundedSubtreeDepth(&zero)
			Expect(err).NotTo(HaveOccurred())
			Expect(depth).To(Equal(treeDisplayDepth(0)))

			depth, err = boundedSubtreeDepth(&huge)
			Expect(err).NotTo(HaveOccurred())
			Expect(depth).To(Equal(treeDisplayDepth(maxContextTreeDepth)))
		})

		It("builds content previews and reports truncation at the requested depth", Label("integration"), func() {
			routes := newContextToolTestRoutes()
			parentID, err := routes.treeService.CreateNode("system", nil, "Parent", "parent", testNodeKindPtr(tree.NodeKindSection))
			Expect(err).NotTo(HaveOccurred())
			childID, err := routes.treeService.CreateNode("system", parentID, "Child", "child", testNodeKindPtr(tree.NodeKindPage))
			Expect(err).NotTo(HaveOccurred())
			page, err := routes.treeService.GetPage(*childID)
			Expect(err).NotTo(HaveOccurred())
			content := "one\n\n two\tthree"
			Expect(routes.treeService.UpdateNodeUncheckedVersion("system", page.ID, page.Title, page.Slug, &content, false)).To(Succeed())

			parent, err := routes.treeService.GetPage(*parentID)
			Expect(err).NotTo(HaveOccurred())
			child, err := routes.treeService.GetPage(*childID)
			Expect(err).NotTo(HaveOccurred())

			Expect(parentPathForNode(child.PageNode)).To(Equal("parent"))
			Expect(routes.subtreeContentPreview(*childID)).To(Equal("one two three"))
			longContent := string(bytes.Repeat([]byte("word "), 70))
			Expect(routes.treeService.UpdateNodeUncheckedVersion("system", page.ID, page.Title, page.Slug, &longContent, false)).To(Succeed())
			Expect(routes.subtreeContentPreview(*childID)).To(HaveLen(240))
			Expect(subtreeExtentFor(parent.PageNode, treeDisplayDepth(0))).To(Equal(subtreeExtentTruncated))
			Expect(subtreeExtentFor(parent.PageNode, treeDisplayDepth(1))).To(Equal(subtreeExtentComplete))
		})
	})

	Describe("JSON and multipart helper guards", Label("unit"), func() {
		It("closes in-memory multipart files without side effects", func() {
			file := &memoryMultipartFile{Reader: bytes.NewReader([]byte("payload"))}

			Expect(file.Close()).To(Succeed())
		})

		It("tracks update-page field presence and malformed raw fields", func() {
			var input updatePageInput

			Expect(json.Unmarshal([]byte(`{"id":"p1","tags":[],"properties":{}}`), &input)).To(Succeed())
			Expect(input).To(matchUpdatePageInputWithExplicitEmptyPatchFields())

			Expect(json.Unmarshal([]byte(`{"tags":{}}`), &input)).To(matchJSONTypeError())
			Expect(json.Unmarshal([]byte(`{"properties":[]}`), &input)).To(matchJSONTypeError())
			Expect(json.Unmarshal([]byte(`{`), &input)).To(matchJSONSyntaxError())
		})

		It("bounds degenerate base64 decoded sizes", func() {
			Expect(base64DecodedSize("=")).To(BeZero())
			Expect(base64DecodedSize("YQ==")).To(BeNumerically("==", 1))
			Expect(base64DecodedSize("YWI=")).To(BeNumerically("==", 2))
		})
	})
})

type checkpointEvictionDecision string

const (
	checkpointSessionEvicted  checkpointEvictionDecision = "session evicted"
	checkpointSessionRetained checkpointEvictionDecision = "session retained"
)

func checkpointEvictionDecisionFor(evicted bool) checkpointEvictionDecision {
	if evicted {
		return checkpointSessionEvicted
	}
	return checkpointSessionRetained
}

type toolIDMembershipState string

const (
	toolIDIncluded toolIDMembershipState = "included"
	toolIDExcluded toolIDMembershipState = "excluded"
)

func toolIDMembershipFor(values []ToolID, needle ToolID) toolIDMembershipState {
	if containsToolID(values, needle) {
		return toolIDIncluded
	}
	return toolIDExcluded
}
