package leafwiki

import (
	"errors"
	"fmt"

	"github.com/golangci/plugin-module-register/register"
	"github.com/perber/wiki/internal/analysis/i18ncatalog"
	"github.com/perber/wiki/internal/analysis/semantichygiene"
	"golang.org/x/tools/go/analysis"
)

var ErrInvalidSettings = errors.New("invalid leafwiki golangci-lint plugin settings")

type settings struct{}

func init() {
	register.Plugin("leafwiki", New)
}

func New(rawSettings any) (register.LinterPlugin, error) {
	if rawSettings != nil {
		if _, err := register.DecodeSettings[settings](rawSettings); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidSettings, err)
		}
	}

	return plugin{}, nil
}

type plugin struct{}

var _ register.LinterPlugin = plugin{}

func (plugin) BuildAnalyzers() ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{
		semantichygiene.Analyzer,
		i18ncatalog.Analyzer,
	}, nil
}

func (plugin) GetLoadMode() string {
	return register.LoadModeTypesInfo
}
