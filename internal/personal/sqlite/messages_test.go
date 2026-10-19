package sqlite

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/giomhern/psst/internal/domain"
)

func TestHistoryNormalizesDatesAndSelectsRecentPage(t *testing.T) {
	path, writer := fixture(t, "history.db", false)
	_, err := writer.Exec(`DELETE FROM message;
        INSERT INTO chat(ROWID,guid) VALUES (1,'synthetic-chat'),(2,'synthetic-empty'),(3,'synthetic-other');
        INSERT INTO handle(ROWID,id) VALUES (1,'a@example.test');
        INSERT INTO message(ROWID,text,date,is_from_me,handle_id) VALUES
          (1,'older seconds',725760000,0,1),
          (2,'middle nanoseconds',757382400000000000,0,1),
          (3,'recent seconds',788918400,0,1),
          (4,'tie first',788918400123456789,0,1),
          (5,'tie second',788918400123456789,1,NULL),
          (6,NULL,NULL,NULL,NULL),
          (7,'other chat',800000000,0,1);
        INSERT INTO chat_message_join VALUES (1,1),(1,2),(1,3),(1,4),(1,4),(1,5),(1,6),(3,7),(1,999);`)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader := openFixture(t, path)
	messages, err := reader.ListMessages(context.Background(), "1", 100)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range messages {
		ids = append(ids, m.ID)
	}
	if !reflect.DeepEqual(ids, []string{"1", "2", "3", "4", "5", "6"}) {
		t.Fatalf("IDs = %v", ids)
	}
	if !messages[0].SentAt.Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)) ||
		!messages[1].SentAt.Equal(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)) ||
		!messages[3].SentAt.Equal(time.Date(2026, 1, 1, 0, 0, 0, 123456789, time.UTC)) {
		t.Fatalf("timestamp conversion failed: %+v", messages)
	}
	if messages[0].Sender != "a@example.test" || !messages[4].FromMe || !messages[5].SentAt.IsZero() {
		t.Fatal("sender or unknown time mismatch")
	}
	recent, err := reader.ListMessages(context.Background(), "1", 2)
	if err != nil || len(recent) != 2 || recent[0].ID != "4" || recent[1].ID != "5" {
		t.Fatalf("recent page = %v, %v", recent, err)
	}
	empty, err := reader.ListMessages(context.Background(), "2", 20)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty = %v, %v", empty, err)
	}
	if _, err := reader.ListMessages(context.Background(), "99", 20); !errors.Is(err, domain.ErrConversationNotFound) {
		t.Fatalf("missing chat = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("history read changed source database")
	}
}

func TestHistoryUnknownDatesAndBoundedContent(t *testing.T) {
	path, writer := fixture(t, "content.db", false)
	_, err := writer.Exec(`DELETE FROM message;
        ALTER TABLE message ADD COLUMN cache_has_attachments INTEGER;
        ALTER TABLE message ADD COLUMN attributedBody BLOB;
        INSERT INTO chat(ROWID,guid) VALUES (1,'synthetic');`)
	if err != nil {
		t.Fatal(err)
	}
	dates := []any{nil, int64(0), int64(-1), "invalid-date", int64(6279811201), int64(999999999999), float64(1234.5), int64(6279811200000000001)}
	for i, date := range dates {
		if _, err := writer.Exec("INSERT INTO message(ROWID,date) VALUES (?,?)", i+1, date); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Exec("INSERT INTO chat_message_join VALUES (1,?)", i+1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := writer.Exec(`UPDATE message SET cache_has_attachments=1 WHERE ROWID=1;
        UPDATE message SET attributedBody=X'010203' WHERE ROWID=2;
        UPDATE message SET text=char(65532) WHERE ROWID=3;`); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec("UPDATE message SET text=? WHERE ROWID=4", strings.Repeat("猫", 4001)); err != nil {
		t.Fatal(err)
	}
	messages, err := openFixture(t, path).ListMessages(context.Background(), "1", 100)
	if err != nil || len(messages) != len(dates) {
		t.Fatalf("content rows = %v, %v", messages, err)
	}
	for _, m := range messages {
		if !m.SentAt.IsZero() {
			t.Fatalf("unsupported date became %v", m.SentAt)
		}
	}
	if !messages[0].HasAttachments || !messages[1].HasRichContent || !messages[2].HasRichContent || !messages[2].HasEmbeddedContent || messages[2].Text != "" {
		t.Fatal("content metadata missing")
	}
	if !messages[3].TextTruncated || utf8.RuneCountInString(messages[3].Text) != 4000 {
		t.Fatal("Unicode text bound failed")
	}
}

func TestHistoryValidationCancellationAndSchema(t *testing.T) {
	path, writer := fixture(t, "errors.db", false)
	reader := openFixture(t, path)
	for _, tc := range []struct {
		id    string
		limit int
	}{{"1", 0}, {"1", 101}, {"0", 1}, {"-1", 1}, {"01", 1}, {"+1", 1}, {"1 OR 1=1", 1}, {"9223372036854775808", 1}} {
		if _, err := reader.ListMessages(context.Background(), tc.id, tc.limit); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.ListMessages(ctx, "1", 20); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	if _, err := writer.Exec("ALTER TABLE message DROP COLUMN date"); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ListMessages(context.Background(), "1", 20); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("schema = %v", err)
	}
}

func TestHistoryReadsWALAndSubsequentCommits(t *testing.T) {
	path, writer := fixture(t, "wal-history.db", true)
	if _, err := writer.Exec("INSERT INTO chat(ROWID,guid) VALUES (1,'synthetic'); INSERT INTO chat_message_join VALUES (1,1)"); err != nil {
		t.Fatal(err)
	}
	reader := openFixture(t, path)
	rows, err := reader.ListMessages(context.Background(), "1", 20)
	if err != nil || len(rows) != 1 {
		t.Fatalf("initial WAL rows = %v, %v", rows, err)
	}
	if _, err := writer.Exec("INSERT INTO message(ROWID,text,date,is_from_me) VALUES (2,'new synthetic message',788918400,1); INSERT INTO chat_message_join VALUES (1,2)"); err != nil {
		t.Fatal(err)
	}
	rows, err = reader.ListMessages(context.Background(), "1", 20)
	if err != nil || len(rows) != 2 {
		t.Fatalf("new WAL rows = %v, %v", rows, err)
	}
}

func TestHistoryDatePolicyBoundaries(t *testing.T) {
	path, writer := fixture(t, "date-boundaries.db", false)
	if _, err := writer.Exec("DELETE FROM message; INSERT INTO chat(ROWID,guid) VALUES(1,'synthetic')"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		raw  int64
		want time.Time
	}{
		{1, time.Date(2001, 1, 1, 0, 0, 1, 0, time.UTC)},
		{6279811200, time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)},
		{1000000000000, time.Date(2001, 1, 1, 0, 16, 40, 0, time.UTC)},
		{6279811200000000000, time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	for i, tc := range cases {
		if _, err := writer.Exec("INSERT INTO message(ROWID,date) VALUES(?,?); INSERT INTO chat_message_join VALUES(1,?)", i+1, tc.raw, i+1); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := openFixture(t, path).ListMessages(context.Background(), "1", 100)
	if err != nil || len(rows) != len(cases) {
		t.Fatalf("boundary rows = %v, %v", rows, err)
	}
	for _, row := range rows {
		for i, tc := range cases {
			if row.ID == strconv.Itoa(i+1) && (!row.SentAt.Equal(tc.want) || row.SentAt.Location() != time.UTC) {
				t.Fatalf("date %d = %v, want %v", tc.raw, row.SentAt, tc.want)
			}
		}
	}
}
