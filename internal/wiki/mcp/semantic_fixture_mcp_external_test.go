package mcp_test

import (
	"github.com/perber/wiki/internal/agenthooks"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/wiki/mcp"
	"github.com/perber/wiki/internal/workspaceid"
)

type oauthAuthorizationCode string
type jsonPayloadField string

func newFixturePageID[T ~string](raw T) tree.PageID {
	return tree.NewPageIDUnchecked(raw)
}

func newFixtureSlug[T ~string](raw T) tree.Slug {
	return tree.NewSlugUnchecked(raw)
}

func newFixtureAssetName[T ~string](raw T) tree.AssetName {
	return tree.AssetNameFromString(raw)
}

func newFixtureRoutePath[T ~string](raw T) tree.RoutePath {
	return tree.NewRoutePathUnchecked(string(raw))
}

func newFixtureWorkspaceID[T ~string](raw T) workspaceid.WorkspaceID {
	return workspaceid.WorkspaceID(raw)
}

func newFixtureToolMessageID[T ~string](raw T) mcp.ToolMessageID {
	return mcp.ToolMessageID(raw)
}

func newFixtureMessageID[T ~string](raw T) sharederrors.MessageID {
	return sharederrors.MessageID(raw)
}

func newFixtureAgentToolName[T ~string](raw T) agenthooks.AgentToolName {
	return agenthooks.AgentToolNameFromString(raw)
}

func newFixtureJSONPayloadField[T ~string](raw T) jsonPayloadField {
	return jsonPayloadField(raw)
}
