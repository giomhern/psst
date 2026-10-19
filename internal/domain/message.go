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
	ID                 string
	SentAt             time.Time
	FromMe             bool
	Sender             string
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
