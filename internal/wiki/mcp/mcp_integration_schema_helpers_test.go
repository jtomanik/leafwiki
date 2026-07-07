package mcp_test

import (
	"encoding/json"
	"fmt"
	"sort"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/agenthooks"
)

// Canonical Markdown links plan scenarios covered by tests in this file:
// - MCP agent context returns canonical examples

func matchInputSchemas(expected, expectedRequired map[string][]string) types.GomegaMatcher {
	GinkgoHelper()

	return WithTransform(func(tools []*sdkmcp.Tool) schemaContractObservation {
		return schemaContractObservationFor(inputSchemasMatch(tools, expected, expectedRequired))
	}, matchSchemaContractSatisfied())
}

func matchOutputSchemas(expected map[string][]string) types.GomegaMatcher {
	GinkgoHelper()

	return WithTransform(func(tools []*sdkmcp.Tool) schemaContractObservation {
		return schemaContractObservationFor(outputSchemasMatch(tools, expected))
	}, matchSchemaContractSatisfied())
}

type schemaContractState string

const (
	schemaContractSatisfied schemaContractState = "satisfied"
	schemaContractViolated  schemaContractState = "violated"
)

type schemaContractObservation struct {
	State schemaContractState
	Err   error
}

func schemaContractObservationFor(err error) schemaContractObservation {
	if err != nil {
		return schemaContractObservation{State: schemaContractViolated, Err: err}
	}
	return schemaContractObservation{State: schemaContractSatisfied}
}

func matchSchemaContractSatisfied() types.GomegaMatcher {
	GinkgoHelper()
	return SatisfyAll(
		HaveField("State", Equal(schemaContractSatisfied)),
		HaveField("Err", Succeed()),
	)
}

func inputSchemasMatch(tools []*sdkmcp.Tool, expected, expectedRequired map[string][]string) error {
	toolByName := toolSchemaIndex(tools)
	for _, tool := range tools {
		if _, ok := expected[tool.Name]; !ok {
			return fmt.Errorf("input schema contract missing expected tool %s", tool.Name)
		}
		if _, ok := expectedRequired[tool.Name]; !ok {
			return fmt.Errorf("input schema contract missing required-property list for %s", tool.Name)
		}
	}
	for name, props := range expected {
		tool, ok := toolByName[name]
		if !ok {
			return fmt.Errorf("input schema contract missing listed tool %s", name)
		}
		schema, err := decodeToolSchemaValue("input", tool.Name, tool.InputSchema)
		if err != nil {
			return err
		}
		if err := rootSchemaHasNoCombinators("input", name, schema); err != nil {
			return err
		}
		properties := schemaProperties(schema)
		gotProps := make([]string, 0, len(properties))
		for prop, property := range properties {
			gotProps = append(gotProps, prop)
			if err := schemaPropertyHasType("input", agenthooks.AgentToolNameFromString(name), prop, property); err != nil {
				return err
			}
		}
		if err := preciseInputPropertySchemas(agenthooks.AgentToolNameFromString(name), properties); err != nil {
			return err
		}
		if err := schemaPropertyOrderSorted("input", name, schema); err != nil {
			return err
		}
		if !sameStringSet(gotProps, props) {
			return fmt.Errorf("input schema for %s properties = %v, want %v", name, sortedStrings(gotProps), sortedStrings(props))
		}
		if !sameStringSet(schemaStringSlice(schema["required"]), expectedRequired[name]) {
			return fmt.Errorf("input schema for %s required properties = %v, want %v", name, sortedStrings(schemaStringSlice(schema["required"])), sortedStrings(expectedRequired[name]))
		}
	}
	return nil
}

func outputSchemasMatch(tools []*sdkmcp.Tool, expected map[string][]string) error {
	toolByName := toolSchemaIndex(tools)
	for _, tool := range tools {
		if tool.OutputSchema == nil {
			return fmt.Errorf("output schema for %s should be present", tool.Name)
		}
		if _, ok := expected[tool.Name]; !ok {
			return fmt.Errorf("output schema contract missing expected tool %s", tool.Name)
		}
	}
	for name, props := range expected {
		tool, ok := toolByName[name]
		if !ok {
			return fmt.Errorf("output schema contract missing listed tool %s", name)
		}
		schema, err := decodeToolSchemaValue("output", tool.Name, tool.OutputSchema)
		if err != nil {
			return err
		}
		if err := rootSchemaHasNoCombinators("output", name, schema); err != nil {
			return err
		}
		properties := schemaProperties(schema)
		gotProps := make([]string, 0, len(properties))
		for prop, property := range properties {
			gotProps = append(gotProps, prop)
			if err := schemaPropertyHasType("output", agenthooks.AgentToolNameFromString(name), prop, property); err != nil {
				return err
			}
		}
		if err := schemaPropertyOrderSorted("output", name, schema); err != nil {
			return err
		}
		if !sameStringSet(gotProps, props) {
			return fmt.Errorf("output schema for %s properties = %v, want %v", name, sortedStrings(gotProps), sortedStrings(props))
		}
		requiredProps := outputRequiredProperties(props, toolOutputOptionalProperties[name])
		if !sameStringSet(schemaStringSlice(schema["required"]), requiredProps) {
			return fmt.Errorf("output schema for %s required properties = %v, want %v", name, sortedStrings(schemaStringSlice(schema["required"])), sortedStrings(requiredProps))
		}
	}
	return nil
}

