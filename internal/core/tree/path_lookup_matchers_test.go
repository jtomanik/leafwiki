package tree

import (
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
)

func MatchPathLookupState(exists bool, canCreate bool) OmegaMatcher {
	return WithTransform(pathLookupValue, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Exists":    Equal(exists),
		"CanCreate": Equal(canCreate),
	}))
}

func MatchExistingPathLookupWithKind(kind NodeKind) OmegaMatcher {
	return WithTransform(pathLookupValue, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Exists": BeTrue(),
		"Segments": HaveExactElements(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Kind": WithTransform(func(actual *NodeKind) NodeKind {
				if actual == nil {
					return ""
				}
				return *actual
			}, Equal(kind)),
		})),
	}))
}

func pathLookupValue(lookup *PathLookup) PathLookup {
	if lookup == nil {
		return PathLookup{}
	}
	return *lookup
}
