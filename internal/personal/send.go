package personal

import (
	"context"

	"github.com/giomhern/psst/internal/domain"
	"github.com/giomhern/psst/internal/personal/applescript"
)

type sendTargetReader interface {
	sendTarget(context.Context, string) (string, error)
	Close() error
}

// Send requests an explicitly confirmed text send to an existing local chat.
// It closes the read-only target lookup before launching automation and never
// retries. Callers must distinguish request acceptance from unknown delivery.
func Send(ctx context.Context, chatID, text string) (domain.SendResult, error) {
	return send(ctx, chatID, text, func(ctx context.Context) (sendTargetReader, error) { return Open(ctx) }, applescript.Send)
}

func send(ctx context.Context, chatID, text string, open func(context.Context) (sendTargetReader, error), request func(context.Context, string, string) (domain.SendResult, error)) (domain.SendResult, error) {
	if err := domain.ValidateMessageQuery(chatID, 1); err != nil {
		return domain.SendResult{}, err
	}
	if err := domain.ValidateSendText(text); err != nil {
		return domain.SendResult{}, err
	}
	reader, err := open(ctx)
	if err != nil {
		return domain.SendResult{}, domain.ErrSendUnavailable
	}
	guid, lookupErr := reader.sendTarget(ctx, chatID)
	closeErr := reader.Close()
	if lookupErr != nil || closeErr != nil || ctx.Err() != nil {
		return domain.SendResult{}, domain.ErrSendUnavailable
	}
	return request(ctx, guid, text)
}

func (r *Reader) sendTarget(ctx context.Context, chatID string) (string, error) {
	return r.store.ConversationGUID(ctx, chatID)
}
