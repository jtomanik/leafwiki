package markdown

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

const (
	canonicalMetadataOpenMarker  = "<!-- leafwiki"
	canonicalMetadataCloseMarker = "-->"
)

var ErrMetadataParse = errors.New("metadata parse error")

type canonicalMetadataYAMLEncoder interface {
	SetIndent(int)
	Encode(interface{}) error
	Close() error
}

var newCanonicalMetadataYAMLEncoder = func(w io.Writer) canonicalMetadataYAMLEncoder {
	return yaml.NewEncoder(w)
}

type pageMetadataYAML struct {
	Version int                    `yaml:"version"`
	Page    pageMetadataPageYAML   `yaml:"page"`
	Tags    []string               `yaml:"tags,omitempty"`
	Fields  map[string]interface{} `yaml:"fields,omitempty"`
	Extra   map[string]interface{} `yaml:"extra,omitempty"`
}

type pageMetadataPageYAML struct {
	ID           string `yaml:"id"`
	Title        string `yaml:"title"`
	CreatedAt    string `yaml:"created_at"`
	UpdatedAt    string `yaml:"updated_at"`
	CreatorID    string `yaml:"creator_id"`
	LastAuthorID string `yaml:"last_author_id"`
}

func ParsePageDocument(raw string) (PageDocument, PageDocumentParseResult, error) {
	normalized := normalizeMarkdownNewlines(raw)
	yamlPart, body, hasCanonical, _, err := splitCanonicalMetadata(normalized)
	if err != nil {
		return PageDocument{}, PageDocumentParseResult{}, err
	}
	if !hasCanonical {
		fm, body, hasLegacy, err := ParseFrontmatter(normalized)
		if err != nil {
			return PageDocument{}, PageDocumentParseResult{}, err
		}
		if !hasLegacy {
			return PageDocument{Body: raw}, PageDocumentParseResult{}, nil
		}
		return PageDocument{
			Body:     body,
			Metadata: frontmatterToPageMetadata(fm),
		}, PageDocumentParseResult{RequiresWriteback: true}, nil
	}

	meta, err := parseCanonicalMetadataYAML(yamlPart)
	if err != nil {
		return PageDocument{}, PageDocumentParseResult{}, err
	}
	strippedLegacyBody := false
	protectedBodyFrontmatter := false
	if strings.HasPrefix(body, "\n") {
		candidate := body[1:]
		if _, _, hasProtectedFrontmatter := splitFrontmatter(candidate); hasProtectedFrontmatter {
			body = candidate
			protectedBodyFrontmatter = true
		}
	}
	if !protectedBodyFrontmatter {
		legacyYAMLPart, cleanedBody, hasLegacyBody := splitFrontmatter(body)
		if hasLegacyBody {
			if _, err := parseFrontmatterYAML(legacyYAMLPart); err == nil {
				body = cleanedBody
				strippedLegacyBody = true
			}
		}
	}

	return PageDocument{
		Body:     body,
		Metadata: meta,
	}, PageDocumentParseResult{RequiresWriteback: strippedLegacyBody}, nil
}

