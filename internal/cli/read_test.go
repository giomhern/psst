package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/giomhern/psst/internal/domain"
)

type stubMessages struct {
	rows              []domain.Message
	listErr, closeErr error
	closed            bool
	chatID            string
	limit             int
	ctx               context.Context
	before            *domain.MessageCursor
	next              *domain.MessageCursor
}

func (r *stubMessages) ListMessagePage(ctx context.Context, id string, limit int, before *domain.MessageCursor) (domain.MessagePage, error) {
	r.ctx, r.chatID, r.limit = ctx, id, limit
	r.before = before
	return domain.MessagePage{Messages: r.rows, NextBefore: r.next}, r.listErr
}
func (r *stubMessages) Close() error { r.closed = true; return r.closeErr }

func TestReadValidationBeforeOpeningPersonalData(t *testing.T) {
	for _, args := range [][]string{
		{"personal", "read", "--help"}, {"personal", "read"}, {"personal", "read", "1", "extra"},
		{"personal", "read", "0"}, {"personal", "read", "private-invalid-id"},
		{"personal", "read", "1", "--limit", "0"}, {"personal", "read", "1", "--limit", "101"},
		{"personal", "read", "1", "--unknown"},
	} {
		var out bytes.Buffer
		err := execute(context.Background(), args, &out, dependencies{openMessages: func(context.Context) (messageReader, error) { t.Fatal("unexpected Personal access"); return nil, nil }})
		wantError := args[len(args)-1] != "--help"
		if (err != nil) != wantError {
			t.Fatalf("args %v: error = %v", args, err)
		}
		if err != nil && strings.Contains(err.Error(), "private-invalid-id") {
			t.Fatal("validation exposed supplied identifier")
		}
	}
}

func TestReadRendersUTCAndEscapesPrivateContent(t *testing.T) {
	reader := &stubMessages{rows: []domain.Message{
		{ID: "1", SentAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), FromMe: true, Text: "hello\n\t\x1b[31m\u202e猫"},
		{ID: "2", Sender: "sender\n\x1b[31m", HasAttachments: true},
		{ID: "3", HasRichContent: true},
		{ID: "4", Text: "bounded", TextTruncated: true, HasAttachments: true},
		{ID: "5"},
		{ID: "6", Text: "caption", HasEmbeddedContent: true, Sender: "alex@example.test", SenderName: "Alex\n\x1b[31m"},
	}}
	var out bytes.Buffer
	err := execute(context.Background(), []string{"personal", "read", "19", "--limit", "6"}, &out, dependencies{openMessages: func(context.Context) (messageReader, error) { return reader, nil }})
	if err != nil || !reader.closed || reader.chatID != "19" || reader.limit != 6 {
		t.Fatalf("read lifecycle failed: %v", err)
	}
	if deadline, ok := reader.ctx.Deadline(); !ok || time.Until(deadline) > 8*time.Second {
		t.Fatal("missing read deadline")
	}
	text := out.String()
	for _, want := range []string{"2026-01-01T00:00:00Z", `"Me"`, `hello\n\t\x1b[31m\u202e猫`, "[attachment not shown]", "[rich content not decoded]", "[text truncated]", "[unknown time]", "[unknown sender]", "[no supported text]", "[embedded content not decoded]", `Alex\n\x1b[31m`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
	if strings.ContainsAny(text, "\x1b\u202e") {
		t.Fatal("terminal controls were not escaped")
	}
}

func TestReadCleanupErrorsAndCancellation(t *testing.T) {
	queryErr, closeErr := errors.New("query failed"), errors.New("close failed")
	reader := &stubMessages{listErr: queryErr, closeErr: closeErr}
	var out bytes.Buffer
	err := readMessagePage(context.Background(), &out, func(context.Context) (messageReader, error) { return reader, nil }, "1", 20, nil)
	if !reader.closed || !errors.Is(err, queryErr) || !errors.Is(err, closeErr) || out.Len() != 0 {
		t.Fatalf("read errors = %v", err)
	}
	reader = &stubMessages{}
	err = readMessagePage(context.Background(), &out, func(context.Context) (messageReader, error) { return reader, nil }, "1", 20, nil)
	if err != nil || !reader.closed || !strings.Contains(out.String(), "No locally synchronized messages") {
		t.Fatalf("empty read = %v", err)
	}
	reader = &stubMessages{rows: []domain.Message{{ID: "1", Text: "synthetic"}}}
	outputErr := errors.New("output failed")
	err = readMessagePage(context.Background(), failingWriter{outputErr}, func(context.Context) (messageReader, error) { return reader, nil }, "1", 20, nil)
	if !reader.closed || !errors.Is(err, outputErr) {
		t.Fatalf("output failure = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = execute(ctx, []string{"personal", "read", "1"}, &out, dependencies{openMessages: func(ctx context.Context) (messageReader, error) { return nil, ctx.Err() }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}
