package links

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

type MarkdownRefactorEngine struct {
	parser goldmark.Markdown
}

type MarkdownSourceKind string

const (
	MarkdownSourceKindPage    MarkdownSourceKind = "page"
	MarkdownSourceKindSection MarkdownSourceKind = "section"
)

type RewriteWarning struct {
	Message string
}

type RewriteReplacement struct {
	Start    int
	End      int
	NewValue string
}

type RewriteResult struct {
	Content      string
	Replacements []RewriteReplacement
	Warnings     []RewriteWarning
}

type rewriteCandidate struct {
	Destination string
}

func NewMarkdownRefactorEngine() *MarkdownRefactorEngine {
	return &MarkdownRefactorEngine{
		parser: goldmark.New(),
	}
}

func (r RewriteResult) Count() int {
	return len(r.Replacements)
}

func RewriteMarkdownLinks(content string, currentPath string, rules []RewriteRule) (string, int) {
	result := NewMarkdownRefactorEngine().Rewrite(content, currentPath, rules)
	return result.Content, result.Count()
}

func (e *MarkdownRefactorEngine) RewriteRelativeLinksForPathChange(content string, oldCurrentPath string, newCurrentPath string, rules []RewriteRule) RewriteResult {
	return e.RewriteRelativeLinksForPathChangeWithSourceKind(content, oldCurrentPath, newCurrentPath, MarkdownSourceKindPage, rules)
}

func (e *MarkdownRefactorEngine) RewriteRelativeLinksForPathChangeWithSourceKind(content string, oldCurrentPath string, newCurrentPath string, sourceKind MarkdownSourceKind, rules []RewriteRule) RewriteResult {
	if content == "" || oldCurrentPath == newCurrentPath {
		return RewriteResult{Content: content}
	}

	candidates := e.collectCandidates(content)
	if len(candidates) == 0 {
		return RewriteResult{Content: content}
	}

	occurrences := markdownlinks.ScanInlineDestinations(content, markdownlinks.InlineScanOptions{})
	replacements, warnings := buildPathChangeRewritePlan(oldCurrentPath, newCurrentPath, sourceKind, rules, candidates, occurrences)
	if len(replacements) == 0 {
		return RewriteResult{
			Content:  content,
			Warnings: warnings,
		}
	}

	return RewriteResult{
		Content:      applyReplacements(content, replacements),
		Replacements: replacements,
		Warnings:     warnings,
	}
}

func (e *MarkdownRefactorEngine) Rewrite(content string, currentPath string, rules []RewriteRule) RewriteResult {
	return e.RewriteWithSourceKind(content, currentPath, MarkdownSourceKindPage, rules)
}

func (e *MarkdownRefactorEngine) RewriteWithSourceKind(content string, currentPath string, sourceKind MarkdownSourceKind, rules []RewriteRule) RewriteResult {
	if len(rules) == 0 || content == "" {
		return RewriteResult{Content: content}
	}

	candidates := e.collectCandidates(content)
	if len(candidates) == 0 {
		return RewriteResult{Content: content}
	}

	occurrences := markdownlinks.ScanInlineDestinations(content, markdownlinks.InlineScanOptions{})
	replacements, warnings := buildRewritePlan(currentPath, sourceKind, rules, candidates, occurrences)
	if len(replacements) == 0 {
		return RewriteResult{
			Content:  content,
			Warnings: warnings,
		}
	}

	return RewriteResult{
		Content:      applyReplacements(content, replacements),
		Replacements: replacements,
		Warnings:     warnings,
	}
}

func (e *MarkdownRefactorEngine) collectCandidates(content string) []rewriteCandidate {
	reader := text.NewReader([]byte(content))
	doc := e.parser.Parser().Parse(reader)

	var candidates []rewriteCandidate

	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch n := node.(type) {
		case *ast.Link:
			candidates = append(candidates, rewriteCandidate{
				Destination: string(n.Destination),
			})
		}

		return ast.WalkContinue, nil
	})

	return candidates
}

