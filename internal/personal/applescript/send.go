// Package applescript requests explicit Personal sends without touching SQLite.
package applescript

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/giomhern/psst/internal/domain"
)

type runner func(context.Context, string, string) ([]byte, bool, error)

// request never retries. started distinguishes failure before launching osascript
// from a timeout/process failure after launch, whose side effects are ambiguous.
func request(ctx context.Context, guid, text string, run runner) (domain.SendResult, error) {
	if err := domain.ValidateSendText(text); err != nil {
		return domain.SendResult{}, err
	}
	if guid == "" || len(guid) > 1024 || !utf8.ValidString(guid) || strings.ContainsRune(guid, 0) || strings.HasPrefix(guid, "-") {
		return domain.SendResult{}, domain.ErrSendUnavailable
	}
	if ctx.Err() != nil {
		return domain.SendResult{}, domain.ErrSendUnavailable
	}
	operation, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	output, started, err := run(operation, guid, text)
	if !started {
		return domain.SendResult{}, domain.ErrSendUnavailable
	}
	if err != nil || len(output) > 64 {
		return domain.SendResult{Status: domain.SendUnknown}, domain.ErrSendUnknown
	}
	switch strings.TrimSpace(string(output)) {
	case "requested":
		return domain.SendResult{Status: domain.SendRequested}, nil
	case "not_requested":
		return domain.SendResult{}, domain.ErrSendUnavailable
	case "permission_denied":
		return domain.SendResult{}, domain.ErrAutomationDenied
	default:
		return domain.SendResult{Status: domain.SendUnknown}, domain.ErrSendUnknown
	}
}
