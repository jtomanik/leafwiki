package links

import (
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
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"TargetPageID":   gomega.Equal(pageID),
		"TargetPagePath": gomega.Equal(targetPath),
		"Broken":         gomega.BeFalse(),
	})
}

func matchBrokenTargetLink(targetPath string) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"TargetPageID":   gomega.BeEmpty(),
		"TargetPagePath": gomega.Equal(targetPath),
		"Broken":         gomega.BeTrue(),
	})
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
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"FromPageID": gomega.Equal(fromPageID),
		"ToPageID":   gomega.Equal(toPageID),
		"ToPath":     gomega.Equal(targetPath),
		"Broken":     gomega.BeFalse(),
	})
}

func matchBrokenOutgoingResultItem(fromPageID tree.PageID, targetPath tree.RoutePath) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"FromPageID": gomega.Equal(fromPageID),
		"ToPageID":   gomega.BeEmpty(),
		"ToPath":     gomega.Equal(targetPath),
		"Broken":     gomega.BeTrue(),
	})
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
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"FromPageID": gomega.Equal(fromPageID),
		"ToPageID":   gomega.BeEmpty(),
		"FromTitle":  gomega.Not(gomega.BeEmpty()),
		"Broken":     gomega.BeTrue(),
	})
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
