package personal

import (
	"context"
	"errors"

	"github.com/giomhern/psst/internal/domain"
)

// ContactDiagnostics contains counts and fixed lookup states, never identifiers
// or names. Counts describe the most recent ten chats, matching the default list.
type ContactDiagnostics struct {
	Chats       int
	NamedChats  int
	Identifiers int
	Matched     int
	Lookup      string
}

// DiagnoseContacts reads recent participant metadata and attempts the same lookup
// as chat listing. It never requests authorization or returns personal strings.
func DiagnoseContacts(ctx context.Context) (report ContactDiagnostics, err error) {
	reader, err := Open(ctx)
	if err != nil {
		return report, err
	}
	defer func() { err = errors.Join(err, reader.Close()) }()
	chats, err := reader.store.ListConversations(ctx, 10)
	if err != nil {
		return report, conversationError(ctx, err)
	}
	report = diagnoseContactNames(ctx, chats, reader.names)
	if ctx.Err() != nil {
		return ContactDiagnostics{}, conversationError(ctx, ctx.Err())
	}
	return report, nil
}

func diagnoseContactNames(ctx context.Context, chats []domain.Conversation, resolver nameResolver) ContactDiagnostics {
	report := ContactDiagnostics{Chats: len(chats)}
	identifiers := make(map[string]bool)
	for _, chat := range chats {
		if chat.ExplicitTitle {
			report.NamedChats++
			continue
		}
		for _, handle := range chat.Participants {
			identifiers[handle] = true
		}
	}
	names, status := lookupNames(ctx, chats, resolver)
	report.Identifiers, report.Lookup = len(identifiers), status
	for handle := range identifiers {
		if names[handle] != "" {
			report.Matched++
		}
	}
	return report
}
