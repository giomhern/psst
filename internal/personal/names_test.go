package personal

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/giomhern/psst/internal/domain"
	"github.com/giomhern/psst/internal/personal/contacts"
)

type fakeNames struct {
	names     map[string]string
	err       error
	requested []string
	called    bool
}

func (f *fakeNames) Lookup(ctx context.Context, handles []string) (map[string]string, error) {
	f.called = true
	f.requested = append([]string(nil), handles...)
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Second {
		panic("missing lookup deadline")
	}
	return f.names, f.err
}

func TestContactTitlePrecedenceAndFallback(t *testing.T) {
	chats := []domain.Conversation{
		{ID: "1", Title: "Named group", ExplicitTitle: true, Participants: []string{"do-not-lookup"}},
		{ID: "2", Title: "old-guid", Participants: []string{"+15550000001"}},
		{ID: "3", Title: "old-group", Participants: []string{"a@example.test", "+15550000002"}, MoreParticipants: true},
		{ID: "4", Title: "old-empty"},
	}
	resolver := &fakeNames{names: map[string]string{"+15550000001": "Alex", "a@example.test": "Sam"}}
	got, err := resolveTitles(context.Background(), chats, resolver)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Named group", "Alex", "Sam, +15550000002, …", "old-empty"}
	for i, chat := range got {
		if chat.Title != want[i] {
			t.Fatalf("chat %d title = %q, want %q", i, chat.Title, want[i])
		}
	}
	if !reflect.DeepEqual(resolver.requested, []string{"+15550000001", "a@example.test", "+15550000002"}) {
		t.Fatalf("unexpected handles = %v", resolver.requested)
	}
}

func TestContactFailuresRemainUsable(t *testing.T) {
	for _, resolver := range []nameResolver{nil, &fakeNames{err: errors.New("permission denied")}, &fakeNames{err: context.DeadlineExceeded}} {
		got, err := resolveTitles(context.Background(), []domain.Conversation{{Title: "guid", Participants: []string{"+15550000001"}}}, resolver)
		if err != nil || got[0].Title != "+15550000001" {
			t.Fatalf("fallback = %v, %v", got, err)
		}
	}
	resolver := &fakeNames{}
	_, err := resolveTitles(context.Background(), []domain.Conversation{{Title: "named", ExplicitTitle: true}}, resolver)
	if err != nil || resolver.called {
		t.Fatal("named chat unnecessarily queried Contacts")
	}
}

func TestResolvedTitlesRemainBoundedAndCancelable(t *testing.T) {
	resolver := &fakeNames{names: map[string]string{"a": strings.Repeat("猫", 200), "b": "Second"}}
	got, err := resolveTitles(context.Background(), []domain.Conversation{{Participants: []string{"a", "b"}}}, resolver)
	if err != nil || utf8.RuneCountInString(got[0].Title) != 200 {
		t.Fatalf("bounded title = %v, %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolveTitles(ctx, []domain.Conversation{{Title: "original"}}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}

func TestContactPartialFailurePreservesSuccessfulTitles(t *testing.T) {
	resolver := &fakeNames{names: map[string]string{"a@example.test": "Alex"}, err: &contacts.LookupError{Reason: "invalid_predicate"}}
	chats, err := resolveTitles(context.Background(), []domain.Conversation{
		{Participants: []string{"a@example.test"}}, {Participants: []string{"b@example.test"}},
	}, resolver)
	if err != nil || chats[0].Title != "Alex" || chats[1].Title != "b@example.test" {
		t.Fatalf("partial titles = %v, %v", chats, err)
	}
}

type deadlineNames struct{ deadline time.Time }

func (r *deadlineNames) Lookup(ctx context.Context, _ []string) (map[string]string, error) {
	r.deadline, _ = ctx.Deadline()
	return map[string]string{"a@example.test": "Alex"}, nil
}

func TestContactLookupBudgetAllowsNativeStartupAndHonorsParent(t *testing.T) {
	resolver := &deadlineNames{}
	chats := []domain.Conversation{{Participants: []string{"a@example.test"}}}
	started := time.Now()
	got, err := resolveTitles(context.Background(), chats, resolver)
	budget := resolver.deadline.Sub(started)
	if err != nil || got[0].Title != "Alex" || budget < 4*time.Second || budget > 5*time.Second+time.Second {
		t.Fatalf("default lookup budget = %v, error = %v", budget, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	want, _ := ctx.Deadline()
	_, err = resolveTitles(ctx, chats, resolver)
	if err != nil || !resolver.deadline.Equal(want) {
		t.Fatalf("lookup did not preserve the earlier parent deadline: %v", err)
	}
}

func TestSenderNamesPreserveIdentifiersAndDeduplicate(t *testing.T) {
	messages := []domain.Message{
		{Sender: "alex@example.test"}, {Sender: "alex@example.test"},
		{Sender: "unknown@example.test"}, {FromMe: true, Sender: "self@example.test"}, {},
	}
	resolver := &fakeNames{names: map[string]string{"alex@example.test": " Alex "}}
	if err := resolveSenders(context.Background(), messages, resolver); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolver.requested, []string{"alex@example.test", "unknown@example.test"}) {
		t.Fatal("lookup was not bounded to distinct incoming senders")
	}
	for i, message := range messages {
		want := ""
		if i < 2 {
			want = "Alex"
		}
		if message.SenderName != want {
			t.Fatalf("message %d: name = %q", i, message.SenderName)
		}
	}
	if messages[0].Sender != "alex@example.test" {
		t.Fatal("name replaced identifier")
	}
}

func TestSenderNameFallbackBoundAndCancellation(t *testing.T) {
	for _, resolver := range []nameResolver{nil, &fakeNames{err: errors.New("unavailable")}, &fakeNames{err: context.DeadlineExceeded}} {
		messages := []domain.Message{{Sender: "alex@example.test"}}
		if err := resolveSenders(context.Background(), messages, resolver); err != nil || messages[0].SenderName != "" {
			t.Fatal("optional lookup failure prevented history")
		}
	}
	messages := []domain.Message{{Sender: "a"}}
	resolver := &fakeNames{names: map[string]string{"a": strings.Repeat("猫", 300)}}
	if err := resolveSenders(context.Background(), messages, resolver); err != nil || utf8.RuneCountInString(messages[0].SenderName) != 200 {
		t.Fatal("sender label was not bounded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := resolveSenders(ctx, messages, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("parent cancellation lost")
	}
	resolver = &fakeNames{}
	if err := resolveSenders(context.Background(), []domain.Message{{FromMe: true, Sender: "a"}, {}}, resolver); err != nil || resolver.called {
		t.Fatal("unnecessary sender lookup")
	}
}
