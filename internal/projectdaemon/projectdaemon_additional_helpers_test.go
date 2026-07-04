package projectdaemon

import (
	"os"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

type parsedControlErrorBody struct {
	Code      sharederrors.ErrorCode
	MessageID sharederrors.MessageID
}

func parsedControlErrorBodyForSpec(raw []byte) parsedControlErrorBody {
	code, messageID, _ := parseControlErrorBody(raw)
	return parsedControlErrorBody{
		Code:      code,
		MessageID: messageID,
	}
}

type fakeDescriptorTempFile struct {
	name      string
	chmodErr  error
	writeErrs map[int]error
	closeErr  error
	writes    int
}

func (f *fakeDescriptorTempFile) Name() string {
	return f.name
}

func (f *fakeDescriptorTempFile) Chmod(os.FileMode) error {
	return f.chmodErr
}

func (f *fakeDescriptorTempFile) Write(raw []byte) (int, error) {
	f.writes++
	if err := f.writeErrs[f.writes]; err != nil {
		return 0, err
	}
	return len(raw), nil
}

func (f *fakeDescriptorTempFile) Close() error {
	return f.closeErr
}