func buildRewritePlan(currentPath string, sourceKind MarkdownSourceKind, rules []RewriteRule, candidates []rewriteCandidate, occurrences []markdownlinks.InlineDestination) ([]RewriteReplacement, []RewriteWarning) {
	var replacements []RewriteReplacement
	var warnings []RewriteWarning

	occurrenceCounts := make(map[string]int, len(occurrences))
	for _, occurrence := range occurrences {
		occurrenceCounts[normalizeCandidateDestination(occurrence.Destination)]++
		newDest, changed, warning := rewriteLinkDestination(currentPath, sourceKind, occurrence.Destination, rules)
		if warning != nil {
			warnings = append(warnings, *warning)
		}
		if changed {
			replacements = append(replacements, RewriteReplacement{
				Start:    occurrence.Start,
				End:      occurrence.End,
				NewValue: newDest,
			})
		}
	}

	for _, candidate := range candidates {
		normalized := normalizeCandidateDestination(candidate.Destination)
		if occurrenceCounts[normalized] > 0 {
			occurrenceCounts[normalized]--
			continue
		}
		warnings = append(warnings, RewriteWarning{
			Message: fmt.Sprintf("Skipped unsupported link syntax for destination %q", candidate.Destination),
		})
	}

	return replacements, dedupeWarnings(warnings)
}

func buildPathChangeRewritePlan(oldCurrentPath string, newCurrentPath string, sourceKind MarkdownSourceKind, rules []RewriteRule, candidates []rewriteCandidate, occurrences []markdownlinks.InlineDestination) ([]RewriteReplacement, []RewriteWarning) {
	var replacements []RewriteReplacement
	var warnings []RewriteWarning

	occurrenceCounts := make(map[string]int, len(occurrences))
	for _, occurrence := range occurrences {
		occurrenceCounts[normalizeCandidateDestination(occurrence.Destination)]++
		newDest, changed, warning := rewriteRelativeLinkForPathChange(oldCurrentPath, newCurrentPath, sourceKind, occurrence.Destination, rules)
		if warning != nil {
			warnings = append(warnings, *warning)
		}
		if changed {
			replacements = append(replacements, RewriteReplacement{
				Start:    occurrence.Start,
				End:      occurrence.End,
				NewValue: newDest,
			})
		}
	}

	for _, candidate := range candidates {
		normalized := normalizeCandidateDestination(candidate.Destination)
		if occurrenceCounts[normalized] > 0 {
			occurrenceCounts[normalized]--
			continue
		}
		warnings = append(warnings, RewriteWarning{
			Message: fmt.Sprintf("Skipped unsupported link syntax for destination %q", candidate.Destination),
		})
	}

	return replacements, dedupeWarnings(warnings)
}

func normalizeCandidateDestination(destination string) string {
	if destination == "" {
		return ""
	}
	destination = strings.TrimSpace(destination)
	destination = strings.TrimPrefix(destination, "<")
	destination = strings.TrimSuffix(destination, ">")
	return destination
}

func rewriteLinkDestination(currentPath string, sourceKind MarkdownSourceKind, destination string, rules []RewriteRule) (string, bool, *RewriteWarning) {
	baseDest, suffix := splitLinkDestination(destination)
	if baseDest == "" || isExternalLinkDestination(baseDest) || isAssetLinkDestination(baseDest) {
		return destination, false, nil
	}

	canonicalPageLink := strings.EqualFold(path.Ext(strings.TrimSpace(baseDest)), ".md")
	targetKind := markdownLinkTargetKind(canonicalPageLink)
	resolvedPath, err := resolveMarkdownRoutePathForSource(sourceMarkdownFileForKind(currentPath, sourceKind), baseDest)
	if err != nil || resolvedPath == "" {
		return destination, false, &RewriteWarning{
			Message: fmt.Sprintf("Skipped unresolved link destination %q", destination),
		}
	}

	newResolvedPath, matchedRule, ok := applyRewriteRulesForKindWithRule(resolvedPath, targetKind, rules)
	if !ok || newResolvedPath == resolvedPath {
		return destination, false, nil
	}

	outputPageLink := canonicalPageLink
	if matchedRule.OutputKind != "" {
		outputPageLink = storedTargetKind(matchedRule.OutputKind) == defaultStoredTargetKind
	}

	var rewrittenBase string
	if strings.HasPrefix(baseDest, "/") {
		rewrittenBase = newResolvedPath
		if outputPageLink {
			rewrittenBase = strings.TrimRight(rewrittenBase, "/") + ".md"
		}
	} else {
		currentPathForRelative := currentPath
		if nextCurrentPath, rewrittenCurrentPath := applyRewriteRulesForKind(normalizeWikiPath(currentPath), string(sourceKind), rules); rewrittenCurrentPath {
			currentPathForRelative = nextCurrentPath
		}
		rewrittenBase = relativeMarkdownDestinationForSource(currentPathForRelative, sourceKind, newResolvedPath, outputPageLink)
		rewrittenBase = preserveExplicitDotSlashStyle(baseDest, rewrittenBase)
	}

	if rewrittenBase == "" {
		return destination, false, &RewriteWarning{
			Message: fmt.Sprintf("Skipped empty rewritten destination for %q", destination),
		}
	}

	return rewrittenBase + suffix, true, nil
}

