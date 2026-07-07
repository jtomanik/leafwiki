package main

import (
	"encoding/json"
	"strings"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/agenthooks"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

type leafwikiActorAuthMethod string

const (
	leafwikiActorAuthMethodAPIKey       leafwikiActorAuthMethod = "api_key"
	leafwikiActorAuthMethodDisabled     leafwikiActorAuthMethod = "disabled"
	leafwikiActorAuthMethodOAuth        leafwikiActorAuthMethod = "oauth"
	leafwikiActorAuthMethodPublicAccess leafwikiActorAuthMethod = "public_access"
	leafwikiActorAuthMethodRemoteUser   leafwikiActorAuthMethod = "remote_user"
)

func newFixtureWorkspaceID(raw string) workspaceid.WorkspaceID {
	ginkgo.GinkgoHelper()
	id, err := workspaceid.ParseWorkspaceID(raw)
	Expect(err).To(Succeed())
	return id
}

func newFixtureUserID(raw string) coreauth.UserID {
	ginkgo.GinkgoHelper()
	return coreauth.UserIDFromString(raw)
}

func newFixtureGrantRole(raw string) wikid.GrantRole {
	ginkgo.GinkgoHelper()
	return wikid.GrantRole(raw)
}

func newFixtureRoleName(raw string) projectdaemon.RoleName {
	ginkgo.GinkgoHelper()
	return projectdaemon.RoleName(raw)
}

func newFixtureAgentEventName(raw string) agenthooks.AgentEventName {
	ginkgo.GinkgoHelper()
	return agenthooks.AgentEventName(raw)
}

func newFixtureAgentToolName(raw string) agenthooks.AgentToolName {
	ginkgo.GinkgoHelper()
	return agenthooks.AgentToolNameFromString(raw)
}

func newFixtureSessionID(raw string) projectdaemon.SessionID {
	ginkgo.GinkgoHelper()
	return projectdaemon.SessionIDFromString(raw)
}

func newFixtureErrorCode(raw string) sharederrors.ErrorCode {
	ginkgo.GinkgoHelper()
	return sharederrors.ErrorCode(raw)
}

func HaveActorSubjectForUser(userID coreauth.UserID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(actual any) coreauth.UserID {
		var subject string
		switch actor := actual.(type) {
		case projectdaemon.ActorContext:
			subject = actor.Subject
		case wikid.WorkspaceSubject:
			subject = actor.Subject
		}
		raw, ok := strings.CutPrefix(subject, "user:")
		if !ok {
			return ""
		}
		return coreauth.UserIDFromString(raw)
	}, Equal(userID))
}

func HaveActorAuthMethod(method leafwikiActorAuthMethod) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return HaveField("AuthMethod", Equal(string(method)))
}

func HaveActorWorkspace(workspaceID workspaceid.WorkspaceID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return HaveField("WorkspaceID", Equal(workspaceID))
}

func HaveCoreAuthUserID(userID coreauth.UserID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return HaveField("ID", Equal(userID))
}

func HaveWikidGrantForWorkspace(workspaceID workspaceid.WorkspaceID, role wikid.GrantRole) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return SatisfyAll(
		HaveField("Subject", HavePrefix("user:")),
		HaveField("WorkspaceID", Equal(workspaceID)),
		HaveField("Role", Equal(role)),
	)
}

func MatchWorkspaceStatus(state wikid.WorkspaceState, fields gstruct.Fields) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	allFields := gstruct.Fields{"State": Equal(state)}
	for name, matcher := range fields {
		allFields[name] = matcher
	}
	return gstruct.MatchFields(gstruct.IgnoreExtras, allFields)
}

type leafwikiWorkspaceStatusFailure struct {
	State wikid.WorkspaceState
	Error leafwikiRuntimeFailure
}

type leafwikiRoleHealthFailure struct {
	Role  projectdaemon.RoleName
	State projectdaemon.RoleState
	Error leafwikiRuntimeFailure
}

type leafwikiRuntimeFailure string

const (
	leafwikiRuntimeFailureExitStatus   leafwikiRuntimeFailure = "exit status 2"
	leafwikiRuntimeFailureRestartLimit leafwikiRuntimeFailure = "restart limit"
	leafwikiRuntimeFailureStopped      leafwikiRuntimeFailure = "stopped"
	leafwikiRuntimeFailureBoom         leafwikiRuntimeFailure = "boom"
)

func MatchWorkspaceStatusFailure(state wikid.WorkspaceState, failure leafwikiRuntimeFailure) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(status wikid.WorkspaceStatus) leafwikiWorkspaceStatusFailure {
		return leafwikiWorkspaceStatusFailure{State: status.State, Error: leafwikiRuntimeFailure(status.Error)}
	}, Equal(leafwikiWorkspaceStatusFailure{State: state, Error: failure}))
}

