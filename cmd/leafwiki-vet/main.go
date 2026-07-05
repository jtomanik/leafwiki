package main

import (
	"github.com/perber/wiki/internal/analysis/leafwikivet"
	"golang.org/x/tools/go/analysis/multichecker"
)

var runCheckers = multichecker.Main

func main() {
	runCheckers(leafwikivet.ProjectAnalyzers()...)
}
