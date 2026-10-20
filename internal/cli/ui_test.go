package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/giomhern/psst/internal/domain"
)

func TestUIHelpAndValidationNeverLaunchOrOpenData(t *testing.T) {
	for _, args := range [][]string{
		{"personal", "ui", "--help"}, {"personal", "ui", "extra"},
		{"personal", "ui", "--limit", "0"}, {"personal", "ui", "--limit", "101"},
	} {
		var out bytes.Buffer
		err := execute(context.Background(), args, &out, dependencies{
			launchUI: func(context.Context, io.Writer, func(context.Context) ([]domain.Conversation, error)) error {
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
		launchUI: func(ctx context.Context, _ io.Writer, load func(context.Context) ([]domain.Conversation, error)) error {
			rows, err := load(ctx)
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