func outputRequiredProperties(props, optional []string) []string {
	required := make([]string, 0, len(props))
	for _, prop := range props {
		if !contains(optional, prop) {
			required = append(required, prop)
		}
	}
	return required
}

func toolSchemaIndex(tools []*sdkmcp.Tool) map[string]*sdkmcp.Tool {
	toolByName := make(map[string]*sdkmcp.Tool, len(tools))
	for _, tool := range tools {
		toolByName[tool.Name] = tool
	}
	return toolByName
}

func decodeToolSchemaValue(kind, name string, schemaValue any) (map[string]any, error) {
	raw, err := json.Marshal(schemaValue)
	if err != nil {
		return nil, fmt.Errorf("%s schema for %s should marshal: %w", kind, name, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("%s schema for %s should decode: %w", kind, name, err)
	}
	if schema["type"] != "object" {
		return nil, fmt.Errorf("%s schema for %s should be an object schema", kind, name)
	}
	return schema, nil
}

func schemaProperties(schema map[string]any) map[string]any {
	GinkgoHelper()

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return properties
}

func schemaPropertyOrderSorted(kind, name string, schema map[string]any) error {
	order := schemaStringSlice(schema["propertyOrder"])
	if len(order) == 0 {
		return nil
	}
	sortedOrder := append([]string{}, order...)
	sort.Strings(sortedOrder)
	if !stringSlicesEqual(order, sortedOrder) {
		return fmt.Errorf("%s schema for %s propertyOrder should be deterministic", kind, name)
	}
	return nil
}

func schemaPropertyHasType(kind string, toolName agenthooks.AgentToolName, prop string, property any) error {
	schema, ok := property.(map[string]any)
	if !ok {
		return fmt.Errorf("%s schema for %s.%s should be an object", kind, string(toolName), prop)
	}
	if _, ok := schema["type"]; ok {
		return nil
	}
	for _, key := range []string{"$ref", "anyOf", "oneOf", "allOf"} {
		if _, ok := schema[key]; ok {
			return nil
		}
	}
	return fmt.Errorf("%s schema for %s.%s should declare a type or combinator", kind, string(toolName), prop)
}

func rootSchemaHasNoCombinators(kind, name string, schema map[string]any) error {
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if _, ok := schema[key]; ok {
			return fmt.Errorf("%s schema for %s should not use root-level combinator %s", kind, name, key)
		}
	}
	return nil
}

func preciseInputPropertySchemas(toolName agenthooks.AgentToolName, properties map[string]any) error {
	switch toolName {
	case newFixtureAgentToolName("wiki_update_page_metadata"):
		for _, prop := range []string{"setTags", "addTags", "removeTags", "removeProperties"} {
			if err := stringArrayPropertySchema(toolName, prop, properties[prop]); err != nil {
				return err
			}
		}
		if err := stringMapPropertySchema(toolName, "setProperties", properties["setProperties"]); err != nil {
			return err
		}
	case newFixtureAgentToolName("wiki_replace_page_section"):
		if err := stringArrayPropertySchema(toolName, "headingPath", properties["headingPath"]); err != nil {
			return err
		}
	}
	return nil
}

func stringArrayPropertySchema(toolName agenthooks.AgentToolName, prop string, raw any) error {
	schema, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("input schema for %s.%s should be an object", string(toolName), prop)
	}
	if schema["type"] != "array" {
		return fmt.Errorf("input schema for %s.%s should be an array", string(toolName), prop)
	}
	items, ok := schema["items"].(map[string]any)
	if !ok {
		return fmt.Errorf("input schema for %s.%s should declare item schema", string(toolName), prop)
	}
	if items["type"] != "string" {
		return fmt.Errorf("input schema for %s.%s should contain string items", string(toolName), prop)
	}
	return nil
}

func stringMapPropertySchema(toolName agenthooks.AgentToolName, prop string, raw any) error {
	schema, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("input schema for %s.%s should be an object", string(toolName), prop)
	}
	if schema["type"] != "object" {
		return fmt.Errorf("input schema for %s.%s should be an object map", string(toolName), prop)
	}
	additional, ok := schema["additionalProperties"].(map[string]any)
	if !ok {
		return fmt.Errorf("input schema for %s.%s should declare additional properties", string(toolName), prop)
	}
	if additional["type"] != "string" {
		return fmt.Errorf("input schema for %s.%s should contain string values", string(toolName), prop)
	}
	return nil
}
