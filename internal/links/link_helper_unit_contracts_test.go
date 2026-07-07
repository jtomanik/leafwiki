package links

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("link helper semantic contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("classifies markdown source and target kinds for stored link contracts", func() {
		Expect(linkKindObservationFor(tree.NodeKindPage, markdownlinks.TargetKindPage, MarkdownSourceKindPage)).To(Equal(linkKindObservation{
			NodeTargetKind:     TargetKindPage,
			MarkdownTargetKind: tree.NodeKindPage,
			SourceKind:         MarkdownSourceKindPage,
			SourceTargetKind:   TargetKindPage,
		}))
		Expect(linkKindObservationFor(tree.NodeKindSection, markdownlinks.TargetKindSection, MarkdownSourceKindSection)).To(Equal(linkKindObservation{
			NodeTargetKind:     TargetKindSection,
			MarkdownTargetKind: tree.NodeKindSection,
			SourceKind:         MarkdownSourceKindSection,
			SourceTargetKind:   TargetKindSection,
		}))
		Expect(storedKindObservationFor(nonCanonicalPageStoredTarget)).To(Equal(storedKindObservation{
			StoredKind: nonCanonicalPageStoredTarget,
			State:      storedKindNonCanonicalPage,
		}))
		Expect(storedKindObservationFor(emptyFixtureTargetKind)).To(Equal(storedKindObservation{
			StoredKind: TargetKindPage,
			State:      storedKindDefaultPage,
		}))
	})

	ginkgo.It("normalizes wiki destinations and preserves source markdown paths", func() {
		Expect(destinationObservationFor("docs/page.md?print=1#intro")).To(Equal(destinationObservation{
			NormalizedPath:  "/docs/page.md",
			ExtensionPolicy: destinationHasExtension,
			AssetPolicy:     destinationWikiPath,
			Base:            "docs/page.md?print=1",
			Suffix:          "#intro",
		}))
		Expect(destinationObservationFor("<docs/section>")).To(Equal(destinationObservation{
			NormalizedPath:  "/<docs/section>",
			ExtensionPolicy: destinationExtensionlessWikiTarget,
			AssetPolicy:     destinationWikiPath,
			Base:            "<docs/section>",
		}))
		Expect(destinationObservationFor("/assets/logo.png")).To(Equal(destinationObservation{
			NormalizedPath:  "/assets/logo.png",
			ExtensionPolicy: destinationHasExtension,
			AssetPolicy:     destinationAssetPath,
			Base:            "/assets/logo.png",
		}))

		Expect(markdownRouteObservationFor(newFixtureRoutePath("/docs/guide"), tree.NodeKindPage)).To(Equal(markdownRouteObservation{
			SourceFile:  newFixtureMarkdownPath("docs/guide.md"),
			ContentFile: newFixtureMarkdownPath("docs/guide.md"),
		}))
		Expect(markdownRouteObservationFor(newFixtureRoutePath("/docs"), tree.NodeKindSection)).To(Equal(markdownRouteObservation{
			SourceFile:  newFixtureMarkdownPath("docs/index.md"),
			ContentFile: newFixtureMarkdownPath("docs/index.md"),
		}))
	})

	ginkgo.It("maps unresolved markdown destinations to stored target kinds", func() {
		Expect(unresolvedTargetObservationFor(markdownlinks.IssueCodeBrokenPage, "missing")).To(Equal(unresolvedTargetObservation{
			StoredKind: TargetKindPage,
			State:      unresolvedDestinationPage,
		}))
		Expect(unresolvedTargetObservationFor(markdownlinks.IssueCodeBrokenLink, "docs/")).To(Equal(unresolvedTargetObservation{
			StoredKind: TargetKindSection,
			State:      unresolvedDestinationSection,
		}))
		Expect(unresolvedTargetObservationFor(markdownlinks.IssueCodeBrokenLink, "docs/page.md")).To(Equal(unresolvedTargetObservation{
			StoredKind: TargetKindPage,
			State:      unresolvedDestinationPage,
		}))
		Expect(unresolvedTargetObservationFor(markdownlinks.IssueCodeBrokenLink, "docs/page")).To(Equal(unresolvedTargetObservation{
			StoredKind: TargetKindUnknown,
			State:      unresolvedDestinationUnknown,
		}))
	})

	ginkgo.It("maps stored outgoing rows into client-visible link states", func() {
		Expect(outgoingResultObservationFor(toOutgoingResultItem(nil, Outgoing{
			FromPageID: newFixturePageID("source"),
			ToPath:     newFixtureRoutePath("/legacy"),
			ToKind:     nonCanonicalPageStoredTarget,
			Broken:     true,
		}))).To(Equal(outgoingResultObservation{
			FromPageID: newFixturePageID("source"),
			ToPath:     newFixtureRoutePath("/legacy"),
			State:      linkResolutionBroken,
		}))
	})
})

