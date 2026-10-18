package personal

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/giomhern/psst/internal/domain"
	"github.com/giomhern/psst/internal/personal/contacts"
)

// A native lookup can exceed 1.5 seconds even for the default ten chats.
// Keep it bounded while allowing framework startup and a complete fallback scan.
const contactLookupTimeout = 5 * time.Second

// This consumer-owned interface allows synthetic name resolution in tests.
type nameResolver interface {
	Lookup(context.Context, []string) (map[string]string, error)
}

// detailedNameResolver is an optional capability used for aggregate diagnostics.
type detailedNameResolver interface {
	LookupDetails(context.Context, []string) (map[string]string, contacts.MatchCounts, error)
}

// ContactsAccess checks or explicitly requests Contacts permission without
// opening Messages. The native helper returns only a known status string.
func ContactsAccess(ctx context.Context, authorize bool) (string, error) {
	status, err := contacts.New().Access(ctx, authorize)
	return string(status), err
}

func resolveTitles(ctx context.Context, chats []domain.Conversation, resolver nameResolver) ([]domain.Conversation, error) {
	names, _, _ := lookupNames(ctx, chats, resolver)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	for i := range chats {
		chat := &chats[i]
		if chat.ExplicitTitle || len(chat.Participants) == 0 {
			continue
		}
		parts := make([]string, 0, len(chat.Participants)+1)
		for _, handle := range chat.Participants {
			name := strings.TrimSpace(names[strings.TrimSpace(handle)])
			if name == "" {
				name = handle
			}
			parts = append(parts, name)
		}
		if chat.MoreParticipants {
			parts = append(parts, "…")
		}
		title := []rune(strings.Join(parts, ", "))
		if len(title) > 200 {
			title = title[:200]
		}
		chat.Title = string(title)
	}
	return chats, nil
}

// Both listing and diagnostics use the same optional lookup budget and fallbacks.
func lookupNames(ctx context.Context, chats []domain.Conversation, resolver nameResolver) (map[string]string, string, contacts.MatchCounts) {
	var handles []string
	for _, chat := range chats {
		if !chat.ExplicitTitle {
			handles = append(handles, chat.Participants...)
		}
	}
	var names map[string]string
	status := "skipped"
	counts := contacts.MatchCounts{}
	if resolver != nil && len(handles) != 0 {
		lookupCtx, cancel := context.WithTimeout(ctx, contactLookupTimeout)
		var err error
		if detailed, ok := resolver.(detailedNameResolver); ok {
			names, counts, err = detailed.LookupDetails(lookupCtx, handles)
		} else {
			names, err = resolver.Lookup(lookupCtx, handles)
		}
		cancel()
		status = "completed"
		if err != nil {
			status = "unavailable"
			var lookupErr *contacts.LookupError
			if errors.As(err, &lookupErr) {
				status = lookupErr.Reason
			} else {
				names = nil
			}
			if errors.Is(err, context.DeadlineExceeded) {
				status = "timeout"
			}
		}
	}
	return names, status, counts
}
