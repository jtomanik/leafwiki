package pages

import (
	"testing"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

func TestAPISuccessMessagesRenderFromCatalog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		messageID sharederrors.MessageID
		want      string
	}{
		{name: "delete", messageID: MessageIDAPIPagesDeleteSuccess, want: "Page deleted"},
		{name: "move", messageID: MessageIDAPIPagesMoveSuccess, want: "Page moved"},
		{name: "sort", messageID: MessageIDAPIPagesSortSuccess, want: "Pages sorted successfully"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := apiSuccessMessage(tt.messageID); got != tt.want {
				t.Fatalf("apiSuccessMessage(%q) = %q, want %q", tt.messageID, got, tt.want)
			}
		})
	}
}
