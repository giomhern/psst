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
type SendResult struct {
	Status  SendStatus
	Receipt SendReceipt
}

// DeliveryStatus is evidence reported by the local database, not an independent
// network acknowledgement. Missing fields and ambiguous matches stay unknown.
type DeliveryStatus uint8

const (
	DeliveryUnknown DeliveryStatus = iota
	DeliveryPending
	DeliverySent
	DeliveryDelivered
	DeliveryFailed
)

// SendReceipt identifies a conservatively matched outgoing row. Never log GUIDs.
// A GUID is absent when matching is ambiguous or observation is unavailable.
type SendReceipt struct {
	GUID   string
	Status DeliveryStatus
}

// Safe errors distinguish user-actionable Personal send outcomes.
var (
	ErrReceiptUnavailable = errors.New("message status unavailable")
	ErrSendUnavailable    = errors.New("could not request a Messages send")
	ErrAutomationDenied   = errors.New("automation permission for Messages was denied")
	ErrSendUnknown        = errors.New("send outcome is unknown; check Messages before sending again")
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
