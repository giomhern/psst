//go:build !darwin

package applescript

import (
	"context"
	"fmt"
	"github.com/giomhern/psst/internal/domain"
)

// Send reports that Messages automation requires macOS. It starts no process.
func Send(context.Context, string, string) (domain.SendResult, error) {
	return domain.SendResult{}, fmt.Errorf("personal sends require macOS: %w", domain.ErrSendUnavailable)
}
