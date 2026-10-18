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
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 1500*time.Millisecond {
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