func relativeMarkdownFileLinkPath(currentPath string, targetPath string) string {
	sourceFile := sourceMarkdownFileForKind(currentPath, MarkdownSourceKindPage)
	targetFile := strings.Trim(normalizeWikiPath(targetPath), "/") + ".md"
	sourceDir := path.Dir(sourceFile)
	if sourceDir == "." {
		sourceDir = ""
	}
	rel, err := filepath.Rel(filepath.FromSlash(sourceDir), filepath.FromSlash(targetFile))
	if err != nil {
		return targetFile
	}
	return filepath.ToSlash(rel)
}

func rewriteRelativeLinkForPathChange(oldCurrentPath string, newCurrentPath string, sourceKind MarkdownSourceKind, destination string, rules []RewriteRule) (string, bool, *RewriteWarning) {
	baseDest, suffix := splitLinkDestination(destination)
	if baseDest == "" || strings.HasPrefix(baseDest, "/") || isExternalLinkDestination(baseDest) || isAssetLinkDestination(baseDest) {
		return destination, false, nil
	}

	canonicalPageLink := strings.EqualFold(path.Ext(strings.TrimSpace(baseDest)), ".md")
	targetKind := markdownLinkTargetKind(canonicalPageLink)
	resolvedPath, err := resolveMarkdownRoutePathForSource(sourceMarkdownFileForKind(oldCurrentPath, sourceKind), baseDest)
	if err != nil || resolvedPath == "" {
		return destination, false, &RewriteWarning{
			Message: fmt.Sprintf("Skipped unresolved link destination %q", destination),
		}
	}

	targetPath := resolvedPath
	outputPageLink := canonicalPageLink
	if rewrittenTarget, matchedRule, ok := applyRewriteRulesForKindWithRule(resolvedPath, targetKind, rules); ok {
		targetPath = rewrittenTarget
		if matchedRule.OutputKind != "" {
			outputPageLink = storedTargetKind(matchedRule.OutputKind) == defaultStoredTargetKind
		}
	}

	rewrittenBase := relativeMarkdownDestinationForSource(newCurrentPath, sourceKind, targetPath, outputPageLink)
	rewrittenBase = preserveExplicitDotSlashStyle(baseDest, rewrittenBase)
	if rewrittenBase == "" {
		return destination, false, &RewriteWarning{
			Message: fmt.Sprintf("Skipped empty rewritten destination for %q", destination),
		}
	}

	if rewrittenBase == baseDest {
		return destination, false, nil
	}

	return rewrittenBase + suffix, true, nil
}

func preserveExplicitDotSlashStyle(original string, rewritten string) string {
	if !strings.HasPrefix(original, "./") || rewritten == "" {
		return rewritten
	}
	if strings.HasPrefix(rewritten, "./") || strings.HasPrefix(rewritten, "../") || strings.HasPrefix(rewritten, "/") {
		return rewritten
	}
	return "./" + rewritten
}

func sourceMarkdownFileForKind(currentPath string, sourceKind MarkdownSourceKind) string {
	route := strings.Trim(normalizeWikiPath(currentPath), "/")
	if sourceKind == MarkdownSourceKindSection {
		if route == "" {
			return "index.md"
		}
		return route + "/index.md"
	}
	if route == "" {
		return "index.md"
	}
	return route + ".md"
}

func resolveMarkdownRoutePathForSource(sourceFile string, destination string) (string, error) {
	target := strings.TrimSpace(destination)
	if target == "" {
		return "", nil
	}
	var resolved string
	if strings.HasPrefix(target, "/") {
		resolved = path.Clean(strings.TrimPrefix(target, "/"))
	} else {
		sourceDir := path.Dir(strings.Trim(sourceFile, "/"))
		if sourceDir == "." {
			sourceDir = ""
		}
		resolved = path.Clean(path.Join(sourceDir, target))
	}
	if resolved == "." || resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "", nil
	}
	if strings.EqualFold(path.Ext(resolved), ".md") {
		routePath := tree.MarkdownPathToRoutePath(resolved)
		if routePath == "" {
			return "/", nil
		}
		return normalizeWikiPath(routePath), nil
	}
	return normalizeWikiPath(resolved), nil
}

