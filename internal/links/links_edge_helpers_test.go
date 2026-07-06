package links

import (
	"database/sql/driver"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/markdownlinks"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

func newManualLinksStore(storageDir string) *LinksStore {
	GinkgoHelper()

	store := &LinksStore{
		storageDir:   storageDir,
		databaseFile: "links.db",
	}
	Expect(store.Connect()).To(Succeed())
	DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
	return store
}

func HaveOutgoingKindsForPage(pageID tree.PageID, kinds ...TargetKind) types.GomegaMatcher {
	GinkgoHelper()
	return WithTransform(func(store *LinksStore) ([]TargetKind, error) {
		outgoing, err := store.GetOutgoingLinksForPage(pageID)
		if err != nil {
			return nil, err
		}
		got := make([]TargetKind, 0, len(outgoing))
		for _, link := range outgoing {
			got = append(got, link.ToKind)
		}
		return got, nil
	}, ConsistOf(kinds))
}

func matchBacklink(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchOutgoingResultItem(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchRewriteResult(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

type appliedRewriteRuleResult struct {
	Path    tree.RoutePath
	Applied bool
}

type rewriteRuleApplicationState uint8

const (
	rewriteRuleApplicationUnapplied rewriteRuleApplicationState = iota
	rewriteRuleApplicationApplied
)

type rewriteRuleApplication struct {
	State rewriteRuleApplicationState
	Path  tree.RoutePath
}

type rewrittenLinkDestinationResult struct {
	Destination string
	Changed     bool
	Warning     *RewriteWarning
}

func applyRewriteRulesResult(destination tree.RoutePath, rules []RewriteRule) appliedRewriteRuleResult {
	path, applied := applyRewriteRules(destination, rules)
	return appliedRewriteRuleResult{Path: path, Applied: applied}
}

func applyRewriteRulesForKindResult(destination tree.RoutePath, targetKind TargetKind, rules []RewriteRule) appliedRewriteRuleResult {
	path, applied := applyRewriteRulesForKind(destination, targetKind, rules)
	return appliedRewriteRuleResult{Path: path, Applied: applied}
}

func rewriteLinkDestinationResult(sourcePath tree.RoutePath, sourceKind MarkdownSourceKind, destination string, rules []RewriteRule, rootPrefix string) rewrittenLinkDestinationResult {
	rewritten, changed, warning := rewriteLinkDestination(sourcePath, sourceKind, destination, rules, rootPrefix)
	return rewrittenLinkDestinationResult{Destination: rewritten, Changed: changed, Warning: warning}
}

func rewriteRelativeLinkForPathChangeResult(sourcePath tree.RoutePath, newSourcePath tree.RoutePath, sourceKind MarkdownSourceKind, destination string, rules []RewriteRule) rewrittenLinkDestinationResult {
	rewritten, changed, warning := rewriteRelativeLinkForPathChange(sourcePath, newSourcePath, sourceKind, destination, rules)
	return rewrittenLinkDestinationResult{Destination: rewritten, Changed: changed, Warning: warning}
}

func rewriteRuleApplicationFor(result appliedRewriteRuleResult) rewriteRuleApplication {
	if result.Applied {
		return rewriteRuleApplication{
			State: rewriteRuleApplicationApplied,
			Path:  result.Path,
		}
	}
	return rewriteRuleApplication{State: rewriteRuleApplicationUnapplied}
}

func matchAppliedRewriteRule(path tree.RoutePath) types.GomegaMatcher {
	return WithTransform(rewriteRuleApplicationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State": Equal(rewriteRuleApplicationApplied),
		"Path":  Equal(path),
	}))
}

func matchUnappliedRewriteRule() types.GomegaMatcher {
	return WithTransform(rewriteRuleApplicationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"State": Equal(rewriteRuleApplicationUnapplied),
	}))
}

func matchUnchangedLinkDestination(destination string, messageID sharederrors.MessageID) types.GomegaMatcher {
	return WithTransform(rewriteDestinationObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Destination":      Equal(destination),
		"State":            Equal(rewriteDestinationUnchanged),
		"WarningMessageID": Equal(messageID),
	}))
}

func matchRewrittenLinkDestination(destination string) types.GomegaMatcher {
	return WithTransform(rewriteDestinationObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Destination":      Equal(destination),
		"State":            Equal(rewriteDestinationChanged),
		"WarningMessageID": Equal(emptyWarningMessageID),
	}))
}

func matchUnchangedLinkDestinationWithoutWarning(destination string) types.GomegaMatcher {
	return WithTransform(rewriteDestinationObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Destination":      Equal(destination),
		"State":            Equal(rewriteDestinationUnchanged),
		"WarningMessageID": Equal(emptyWarningMessageID),
	}))
}

var emptyWarningMessageID sharederrors.MessageID

type rewriteDestinationState uint8

const (
	rewriteDestinationUnchanged rewriteDestinationState = iota
	rewriteDestinationChanged
)

type rewriteDestinationObservation struct {
	Destination      string
	State            rewriteDestinationState
	WarningMessageID sharederrors.MessageID
}

