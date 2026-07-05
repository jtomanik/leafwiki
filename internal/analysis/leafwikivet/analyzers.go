package leafwikivet

import (
	"github.com/perber/wiki/internal/analysis/architecturehygiene"
	"github.com/perber/wiki/internal/analysis/i18ncatalog"
	"github.com/perber/wiki/internal/analysis/semantichygiene"
	"github.com/perber/wiki/internal/analysis/testhygiene"
	"golang.org/x/tools/go/analysis"
)

func PolicyAnalyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		semantichygiene.Analyzer,
		testhygiene.Analyzer,
		architecturehygiene.Analyzer,
	}
}

func ProjectAnalyzers() []*analysis.Analyzer {
	analyzers := PolicyAnalyzers()
	analyzers = append(analyzers, i18ncatalog.Analyzer)
	return analyzers
}