type linkKindObservation struct {
	NodeTargetKind     TargetKind
	MarkdownTargetKind tree.NodeKind
	SourceKind         MarkdownSourceKind
	SourceTargetKind   TargetKind
}

func linkKindObservationFor(nodeKind tree.NodeKind, markdownKind markdownlinks.TargetKind, sourceKind MarkdownSourceKind) linkKindObservation {
	return linkKindObservation{
		NodeTargetKind:     TargetKindFromNodeKind(nodeKind),
		MarkdownTargetKind: markdownTargetNodeKind(markdownKind),
		SourceKind:         MarkdownSourceKindFromNodeKind(nodeKind),
		SourceTargetKind:   sourceKind.TargetKind(),
	}
}

type storedKindState uint8

const (
	storedKindDefaultPage storedKindState = iota
	storedKindNonCanonicalPage
)

type storedKindObservation struct {
	StoredKind TargetKind
	State      storedKindState
}

func storedKindObservationFor(kind TargetKind) storedKindObservation {
	state := storedKindDefaultPage
	if kind == nonCanonicalPageStoredTarget {
		state = storedKindNonCanonicalPage
	}
	return storedKindObservation{
		StoredKind: kind.Stored(),
		State:      state,
	}
}

type destinationExtensionPolicy uint8

const (
	destinationExtensionlessWikiTarget destinationExtensionPolicy = iota
	destinationHasExtension
)

type destinationAssetPolicy uint8

const (
	destinationWikiPath destinationAssetPolicy = iota
	destinationAssetPath
)

type destinationObservation struct {
	NormalizedPath  string
	ExtensionPolicy destinationExtensionPolicy
	AssetPolicy     destinationAssetPolicy
	Base            string
	Suffix          string
}

func destinationObservationFor(destination string) destinationObservation {
	base, suffix := splitLinkDestinationSuffix(destination)
	observation := destinationObservation{
		NormalizedPath:  normalizeWikiPath(destination),
		ExtensionPolicy: destinationExtensionlessWikiTarget,
		AssetPolicy:     destinationWikiPath,
		Base:            base,
		Suffix:          suffix,
	}
	if !isExtensionlessWikiDestination(destination) {
		observation.ExtensionPolicy = destinationHasExtension
	}
	if isAssetLinkDestination(destination) {
		observation.AssetPolicy = destinationAssetPath
	}
	return observation
}

type markdownRouteObservation struct {
	SourceFile  tree.MarkdownPath
	ContentFile tree.MarkdownPath
}

func markdownRouteObservationFor(routePath tree.RoutePath, kind tree.NodeKind) markdownRouteObservation {
	return markdownRouteObservation{
		SourceFile:  markdownSourceFileForRoute(routePath, kind),
		ContentFile: markdownContentPathForRoute(routePath, kind),
	}
}

type unresolvedDestinationState uint8

const (
	unresolvedDestinationUnknown unresolvedDestinationState = iota
	unresolvedDestinationPage
	unresolvedDestinationSection
)

type unresolvedTargetObservation struct {
	StoredKind TargetKind
	State      unresolvedDestinationState
}

func unresolvedTargetObservationFor(code markdownlinks.IssueCode, href string) unresolvedTargetObservation {
	storedKind := unresolvedStoredTargetKind(markdownlinks.Resolution{Code: code}, href)
	state := unresolvedDestinationUnknown
	switch storedKind {
	case TargetKindPage:
		state = unresolvedDestinationPage
	case TargetKindSection:
		state = unresolvedDestinationSection
	}
	return unresolvedTargetObservation{
		StoredKind: storedKind,
		State:      state,
	}
}