func normalizeMarkdownNewlines(raw string) string {
	s := strings.TrimPrefix(raw, "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func splitCanonicalMetadata(raw string) (yamlPart string, body string, has bool, hasBodySeparator bool, err error) {
	firstLine := raw
	if firstNL := strings.IndexByte(raw, '\n'); firstNL >= 0 {
		firstLine = raw[:firstNL]
	}
	if strings.HasPrefix(strings.TrimSpace(firstLine), canonicalMetadataOpenMarker) && firstLine != canonicalMetadataOpenMarker {
		return "", raw, true, false, errors.Join(ErrMetadataParse, fmt.Errorf("canonical metadata opening marker must be exactly %q", canonicalMetadataOpenMarker))
	}

	if raw != canonicalMetadataOpenMarker && !strings.HasPrefix(raw, canonicalMetadataOpenMarker+"\n") {
		return "", raw, false, false, nil
	}

	firstNL := strings.IndexByte(raw, '\n')
	if firstNL == -1 {
		return "", raw, true, false, errors.Join(ErrMetadataParse, errors.New("canonical metadata closing marker missing"))
	}

	yamlStart := firstNL + 1
	pos := yamlStart
	for pos <= len(raw) {
		nextNL := strings.IndexByte(raw[pos:], '\n')
		var line string
		var lineEnd int
		if nextNL == -1 {
			lineEnd = len(raw)
			line = raw[pos:lineEnd]
		} else {
			lineEnd = pos + nextNL
			line = raw[pos:lineEnd]
		}

		if line == canonicalMetadataCloseMarker {
			bodyStart := lineEnd
			if bodyStart < len(raw) && raw[bodyStart:bodyStart+1] == "\n" {
				bodyStart++
			}
			hasBodySeparator := false
			if strings.HasPrefix(raw[bodyStart:], "\n") {
				hasBodySeparator = true
				bodyStart++
			}
			return strings.TrimSuffix(raw[yamlStart:pos], "\n"), raw[bodyStart:], true, hasBodySeparator, nil
		}

		if nextNL == -1 {
			break
		}
		pos = lineEnd + 1
	}

	return "", raw, true, false, errors.Join(ErrMetadataParse, errors.New("canonical metadata closing marker missing"))
}

func parseCanonicalMetadataYAML(yamlPart string) (PageMetadata, error) {
	var raw pageMetadataYAML
	decoder := yaml.NewDecoder(strings.NewReader(yamlPart))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		return PageMetadata{}, errors.Join(ErrMetadataParse, err)
	}
	if raw.Version != 1 {
		return PageMetadata{}, errors.Join(ErrMetadataParse, fmt.Errorf("unsupported metadata version %d", raw.Version))
	}
	if raw.Fields == nil {
		raw.Fields = map[string]interface{}{}
	}
	if raw.Extra == nil {
		raw.Extra = map[string]interface{}{}
	}

	meta := PageMetadata{
		Version: raw.Version,
		Page: PageMetadataPage{
			ID:           raw.Page.ID,
			Title:        raw.Page.Title,
			CreatedAt:    raw.Page.CreatedAt,
			UpdatedAt:    raw.Page.UpdatedAt,
			CreatorID:    raw.Page.CreatorID,
			LastAuthorID: raw.Page.LastAuthorID,
		},
		Tags:   raw.Tags,
		Fields: raw.Fields,
		Extra:  raw.Extra,
	}
	if err := validateCanonicalMetadata(meta); err != nil {
		return PageMetadata{}, err
	}
	return meta, nil
}

func validateCanonicalMetadata(meta PageMetadata) error {
	if meta.Version != 1 {
		return errors.Join(ErrMetadataParse, fmt.Errorf("unsupported metadata version %d", meta.Version))
	}
	if err := validateCanonicalPageID(meta.Page.ID); err != nil {
		return err
	}
	if err := validateCanonicalFields(meta.Fields); err != nil {
		return err
	}
	return nil
}

