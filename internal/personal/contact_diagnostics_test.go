package personal

import (
	"context"
	"errors"
	"testing"

	"github.com/giomhern/psst/internal/domain"
)

func TestContactDiagnosticsDistinguishFallbackReasons(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resolver nameResolver
		status   string
		matched  int
	}{
		{"matched", &fakeNames{names: map[string]string{"a@example.test": "Alex"}}, "completed", 1},
		{"no matches", &fakeNames{}, "completed", 0},
		{"timeout", &fakeNames{err: context.DeadlineExceeded}, "timeout", 0},
		{"unavailable", &fakeNames{err: errors.New("private helper details")}, "unavailable", 0},
		{"disabled", nil, "skipped", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := diagnoseContactNames(context.Background(), []domain.Conversation{
				{Title: "Private group name", ExplicitTitle: true, Participants: []string{"ignored"}},
				{Participants: []string{"a@example.test", "b@example.test"}},
				{Participants: []string{"a@example.test"}},
			}, tc.resolver)
			want := ContactDiagnostics{Chats: 3, NamedChats: 1, Identifiers: 2, Matched: tc.matched, Lookup: tc.status}
			if report != want {
				t.Fatalf("report = %+v, want %+v", report, want)
			}
		})
	}
	resolver := &fakeNames{}
	report := diagnoseContactNames(context.Background(), []domain.Conversation{{ExplicitTitle: true}}, resolver)
	if resolver.called || report.Lookup != "skipped" {
		t.Fatal("named chats unexpectedly queried Contacts")
	}
}
