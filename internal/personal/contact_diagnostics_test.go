package personal

import (
	"context"
	"errors"
	"testing"

	"github.com/giomhern/psst/internal/domain"
	"github.com/giomhern/psst/internal/personal/contacts"
)

func TestContactDiagnosticsDistinguishFallbackReasons(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resolver nameResolver
		status   string
		matched  int
	}{
		{"matched", &fakeNames{names: map[string]string{"a@example.test": "Alex"}}, "completed", 1},
		{"partial names", &fakeNames{names: map[string]string{"a@example.test": "Alex"}, err: &contacts.LookupError{Reason: "invalid_predicate"}}, "invalid_predicate", 1},
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

type detailedFakeNames struct {
	fakeNames
	counts contacts.MatchCounts
}

func (f *detailedFakeNames) LookupDetails(ctx context.Context, handles []string) (map[string]string, contacts.MatchCounts, error) {
	names, err := f.Lookup(ctx, handles)
	return names, f.counts, err
}

func TestContactDiagnosticsUseDetailedLookupOnce(t *testing.T) {
	resolver := &detailedFakeNames{counts: contacts.MatchCounts{Reported: true, Missing: 1, Ambiguous: 1}}
	report := diagnoseContactNames(context.Background(), []domain.Conversation{{Participants: []string{"a@example.test", "b@example.test"}}}, resolver)
	if !resolver.called || report.Outcomes != resolver.counts || report.Lookup != "completed" {
		t.Fatalf("detailed report = %+v", report)
	}
}
