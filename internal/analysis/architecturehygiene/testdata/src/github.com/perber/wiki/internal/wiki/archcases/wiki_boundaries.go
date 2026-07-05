package archcases

import (
	_ "github.com/perber/wiki/cmd/leafwiki" // want "semh:architecture.import-boundary.wiki-to-cmd-leafwiki: Route/domain assembly must not depend on CLI startup code."
	_ "github.com/perber/wiki/internal/wiki/routes"
)
