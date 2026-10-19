package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
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

// ListMessages returns the most recent page without a continuation boundary.
func (r *Reader) ListMessages(ctx context.Context, chatID string, limit int) ([]domain.Message, error) {
	page, err := r.ListMessagePage(ctx, chatID, limit, nil)
	return page.Messages, err
}

// ListMessagePage reads an exclusive keyset page. Dated rows precede unknown-time
// rows in traversal order; each returned page is chronological with unknowns last.
// The transaction ends before return. No cursor row needs to remain present.
func (r *Reader) ListMessagePage(ctx context.Context, chatID string, limit int, before *domain.MessageCursor) (domain.MessagePage, error) {
	if err := domain.ValidateMessagePage(chatID, limit, before); err != nil {
		return domain.MessagePage{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.MessagePage{}, fmt.Errorf("begin history read: %w", err)
	}
	defer tx.Rollback()
	schema, err := inspectSchema(ctx, tx)
	if err != nil {
		return domain.MessagePage{}, err
	}
	if len(schema.Missing) != 0 {
		return domain.MessagePage{}, ErrUnsupportedSchema
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM chat WHERE ROWID=?)", chatID).Scan(&exists); err != nil {
		return domain.MessagePage{}, fmt.Errorf("check history chat: %w", err)
	}
	if !exists {
		return domain.MessagePage{}, domain.ErrConversationNotFound
	}
	columns, err := columnNames(ctx, tx, "message")
	if err != nil {
		return domain.MessagePage{}, err
	}
	attachments, rich := "0", "0"
	if columns["cache_has_attachments"] {
		attachments = "COALESCE(m.cache_has_attachments=1,0)"
	}
	if columns["attributedbody"] {
		rich = "COALESCE(length(m.attributedBody)>0,0)"
	}
	boundary := "1"
	args := []any{chatID}
	if before != nil {
		if before.SentAt.IsZero() {
			boundary = "sent_ns IS NULL AND id < ?"
			args = append(args, before.MessageID)
		} else {
			ns := (before.SentAt.Unix()-appleEpochUnixSeconds)*1e9 + int64(before.SentAt.Nanosecond())
			boundary = "(sent_ns < ? OR (sent_ns = ? AND id < ?) OR sent_ns IS NULL)"
			args = append(args, ns, ns, before.MessageID)
		}
	}
	args = append(args, limit+1)
	// Fixed SQL expressions only; chat and limit stay bound. IN deduplicates join
	// links and uses message rowids. Limit the page before fetching body fields.
	query := `WITH candidates AS (
        SELECT m.ROWID AS id, ` + messageDateSQL + ` AS sent_ns
        FROM message m
        WHERE m.ROWID > 0 AND m.ROWID IN(SELECT j.message_id FROM chat_message_join j WHERE j.chat_id=?)
    ), recent AS (
        SELECT id,sent_ns FROM candidates WHERE ` + boundary + `
        ORDER BY sent_ns IS NULL, sent_ns DESC, id DESC LIMIT ?
    ) SELECT m.ROWID,r.sent_ns,COALESCE(m.is_from_me=1,0),
        CASE WHEN typeof(h.id)='text' THEN substr(trim(h.id),1,320) ELSE '' END,
        CASE WHEN typeof(m.text)='text' THEN substr(m.text,1,` + strconv.Itoa(domain.MaxMessageTextRunes+1) + `) END,
        ` + attachments + `, ` + rich + `
      FROM recent r JOIN message m ON m.ROWID=r.id LEFT JOIN handle h ON h.ROWID=m.handle_id
      ORDER BY r.sent_ns IS NULL, r.sent_ns DESC, r.id DESC`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return domain.MessagePage{}, fmt.Errorf("query history: %w", err)
	}
	messages := make([]domain.Message, 0, limit+1)
	for rows.Next() {
		var message domain.Message
		var id int64
		var sent sql.NullInt64
		var text sql.NullString
		if err := rows.Scan(&id, &sent, &message.FromMe, &message.Sender, &text, &message.HasAttachments, &message.HasRichContent); err != nil {
			rows.Close()
			return domain.MessagePage{}, fmt.Errorf("decode history: %w", err)
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
		return domain.MessagePage{}, fmt.Errorf("iterate history: %w", err)
	}
	if err := rows.Close(); err != nil {
		return domain.MessagePage{}, fmt.Errorf("close history rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.MessagePage{}, fmt.Errorf("finish history read: %w", err)
	}
	page := domain.MessagePage{}
	if len(messages) > limit {
		messages = messages[:limit]
		last := messages[len(messages)-1]
		// IDs were formatted directly from int64 database rowids above.
		id, err := strconv.ParseInt(last.ID, 10, 64)
		if err != nil {
			return domain.MessagePage{}, fmt.Errorf("decode history boundary: %w", err)
		}
		page.NextBefore = &domain.MessageCursor{ChatID: chatID, MessageID: id, SentAt: last.SentAt}
	}
	// Reverse each segment, preserving the existing unknown-times-last display.
	split := len(messages)
	for i, message := range messages {
		if message.SentAt.IsZero() {
			split = i
			break
		}
	}
	slices.Reverse(messages[:split])
	slices.Reverse(messages[split:])
	page.Messages = messages
	return page, nil
}
