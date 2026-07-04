package links

import (
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
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
	return gcustom.MakeMatcher(func(link TargetLink) (bool, error) {
		return link.TargetPageID == pageID &&
			link.TargetPagePath == targetPath &&
			!link.Broken, nil
	}).WithMessage("describe a resolved target link")
}

func matchBrokenTargetLink(targetPath string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(link TargetLink) (bool, error) {
		return link.TargetPageID == "" &&
			link.TargetPagePath == targetPath &&
			link.Broken, nil
	}).WithMessage("describe a broken target link")
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
	return gcustom.MakeMatcher(func(item OutgoingResultItem) (bool, error) {
		return item.FromPageID == fromPageID &&
			item.ToPageID == toPageID &&
			item.ToPath == targetPath &&
			!item.Broken, nil
	}).WithMessage("describe a resolved outgoing link")
}

func matchBrokenOutgoingResultItem(fromPageID tree.PageID, targetPath tree.RoutePath) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(item OutgoingResultItem) (bool, error) {
		return item.FromPageID == fromPageID &&
			item.ToPageID == "" &&
			item.ToPath == targetPath &&
			item.Broken, nil
	}).WithMessage("describe a broken outgoing link")
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
	return gcustom.MakeMatcher(func(actual any) (bool, error) {
		switch item := actual.(type) {
		case Backlink:
			return item.FromPageID == fromPageID &&
				item.ToPageID == "" &&
				item.FromTitle != "" &&
				item.Broken, nil
		case BacklinkResultItem:
			return item.FromPageID == fromPageID &&
				item.ToPageID == "" &&
				item.FromTitle != "" &&
				item.Broken, nil
		default:
			return false, nil
		}
	}).WithMessage("describe a broken backlink")
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
