package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	wikimcp "github.com/perber/wiki/internal/wiki/mcp"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

func assertContextHistoryOpaque(contextOut map[string]any) {
	GinkgoHelper()

	for _, raw := range arrayField(contextOut, "contextHistory") {
		Expect(raw).To(BeAssignableToTypeOf(map[string]any{}))
		entry := raw.(map[string]any)
		for _, internal := range []string{"commitHash", "validationHash"} {
			Expect(entry).NotTo(HaveKey(internal))
		}
	}
}

func schemaStringSlice(value any) []string {
	rawItems, ok := value.([]any)
	if !ok {
		return nil
	}
	items := make([]string, 0, len(rawItems))
	for _, raw := range rawItems {
		item, ok := raw.(string)
		if ok {
			items = append(items, item)
		}
	}
	return items
}

func matchStringSet(want []string) types.GomegaMatcher {
	GinkgoHelper()

	want = append([]string{}, want...)
	sort.Strings(want)
	return WithTransform(func(got []string) []string {
		sorted := append([]string{}, got...)
		sort.Strings(sorted)
		return sorted
	}, Equal(want))
}

func sameStringSet(got, want []string) bool {
	return stringSlicesEqual(sortedStrings(got), sortedStrings(want))
}

func sortedStrings(values []string) []string {
	out := append([]string{}, values...)
	sort.Strings(out)
	return out
}

func stringSlicesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func copyToolInputProperties(src map[string][]string) map[string][]string {
	out := make(map[string][]string, len(src))
	for name, props := range src {
		out[name] = append([]string{}, props...)
	}
	return out
}

func matchToolNames(want []string) types.GomegaMatcher {
	GinkgoHelper()

	sortedWant := append([]string{}, want...)
	sort.Strings(sortedWant)
	return Equal(sortedWant)
}

func matchJSONRPCErrorCode(code int64) types.GomegaMatcher {
	GinkgoHelper()

	return Satisfy(func(err error) bool {
		var rpcErr *jsonrpc.Error
		return errors.As(err, &rpcErr) && rpcErr.Code == code
	})
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func callToolStructured(session *sdkmcp.ClientSession, name string, args map[string]any) map[string]any {
	GinkgoHelper()

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	Expect(err).NotTo(HaveOccurred(), "CallTool %s should return a result", name)
	Expect(result).To(matchSuccessfulToolResultWithStructuredContent(BeAssignableToTypeOf(map[string]any{})), "CallTool %s should return structured content, got error content %#v and structured content %T: %#v", name, result.Content, result.StructuredContent, result.StructuredContent)
	return result.StructuredContent.(map[string]any)
}

type toolMessageOutput struct {
	MessageID wikimcp.ToolMessageID `json:"messageId"`
	Message   string                `json:"message"`
}

func messageOutputFromStructuredContent(value map[string]any) toolMessageOutput {
	GinkgoHelper()

	raw, err := json.Marshal(value)
	Expect(err).NotTo(HaveOccurred(), "message output should marshal")
	var out toolMessageOutput
	Expect(json.Unmarshal(raw, &out)).To(Succeed(), "message output should decode")
	return out
}

type mcpToolErrorResult struct {
	Text      string
	Code      sharederrors.ErrorCode
	MessageID sharederrors.MessageID
	Message   string
	Args      []string
}

type mcpToolErrorPayloadWire struct {
	Code      sharederrors.ErrorCode `json:"code"`
	MessageID sharederrors.MessageID `json:"messageId"`
	Message   string                 `json:"message"`
	Args      []string               `json:"args"`
}

func mcpToolErrorPayloadFromWire(name any, payload map[string]any) mcpToolErrorResult {
	GinkgoHelper()

	raw, err := json.Marshal(payload)
	Expect(err).NotTo(HaveOccurred(), "CallTool %s structured error should marshal", name)
	var typed mcpToolErrorPayloadWire
	Expect(json.Unmarshal(raw, &typed)).To(Succeed(), "CallTool %s structured error should decode", name)
	return mcpToolErrorResult{
		Code:      typed.Code,
		MessageID: typed.MessageID,
		Message:   typed.Message,
		Args:      typed.Args,
	}
}

func callToolStructuredError(session *sdkmcp.ClientSession, name any, args map[string]any) mcpToolErrorResult {
	GinkgoHelper()

	return callToolErrorResult(session, name, args)
}

func matchMCPStructuredError(code sharederrors.ErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	GinkgoHelper()

	return testmatchers.HaveMCPStructuredError(code, messageID)
}

func callToolErrorResult(session *sdkmcp.ClientSession, name any, args map[string]any) mcpToolErrorResult {
	GinkgoHelper()

	switch typed := name.(type) {
	case wikimcp.ToolID:
		return callToolErrorResultByToolID(session, typed, args)
	case string:
		result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name:      typed,
			Arguments: args,
		})
		Expect(err).NotTo(HaveOccurred(), "CallTool %s should return a tool result", name)
		return toolErrorResultFromCallResult(name, result)
	default:
		result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name:      fmt.Sprint(typed),
			Arguments: args,
		})
		Expect(err).NotTo(HaveOccurred(), "CallTool %s should return a tool result", name)
		return toolErrorResultFromCallResult(name, result)
	}
}

func callToolErrorResultByToolID(session *sdkmcp.ClientSession, name wikimcp.ToolID, args map[string]any) mcpToolErrorResult {
	GinkgoHelper()

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      name.String(),
		Arguments: args,
	})
	Expect(err).NotTo(HaveOccurred(), "CallTool %s should return a tool result", name)
	return toolErrorResultFromCallResult(name, result)
}

func toolErrorResultFromCallResult(name any, result *sdkmcp.CallToolResult) mcpToolErrorResult {
	GinkgoHelper()

	Expect(result).NotTo(BeNil(), "CallTool %s should return a tool result", name)

	out := mcpToolErrorResult{}
	for _, content := range result.Content {
		if text, ok := content.(*sdkmcp.TextContent); ok {
			out.Text = text.Text
			break
		}
	}
	Expect(out.Text).NotTo(BeEmpty(), "CallTool %s should return text error content", name)
	errorPayload, err := structuredToolErrorPayload(result)
	Expect(err).To(Succeed(), "CallTool %s should expose a structured error payload", name)
	decoded := mcpToolErrorPayloadFromWire(name, errorPayload)
	out.Code = decoded.Code
	out.MessageID = decoded.MessageID
	out.Message = decoded.Message
	out.Args = decoded.Args
	return out
}

func structuredToolErrorPayload(result *sdkmcp.CallToolResult) (map[string]any, error) {
	GinkgoHelper()

	if result == nil {
		return nil, fmt.Errorf("missing tool result")
	}
	if payload, ok := result.Meta["error"].(map[string]any); ok {
		return payload, nil
	}
	if payload, ok := result.StructuredContent.(map[string]any); ok {
		if errorPayload, ok := payload["error"].(map[string]any); ok {
			return errorPayload, nil
		}
		return payload, nil
	}
	return nil, fmt.Errorf("missing structured tool error payload")
}

func callToolProtocolError(session *sdkmcp.ClientSession, name string, args map[string]any) error {
	GinkgoHelper()

	_, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	return err
}
