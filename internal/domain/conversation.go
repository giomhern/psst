// Package domain defines values shared by application and adapter boundaries.
package domain

import "fmt"

// MaxConversationLimit bounds a single conversation listing.
const MaxConversationLimit = 100

// Conversation is a local chat summary. ID is scoped to the local database;
// Title is display text, not trusted terminal markup or a send destination.
type Conversation struct {
	ID    string
	Title string
}

// ValidateConversationLimit rejects unbounded or invalid listings.
func ValidateConversationLimit(limit int) error {
	if limit < 1 || limit > MaxConversationLimit {
		return fmt.Errorf("limit must be between 1 and %d", MaxConversationLimit)
	}
	return nil
}
