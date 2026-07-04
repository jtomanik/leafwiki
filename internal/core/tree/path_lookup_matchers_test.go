package tree

import (
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
)

func MatchPathLookupState(exists bool, canCreate bool) OmegaMatcher {
	return gcustom.MakeMatcher(func(lookup *PathLookup) (bool, error) {
		if lookup == nil {
			return false, nil
		}
		return lookup.Exists == exists && lookup.CanCreate == canCreate, nil
	}).WithMessage("describe path lookup creation state")
}

func MatchExistingPathLookupWithKind(kind NodeKind) OmegaMatcher {
	return gcustom.MakeMatcher(func(lookup *PathLookup) (bool, error) {
		if lookup == nil || !lookup.Exists || len(lookup.Segments) != 1 {
			return false, nil
		}
		segment := lookup.Segments[0]
		return segment.Kind != nil && *segment.Kind == kind, nil
	}).WithMessage("describe existing path lookup by node kind")
}
