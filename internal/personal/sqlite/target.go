package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/giomhern/psst/internal/domain"
)

// ConversationGUID resolves exactly one local row ID for Messages automation.
// Display names and participant lists are never treated as send destinations.
func (r *Reader) ConversationGUID(ctx context.Context, chatID string) (string, error) {
	if err := domain.ValidateMessageQuery(chatID, 1); err != nil {
		return "", err
	}
	var guid string
	err := r.db.QueryRowContext(ctx, `SELECT CASE WHEN typeof(guid)='text'
  AND length(CAST(guid AS BLOB)) BETWEEN 1 AND 1024 THEN guid ELSE '' END
  FROM chat WHERE ROWID=?`, chatID).Scan(&guid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrConversationNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolve local chat: %w", err)
	}
	if guid == "" {
		return "", domain.ErrSendUnavailable
	}
	return guid, nil
}
