package links

import (
	"database/sql"
	"path/filepath"

	"github.com/perber/wiki/internal/core/shared/sqliteutil"
)

var (
	linksSQLOpen                  = sql.Open
	linksFilepathRel              = filepath.Rel
	linksResolveMarkdownRoutePath = resolveMarkdownRoutePathForSource
	linksIsSQLiteRecoverableError = sqliteutil.IsSQLiteRecoverableError
	linksRemoveSQLiteFiles        = sqliteutil.RemoveSQLiteFiles
	linksCloseRows                = func(rows interface{ Close() error }) error {
		return rows.Close()
	}
	linksCloseStatement = func(stmt interface{ Close() error }) error {
		return stmt.Close()
	}
)
