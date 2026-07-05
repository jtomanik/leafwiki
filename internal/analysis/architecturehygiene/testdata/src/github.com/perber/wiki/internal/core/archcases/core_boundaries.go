package archcases

import (
	_ "github.com/perber/wiki/internal/core/tree"
	_ "github.com/perber/wiki/internal/http/private"          // want "semh:architecture.import-boundary.core-to-http: Core domain code must stay transport-agnostic."
	_ "github.com/perber/wiki/internal/projectdaemon/runtime" // want "semh:architecture.import-boundary.core-to-projectdaemon: Core domain code must not know daemon/runtime orchestration."
	_ "github.com/perber/wiki/internal/wiki/routes"           // want "semh:architecture.import-boundary.core-to-wiki: Core domain code must not depend on HTTP/wiki route adapters."
	_ "github.com/perber/wiki/internal/wikiish"
)
