package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/giomhern/psst/internal/domain"
	"github.com/giomhern/psst/internal/personal"
)

type stubReader struct {
	rows              []domain.Conversation
	listErr, closeErr error
	limit             int
	closed            bool
	deadline          time.Time
	hasDeadline       bool
}

func (r *stubReader) ListConversations(ctx context.Context, limit int) ([]domain.Conversation, error) {
	r.limit = limit
	r.deadline, r.hasDeadline = ctx.Deadline()
	return r.rows, r.listErr
}
func (r *stubReader) Close() error { r.closed = true; return r.closeErr }

func TestCobraHelpAndInvalidArgumentsDoNotOpenDatabase(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		fails bool
	}{
		{"root help", nil, false}, {"explicit help", []string{"--help"}, false},
		{"personal help", []string{"personal"}, false}, {"chat help", []string{"personal", "chats", "--help"}, false},
		{"help command", []string{"help", "personal", "chats"}, false},
		{"unknown command", []string{"personal", "unknown"}, true},
		{"zero limit", []string{"personal", "chats", "--limit", "0"}, true},
		{"negative limit", []string{"personal", "chats", "--limit=-1"}, true},
		{"oversized limit", []string{"personal", "chats", "--limit", "101"}, true},
		{"invalid integer", []string{"personal", "chats", "--limit", "oops"}, true},
		{"extra argument", []string{"personal", "chats", "extra"}, true},
		{"unknown flag", []string{"personal", "chats", "--unknown"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			err := execute(context.Background(), tc.args, &out, dependencies{
				checkDatabase: func(context.Context) personal.DatabaseCheck {
					t.Fatal("unexpected doctor probe")
					return personal.DatabaseCheck{}
				},
				openReader: func(context.Context) (conversationReader, error) {
					t.Fatal("unexpected database access")
					return nil, nil
				},
			})
			if (err != nil) != tc.fails {
				t.Fatalf("error = %v; expected failure = %v", err, tc.fails)
			}
			if !tc.fails && !strings.Contains(out.String(), "Usage:") {
				t.Fatalf("missing help: %s", out.String())
			}
			if tc.fails && out.Len() != 0 {
				t.Fatalf("error/usage printed twice: %s", out.String())
			}
		})
	}
}

func TestChatsListsAndEscapesTitles(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		limit int
	}{
		{[]string{"personal", "chats"}, 10}, {[]string{"personal", "chats", "--limit", "2"}, 2},
	} {
		reader := &stubReader{rows: []domain.Conversation{{ID: "7", Title: "猫 group"}, {ID: "8", Title: "line\n\t\x1b[31m\u202e"}}}
		var out bytes.Buffer
		err := execute(context.Background(), tc.args, &out, dependencies{openReader: func(context.Context) (conversationReader, error) { return reader, nil }})
		if err != nil {
			t.Fatal(err)
		}
		if reader.limit != tc.limit || !reader.closed {
			t.Fatalf("limit = %d, closed = %v", reader.limit, reader.closed)
		}
		if !reader.hasDeadline || time.Until(reader.deadline) > 3*time.Second {
			t.Fatal("missing query deadline")
		}
		text := out.String()
		if !strings.Contains(text, "猫 group") || !strings.Contains(text, `line\n\t\x1b[31m\u202e`) {
			t.Fatalf("titles not escaped: %q", text)
		}
		if strings.ContainsAny(text, "\x1b\u202e") {
			t.Fatal("raw terminal controls leaked")
		}
	}
}

func TestChatsEmptyErrorsAndCleanup(t *testing.T) {
	outputErr := errors.New("output unavailable")
	listErr := errors.New("safe listing failure")
	closeErr := errors.New("safe close failure")
	for _, tc := range []struct {
		name       string
		reader     stubReader
		failOutput bool
		want       error
	}{
		{name: "empty"},
		{name: "list failure", reader: stubReader{listErr: listErr}, want: listErr},
		{name: "close failure", reader: stubReader{closeErr: closeErr}, want: closeErr},
		{name: "output failure", reader: stubReader{rows: []domain.Conversation{{ID: "1", Title: "synthetic"}}}, failOutput: true, want: outputErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			deps := dependencies{openReader: func(context.Context) (conversationReader, error) { return &tc.reader, nil }}
			var err error
			if tc.failOutput {
				err = execute(context.Background(), []string{"personal", "chats"}, failingWriter{outputErr}, deps)
			} else {
				err = execute(context.Background(), []string{"personal", "chats"}, &out, deps)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if !tc.reader.closed {
				t.Fatal("reader not closed")
			}
			if tc.name == "empty" && !strings.Contains(out.String(), "No local conversations") {
				t.Fatalf("empty output = %q", out.String())
			}
			if tc.name == "list failure" && out.Len() != 0 {
				t.Fatal("listing failure printed partial data")
			}
		})
	}
}

func TestChatsPropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := &stubReader{listErr: context.Canceled}
	var out bytes.Buffer
	err := execute(ctx, []string{"personal", "chats"}, &out, dependencies{openReader: func(ctx context.Context) (conversationReader, error) {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("cancellation was lost")
		}
		return reader, nil
	}})
	if !errors.Is(err, context.Canceled) || !reader.closed {
		t.Fatalf("error = %v, closed = %v", err, reader.closed)
	}
}
