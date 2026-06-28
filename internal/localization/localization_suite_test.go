package localization

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestLocalizationSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Localization Suite")
}