func relativeMarkdownDestinationForSource(currentPath string, sourceKind MarkdownSourceKind, targetPath string, pageLink bool) string {
	sourceFile := sourceMarkdownFileForKind(currentPath, sourceKind)
	target := strings.Trim(normalizeWikiPath(targetPath), "/")
	if pageLink {
		target += ".md"
	}
	if target == "" {
		return ""
	}
	sourceDir := path.Dir(sourceFile)
	if sourceDir == "." {
		sourceDir = ""
	}
	rel, err := filepath.Rel(filepath.FromSlash(sourceDir), filepath.FromSlash(target))
	if err != nil {
		return target
	}
	rel = filepath.ToSlash(rel)
	if rel == "." && !pageLink {
		return "."
	}
	return rel
}

func applyReplacements(content string, replacements []RewriteReplacement) string {
	if len(replacements) == 0 {
		return content
	}

	var builder strings.Builder
	last := 0
	for _, replacement := range replacements {
		builder.WriteString(content[last:replacement.Start])
		builder.WriteString(replacement.NewValue)
		last = replacement.End
	}
	builder.WriteString(content[last:])
	return builder.String()
}

func dedupeWarnings(warnings []RewriteWarning) []RewriteWarning {
	if len(warnings) < 2 {
		return warnings
	}

	seen := make(map[string]struct{}, len(warnings))
	deduped := make([]RewriteWarning, 0, len(warnings))
	for _, warning := range warnings {
		if _, ok := seen[warning.Message]; ok {
			continue
		}
		seen[warning.Message] = struct{}{}
		deduped = append(deduped, warning)
	}
	return deduped
}

func splitLinkDestination(destination string) (string, string) {
	if destination == "" {
		return "", ""
	}
	if idx := strings.IndexAny(destination, "?#"); idx != -1 {
		return destination[:idx], destination[idx:]
	}
	return destination, ""
}

func isExternalLinkDestination(destination string) bool {
	lower := strings.ToLower(destination)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "mailto:") ||
		strings.HasPrefix(lower, "#")
}

func applyRewriteRules(resolvedPath string, rules []RewriteRule) (string, bool) {
	return applyRewriteRulesForKind(resolvedPath, "", rules)
}

func applyRewriteRulesForKind(resolvedPath string, targetKind string, rules []RewriteRule) (string, bool) {
	newPath, _, ok := applyRewriteRulesForKindWithRule(resolvedPath, targetKind, rules)
	return newPath, ok
}

func applyRewriteRulesForKindWithRule(resolvedPath string, targetKind string, rules []RewriteRule) (string, RewriteRule, bool) {
	for _, rule := range rules {
		if resolvedPath == rule.OldPath {
			if !rewriteRuleMatchesExactKind(rule, targetKind) {
				continue
			}
			return rule.NewPath, rule, true
		}
		if strings.HasPrefix(resolvedPath, rule.OldPath+"/") {
			if rule.OutputKind != "" {
				continue
			}
			if rule.Kind != "" && storedTargetKind(rule.Kind) != "section" {
				continue
			}
			return rule.NewPath + strings.TrimPrefix(resolvedPath, rule.OldPath), rule, true
		}
	}
	return "", RewriteRule{}, false
}

func rewriteRuleMatchesExactKind(rule RewriteRule, targetKind string) bool {
	if rule.Kind == "" {
		return true
	}
	if targetKind == "" {
		return false
	}
	return storedTargetKind(rule.Kind) == storedTargetKind(targetKind)
}

func markdownLinkTargetKind(pageLink bool) string {
	if pageLink {
		return "page"
	}
	return "section"
}

func relativeWikiLinkPath(currentPath string, targetPath string) string {
	base := normalizeWikiPath(currentPath)
	target := normalizeWikiPath(targetPath)
	if target == "" {
		return ""
	}

	baseParts := splitWikiPathSegments(base)
	targetParts := splitWikiPathSegments(target)

	common := 0
	for common < len(baseParts) && common < len(targetParts) && baseParts[common] == targetParts[common] {
		common++
	}

	var relParts []string
	for i := common; i < len(baseParts); i++ {
		relParts = append(relParts, "..")
	}
	relParts = append(relParts, targetParts[common:]...)

	rel := strings.Join(relParts, "/")
	if rel == "" {
		return ""
	}
	return rel
}

func splitWikiPathSegments(value string) []string {
	normalized := strings.Trim(normalizeWikiPath(value), "/")
	if normalized == "" {
		return nil
	}
	return strings.Split(normalized, "/")
}
