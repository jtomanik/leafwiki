package e2eproxy

import (
	_ "github.com/perber/wiki/internal/core/tree" // want "e2e-proxy must not import LeafWiki internal packages; assert protocol semantics or define local black-box test helpers"
)
