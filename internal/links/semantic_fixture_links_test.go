package links

import (
	"fmt"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/tree"
)

func newFixturePageID[T ~string](raw T) tree.PageID {
	return tree.NewPageIDUnchecked(raw)
}

func newFixtureSlug[T ~string](raw T) tree.Slug {
	return tree.NewSlugUnchecked(raw)
}

func newFixtureUserID[T ~string](raw T) tree.UserID {
	return tree.NewUserIDUnchecked(string(raw))
}

func matchResolvedTargetLink(pageID tree.PageID, targetPath string) types.GomegaMatcher {
	return gomega.WithTransform(targetLinkObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"TargetPageID":   gomega.Equal(pageID),
		"TargetPagePath": gomega.Equal(targetPath),
		"State":          gomega.Equal(linkResolutionResolved),
	}))
}

func matchBrokenTargetLink(targetPath string) types.GomegaMatcher {
	return gomega.WithTransform(targetLinkObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"TargetPageID":   gomega.Equal(tree.PageID("")),
		"TargetPagePath": gomega.Equal(targetPath),
		"State":          gomega.Equal(linkResolutionBroken),
	}))
}

type linkResolutionState uint8

const (
	linkResolutionResolved linkResolutionState = iota
	linkResolutionBroken
)

type targetLinkObservation struct {
	TargetPageID   tree.PageID
	TargetPagePath string
	State          linkResolutionState
}

func targetLinkObservationFor(link TargetLink) targetLinkObservation {
	state := linkResolutionResolved
	if link.Broken {
		state = linkResolutionBroken
	}
	return targetLinkObservation{
		TargetPageID:   link.TargetPageID,
		TargetPagePath: link.TargetPagePath,
		State:          state,
	}
}

func matchOutgoingResult(count int, outgoings ...any) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Count":     gomega.Equal(count),
		"Outgoings": gomega.ConsistOf(outgoings...),
	}))
}

func matchBacklinkResult(count int, backlinks ...any) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Count":     gomega.Equal(count),
		"Backlinks": gomega.ConsistOf(backlinks...),
	}))
}

func matchResolvedOutgoingResultItem(fromPageID tree.PageID, toPageID tree.PageID, targetPath tree.RoutePath) types.GomegaMatcher {
	return gomega.WithTransform(outgoingResultObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"FromPageID": gomega.Equal(fromPageID),
		"ToPageID":   gomega.Equal(toPageID),
		"ToPath":     gomega.Equal(targetPath),
		"State":      gomega.Equal(linkResolutionResolved),
	}))
}

func matchBrokenOutgoingResultItem(fromPageID tree.PageID, targetPath tree.RoutePath) types.GomegaMatcher {
	return gomega.WithTransform(outgoingResultObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"FromPageID": gomega.Equal(fromPageID),
		"ToPageID":   gomega.Equal(tree.PageID("")),
		"ToPath":     gomega.Equal(targetPath),
		"State":      gomega.Equal(linkResolutionBroken),
	}))
}

type outgoingResultObservation struct {
	FromPageID tree.PageID
	ToPageID   tree.PageID
	ToPath     tree.RoutePath
	State      linkResolutionState
}

func outgoingResultObservationFor(item OutgoingResultItem) outgoingResultObservation {
	state := linkResolutionResolved
	if item.Broken {
		state = linkResolutionBroken
	}
	return outgoingResultObservation{
		FromPageID: item.FromPageID,
		ToPageID:   item.ToPageID,
		ToPath:     item.ToPath,
		State:      state,
	}
}

func matchOutgoingResultItemTitle(title string) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ToPageTitle": gomega.Equal(title),
	})
}

func matchBacklinkResultItem(fromPageID tree.PageID, toPageID tree.PageID) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"FromPageID": gomega.Equal(fromPageID),
		"ToPageID":   gomega.Equal(toPageID),
		"FromTitle":  gomega.Not(gomega.BeEmpty()),
	})
}

func matchBacklinkResultItemFromKind(fromKind tree.NodeKind) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"FromKind": gomega.Equal(fromKind),
	})
}

func matchBrokenBacklink(fromPageID tree.PageID) types.GomegaMatcher {
	return gomega.WithTransform(backlinkObservationFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"FromPageID": gomega.Equal(fromPageID),
		"ToPageID":   gomega.Equal(tree.PageID("")),
		"FromTitle":  gomega.Not(gomega.BeEmpty()),
		"State":      gomega.Equal(linkResolutionBroken),
	}))
}

type backlinkObservation struct {
	FromPageID tree.PageID
	ToPageID   tree.PageID
	FromTitle  string
	State      linkResolutionState
}

func backlinkObservationFor(actual any) (backlinkObservation, error) {
	switch item := actual.(type) {
	case Backlink:
		return backlinkObservationFromFields(item.FromPageID, item.ToPageID, item.FromTitle, item.Broken), nil
	case BacklinkResultItem:
		return backlinkObservationFromFields(item.FromPageID, item.ToPageID, item.FromTitle, item.Broken), nil
	default:
		return backlinkObservation{}, fmt.Errorf("expected backlink, got %T", actual)
	}
}

func backlinkObservationFromFields(fromPageID tree.PageID, toPageID tree.PageID, fromTitle string, broken bool) backlinkObservation {
	state := linkResolutionResolved
	if broken {
		state = linkResolutionBroken
	}
	return backlinkObservation{
		FromPageID: fromPageID,
		ToPageID:   toPageID,
		FromTitle:  fromTitle,
		State:      state,
	}
}

func matchBacklinkTitle(title string) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"FromTitle": gomega.Equal(title),
	})
}

func matchLinkStatusCounts(backlinks int, brokenIncoming int, outgoings int, brokenOutgoings int) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Backlinks":       gomega.Equal(backlinks),
		"BrokenIncoming":  gomega.Equal(brokenIncoming),
		"Outgoings":       gomega.Equal(outgoings),
		"BrokenOutgoings": gomega.Equal(brokenOutgoings),
	})
}
