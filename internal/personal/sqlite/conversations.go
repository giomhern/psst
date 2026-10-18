package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/giomhern/psst/internal/domain"
)

// ErrUnsupportedSchema means the baseline required by Personal queries is absent.
var ErrUnsupportedSchema = errors.New("unsupported Messages schema")

// ListConversations returns at most limit chats, ordered by newest locally stored
// message rowid, then chat rowid. This is not timestamp-based activity ordering.
// Titles are bounded to 200 Unicode characters; message bodies are not selected.
func (r *Reader) ListConversations(ctx context.Context, limit int) ([]domain.Conversation, error) {
	if err := domain.ValidateConversationLimit(limit); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin conversation listing: %w", err)
	}
	defer tx.Rollback()
	schema, err := inspectSchema(ctx, tx)
	if err != nil {
		return nil, err
	}
	if len(schema.Missing) != 0 {
		return nil, ErrUnsupportedSchema
	}
	columns, err := columnNames(ctx, tx, "chat")
	if err != nil {
		return nil, err
	}
	title := `COALESCE(NULLIF(trim(c.guid), ''), 'Untitled conversation')`
	if columns["display_name"] {
		title = `COALESCE(NULLIF(trim(c.display_name), ''), NULLIF(trim(c.guid), ''), 'Untitled conversation')`
	}
	// Only the fixed title expressions above are inserted into SQL. User input
	// is bound through placeholders. The correlated aggregate avoids duplicates
	// from group chats, multiple message links, or chats without messages.
	query := `SELECT c.ROWID, substr(` + title + `, 1, 200),
        (SELECT MAX(m.ROWID) FROM chat_message_join AS cmj
         JOIN message AS m ON m.ROWID = cmj.message_id
         WHERE cmj.chat_id = c.ROWID) AS latest_message
        FROM chat AS c
        ORDER BY latest_message IS NULL, latest_message DESC, c.ROWID DESC
        LIMIT ?`
	rows, err := tx.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query conversations: %w", err)
	}
	conversations := make([]domain.Conversation, 0, limit)
	for rows.Next() {
		var id int64
		var title string
		var latest sql.NullInt64
		if err := rows.Scan(&id, &title, &latest); err != nil {
			rows.Close()
			return nil, fmt.Errorf("decode conversation: %w", err)
		}
		conversations = append(conversations, domain.Conversation{ID: strconv.FormatInt(id, 10), Title: title})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate conversations: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close conversation rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("finish conversation listing: %w", err)
	}
	return conversations, nil
}
