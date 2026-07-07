package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	coreauth "github.com/perber/wiki/internal/core/auth"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync"
)

func matchMCPUser(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

func matchPresenceStatusOutput(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchPresenceSession(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchRefreshOutput(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

type privateActorContextState string

const (
	privateActorContextHandled  privateActorContextState = "handled"
	privateActorContextDeferred privateActorContextState = "deferred"
)

type privateActorContextOutcome struct {
	User  *coreauth.User
	Err   error
	State privateActorContextState
}

func privateActorContextFor(routes *Routes, header http.Header) privateActorContextOutcome {
	GinkgoHelper()
	user, handled, err := routes.actorFromPrivateContextHeader(header)
	state := privateActorContextDeferred
	if handled {
		state = privateActorContextHandled
	}
	return privateActorContextOutcome{User: user, Err: err, State: state}
}

func matchPrivateActorContext(state privateActorContextState, userMatcher types.GomegaMatcher, errMatcher types.GomegaMatcher) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State": Equal(state),
		"User":  userMatcher,
		"Err":   errMatcher,
	})
}

type changesSinceCommitState string

const (
	changesReachedRequestedCommit       changesSinceCommitState = "reached requested commit"
	changesStoppedBeforeRequestedCommit changesSinceCommitState = "stopped before requested commit"
)

type changesSinceCommitOutcome struct {
	Changes []recentChangeOutput
	State   changesSinceCommitState
}

func changesSinceCommitOutcomeFor(routes *Routes, status workspacesync.SyncStatus, commitHash workspacesync.CommitHash) changesSinceCommitOutcome {
	GinkgoHelper()
	changes, complete := routes.changesSinceCommit(context.Background(), status, commitHash)
	state := changesStoppedBeforeRequestedCommit
	if complete {
		state = changesReachedRequestedCommit
	}
	return changesSinceCommitOutcome{Changes: changes, State: state}
}

func matchChangesSinceCommit(state changesSinceCommitState, changesMatcher types.GomegaMatcher) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State":   Equal(state),
		"Changes": changesMatcher,
	})
}

type contextRefreshDecision string

const (
	contextRefreshRequested contextRefreshDecision = "requested"
	contextRefreshSkipped   contextRefreshDecision = "skipped"
)

func contextRefreshDecisionFor(syncMode string, status workspacesync.SyncStatus) contextRefreshDecision {
	if shouldRefreshForContext(syncMode, status) {
		return contextRefreshRequested
	}
	return contextRefreshSkipped
}

type subtreeExtent string

const (
	subtreeExtentComplete  subtreeExtent = "complete"
	subtreeExtentTruncated subtreeExtent = "truncated"
)

func subtreeExtentFor(node *tree.PageNode, depth treeDisplayDepth) subtreeExtent {
	if subtreeTruncated(node, depth) {
		return subtreeExtentTruncated
	}
	return subtreeExtentComplete
}

type validationPagePresenceState string

const (
	validationPagePresent validationPagePresenceState = "present"
	validationPageAbsent  validationPagePresenceState = "absent"
)

type validationPagePresence struct {
	PageID tree.PageID
	State  validationPagePresenceState
}

func validationPagePresenceFor(routes *Routes, pageID tree.PageID) validationPagePresence {
	state := validationPageAbsent
	if routes.validationPageIDExists(pageID) {
		state = validationPagePresent
	}
	return validationPagePresence{PageID: pageID, State: state}
}

func matchValidationPagePresence(state validationPagePresenceState, pageIDMatcher types.GomegaMatcher) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State":  Equal(state),
		"PageID": pageIDMatcher,
	})
}

type validationAssetPresenceState string

const (
	validationAssetPresent validationAssetPresenceState = "present"
	validationAssetAbsent  validationAssetPresenceState = "absent"
)

type validationAssetPresence struct {
	PageID      tree.PageID
	Destination string
	State       validationAssetPresenceState
}

func validationAssetDestinationFor(pageID tree.PageID, destination string, exists func(string) bool) validationAssetPresence {
	state := validationAssetAbsent
	if exists != nil && exists(destination) {
		state = validationAssetPresent
	}
	return validationAssetPresence{PageID: pageID, Destination: destination, State: state}
}

