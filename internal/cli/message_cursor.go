package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"time"

	"github.com/giomhern/psst/internal/domain"
)

const maxMessageCursorBytes = 256

type messageCursorToken struct {
	Version   int        `json:"v"`
	ChatID    string     `json:"chat"`
	MessageID int64      `json:"id"`
	SentAt    *time.Time `json:"time"`
}

func encodeMessageCursor(cursor *domain.MessageCursor) (string, error) {
	if cursor == nil {
		return "", nil
	}
	if err := domain.ValidateMessagePage(cursor.ChatID, 1, cursor); err != nil {
		return "", domain.ErrInvalidMessageCursor
	}
	token := messageCursorToken{Version: 1, ChatID: cursor.ChatID, MessageID: cursor.MessageID}
	if !cursor.SentAt.IsZero() {
		timestamp := cursor.SentAt.UTC()
		token.SentAt = &timestamp
	}
	raw, err := json.Marshal(token)
	if err != nil {
		return "", domain.ErrInvalidMessageCursor
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	if len(encoded) > maxMessageCursorBytes {
		return "", domain.ErrInvalidMessageCursor
	}
	return encoded, nil
}

func decodeMessageCursor(raw, chatID string) (*domain.MessageCursor, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > maxMessageCursorBytes {
		return nil, domain.ErrInvalidMessageCursor
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil {
		return nil, domain.ErrInvalidMessageCursor
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var token messageCursorToken
	if err := decoder.Decode(&token); err != nil {
		return nil, domain.ErrInvalidMessageCursor
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, domain.ErrInvalidMessageCursor
	}
	if token.Version != 1 {
		return nil, domain.ErrInvalidMessageCursor
	}
	cursor := &domain.MessageCursor{ChatID: token.ChatID, MessageID: token.MessageID}
	if token.SentAt != nil {
		cursor.SentAt = token.SentAt.UTC()
		if cursor.SentAt.IsZero() {
			return nil, domain.ErrInvalidMessageCursor
		}
	}
	if err := domain.ValidateMessagePage(chatID, 1, cursor); err != nil {
		return nil, domain.ErrInvalidMessageCursor
	}
	return cursor, nil
}
