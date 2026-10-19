package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/giomhern/psst/internal/domain"
)

const appleEpochUnixSeconds = 978307200

// Messages stores Apple reference-date seconds on older systems and nanoseconds
// on newer systems. This explicitly bounded policy supports contemporary data:
// positive integer seconds through 2200, or integer nanoseconds >= 1000 seconds
// after the 2001 epoch through 2200. This is magnitude-based unit inference,
// not validation of arbitrary units. Values outside these ranges stay unknown.
// Both branches produce integer ns without overflowing SQLite's signed int64.
const messageDateSQL = `CASE WHEN typeof(m.date)='integer' THEN
    CASE WHEN m.date BETWEEN 1 AND 6279811200 THEN m.date*1000000000
         WHEN m.date BETWEEN 1000000000000 AND 6279811200000000000 THEN m.date END
    END`

// ListMessages reads one recent page, choosing by normalized timestamp and rowid
// and returning chronological order. Unknown dates follow known dates. No rich
// body or attachment bytes are selected. The read transaction ends before return.
func (r *Reader) ListMessages(ctx context.Context, chatID string, limit int) ([]domain.Message, error) {
	if err := domain.ValidateMessageQuery(chatID, limit); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin history read: %w", err)
	}
	defer tx.Rollback()
	schema, err := inspectSchema(ctx, tx)
	if err != nil {
		return nil, err
	}
	if len(schema.Missing) != 0 {
		return nil, ErrUnsupportedSchema
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM chat WHERE ROWID=?)", chatID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check history chat: %w", err)
	}
	if !exists {
		return nil, domain.ErrConversationNotFound
	}
	columns, err := columnNames(ctx, tx, "message")
	if err != nil {
		return nil, err
	}
	attachments, rich := "0", "0"
	if columns["cache_has_attachments"] {
		attachments = "COALESCE(m.cache_has_attachments=1,0)"
	}
	if columns["attributedbody"] {
		rich = "COALESCE(length(m.attributedBody)>0,0)"
	}
	// Fixed SQL expressions only; chat and limit stay bound. IN deduplicates join
	// links and uses message rowids. Limit the page before fetching body fields.
	query := `WITH recent AS (
        SELECT m.ROWID AS id, ` + messageDateSQL + ` AS sent_ns
        FROM message m
        WHERE m.ROWID IN(SELECT j.message_id FROM chat_message_join j WHERE j.chat_id=?)
        ORDER BY sent_ns IS NULL, sent_ns DESC, id DESC LIMIT ?
    ) SELECT m.ROWID,r.sent_ns,COALESCE(m.is_from_me=1,0),
        CASE WHEN typeof(h.id)='text' THEN substr(trim(h.id),1,320) ELSE '' END,
        CASE WHEN typeof(m.text)='text' THEN substr(m.text,1,` + strconv.Itoa(domain.MaxMessageTextRunes+1) + `) END,
        ` + attachments + `, ` + rich + `
      FROM recent r JOIN message m ON m.ROWID=r.id LEFT JOIN handle h ON h.ROWID=m.handle_id
      ORDER BY r.sent_ns IS NULL, r.sent_ns ASC, r.id ASC`
	rows, err := tx.QueryContext(ctx, query, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	messages := make([]domain.Message, 0, limit)
	for rows.Next() {
		var message domain.Message
		var id int64
		var sent sql.NullInt64
		var text sql.NullString
		if err := rows.Scan(&id, &sent, &message.FromMe, &message.Sender, &text, &message.HasAttachments, &message.HasRichContent); err != nil {
			rows.Close()
			return nil, fmt.Errorf("decode history: %w", err)
		}
		message.ID = strconv.FormatInt(id, 10)
		if sent.Valid {
			message.SentAt = time.Unix(appleEpochUnixSeconds+sent.Int64/1e9, sent.Int64%1e9).UTC()
		}
		runes := []rune(text.String)
		if len(runes) > domain.MaxMessageTextRunes {
			message.TextTruncated = true
			runes = runes[:domain.MaxMessageTextRunes]
		}
		message.Text = string(runes)
		// U+FFFC marks embedded content; never render it as apparently blank text.
		if strings.ContainsRune(message.Text, '\uFFFC') {
			message.HasRichContent = true
			message.HasEmbeddedContent = true
			message.Text = strings.ReplaceAll(message.Text, "\uFFFC", "")
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate history: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close history rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("finish history read: %w", err)
	}
	return messages, nil
}
