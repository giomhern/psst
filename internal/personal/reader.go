package personal

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/giomhern/psst/internal/domain"
	"github.com/giomhern/psst/internal/personal/sqlite"
)

// Reader owns a local read-only adapter and keeps raw database errors out of
// user-facing failures. Construct it with Open, then call Close when finished.
type Reader struct {
	store *sqlite.Reader
	names nameResolver
}

func openReader(ctx context.Context, path string) (*Reader, error) {
	store, err := sqlite.Open(ctx, path)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) {
			return nil, errors.New(databaseError(err).Detail)
		}
		return nil, conversationError(ctx, err)
	}
	return &Reader{store: store}, nil
}

// ListConversations reads bounded chat summaries without uploading or logging them.
func (r *Reader) ListConversations(ctx context.Context, limit int) ([]domain.Conversation, error) {
	if err := domain.ValidateConversationLimit(limit); err != nil {
		return nil, err
	}
	conversations, err := r.store.ListConversations(ctx, limit)
	if err != nil {
		return nil, conversationError(ctx, err)
	}
	conversations, err = resolveTitles(ctx, conversations, r.names)
	if err != nil {
		return nil, conversationError(ctx, err)
	}
	return conversations, nil
}

// ListMessages reads a bounded recent page without invoking Contacts or logging
// message content. Sender identifiers remain local display data.
func (r *Reader) ListMessages(ctx context.Context, chatID string, limit int) ([]domain.Message, error) {
	page, err := r.ListMessagePage(ctx, chatID, limit, nil)
	return page.Messages, err
}

// ListMessagePage reads a bounded history page with an optional exclusive cursor.
func (r *Reader) ListMessagePage(ctx context.Context, chatID string, limit int, before *domain.MessageCursor) (domain.MessagePage, error) {
	if err := domain.ValidateMessagePage(chatID, limit, before); err != nil {
		return domain.MessagePage{}, err
	}
	page, err := r.store.ListMessagePage(ctx, chatID, limit, before)
	if errors.Is(err, domain.ErrConversationNotFound) {
		return domain.MessagePage{}, domain.ErrConversationNotFound
	}
	if err != nil {
		return domain.MessagePage{}, conversationError(ctx, err)
	}
	return page, nil
}

// Close releases the read-only database connection without exposing driver errors.
func (r *Reader) Close() error {
	if err := r.store.Close(); err != nil {
		return errors.New("could not close Personal database connection")
	}
	return nil
}

func conversationError(ctx context.Context, err error) error {
	if errors.Is(err, sqlite.ErrUnsupportedSchema) {
		return errors.New("unsupported Messages schema; run psst doctor for compatibility details")
	}
	if ctx.Err() != nil {
		return fmt.Errorf("personal operation stopped: %w", ctx.Err())
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("personal operation stopped: %w", context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("personal operation stopped: %w", context.DeadlineExceeded)
	}
	return errors.New("could not read Personal data; run psst doctor to check access and schema compatibility")
}
