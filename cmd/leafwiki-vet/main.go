package main

import (
	"github.com/perber/wiki/internal/analysis/semantichygiene"
	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() {
	singlechecker.Main(semantichygiene.Analyzer)
}