func validateCanonicalPageID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.Join(ErrMetadataParse, errors.New("page.id is required"))
	}
	if strings.ContainsAny(id, `/\`) {
		return errors.Join(ErrMetadataParse, errors.New("page.id must not contain path separators"))
	}
	if id == "." || id == ".." {
		return errors.Join(ErrMetadataParse, errors.New("page.id must not be a dot component"))
	}
	return nil
}

func validateCanonicalFields(fields map[string]interface{}) error {
	for key, value := range fields {
		if IsReservedMetadataKey(key) {
			return errors.Join(ErrMetadataParse, fmt.Errorf("field %q uses reserved leafwiki_ prefix", key))
		}
		if !isCanonicalFieldValue(value) {
			return errors.Join(ErrMetadataParse, fmt.Errorf("field %q has unsupported value type %T", key, value))
		}
	}
	return nil
}

func isCanonicalFieldValue(value interface{}) bool {
	switch value.(type) {
	case string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	default:
		return false
	}
}

func RenderPageDocument(doc PageDocument) (string, error) {
	if err := validateCanonicalMetadata(doc.Metadata); err != nil {
		return "", err
	}
	yamlPart, err := renderCanonicalMetadataYAML(doc.Metadata)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	out.WriteString(canonicalMetadataOpenMarker)
	out.WriteByte('\n')
	out.WriteString(yamlPart)
	out.WriteString(canonicalMetadataCloseMarker)
	out.WriteByte('\n')
	if doc.Body != "" {
		out.WriteByte('\n')
		body := normalizeMarkdownNewlines(doc.Body)
		if _, _, hasBodyFrontmatter := splitFrontmatter(body); hasBodyFrontmatter {
			out.WriteByte('\n')
		}
		out.WriteString(body)
	}
	return out.String(), nil
}

func BuildMarkdownWithMetadata(fm Frontmatter, body string) (string, error) {
	return RenderPageDocument(PageDocument{
		Body:     body,
		Metadata: frontmatterToPageMetadata(fm),
	})
}

func renderCanonicalMetadataYAML(meta PageMetadata) (string, error) {
	mapping := &yaml.Node{Kind: yaml.MappingNode}
	appendYAMLScalar(mapping, "version", meta.Version)
	appendYAMLNode(mapping, "page", pageMetadataMapping(meta.Page))
	if len(meta.Tags) > 0 {
		appendYAMLNode(mapping, "tags", stringSequenceNode(meta.Tags))
	}
	if len(meta.Fields) > 0 {
		fields, err := sortedMappingNode(meta.Fields)
		if err != nil {
			return "", err
		}
		appendYAMLNode(mapping, "fields", fields)
	}
	if len(meta.Extra) > 0 {
		extra, err := sortedMappingNode(meta.Extra)
		if err != nil {
			return "", err
		}
		appendYAMLNode(mapping, "extra", extra)
	}

	var b bytes.Buffer
	encoder := newCanonicalMetadataYAMLEncoder(&b)
	encoder.SetIndent(2)
	if err := encoder.Encode(mapping); err != nil {
		_ = encoder.Close()
		return "", errors.Join(ErrMetadataParse, err)
	}
	if err := encoder.Close(); err != nil {
		return "", errors.Join(ErrMetadataParse, err)
	}
	return b.String(), nil
}

func pageMetadataMapping(page PageMetadataPage) *yaml.Node {
	mapping := &yaml.Node{Kind: yaml.MappingNode}
	if strings.TrimSpace(page.ID) != "" {
		appendYAMLScalar(mapping, "id", strings.TrimSpace(page.ID))
	}
	if strings.TrimSpace(page.Title) != "" {
		appendYAMLScalar(mapping, "title", strings.TrimSpace(page.Title))
	}
	if strings.TrimSpace(page.CreatedAt) != "" {
		appendYAMLScalar(mapping, "created_at", strings.TrimSpace(page.CreatedAt))
	}
	if strings.TrimSpace(page.UpdatedAt) != "" {
		appendYAMLScalar(mapping, "updated_at", strings.TrimSpace(page.UpdatedAt))
	}
	if strings.TrimSpace(page.CreatorID) != "" {
		appendYAMLScalar(mapping, "creator_id", strings.TrimSpace(page.CreatorID))
	}
	if strings.TrimSpace(page.LastAuthorID) != "" {
		appendYAMLScalar(mapping, "last_author_id", strings.TrimSpace(page.LastAuthorID))
	}
	return mapping
}

func stringSequenceNode(values []string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.SequenceNode}
	for _, value := range values {
		node.Content = append(node.Content, scalarNode(strings.TrimSpace(value)))
	}
	return node
}

func sortedMappingNode(values map[string]interface{}) (*yaml.Node, error) {
	mapping := &yaml.Node{Kind: yaml.MappingNode}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		valueNode, err := toYAMLNode(values[key])
		if err != nil {
			return nil, err
		}
		appendYAMLNode(mapping, key, valueNode)
	}
	return mapping, nil
}

func appendYAMLScalar(mapping *yaml.Node, key string, value interface{}) {
	valueNode, err := toYAMLNode(value)
	if err != nil {
		return
	}
	appendYAMLNode(mapping, key, valueNode)
}

func appendYAMLNode(mapping *yaml.Node, key string, value *yaml.Node) {
	mapping.Content = append(mapping.Content, scalarNode(key), value)
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}