func cachedValidationAssetDestinationFor(exists func(tree.PageID, string) bool, pageID tree.PageID, destination string) validationAssetPresence {
	state := validationAssetAbsent
	if exists != nil && exists(pageID, destination) {
		state = validationAssetPresent
	}
	return validationAssetPresence{PageID: pageID, Destination: destination, State: state}
}

func matchValidationAssetPresence(state validationAssetPresenceState, destinationMatcher types.GomegaMatcher) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State":       Equal(state),
		"Destination": destinationMatcher,
	})
}

func matchRedactedWorkspacePaths(rootDir string, dataDir string, placeholders ...string) types.GomegaMatcher {
	GinkgoHelper()
	matchers := []types.GomegaMatcher{
		Not(ContainSubstring(rootDir)),
		Not(ContainSubstring(dataDir)),
	}
	for _, placeholder := range placeholders {
		matchers = append(matchers, ContainSubstring(placeholder))
	}
	return SatisfyAll(matchers...)
}

func matchJSONSyntaxError() types.GomegaMatcher {
	GinkgoHelper()
	return Satisfy(func(err error) bool {
		var syntaxErr *json.SyntaxError
		return errors.As(err, &syntaxErr)
	})
}

func matchJSONTypeError() types.GomegaMatcher {
	GinkgoHelper()
	return Satisfy(func(err error) bool {
		var typeErr *json.UnmarshalTypeError
		return errors.As(err, &typeErr)
	})
}

type validationResolutionState string

const (
	validationResolved   validationResolutionState = "resolved"
	validationUnresolved validationResolutionState = "unresolved"
)

type validationMarkdownLinkResolution struct {
	PageID tree.PageID
	Kind   tree.NodeKind
	Code   wikivalidation.IssueCode
	State  validationResolutionState
}

func validationMarkdownLinkResolutionFor(routes *Routes, sourceRoutePath tree.RoutePath, sourceKind tree.NodeKind, destination string) validationMarkdownLinkResolution {
	GinkgoHelper()
	pageID, kind, resolved, code := routes.resolveValidationMarkdownLink(sourceRoutePath, sourceKind, destination)
	state := validationUnresolved
	if resolved {
		state = validationResolved
	}
	return validationMarkdownLinkResolution{
		PageID: pageID,
		Kind:   kind,
		Code:   code,
		State:  state,
	}
}

func matchValidationMarkdownLink(state validationResolutionState, pageIDMatcher types.GomegaMatcher, kindMatcher types.GomegaMatcher, codeMatcher types.GomegaMatcher) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State":  Equal(state),
		"PageID": pageIDMatcher,
		"Kind":   kindMatcher,
		"Code":   codeMatcher,
	})
}

type validationPageIDResolution struct {
	PageID tree.PageID
	State  validationResolutionState
}

func validationPageIDResolutionFor(routes *Routes, routePath tree.RoutePath) validationPageIDResolution {
	GinkgoHelper()
	pageID, resolved := routes.resolveValidationPageID(routePath)
	state := validationUnresolved
	if resolved {
		state = validationResolved
	}
	return validationPageIDResolution{PageID: pageID, State: state}
}

func validationPageIDKindResolutionFor(routes *Routes, routePath tree.RoutePath, kind tree.NodeKind) validationPageIDResolution {
	GinkgoHelper()
	pageID, resolved := routes.resolveValidationPageIDForKind(routePath, kind)
	state := validationUnresolved
	if resolved {
		state = validationResolved
	}
	return validationPageIDResolution{PageID: pageID, State: state}
}

func matchValidationPageID(state validationResolutionState, pageIDMatcher types.GomegaMatcher) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State":  Equal(state),
		"PageID": pageIDMatcher,
	})
}

func mcpTokenInfoRequest(userID tree.UserID) *sdkmcp.CallToolRequest {
	GinkgoHelper()
	return &sdkmcp.CallToolRequest{Extra: &sdkmcp.RequestExtra{TokenInfo: &sdkauth.TokenInfo{UserID: userID.MetadataValue()}}}
}
