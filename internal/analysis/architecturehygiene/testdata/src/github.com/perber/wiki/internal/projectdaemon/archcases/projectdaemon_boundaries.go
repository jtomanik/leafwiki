package archcases

import (
	_ "github.com/perber/wiki/cmd/leafwiki" // want "semh:architecture.import-boundary.projectdaemon-to-cmd-leafwiki: Daemon primitives must not depend on the executable entrypoint."
	_ "github.com/perber/wiki/internal/projectdaemon/runtime"
)
