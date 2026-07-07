package search

import (
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("SQLite search query planning", ginkgo.Label("unit"), func() {
	ginkgo.It("maps stored node kinds into route node kinds", func() {
		Expect(searchNodeKindObservationFor(string(searchSQLiteNodeKindFromNodeKind(tree.NodeKindPage)))).To(Equal(searchNodeKindObservation{
			State: searchNodeKindRecognized,
			Kind:  tree.NodeKindPage,
		}))
		Expect(searchNodeKindObservationFor("unsupported")).To(Equal(searchNodeKindObservation{
			State: searchNodeKindRejected,
		}))
	})

	ginkgo.It("preserves text-search and page-filter plan semantics", func() {
		Expect(searchQueryPlanFor("docs", []tree.PageID{newFixturePageID("page-a"), newFixturePageID("page-b")})).To(Equal(searchQueryPlanObservation{
			NodeKind:      tree.NodeKindSection,
			TextQuery:     searchFuzzyTextQuery,
			PageFilter:    searchTypedPageIDFilter,
			PageIDs:       []tree.PageID{newFixturePageID("page-a"), newFixturePageID("page-b")},
			TitleExpr:     searchHighlightedTitleExpr,
			ExcerptExpr:   searchSnippetExcerptExpr,
			RankExpr:      searchBM25RankExpr,
			OrderExpr:     searchBM25OrderExpr,
			FilterOutcome: searchCombinedTextAndPageFilter,
		}))

		Expect(searchQueryPlanFor("", nil)).To(Equal(searchQueryPlanObservation{
			NodeKind:      tree.NodeKindSection,
			TextQuery:     searchNoTextQuery,
			PageFilter:    searchNoPageFilter,
			TitleExpr:     searchStoredTitleExpr,
			ExcerptExpr:   searchEmptyExcerptExpr,
			RankExpr:      searchNeutralRankExpr,
			OrderExpr:     searchTitlePathOrderExpr,
			FilterOutcome: searchNoSearchFilter,
		}))
	})
})

type searchNodeKindState uint8

const (
	searchNodeKindUnknown searchNodeKindState = iota
	searchNodeKindRecognized
	searchNodeKindRejected
)

type searchNodeKindObservation struct {
	State searchNodeKindState
	Kind  tree.NodeKind
}

func searchNodeKindObservationFor(raw string) searchNodeKindObservation {
	kind, err := parseSearchSQLiteNodeKind(raw)
	if err != nil {
		return searchNodeKindObservation{State: searchNodeKindRejected}
	}
	return searchNodeKindObservation{
		State: searchNodeKindRecognized,
		Kind:  kind,
	}
}

type searchTextQueryPlan uint8

const (
	searchUnknownTextQuery searchTextQueryPlan = iota
	searchNoTextQuery
	searchFuzzyTextQuery
)

type searchPageFilterPlan uint8

const (
	searchUnknownPageFilter searchPageFilterPlan = iota
	searchNoPageFilter
	searchTypedPageIDFilter
)

type searchTitlePlan uint8

const (
	searchUnknownTitleExpr searchTitlePlan = iota
	searchStoredTitleExpr
	searchHighlightedTitleExpr
)

type searchExcerptPlan uint8

const (
	searchUnknownExcerptExpr searchExcerptPlan = iota
	searchEmptyExcerptExpr
	searchSnippetExcerptExpr
)

type searchRankPlan uint8

const (
	searchUnknownRankExpr searchRankPlan = iota
	searchNeutralRankExpr
	searchBM25RankExpr
)

type searchOrderPlan uint8

const (
	searchUnknownOrderExpr searchOrderPlan = iota
	searchTitlePathOrderExpr
	searchBM25OrderExpr
)

type searchFilterOutcome uint8

const (
	searchUnknownFilter searchFilterOutcome = iota
	searchNoSearchFilter
	searchCombinedTextAndPageFilter
)

type searchQueryPlanObservation struct {
	NodeKind      tree.NodeKind
	TextQuery     searchTextQueryPlan
	PageFilter    searchPageFilterPlan
	PageIDs       []tree.PageID
	TitleExpr     searchTitlePlan
	ExcerptExpr   searchExcerptPlan
	RankExpr      searchRankPlan
	OrderExpr     searchOrderPlan
	FilterOutcome searchFilterOutcome
}

func searchQueryPlanFor(query string, pageIDs []tree.PageID) searchQueryPlanObservation {
	kind, _ := parseSearchSQLiteNodeKind(string(searchSQLiteNodeKindFromNodeKind(tree.NodeKindSection)))
	ftsQuery := buildFuzzyQuery(query)
	where, args := buildSearchWhereClause(query, ftsQuery, pageIDs)
	hasQuery := strings.TrimSpace(query) != ""

	observation := searchQueryPlanObservation{
		NodeKind:    kind,
		TitleExpr:   observedSearchTitlePlan(hasQuery),
		ExcerptExpr: observedSearchExcerptPlan(hasQuery),
		RankExpr:    observedSearchRankPlan(hasQuery),
		OrderExpr:   observedSearchOrderPlan(hasQuery),
	}
	if !hasQuery {
		observation.TextQuery = searchNoTextQuery
	} else if len(args) > 0 && args[0] == ftsQuery && strings.Contains(where, "pages MATCH ?") {
		observation.TextQuery = searchFuzzyTextQuery
	}

	queryArgOffset := 0
	if hasQuery {
		queryArgOffset = 1
	}
	if len(pageIDs) == 0 {
		observation.PageFilter = searchNoPageFilter
	} else if searchFilterArgsMatchPageIDs(args[queryArgOffset:], pageIDs) &&
		strings.Contains(where, strings.TrimSuffix(strings.Repeat("?,", len(pageIDs)), ",")) {
		observation.PageFilter = searchTypedPageIDFilter
		observation.PageIDs = append([]tree.PageID(nil), pageIDs...)
	}

	if !hasQuery && len(pageIDs) == 0 && where == "" && len(args) == 0 {
		observation.FilterOutcome = searchNoSearchFilter
	} else if observation.TextQuery == searchFuzzyTextQuery && observation.PageFilter == searchTypedPageIDFilter {
		observation.FilterOutcome = searchCombinedTextAndPageFilter
	}
	return observation
}

func searchFilterArgsMatchPageIDs(args []interface{}, pageIDs []tree.PageID) bool {
	if len(args) != len(pageIDs) {
		return false
	}
	for i, arg := range args {
		if arg != pageIDs[i] {
			return false
		}
	}
	return true
}

func observedSearchTitlePlan(hasQuery bool) searchTitlePlan {
	if searchTitleExpr(hasQuery) == "highlight(pages, 4, '<b>', '</b>')" {
		return searchHighlightedTitleExpr
	}
	if searchTitleExpr(hasQuery) == "title" {
		return searchStoredTitleExpr
	}
	return searchUnknownTitleExpr
}

func observedSearchExcerptPlan(hasQuery bool) searchExcerptPlan {
	if searchExcerptExpr(hasQuery) == "snippet(pages, 6, '<b>', '</b>', '...', 16)" {
		return searchSnippetExcerptExpr
	}
	if searchExcerptExpr(hasQuery) == "''" {
		return searchEmptyExcerptExpr
	}
	return searchUnknownExcerptExpr
}

func observedSearchRankPlan(hasQuery bool) searchRankPlan {
	if searchRankExpr(hasQuery) == "0.0" {
		return searchNeutralRankExpr
	}
	if strings.Contains(searchRankExpr(hasQuery), "bm25(pages") {
		return searchBM25RankExpr
	}
	return searchUnknownRankExpr
}

func observedSearchOrderPlan(hasQuery bool) searchOrderPlan {
	if searchOrderByExpr(hasQuery) == "title COLLATE NOCASE ASC, path COLLATE NOCASE ASC" {
		return searchTitlePathOrderExpr
	}
	if searchOrderByExpr(hasQuery) == "bm25_score ASC" {
		return searchBM25OrderExpr
	}
	return searchUnknownOrderExpr
}
