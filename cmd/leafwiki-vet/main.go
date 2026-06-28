package main

import (
	"github.com/perber/wiki/internal/analysis/semantichygiene"
	"golang.org/x/tools/go/analysis/singlechecker"
)

var runSingleChecker = singlechecker.Main

func main() {
	runSingleChecker(semantichygiene.Analyzer)
}
