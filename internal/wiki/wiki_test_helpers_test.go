package wiki

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/tree"
	corelinks "github.com/perber/wiki/internal/links"
	"github.com/perber/wiki/internal/projectdaemon"
	wikihealth "github.com/perber/wiki/internal/wiki/health"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
)

func closeWithErrorCheckForTest(closer func() error) {
	ginkgo.GinkgoHelper()

	Expect(closer()).To(Succeed())
}

func existAsFileSystemPath() types.GomegaMatcher {
	return WithTransform(func(path string) error {
		_, err := os.Stat(path)
		return err
	}, Succeed())
}

func beMissingFileSystemPath() types.GomegaMatcher {
	return WithTransform(func(path string) error {
		_, err := os.Stat(path)
		return err
	}, MatchError(os.ErrNotExist))
}

func matchResolvedOutgoingLinkToPath(path tree.RoutePath) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(outgoingLinkResolutionFromItem, Equal(outgoingLinkResolutionObservation{
		ToPath: path,
		State:  outgoingLinkResolved,
	}))
}

type outgoingLinkResolutionState string

const (
	outgoingLinkResolved outgoingLinkResolutionState = "resolved"
	outgoingLinkBroken   outgoingLinkResolutionState = "broken"
)

type outgoingLinkResolutionObservation struct {
	ToPath tree.RoutePath
	State  outgoingLinkResolutionState
}

func outgoingLinkResolutionFromItem(item corelinks.OutgoingResultItem) outgoingLinkResolutionObservation {
	state := outgoingLinkResolved
	if item.Broken {
		state = outgoingLinkBroken
	}
	return outgoingLinkResolutionObservation{
		ToPath: item.ToPath,
		State:  state,
	}
}

type wikiServiceSet struct {
	User          any
	Auth          any
	APIKeys       any
	OAuth         any
	Branding      any
	Tree          any
	Asset         any
	Search        any
	Links         any
	Tags          any
	Properties    any
	WorkspaceSync any
}

func haveControlPlaneOnlyServices() types.GomegaMatcher {
	return WithTransform(func(w *Wiki) wikiServiceSet {
		return wikiServiceSet{
			User:          w.user,
			Auth:          w.auth,
			APIKeys:       w.apiKeys,
			OAuth:         w.oauth,
			Branding:      w.branding,
			Tree:          w.tree,
			Asset:         w.asset,
			Search:        w.searchIndex,
			Links:         w.links,
			Tags:          w.tags,
			Properties:    w.props,
			WorkspaceSync: w.workspaceSync,
		}
	}, gstruct.MatchAllFields(gstruct.Fields{
		"User":          Not(BeNil()),
		"Auth":          Not(BeNil()),
		"APIKeys":       Not(BeNil()),
		"OAuth":         Not(BeNil()),
		"Branding":      Not(BeNil()),
		"Tree":          BeNil(),
		"Asset":         BeNil(),
		"Search":        BeNil(),
		"Links":         BeNil(),
		"Tags":          BeNil(),
		"Properties":    BeNil(),
		"WorkspaceSync": BeNil(),
	}))
}

func haveWorkspaceSyncValidationPath(path types.GomegaMatcher) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("LastCommitHash", Not(BeEmpty())),
		HaveField("ValidationErrors", ContainElement(HaveField("Path", path))),
	)
}

type runtimeHealthWireSnapshot struct {
	Healthy bool
	Checks  map[string]string
}

func runtimeHealthWireSnapshotFor(required []projectdaemon.RoleName, roles []projectdaemon.RoleHealth) runtimeHealthWireSnapshot {
	ginkgo.GinkgoHelper()

	useCase := wikihealth.NewHealthUseCase(nil, nil, wikiTestTempDir(), wikihealth.HealthUseCaseOptions{
		RequiredRoles: required,
		RoleHealth: func() []projectdaemon.RoleHealth {
			return roles
		},
	})
	healthy, checks := useCase.Execute()
	return runtimeHealthWireSnapshot{
		Healthy: healthy,
		Checks:  checks.HTTPMap(),
	}
}

func haveRuntimeRoleHealth(role projectdaemon.RoleName, state projectdaemon.RoleState) types.GomegaMatcher {
	base := runtimeHealthWireSnapshotFor(nil, nil)
	withRole := runtimeHealthWireSnapshotFor([]projectdaemon.RoleName{role}, []projectdaemon.RoleHealth{{Name: role, State: state}})

	matchers := make([]types.GomegaMatcher, 0, len(withRole.Checks))
	for key, value := range withRole.Checks {
		if _, ok := base.Checks[key]; ok {
			continue
		}
		matchers = append(matchers, HaveKeyWithValue(key, value))
	}
	return SatisfyAll(matchers...)
}

