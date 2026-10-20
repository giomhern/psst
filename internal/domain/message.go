package domain

import (
	"errors"
	"strconv"
	"time"
)

// ErrConversationNotFound means the requested local chat does not exist.
var ErrConversationNotFound = errors.New("local chat not found")

// MaxMessageLimit bounds a single recent-history request.
const MaxMessageLimit = 100

// MaxMessageTextRunes bounds text retained for one history row.
const MaxMessageTextRunes = 4000

// Message is a local history row, not a delivery receipt. SentAt is UTC; its
// zero value means the stored timestamp is absent or unsupported. Sender is a
// local identifier, not a verified identity or send destination.
type Message struct {
	ID     string
	SentAt time.Time
	FromMe bool
	Sender string
	// SenderName is an optional local Contacts label; Sender remains the identifier.
	SenderName         string
	Text               string
	TextTruncated      bool
	HasAttachments     bool
	HasRichContent     bool
	HasEmbeddedContent bool
}

// ValidateMessageQuery accepts the positive decimal IDs printed by personal chats
// and a bounded page size. It never includes supplied identifiers in errors.
func ValidateMessageQuery(chatID string, limit int) error {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != chatID {
		return errors.New("chat ID must be a positive decimal ID from personal chats")
	}
	if limit < 1 || limit > MaxMessageLimit {
		return errors.New("message limit must be between 1 and 100")
	}
	return nil
}

// ErrInvalidMessageCursor means a history boundary is malformed or belongs to
// another chat. Its message never includes cursor contents.
var ErrInvalidMessageCursor = errors.New("invalid history cursor; use the --before token from the same chat")

// MessageCursor is a position in a local chat's history, not an authorization
// credential. Zero SentAt denotes the unknown-time segment; MessageID breaks ties.
type MessageCursor struct {
	ChatID    string
	MessageID int64
	SentAt    time.Time
}

// MessagePage contains one bounded page. NextBefore is nil at the end of history.
type MessagePage struct {
	Messages   []Message
	NextBefore *MessageCursor
}

// ValidateMessagePage checks the chat, page size and optional typed boundary.
func ValidateMessagePage(chatID string, limit int, before *MessageCursor) error {
	if err := ValidateMessageQuery(chatID, limit); err != nil {
		return err
	}
	if before == nil {
		return nil
	}
	if before.ChatID != chatID || before.MessageID < 1 {
		return ErrInvalidMessageCursor
	}
	if !before.SentAt.IsZero() {
		first := time.Date(2001, 1, 1, 0, 0, 0, 1, time.UTC)
		last := time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)
		if before.SentAt.Before(first) || before.SentAt.After(last) {
			return ErrInvalidMessageCursor
		}
	}
	return nil
}
