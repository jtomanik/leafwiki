package frontd

import (
	"encoding/json"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

type frontdStructuredErrorResponse struct {
	Status    int
	Code      sharederrors.ErrorCode
	MessageID sharederrors.MessageID
	Message   string
}

func matchStructuredFrontdError(status int, code sharederrors.ErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	return WithTransform(func(rec *httptest.ResponseRecorder) frontdStructuredErrorResponse {
		GinkgoHelper()
		var body struct {
			Error struct {
				Code      sharederrors.ErrorCode `json:"code"`
				MessageID sharederrors.MessageID `json:"messageId"`
				Message   string                 `json:"message"`
			} `json:"error"`
		}
		Expect(json.Unmarshal(rec.Body.Bytes(), &body)).To(Succeed(), "structured error body: %s", rec.Body.Bytes())
		return frontdStructuredErrorResponse{
			Status:    rec.Code,
			Code:      body.Error.Code,
			MessageID: body.Error.MessageID,
			Message:   body.Error.Message,
		}
	}, SatisfyAll(
		HaveField("Status", Equal(status)),
		HaveField("Code", Equal(code)),
		HaveField("MessageID", Equal(messageID)),
		HaveField("Message", Not(BeEmpty())),
	))
}