func MatchProjectDaemonRoleFailure(role projectdaemon.RoleName, state projectdaemon.RoleState, failure leafwikiRuntimeFailure) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(health projectdaemon.RoleHealth) leafwikiRoleHealthFailure {
		return leafwikiRoleHealthFailure{Role: health.Name, State: health.State, Error: leafwikiRuntimeFailure(health.Error)}
	}, Equal(leafwikiRoleHealthFailure{Role: role, State: state, Error: failure}))
}

func MatchSDKTokenUserID(userID coreauth.UserID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(info *sdkauth.TokenInfo) coreauth.UserID {
		if info == nil {
			return ""
		}
		return coreauth.UserIDFromString(info.UserID)
	}, Equal(userID))
}

func MatchTokenVerifyUserID(userID coreauth.UserID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(body any) (coreauth.UserID, error) {
		var raw []byte
		switch value := body.(type) {
		case []byte:
			raw = value
		case string:
			raw = []byte(value)
		}
		var payload struct {
			UserID coreauth.UserID `json:"userId"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return "", err
		}
		return payload.UserID, nil
	}, Equal(userID))
}

func MatchUserIDString(userID coreauth.UserID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(raw string) coreauth.UserID {
		return coreauth.UserIDFromString(raw)
	}, Equal(userID))
}

type leafwikiUsageObservation struct {
	MessageIDs    []leafwikiUsageMessageID
	Supported     []leafwikiUsageToken
	RemovedLegacy []leafwikiUsageToken
	LegacyOverlap []leafwikiUsageToken
}

func MatchLeafwikiUsageContract() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(observeLeafwikiUsage, Equal(leafwikiUsageObservation{
		MessageIDs: []leafwikiUsageMessageID{
			leafwikiUsageMessageCLIHelpUsage,
			leafwikiUsageMessageCLIHelpBody,
		},
		Supported: []leafwikiUsageToken{
			leafwikiUsageTokenJWTSecret,
			leafwikiUsageTokenAdminPassword,
			leafwikiUsageTokenAllowInsecure,
			leafwikiUsageTokenDataDir,
			leafwikiUsageTokenRootDir,
			leafwikiUsageTokenLogTarget,
			leafwikiUsageTokenLogFile,
			leafwikiUsageTokenMCP,
			leafwikiUsageTokenAPIKey,
			leafwikiUsageTokenDaemonIdleTimeout,
			leafwikiUsageTokenConfig,
			leafwikiUsageTokenAgentHookCommand,
			leafwikiUsageTokenDaemonCommand,
			leafwikiUsageTokenRootDirEnv,
			leafwikiUsageTokenLogTargetEnv,
			leafwikiUsageTokenLogFileEnv,
			leafwikiUsageTokenMCPEnv,
			leafwikiUsageTokenMCPAPIKeyEnv,
		},
		RemovedLegacy: []leafwikiUsageToken{
			leafwikiUsageTokenEnableRevision,
			leafwikiUsageTokenWorkspaceSync,
			leafwikiUsageTokenRevisionHistory,
			leafwikiUsageTokenEnableMCP,
			leafwikiUsageTokenMCPStdio,
			leafwikiUsageTokenEnableRevisionEnv,
			leafwikiUsageTokenWorkspaceSyncEnv,
			leafwikiUsageTokenRevisionHistoryEnv,
			leafwikiUsageTokenRuntimeStackEnv,
			leafwikiUsageTokenEnableMCPEnv,
			leafwikiUsageTokenMCPStdioEnv,
		},
	}))
}

func ContainRenderedLeafwikiUsageMessage(messageID leafwikiUsageMessageID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	rendered := localization.English.Render(string(messageID), "").Message
	return ContainSubstring(rendered)
}

func observeLeafwikiUsage(usage leafwikiUsageContract) leafwikiUsageObservation {
	supported := make(map[leafwikiUsageToken]struct{}, len(usage.Supported))
	for _, token := range usage.Supported {
		supported[token] = struct{}{}
	}
	var overlap []leafwikiUsageToken
	for _, token := range usage.RemovedLegacy {
		if _, ok := supported[token]; ok {
			overlap = append(overlap, token)
		}
	}
	return leafwikiUsageObservation{
		MessageIDs:    usage.MessageIDs,
		Supported:     usage.Supported,
		RemovedLegacy: usage.RemovedLegacy,
		LegacyOverlap: overlap,
	}
}