func rewriteDestinationObservationFor(result rewrittenLinkDestinationResult) rewriteDestinationObservation {
	state := rewriteDestinationUnchanged
	if result.Changed {
		state = rewriteDestinationChanged
	}
	var messageID sharederrors.MessageID
	if result.Warning != nil {
		messageID = result.Warning.MessageID
	}
	return rewriteDestinationObservation{
		Destination:      result.Destination,
		State:            state,
		WarningMessageID: messageID,
	}
}

func matchBrokenStoredBacklink(fromPageID tree.PageID) types.GomegaMatcher {
	return WithTransform(storedBacklinkObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"FromPageID": Equal(fromPageID),
		"State":      Equal(linkResolutionBroken),
	}))
}

func matchHealedStoredBacklink(fromPageID tree.PageID, toPageID tree.PageID, targetKind TargetKind) types.GomegaMatcher {
	return WithTransform(storedBacklinkObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"FromPageID": Equal(fromPageID),
		"ToPageID":   Equal(toPageID),
		"ToKind":     Equal(targetKind),
		"State":      Equal(linkResolutionResolved),
	}))
}

type storedBacklinkObservation struct {
	FromPageID tree.PageID
	ToPageID   tree.PageID
	ToKind     TargetKind
	State      linkResolutionState
}

func storedBacklinkObservationFor(backlink Backlink) storedBacklinkObservation {
	state := linkResolutionResolved
	if backlink.Broken {
		state = linkResolutionBroken
	}
	return storedBacklinkObservation{
		FromPageID: backlink.FromPageID,
		ToPageID:   backlink.ToPageID,
		ToKind:     backlink.ToKind,
		State:      state,
	}
}

type wikiDestinationExtension uint8

const (
	wikiDestinationCanonicalOrNonWiki wikiDestinationExtension = iota
	wikiDestinationExtensionless
)

func wikiDestinationExtensionFor(destination string) wikiDestinationExtension {
	if isExtensionlessWikiDestination(destination) {
		return wikiDestinationExtensionless
	}
	return wikiDestinationCanonicalOrNonWiki
}

type linksTableSchema struct {
	ColumnNames       []string
	PrimaryKeyColumns []string
}

func linksTableSchemaFor(columns []linksTableColumn) linksTableSchema {
	schema := linksTableSchema{
		ColumnNames:       make([]string, 0, len(columns)),
		PrimaryKeyColumns: make([]string, 0, len(columns)),
	}
	for _, column := range columns {
		schema.ColumnNames = append(schema.ColumnNames, column.Name)
		if column.PK > 0 {
			schema.PrimaryKeyColumns = append(schema.PrimaryKeyColumns, column.Name)
		}
	}
	return schema
}

func matchKindAwareLinksTableSchema() types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ColumnNames":       ContainElement("to_kind"),
		"PrimaryKeyColumns": ContainElements("from_page_id", "to_path", "to_kind"),
	})
}

type rewriteRuleKindEligibility uint8

const (
	rewriteRuleKindEligible rewriteRuleKindEligibility = iota
	rewriteRuleKindRejected
)

func rewriteRuleKindEligibilityFor(rule RewriteRule, targetKind TargetKind) rewriteRuleKindEligibility {
	if rewriteRuleMatchesExactKind(rule, targetKind) {
		return rewriteRuleKindEligible
	}
	return rewriteRuleKindRejected
}

func matchTreePageContentError() types.GomegaMatcher {
	return MatchError(tree.ErrGetPageContent)
}

func matchLinksError(target error) types.GomegaMatcher {
	return MatchError(target)
}

func linksStoreWithExecError(err error) *LinksStore {
	GinkgoHelper()
	return newScriptedLinksStore(&linksScriptedDBScript{
		exec: func(string, []driver.NamedValue) (driver.Result, error) {
			return nil, err
		},
	})
}

func linksStoreWithQueryError(err error) *LinksStore {
	GinkgoHelper()
	return newScriptedLinksStore(&linksScriptedDBScript{
		query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return nil, err
		},
	})
}

func markdownlinksResolution(code markdownlinks.IssueCode) markdownlinks.Resolution {
	return markdownlinks.Resolution{Code: code}
}

func newLoadedLinksTreeService() *tree.TreeService {
	GinkgoHelper()

	treeService := tree.NewTreeService(linksTempDir())
	Expect(treeService.LoadTree()).To(Succeed())
	return treeService
}

func createLoadedLinksPage(treeService *tree.TreeService, title string, slug tree.Slug, content string) *tree.Page {
	GinkgoHelper()

	kind := tree.NodeKindPage
	pageID, err := treeService.CreateNode(newFixtureUserID("links-tester"), nil, title, slug, &kind)
	Expect(err).NotTo(HaveOccurred())
	Expect(pageID).NotTo(BeNil())
	Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("links-tester"), *pageID, title, slug, &content, false)).To(Succeed())

	page, err := treeService.GetPage(*pageID)
	Expect(err).NotTo(HaveOccurred())
	return page
}

func setLinksSeam[T any](target *T, replacement T) func() {
	GinkgoHelper()

	original := *target
	*target = replacement
	restored := false
	restore := func() {
		if restored {
			return
		}
		*target = original
		restored = true
	}
	DeferCleanup(restore)
	return restore
}
