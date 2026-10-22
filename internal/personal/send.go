package personal

import (
	"context"
	"time"

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
	var after int64
	observer, canObserve := reader.(sendObserver)
	if canObserve {
		var checkpointErr error
		after, checkpointErr = observer.sendCheckpoint(ctx)
		canObserve = checkpointErr == nil
	}
	closeErr := reader.Close()
	if lookupErr != nil || closeErr != nil || ctx.Err() != nil {
		return domain.SendResult{}, domain.ErrSendUnavailable
	}
	started := time.Now()
	result, err := request(ctx, guid, text)
	if result.Status != domain.SendRequested || !canObserve || ctx.Err() != nil {
		return result, err
	}
	result.Receipt = observeRequested(ctx, chatID, guid, text, after, started, open, waitForObservation)
	return result, err
}

func (r *Reader) sendTarget(ctx context.Context, chatID string) (string, error) {
	return r.store.ConversationGUID(ctx, chatID)
}

type sendObserver interface {
	sendCheckpoint(context.Context) (int64, error)
	matchSend(context.Context, string, string, string, int64, time.Time, time.Time) (domain.SendReceipt, error)
}

func waitForObservation(ctx context.Context) error {
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func observeRequested(ctx context.Context, chatID, guid, text string, after int64, started time.Time, open func(context.Context) (sendTargetReader, error), wait func(context.Context) error) domain.SendReceipt {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ctx = bounded
	reader, err := open(ctx)
	if err != nil {
		return domain.SendReceipt{}
	}
	observer, ok := reader.(sendObserver)
	var receipt domain.SendReceipt
	var observedGUID string
	if ok {
		// Do not select the first arrival: allow a short window for competing
		// identical sends and for Messages to populate its join and body fields.
		for i := 0; i < 6; i++ {
			if wait(ctx) != nil {
				receipt = domain.SendReceipt{}
				break
			}
			receipt, err = observer.matchSend(ctx, chatID, guid, text, after, started, time.Now())
			if receipt.GUID != "" && observedGUID != "" && receipt.GUID != observedGUID {
				err = domain.ErrReceiptUnavailable
			}
			if receipt.GUID != "" {
				observedGUID = receipt.GUID
			}
			if err != nil {
				receipt = domain.SendReceipt{}
				break
			}
		}
	}
	if reader.Close() != nil {
		return domain.SendReceipt{}
	}
	return receipt
}

func (r *Reader) sendCheckpoint(ctx context.Context) (int64, error) {
	return r.store.SendCheckpoint(ctx)
}
func (r *Reader) matchSend(ctx context.Context, chatID, guid, text string, after int64, started, ended time.Time) (domain.SendReceipt, error) {
	return r.store.MatchSend(ctx, chatID, guid, text, after, started, ended)
}

// Receipt checks local evidence for an already matched message; it never sends.
func Receipt(ctx context.Context, chatID, guid string) (domain.SendReceipt, error) {
	reader, err := Open(ctx)
	if err != nil {
		return domain.SendReceipt{}, domain.ErrReceiptUnavailable
	}
	receipt, readErr := reader.store.SendReceipt(ctx, chatID, guid)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		return domain.SendReceipt{}, domain.ErrReceiptUnavailable
	}
	return receipt, nil
}
