package pagesave

import (
	"log/slog"

	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/properties"
)

// PropertiesSideEffect updates the properties index after every page mutation.
type PropertiesSideEffect struct {
	svc propertiesIndexService
	log *slog.Logger
}

type propertiesIndexService interface {
	SetPropertiesForPage(pageID tree.PageID, props map[string]properties.PropertyEntry) error
	DeletePropertiesForPage(pageID tree.PageID) error
}

func NewPropertiesSideEffect(svc *properties.PropertiesService, log *slog.Logger) *PropertiesSideEffect {
	if log == nil {
		log = slog.Default()
	}
	var propSvc propertiesIndexService
	if svc != nil {
		propSvc = svc
	}
	return &PropertiesSideEffect{svc: propSvc, log: log}
}

func (e *PropertiesSideEffect) Apply(event PageSaveEvent) {
	if e.svc == nil {
		return
	}
	switch event.Operation {
	case PageOperationCreate, PageOperationUpdate, PageOperationRestore:
		if event.After != nil {
			e.setProperties(event.After)
			return
		}
		for _, p := range event.AffectedPages {
			e.setProperties(p)
		}

	case PageOperationMove:
		// page_id is stable across moves; properties are unchanged.

	case PageOperationDelete:
		for _, p := range event.AffectedPages {
			e.deleteProperties(p)
		}
	}
}

func (e *PropertiesSideEffect) setProperties(p *tree.Page) {
	props := properties.ExtractPropertiesFromContent(p.RawContent)
	if err := e.svc.SetPropertiesForPage(p.ID, props); err != nil {
		e.log.Warn("failed to set properties for page", "pageID", p.ID, "error", err)
	}
}

func (e *PropertiesSideEffect) deleteProperties(p *tree.Page) {
	if err := e.svc.DeletePropertiesForPage(p.ID); err != nil {
		e.log.Warn("failed to delete properties for page", "pageID", p.ID, "error", err)
	}
}
