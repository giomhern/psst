package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/giomhern/psst/internal/domain"
)

func TestMessageCursorRoundTripAndBounds(t *testing.T) {
	for _, timestamp := range []time.Time{{}, time.Date(2026, 1, 1, 0, 0, 0, 123456789, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 123, time.FixedZone("synthetic", 19800)), time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)} {
		cursor := &domain.MessageCursor{ChatID: "9223372036854775807", MessageID: 9223372036854775807, SentAt: timestamp}
		token, err := encodeMessageCursor(cursor)
		if err != nil || len(token) > 256 || strings.ContainsAny(token, "+/= \n") {
			t.Fatalf("encode failed: %v", err)
		}
		decoded, err := decodeMessageCursor(token, cursor.ChatID)
		if err != nil || decoded.ChatID != cursor.ChatID || decoded.MessageID != cursor.MessageID || !decoded.SentAt.Equal(timestamp) {
			t.Fatalf("round trip failed: %+v, %v", decoded, err)
		}
	}
	token, err := encodeMessageCursor(nil)
	if token != "" || err != nil {
		t.Fatal("nil cursor did not represent end")
	}
}

func TestMessageCursorRejectsInvalidTokensWithoutPersonalAccess(t *testing.T) {
	tokens := []string{"private-invalid-token", strings.Repeat("x", 257), "="}
	for _, raw := range []string{
		`null`, `{"v":2,"chat":"1","id":1,"time":null}`,
		`{"v":1,"chat":"2","id":1,"time":null}`,
		`{"v":1,"chat":"1","id":0,"time":null}`,
		`{"v":1,"chat":"1","id":9223372036854775808,"time":null}`,
		`{"v":1,"chat":"1","id":1,"time":"2300-01-01T00:00:00Z"}`,
		`{"v":1,"chat":"1","id":1,"time":"0001-01-01T00:00:00Z"}`,
		`{"v":1,"chat":"1","id":1,"time":null,"private-field":1}`,
		`{"v":1,"chat":"1","id":1,"time":null} {}`,
	} {
		tokens = append(tokens, base64.RawURLEncoding.EncodeToString([]byte(raw)))
	}
	tokens = append(tokens, "") // Explicit --before with no token must not restart history.
	for _, token := range tokens {
		var out bytes.Buffer
		err := execute(context.Background(), []string{"personal", "read", "1", "--before", token}, &out, dependencies{
			openMessages: func(context.Context) (messageReader, error) {
				t.Fatal("invalid cursor opened Personal data")
				return nil, nil
			},
		})
		if !errors.Is(err, domain.ErrInvalidMessageCursor) || strings.Contains(err.Error(), "private-invalid-token") || out.Len() != 0 {
			t.Fatalf("unsafe validation: %v, %q", err, out.String())
		}
	}
}

func TestReadPrintsAndConsumesContinuation(t *testing.T) {
	cursor := &domain.MessageCursor{ChatID: "1", MessageID: 3, SentAt: time.Date(2026, 1, 1, 0, 0, 0, 123, time.UTC)}
	token, err := encodeMessageCursor(cursor)
	if err != nil {
		t.Fatal(err)
	}
	reader := &stubMessages{rows: []domain.Message{{ID: "3", SentAt: cursor.SentAt, Text: "synthetic"}}, next: cursor}
	var out bytes.Buffer
	err = execute(context.Background(), []string{"personal", "read", "1"}, &out, dependencies{openMessages: func(context.Context) (messageReader, error) { return reader, nil }})
	if err != nil || !strings.Contains(out.String(), "Next --before: "+token) || strings.Contains(out.String(), "End of locally") {
		t.Fatalf("continuation = %q, %v", out.String(), err)
	}
	reader = &stubMessages{rows: []domain.Message{{ID: "2", Text: "older synthetic"}}}
	out.Reset()
	err = execute(context.Background(), []string{"personal", "read", "1", "--before", token, "--limit", "1"}, &out, dependencies{openMessages: func(context.Context) (messageReader, error) { return reader, nil }})
	if err != nil || reader.before == nil || *reader.before != *cursor || reader.limit != 1 || !reader.closed || !strings.Contains(out.String(), "End of locally synchronized history.") {
		t.Fatalf("continuation read failed: %v", err)
	}
	reader = &stubMessages{}
	out.Reset()
	err = execute(context.Background(), []string{"personal", "read", "1", "--before", token}, &out, dependencies{openMessages: func(context.Context) (messageReader, error) { return reader, nil }})
	if err != nil || !strings.Contains(out.String(), "No older locally synchronized messages.") {
		t.Fatalf("end = %q, %v", out.String(), err)
	}
}
