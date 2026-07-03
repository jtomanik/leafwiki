package frontd

import (
	"encoding/json"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/workspaceid"
)

type frontdPathParseOutcome uint8

const (
	frontdPathAccepted frontdPathParseOutcome = iota + 1
	frontdPathRejected
)

type frontdStructuredErrorResponse struct {
	Status    int
	Code      sharederrors.ErrorCode
	MessageID sharederrors.MessageID
	Message   string
}

type frontdBasePathResult struct {
	Outcome frontdPathParseOutcome
	Path    string
}

type frontdWorkspaceAPIPathResult struct {
	Outcome      frontdPathParseOutcome
	WorkspaceID  workspaceid.WorkspaceID
	UpstreamPath string
}

type frontdWorkspaceMCPPathResult struct {
	Outcome     frontdPathParseOutcome
	WorkspaceID workspaceid.WorkspaceID
}

func matchStructuredFrontdError(status int, code sharederrors.ErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	return WithTransform(func(rec *httptest.ResponseRecorder) frontdStructuredErrorResponse {
		GinkgoHelper()
		var body struct {
			Error struct {
				Code      sharederrors.ErrorCode `json:"code"`
				MessageID sharederrors.MessageID `json:"messageId"`
				Message   string                 `json:"message"`
			} `json:"error"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), "structured error body: %s", rec.Body.Bytes())
		return frontdStructuredErrorResponse{
			Status:    rec.Code,
			Code:      body.Error.Code,
			MessageID: body.Error.MessageID,
			Message:   body.Error.Message,
		}
	}, SatisfyAll(
		HaveField("Status", Equal(status)),
		HaveField("Code", Equal(code)),
		HaveField("MessageID", Equal(messageID)),
		HaveField("Message", Not(BeEmpty())),
	))
}

func basePathResult(path string, basePath string) frontdBasePathResult {
	strippedPath, ok := stripBasePath(path, basePath)
	return frontdBasePathResult{
		Outcome: frontdPathOutcome(ok),
		Path:    strippedPath,
	}
}

func workspaceAPIPathResult(path string) frontdWorkspaceAPIPathResult {
	workspaceID, upstreamPath, ok := parseWorkspaceAPIPath(path)
	return frontdWorkspaceAPIPathResult{
		Outcome:      frontdPathOutcome(ok),
		WorkspaceID:  workspaceID,
		UpstreamPath: upstreamPath,
	}
}

func workspaceMCPPathResult(path string) frontdWorkspaceMCPPathResult {
	workspaceID, ok := parseWorkspaceMCPPath(path)
	return frontdWorkspaceMCPPathResult{
		Outcome:     frontdPathOutcome(ok),
		WorkspaceID: workspaceID,
	}
}

func frontdPathOutcome(ok bool) frontdPathParseOutcome {
	if ok {
		return frontdPathAccepted
	}
	return frontdPathRejected
}

func ResolveBasePathTo(path string) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Outcome", Equal(frontdPathAccepted)),
		HaveField("Path", Equal(path)),
	)
}

func RejectWorkspaceAPIPath() types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Outcome", Equal(frontdPathRejected)),
		HaveField("WorkspaceID", BeEmpty()),
		HaveField("UpstreamPath", BeEmpty()),
	)
}

func ResolveWorkspaceAPIPath(workspaceID workspaceid.WorkspaceID, upstreamPath string) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Outcome", Equal(frontdPathAccepted)),
		HaveField("WorkspaceID", Equal(workspaceID)),
		HaveField("UpstreamPath", Equal(upstreamPath)),
	)
}

func RejectWorkspaceMCPPath() types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Outcome", Equal(frontdPathRejected)),
		HaveField("WorkspaceID", BeEmpty()),
	)
}

func ResolveWorkspaceMCPPath(workspaceID workspaceid.WorkspaceID) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Outcome", Equal(frontdPathAccepted)),
		HaveField("WorkspaceID", Equal(workspaceID)),
	)
}

func HaveMCPSession(sessionID MCPSessionID) types.GomegaMatcher {
	return WithTransform(snapshotMCPSessionBindings, HaveKey(sessionID))
}

func HaveMCPSessionBinding(sessionID MCPSessionID, workspaceID workspaceid.WorkspaceID) types.GomegaMatcher {
	return WithTransform(snapshotMCPSessionBindings, HaveKeyWithValue(sessionID, workspaceID))
}

func snapshotMCPSessionBindings(bindings *MCPSessionBindings) map[MCPSessionID]workspaceid.WorkspaceID {
	bindings.mu.Lock()
	defer bindings.mu.Unlock()

	snapshot := make(map[MCPSessionID]workspaceid.WorkspaceID, len(bindings.sessions))
	for sessionID, workspaceID := range bindings.sessions {
		snapshot[sessionID] = workspaceID
	}
	return snapshot
}
