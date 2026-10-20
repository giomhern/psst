package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// MaxSendTextRunes bounds one Personal text send. Attachments are unsupported.
const MaxSendTextRunes = 4000

// SendStatus describes automation acceptance, never recipient delivery.
type SendStatus uint8

const (
	// SendNotRequested means the adapter knows no send command was accepted.
	SendNotRequested SendStatus = iota
	// SendRequested means Messages accepted the automation command.
	SendRequested
	// SendUnknown means the command may have taken effect. Do not retry blindly.
	SendUnknown
)

// SendResult deliberately contains no body, recipient, or automation payload.
type SendResult struct{ Status SendStatus }

// Safe errors distinguish user-actionable Personal send outcomes.
var (
	ErrSendUnavailable  = errors.New("could not request a Messages send")
	ErrAutomationDenied = errors.New("automation permission for Messages was denied")
	ErrSendUnknown      = errors.New("send outcome is unknown; check Messages before sending again")
)

// ValidateSendText rejects empty, oversized or invalid argv text without echoing it.
func ValidateSendText(text string) error {
	if len(text) > MaxSendTextRunes*utf8.UTFMax || !utf8.ValidString(text) || utf8.RuneCountInString(text) > MaxSendTextRunes {
		return errors.New("message must contain at most 4000 valid Unicode characters")
	}
	if strings.TrimSpace(text) == "" || strings.ContainsRune(text, 0) {
		return errors.New("message must contain text and cannot contain a NUL character")
	}
	return nil
}