type mcpToolSuccessMatcher struct {
	structuredContent types.GomegaMatcher
}

func haveSuccessfulMCPToolResult(structuredContent types.GomegaMatcher) types.GomegaMatcher {
	return mcpToolSuccessMatcher{structuredContent: structuredContent}
}

func (matcher mcpToolSuccessMatcher) Match(actual any) (bool, error) {
	result, ok := actual.(*sdkmcp.CallToolResult)
	if !ok {
		return false, fmt.Errorf("expected *mcp.CallToolResult, got %T", actual)
	}
	if result == nil || result.IsError {
		return false, nil
	}
	return matcher.structuredContent.Match(result.StructuredContent)
}

func (matcher mcpToolSuccessMatcher) FailureMessage(actual any) string {
	return fmt.Sprintf("Expected\n\t%#v\nto be a successful MCP tool result with matching structured content", actual)
}

func (matcher mcpToolSuccessMatcher) NegatedFailureMessage(actual any) string {
	return fmt.Sprintf("Expected\n\t%#v\nnot to be a successful MCP tool result with matching structured content", actual)
}

func haveSuccessfulMCPRevisionHistory(count int) types.GomegaMatcher {
	return haveSuccessfulMCPToolResult(HaveKeyWithValue("revisions", HaveLen(count)))
}

func mcpCreatedPageID(result *sdkmcp.CallToolResult) tree.PageID {
	content, ok := result.StructuredContent.(map[string]any)
	if !ok {
		return ""
	}
	page, ok := content["page"].(map[string]any)
	if !ok {
		return ""
	}
	id, ok := page["id"].(string)
	if !ok {
		return ""
	}
	return tree.PageIDFromString(id)
}

func createWikiTestInstance() *Wiki {
	ginkgo.GinkgoHelper()

	wikiInstance, err := NewWiki(&WikiOptions{
		StorageDir:          wikiTestTempDir(),
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	Expect(err).To(Succeed())
	return wikiInstance
}

func createWikiTestInstanceWithWorkspace(workspace Workspace) *Wiki {
	ginkgo.GinkgoHelper()

	wikiInstance, err := NewWiki(&WikiOptions{
		Workspace:           workspace,
		AdminPassword:       "admin",
		JWTSecret:           "secretkey",
		AccessTokenTimeout:  15 * time.Minute,
		RefreshTokenTimeout: 7 * 24 * time.Hour,
	})
	Expect(err).To(Succeed())
	return wikiInstance
}

func pageNodeKind() *tree.NodeKind {
	kind := tree.NodeKindPage
	return &kind
}

func pageIDPtr(id tree.PageID) *tree.PageID {
	return &id
}

func createPageForTest(w *Wiki, userID string, parentID *tree.PageID, title, slug string, kind *tree.NodeKind) *tree.Page {
	ginkgo.GinkgoHelper()

	out, err := wikipages.NewCreatePageUseCase(w.tree, w.slug, w.newPageOrchestrator(), w.log).Execute(
		context.Background(),
		wikipages.CreatePageInput{UserID: tree.UserIDFromString(userID), ParentID: parentID, Title: title, Slug: tree.SlugFromString(slug), Kind: kind},
	)
	Expect(err).To(Succeed())
	return out.Page
}

func updatePageForTest(w *Wiki, userID string, id tree.PageID, title, slug string, content *string, kind *tree.NodeKind) *tree.Page {
	ginkgo.GinkgoHelper()

	current, err := w.tree.GetPage(id)
	Expect(err).To(Succeed())

	out, err := wikipages.NewUpdatePageUseCase(w.tree, w.slug, w.newPageOrchestrator(), w.log).Execute(
		context.Background(),
		wikipages.UpdatePageInput{UserID: tree.UserIDFromString(userID), ID: id, Version: tree.PageVersionFromString(current.Version()), Title: title, Slug: tree.SlugFromString(slug), Content: content, Kind: kind},
	)
	Expect(err).To(Succeed())
	return out.Page
}

func deletePageForTest(w *Wiki, userID string, id tree.PageID, recursive bool) {
	ginkgo.GinkgoHelper()

	current, err := w.tree.GetPage(id)
	Expect(err).To(Succeed())

	err = wikipages.NewDeletePageUseCase(w.tree, w.asset, w.newPageOrchestrator(), w.log).Execute(
		context.Background(),
		wikipages.DeletePageInput{UserID: tree.UserIDFromString(userID), ID: id, Version: tree.PageVersionFromString(current.Version()), Recursive: recursive},
	)
	Expect(err).To(Succeed())
}

func mcpServerStoppedSuccessfully(err error) bool {
	return err == nil || errors.Is(err, context.Canceled)
}
