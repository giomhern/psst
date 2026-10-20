package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/giomhern/psst/internal/domain"
	"github.com/giomhern/psst/internal/tui"
)

func TestUIHelpAndValidationNeverLaunchOrOpenData(t *testing.T) {
	for _, args := range [][]string{
		{"personal", "ui", "--help"}, {"personal", "ui", "extra"},
		{"personal", "ui", "--limit", "0"}, {"personal", "ui", "--limit", "101"},
	} {
		var out bytes.Buffer
		err := execute(context.Background(), args, &out, dependencies{
			launchUI: func(context.Context, io.Writer, tui.Source) error {
				t.Fatal("unexpected UI launch")
				return nil
			},
			openReader: func(context.Context) (conversationReader, error) {
				t.Fatal("unexpected Personal access")
				return nil, nil
			},
		})
		wantError := args[len(args)-1] != "--help"
		if (err != nil) != wantError {
			t.Fatalf("args %v: error = %v", args, err)
		}
	}
}

func TestUIUsesReaderAndClosesAfterLoad(t *testing.T) {
	reader := &stubReader{rows: []domain.Conversation{{ID: "1", Title: "Synthetic"}}}
	var out bytes.Buffer
	err := execute(context.Background(), []string{"personal", "ui", "--limit", "25"}, &out, dependencies{
		openReader: func(context.Context) (conversationReader, error) { return reader, nil },
		launchUI: func(ctx context.Context, _ io.Writer, source tui.Source) error {
			rows, err := source.Chats(ctx)
			if len(rows) != 1 || !reader.closed || reader.limit != 25 {
				t.Fatal("UI loader did not own and close its reader")
			}
			return err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	reader = &stubReader{listErr: context.Canceled, closeErr: errors.New("safe close failure")}
	_, err = loadChats(context.Background(), func(context.Context) (conversationReader, error) { return reader, nil }, 50)
	if !reader.closed || !errors.Is(err, context.Canceled) || !errors.Is(err, reader.closeErr) {
		t.Fatalf("load cleanup lost errors: %v", err)
	}
}

func TestUIHistoryLoaderClosesAndPropagatesFailures(t *testing.T) {
	for _, fail := range []bool{false, true} {
		reader := &stubMessages{rows: []domain.Message{{Text: "Synthetic"}}}
		if fail {
			reader.listErr = context.Canceled
			reader.closeErr = errors.New("safe close failure")
		}
		var out bytes.Buffer
		err := execute(context.Background(), []string{"personal", "ui"}, &out, dependencies{
			openMessages: func(context.Context) (messageReader, error) { return reader, nil },
			launchUI: func(ctx context.Context, _ io.Writer, source tui.Source) error {
				page, err := source.Messages(ctx, "7", 20, nil)
				if !reader.closed || reader.chatID != "7" || reader.limit != 20 || reader.before != nil {
					t.Fatal("incorrect history query or unclosed reader")
				}
				if !fail && (len(page.Messages) != 1 || err != nil) {
					t.Fatal("history not returned")
				}
				return err
			},
		})
		if fail && (!errors.Is(err, context.Canceled) || !errors.Is(err, reader.closeErr)) {
			t.Fatalf("lost read/close errors: %v", err)
		}
	}
}

func TestUIForwardsOlderHistoryQuery(t *testing.T) {
	reader := &stubMessages{}
	cursor := &domain.MessageCursor{ChatID: "7", MessageID: 42}
	var out bytes.Buffer
	err := execute(context.Background(), []string{"personal", "ui"}, &out, dependencies{
		openMessages: func(context.Context) (messageReader, error) { return reader, nil },
		launchUI: func(ctx context.Context, _ io.Writer, source tui.Source) error {
			_, err := source.Messages(ctx, "7", 3, cursor)
			return err
		},
	})
	if err != nil || !reader.closed || reader.limit != 3 || reader.before != cursor || reader.chatID != "7" {
		t.Fatalf("older query not forwarded: %v", err)
	}
}
