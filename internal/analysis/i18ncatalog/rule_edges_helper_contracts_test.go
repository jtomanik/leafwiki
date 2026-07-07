package i18ncatalog_test

import (
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	i18ncatalog "github.com/perber/wiki/internal/analysis/i18ncatalog"
)

var _ = ginkgo.Describe("i18n catalog helper contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("keeps gin payload checks scoped to production policy files", func() {
		Expect(i18ncatalog.GinPayloadPolicyDecisionsForTest([]string{
			"/repo/internal/wiki/routes/handler.go",
			"/repo/cmd/leafwiki/main.go",
			"/repo/internal/wiki/routes/handler_test.go",
			"/repo/internal/localization/messages.go",
			"/repo/pkg/example/example.go",
		})).To(Equal([]bool{true, true, false, false, false}))
	})

	ginkgo.It("recognizes shared localized error detail constructors by semantic helper name", func() {
		Expect(i18ncatalog.LocalizedErrorDetailHelperDecisionsForTest([]string{
			"NewLocalizedErrorDetail",
			"NewLocalizedErrorDetailFromCode",
			"LocalizedErrorDetailFromError",
			"NewErrorDetail",
		})).To(Equal([]bool{true, true, true, false}))
	})

	ginkgo.It("falls back to imports when localized error detail type facts are unavailable", func() {
		Expect([]bool{
			i18ncatalog.SharedErrorsLocalizedErrorDetailFallbackForTest(`import "github.com/perber/wiki/internal/core/shared/errors"`, `errors.NewLocalizedErrorDetail("code")`, true),
			i18ncatalog.SharedErrorsLocalizedErrorDetailFallbackForTest(`import sharederrors "github.com/perber/wiki/internal/core/shared/errors"`, `sharederrors.NewLocalizedErrorDetailFromCode("code")`, true),
			i18ncatalog.SharedErrorsLocalizedErrorDetailFallbackForTest(`import other "github.com/perber/wiki/internal/other"`, `other.NewLocalizedErrorDetail("code")`, true),
			i18ncatalog.SharedErrorsLocalizedErrorDetailFallbackForTest(`import "github.com/perber/wiki/internal/core/shared/errors"`, `errors.NotLocalizedErrorDetail("code")`, true),
			i18ncatalog.SharedErrorsLocalizedErrorDetailFallbackForTest(`import "github.com/perber/wiki/internal/core/shared/errors"`, `factory().NewLocalizedErrorDetail("code")`, true),
			i18ncatalog.SharedErrorsLocalizedErrorDetailFallbackForTest(`import "github.com/perber/wiki/internal/core/shared/errors"`, `errors.NewLocalizedErrorDetail("code")`, false),
		}).To(Equal([]bool{true, true, false, false, false, false}))
	})

	ginkgo.It("matches the LeafWiki localized error detail type identity exactly", func() {
		Expect(i18ncatalog.LeafWikiLocalizedErrorDetailTypeDecisionsForTest()).To(Equal([]bool{true, false, false}))
	})

	ginkgo.It("extracts semantic names from expression shapes used by payload policies", func() {
		Expect(i18ncatalog.ExpressionSemanticNamesForTest([]string{
			"errorCode",
			"rfcErr.ErrorField",
			"factory().ErrorField",
			`"literal"`,
		})).To(Equal([]string{"errorCode", "rfcErr.ErrorField", "ErrorField", ""}))
	})

	ginkgo.It("keeps repository helper outputs stable", func() {
		Expect(i18ncatalog.DefaultImportNamesForTest([]string{
			"github.com/perber/wiki/internal/core/shared/errors",
			"errors",
			"",
		})).To(Equal([]string{"errors", "errors", ""}))
		Expect(i18ncatalog.FirstDifferentLinesForTest([][2]string{
			{"same\nprefix\nactual", "same\nprefix\nexpected"},
			{"short", "shorter"},
		})).To(Equal([]int{3, 1}))
		Expect(observeGeneratedRunMessagesContent(i18ncatalog.GeneratedRunMessagesContentForTest([]i18ncatalog.RunMessagePairForTest{
			{Name: "LEAFWIKI_RUN_MSG_USAGE", ID: "shell.run.usage"},
			{Name: "LEAFWIKI_RUN_MSG_QUOTED", ID: "shell.run.quoted"},
		}, map[string]string{
			"LEAFWIKI_RUN_MSG_USAGE":  "'Usage text'",
			"LEAFWIKI_RUN_MSG_QUOTED": "'Quoted text'",
		}))).To(Equal(generatedRunMessagesObservation{
			HasShebang:    true,
			HasUsageLine:  true,
			HasQuotedLine: true,
		}))
	})
})

type generatedRunMessagesObservation struct {
	HasShebang    bool
	HasUsageLine  bool
	HasQuotedLine bool
}

func observeGeneratedRunMessagesContent(content string) generatedRunMessagesObservation {
	return generatedRunMessagesObservation{
		HasShebang:    strings.HasPrefix(content, "#!/usr/bin/env bash\n"),
		HasUsageLine:  strings.Contains(content, "LEAFWIKI_RUN_MSG_USAGE="),
		HasQuotedLine: strings.Contains(content, "LEAFWIKI_RUN_MSG_QUOTED="),
	}
}
